package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	imagepkg "github.com/432539/gpt2api/internal/image"
	"github.com/432539/gpt2api/internal/settings"
	"github.com/432539/gpt2api/internal/textgen"
	"github.com/432539/gpt2api/internal/upstream/chatgpt"
	"github.com/432539/gpt2api/internal/usage"
	"github.com/432539/gpt2api/internal/videogen"
	"github.com/432539/gpt2api/internal/videoworkflow"
)

type videoWorkflowImageGenerator struct {
	runner *imagepkg.Runner
}

func (g videoWorkflowImageGenerator) GenerateImage(ctx context.Context, request videoworkflow.ImageGenerationRequest) ([]videoworkflow.GeneratedImage, error) {
	if g.runner == nil {
		return nil, errors.New("video workflow image generator is unavailable")
	}
	references := make([]imagepkg.ReferenceImage, 0, len(request.References))
	for index, data := range request.References {
		references = append(references, imagepkg.ReferenceImage{Data: data, FileName: fmt.Sprintf("reference-%d.png", index+1)})
	}
	result := g.runner.Run(ctx, imagepkg.RunOptions{
		TaskID: request.TaskID, UserID: request.UserID, UpstreamModel: request.Model, Prompt: request.Prompt,
		N: request.Count, Size: request.Size, References: references, ReturnImageBytes: true,
	})
	if result == nil || result.Status != imagepkg.StatusSuccess {
		if result == nil {
			return nil, errors.New("image generator returned no result")
		}
		return nil, fmt.Errorf("image generation failed (%s): %s", result.ErrorCode, result.ErrorMessage)
	}
	count := len(result.ImageBytes)
	if len(result.SignedURLs) > count {
		count = len(result.SignedURLs)
	}
	images := make([]videoworkflow.GeneratedImage, 0, count)
	for index := 0; index < count; index++ {
		generated := videoworkflow.GeneratedImage{TaskID: result.ConversationID}
		if index < len(result.ImageBytes) {
			generated.Data = result.ImageBytes[index]
		}
		if index < len(result.SignedURLs) {
			generated.URL = result.SignedURLs[index]
		}
		if index < len(result.ContentTypes) {
			generated.MIMEType = result.ContentTypes[index]
		}
		images = append(images, generated)
	}
	return images, nil
}

type videoWorkflowTextGenerator struct {
	client *textgen.Client
}

func (g videoWorkflowTextGenerator) GenerateText(ctx context.Context, request videoworkflow.TextGenerationRequest) (json.RawMessage, int64, error) {
	if g.client == nil {
		return nil, 0, errors.New("video workflow text generator is unavailable")
	}
	stream, err := g.client.Chat(ctx, textgen.Options{
		Model: request.Model,
		Messages: []chatgpt.ChatMessage{
			{Role: "system", Content: "你是视频分镜引擎。只输出合法 JSON，不要 Markdown；四幕工作流必须返回包含 scenes 数组的对象。"},
			{Role: "user", Content: request.Prompt},
		},
		MaxTokens: 8192,
	})
	if err != nil {
		return nil, 0, err
	}
	var content strings.Builder
	for chunk := range stream {
		if chunk.Err != nil {
			return nil, 0, chunk.Err
		}
		content.WriteString(chunk.Delta)
	}
	raw := normalizeVideoWorkflowJSON(content.String())
	if !json.Valid(raw) {
		return nil, 0, errors.New("text generator returned invalid JSON")
	}
	return raw, 0, nil
}

func normalizeVideoWorkflowJSON(value string) json.RawMessage {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "```") {
		if lineEnd := strings.IndexByte(value, '\n'); lineEnd >= 0 {
			value = value[lineEnd+1:]
		}
		value = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(value), "```"))
	}
	if json.Valid([]byte(value)) {
		return json.RawMessage(value)
	}
	objectStart, objectEnd := strings.IndexByte(value, '{'), strings.LastIndexByte(value, '}')
	arrayStart, arrayEnd := strings.IndexByte(value, '['), strings.LastIndexByte(value, ']')
	if objectStart >= 0 && objectEnd > objectStart {
		candidate := strings.TrimSpace(value[objectStart : objectEnd+1])
		if json.Valid([]byte(candidate)) {
			return json.RawMessage(candidate)
		}
	}
	if arrayStart >= 0 && arrayEnd > arrayStart {
		candidate := strings.TrimSpace(value[arrayStart : arrayEnd+1])
		if json.Valid([]byte(candidate)) {
			return json.RawMessage(candidate)
		}
	}
	return json.RawMessage(value)
}

type videoWorkflowVideoGenerator struct {
	client         *videogen.Client
	configProvider *settings.Service
}

func (g videoWorkflowVideoGenerator) VideoConfigSnapshot() json.RawMessage {
	if g.client == nil {
		return nil
	}
	raw, _ := json.Marshal(g.client.TaskConfigSnapshot())
	return raw
}

func (g videoWorkflowVideoGenerator) VideoConfigSnapshotForModel(model string) json.RawMessage {
	if g.configProvider == nil {
		return g.VideoConfigSnapshot()
	}
	raw, _ := json.Marshal(taskConfigSnapshotForConfig(g.configProvider.VideoGenConfigForModel(model)))
	return raw
}

func (g videoWorkflowVideoGenerator) GenerateVideo(ctx context.Context, request videoworkflow.VideoGenerationRequest) (*videoworkflow.GeneratedVideo, error) {
	if g.client == nil {
		return nil, errors.New("video workflow video generator is unavailable")
	}
	if strings.TrimSpace(request.ProviderTaskID) != "" {
		return g.resumeVideo(ctx, request)
	}
	snapshot := g.taskConfigSnapshotForModel(request.Model)
	if len(request.ProviderConfig) > 0 {
		if err := json.Unmarshal(request.ProviderConfig, &snapshot); err != nil {
			return nil, fmt.Errorf("decode video provider snapshot: %w", err)
		}
	}
	if err := validateVideoWorkflowProviderSnapshot(request.Model, snapshot); err != nil {
		return nil, err
	}
	images := make([]videogen.ImageInput, 0, len(request.ReferenceURLs))
	for index, rawURL := range request.ReferenceURLs {
		images = append(images, videogen.ImageInput{URL: rawURL, Name: fmt.Sprintf("workflow-reference-%d", index+1)})
	}
	result, err := g.client.GenerateForTaskSnapshot(ctx, snapshot, videogen.Options{
		Model: videoWorkflowUpstreamModel(request.Model, snapshot), Prompt: request.Prompt, Images: images,
		DurationSec: 15, AspectRatio: request.AspectRatio, Resolution: request.Resolution,
		OnSubmitted: func(result videogen.Result) error {
			if request.OnSubmitted == nil {
				return nil
			}
			state, _ := json.Marshal(map[string]any{"phase": "submitted", "task_id": result.TaskID, "provider_config": snapshot})
			return request.OnSubmitted(result.TaskID, state)
		},
		OnProgress: func(result videogen.Result) { notifyVideoWorkflowProgress(request, result) },
	})
	if err != nil {
		return nil, err
	}
	return videoWorkflowGeneratedVideo(result)
}

func (g videoWorkflowVideoGenerator) taskConfigSnapshotForModel(model string) videogen.TaskConfigSnapshot {
	if g.configProvider != nil {
		return taskConfigSnapshotForConfig(g.configProvider.VideoGenConfigForModel(model))
	}
	if g.client == nil {
		return videogen.TaskConfigSnapshot{}
	}
	return g.client.TaskConfigSnapshot()
}

func taskConfigSnapshotForConfig(cfg videogen.Config) videogen.TaskConfigSnapshot {
	return videogen.TaskConfigSnapshot{
		ChannelType: cfg.ChannelType, BaseURL: cfg.BaseURL, Model: cfg.Model, TimeoutSec: cfg.TimeoutSec,
		DurationSec: cfg.DurationSec, AspectRatio: cfg.AspectRatio, Resolution: cfg.Resolution, GenerateAudio: cfg.GenerateAudio,
	}
}

func videoWorkflowUpstreamModel(requested string, snapshot videogen.TaskConfigSnapshot) string {
	requested = strings.TrimSpace(requested)
	switch strings.ToLower(requested) {
	case "", "default", "seedance-2.0":
		return strings.TrimSpace(snapshot.Model)
	default:
		return requested
	}
}

func validateVideoWorkflowProviderSnapshot(requested string, snapshot videogen.TaskConfigSnapshot) error {
	model := strings.ToLower(strings.TrimSpace(requested))
	requiredChannel := videogen.GuessWorkflowModelChannel(model)
	if requiredChannel == "" {
		return nil
	}
	channelType := strings.ToLower(strings.TrimSpace(snapshot.ChannelType))
	if channelType != requiredChannel {
		return fmt.Errorf("video workflow model %q requires provider channel_type %q, got %q", strings.TrimSpace(requested), requiredChannel, strings.TrimSpace(snapshot.ChannelType))
	}
	return nil
}

func (g videoWorkflowVideoGenerator) resumeVideo(ctx context.Context, request videoworkflow.VideoGenerationRequest) (*videoworkflow.GeneratedVideo, error) {
	snapshot := g.client.TaskConfigSnapshot()
	if len(request.ProviderConfig) > 0 {
		if err := json.Unmarshal(request.ProviderConfig, &snapshot); err != nil {
			return nil, fmt.Errorf("decode video provider snapshot: %w", err)
		}
	}
	if err := validateVideoWorkflowProviderSnapshot(request.Model, snapshot); err != nil {
		return nil, err
	}
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		result, err := g.client.GetTaskForSnapshot(ctx, snapshot, request.ProviderTaskID)
		if err != nil {
			return nil, err
		}
		notifyVideoWorkflowProgress(request, *result)
		switch strings.ToLower(strings.TrimSpace(result.Status)) {
		case "completed", "succeeded", "success":
			return videoWorkflowGeneratedVideo(result)
		case "failed", "canceled", "cancelled", "expired":
			return nil, fmt.Errorf("video task %s failed: %s", request.ProviderTaskID, result.ErrorMessage)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func notifyVideoWorkflowProgress(request videoworkflow.VideoGenerationRequest, result videogen.Result) {
	if request.OnProgress == nil {
		return
	}
	state, _ := json.Marshal(map[string]any{
		"status": result.Status, "progress": result.Progress, "progress_known": result.ProgressKnown,
		"model_id": result.ModelID, "result_url": result.ResultURL,
	})
	request.OnProgress(result.TaskID, result.Progress, state)
}

func videoWorkflowGeneratedVideo(result *videogen.Result) (*videoworkflow.GeneratedVideo, error) {
	if result == nil || strings.TrimSpace(result.ResultURL) == "" {
		return nil, errors.New("video generator returned no result URL")
	}
	return &videoworkflow.GeneratedVideo{
		URL: result.ResultURL, MIMEType: "video/mp4", TaskID: result.TaskID,
	}, nil
}

type videoWorkflowUsageLogger struct {
	logger *usage.Logger
}

func (l videoWorkflowUsageLogger) LogVideoWorkflowUsage(event videoworkflow.RuntimeUsageEvent) {
	if l.logger == nil {
		return
	}
	l.logger.Write(&usage.Log{
		UserID: event.UserID, RequestID: event.RequestID, Type: event.Type,
		CreditCost: event.CreditCost, DurationMs: int(event.DurationMS), Status: event.Status,
		ErrorCode: event.ErrorCode, CreatedAt: time.Now(),
	})
}
