package videoworkflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	imagepkg "github.com/432539/gpt2api/internal/image"
	"github.com/432539/gpt2api/internal/textgen"
	"github.com/432539/gpt2api/internal/upstream/chatgpt"
	"github.com/432539/gpt2api/internal/usage"
	"github.com/432539/gpt2api/internal/videogen"
)

type ImageRunnerAdapter struct{ Runner *imagepkg.Runner }

func (a ImageRunnerAdapter) GenerateImage(ctx context.Context, request ImageGenerationRequest) ([]GeneratedImage, error) {
	if a.Runner == nil {
		return nil, errors.New("image runner is not configured")
	}
	references := make([]imagepkg.ReferenceImage, 0, len(request.References))
	for i, data := range request.References {
		references = append(references, imagepkg.ReferenceImage{Data: data, FileName: fmt.Sprintf("reference-%d.png", i+1)})
	}
	result := a.Runner.Run(ctx, imagepkg.RunOptions{
		TaskID: request.TaskID, UserID: request.UserID, UpstreamModel: request.Model, Prompt: request.Prompt,
		N: request.Count, Size: request.Size, OutputFormat: "png", ReturnImageBytes: true, References: references,
	})
	if result == nil || result.Status != imagepkg.StatusSuccess {
		if result == nil {
			return nil, errors.New("image runner returned nil result")
		}
		return nil, fmt.Errorf("image generation failed: %s: %s", result.ErrorCode, result.ErrorMessage)
	}
	count := len(result.ImageBytes)
	if len(result.SignedURLs) > count {
		count = len(result.SignedURLs)
	}
	if count == 0 {
		return nil, errors.New("image runner returned no image")
	}
	out := make([]GeneratedImage, 0, count)
	for i := 0; i < count; i++ {
		generated := GeneratedImage{TaskID: request.TaskID, MIMEType: "image/png"}
		if i < len(result.ImageBytes) {
			generated.Data = result.ImageBytes[i]
		}
		if i < len(result.SignedURLs) {
			generated.URL = result.SignedURLs[i]
		}
		if i < len(result.ContentTypes) && result.ContentTypes[i] != "" {
			generated.MIMEType = result.ContentTypes[i]
		}
		out = append(out, generated)
	}
	return out, nil
}

type TextClientAdapter struct{ Client *textgen.Client }

func (a TextClientAdapter) GenerateText(ctx context.Context, request TextGenerationRequest) (json.RawMessage, int64, error) {
	if a.Client == nil {
		return nil, 0, errors.New("text client is not configured")
	}
	stream, err := a.Client.Chat(ctx, textgen.Options{
		Model: request.Model, Messages: []chatgpt.ChatMessage{{Role: "system", Content: "只输出严格 JSON，不要使用 Markdown 代码块。"}, {Role: "user", Content: request.Prompt}},
		MaxTokens: 8192,
	})
	if err != nil {
		return nil, 0, err
	}
	var builder strings.Builder
	for chunk := range stream {
		if chunk.Err != nil {
			return nil, 0, chunk.Err
		}
		builder.WriteString(chunk.Delta)
	}
	raw := extractJSONObject(builder.String())
	if !json.Valid(raw) {
		return nil, 0, errors.New("text client returned invalid JSON")
	}
	return raw, 0, nil
}

type VideoClientAdapter struct {
	Client       *videogen.Client
	PollInterval time.Duration
}

func (a VideoClientAdapter) VideoConfigSnapshot() json.RawMessage {
	if a.Client == nil {
		return nil
	}
	raw, _ := json.Marshal(a.Client.TaskConfigSnapshot())
	return raw
}

func (a VideoClientAdapter) GenerateVideo(ctx context.Context, request VideoGenerationRequest) (*GeneratedVideo, error) {
	if a.Client == nil {
		return nil, errors.New("video client is not configured")
	}
	var result *videogen.Result
	var err error
	snapshot := a.Client.TaskConfigSnapshot()
	if len(request.ProviderConfig) > 0 {
		if err := json.Unmarshal(request.ProviderConfig, &snapshot); err != nil {
			return nil, err
		}
	}
	if request.ProviderTaskID != "" {
		result, err = a.resume(ctx, snapshot, request.ProviderTaskID, request.OnProgress)
	} else {
		images := make([]videogen.ImageInput, 0, len(request.ReferenceURLs))
		for i, reference := range request.ReferenceURLs {
			images = append(images, videogen.ImageInput{URL: reference, Name: fmt.Sprintf("reference-%d", i+1)})
		}
		generateAudio := true
		result, err = a.Client.GenerateForTaskSnapshot(ctx, snapshot, videogen.Options{
			Model: request.Model, Prompt: request.Prompt, Images: images, DurationSec: request.DurationSec,
			AspectRatio: request.AspectRatio, Resolution: request.Resolution, GenerateAudio: &generateAudio,
			OnSubmitted: func(result videogen.Result) error {
				if request.OnSubmitted == nil {
					return nil
				}
				state, _ := json.Marshal(map[string]any{"phase": "submitted", "task_id": result.TaskID, "provider_config": snapshot})
				return request.OnSubmitted(result.TaskID, state)
			},
			OnProgress: func(progress videogen.Result) { notifyVideoProgress(request.OnProgress, progress) },
		})
	}
	if err != nil {
		return nil, err
	}
	if result == nil || strings.TrimSpace(result.ResultURL) == "" || !videoStatusSucceeded(result.Status) {
		return nil, fmt.Errorf("video generation did not succeed: %s", resultStatus(result))
	}
	cost := int64(math.Round(result.CostDetail.Price))
	return &GeneratedVideo{URL: result.ResultURL, MIMEType: "video/mp4", TaskID: result.TaskID, CreditCost: cost}, nil
}

func (a VideoClientAdapter) resume(ctx context.Context, snapshot videogen.TaskConfigSnapshot, taskID string, notify func(string, int, json.RawMessage)) (*videogen.Result, error) {
	interval := a.PollInterval
	if interval <= 0 {
		interval = 3 * time.Second
	}
	for {
		result, err := a.Client.GetTaskForSnapshot(ctx, snapshot, taskID)
		if err != nil {
			return nil, err
		}
		notifyVideoProgress(notify, *result)
		if videoStatusSucceeded(result.Status) {
			return result, nil
		}
		if videoStatusFailed(result.Status) {
			return nil, fmt.Errorf("video task %s failed: %s", taskID, result.ErrorMessage)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
	}
}

type UsageLoggerAdapter struct{ Logger *usage.Logger }

func (a UsageLoggerAdapter) LogVideoWorkflowUsage(event RuntimeUsageEvent) {
	if a.Logger == nil {
		return
	}
	a.Logger.Write(&usage.Log{
		UserID: event.UserID, RequestID: event.RequestID, Type: event.Type, CreditCost: event.CreditCost,
		DurationMs: int(event.DurationMS), Status: event.Status, ErrorCode: event.ErrorCode,
	})
}

func notifyVideoProgress(notify func(string, int, json.RawMessage), result videogen.Result) {
	if notify == nil {
		return
	}
	state, _ := json.Marshal(map[string]any{"status": result.Status, "result_url": result.ResultURL, "error_message": result.ErrorMessage})
	notify(result.TaskID, result.Progress, state)
}

func videoStatusSucceeded(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "success", "succeeded", "completed", "done":
		return true
	default:
		return false
	}
}

func videoStatusFailed(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "failed", "error", "expired", "canceled", "cancelled":
		return true
	default:
		return false
	}
}

func resultStatus(result *videogen.Result) string {
	if result == nil {
		return "nil"
	}
	if result.ErrorMessage != "" {
		return result.Status + ": " + result.ErrorMessage
	}
	return result.Status
}

func extractJSONObject(value string) json.RawMessage {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "```json")
	value = strings.TrimPrefix(value, "```")
	value = strings.TrimSuffix(value, "```")
	value = strings.TrimSpace(value)
	startObject, endObject := strings.IndexByte(value, '{'), strings.LastIndexByte(value, '}')
	startArray, endArray := strings.IndexByte(value, '['), strings.LastIndexByte(value, ']')
	if startObject >= 0 && endObject >= startObject && (startArray < 0 || startObject < startArray) {
		value = value[startObject : endObject+1]
	} else if startArray >= 0 && endArray >= startArray {
		value = value[startArray : endArray+1]
	}
	return json.RawMessage(value)
}

var _ RuntimeImageGenerator = ImageRunnerAdapter{}
var _ RuntimeTextGenerator = TextClientAdapter{}
var _ RuntimeVideoGenerator = VideoClientAdapter{}
var _ RuntimeUsageLogger = UsageLoggerAdapter{}
