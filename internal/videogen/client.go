// Package videogen calls the AI Gen Platform video generation API.
package videogen

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	defaultBaseURL       = "http://app.echoon.top/api/v1"
	defaultModelID       = "0e37fa2d-72b3-483a-81b4-ad595cd147c7"
	defaultModelName     = "Seedance-2.0-D-V"
	defaultModel         = defaultModelID
	defaultTimeoutSec    = 900
	defaultPollInterval  = 3 * time.Second
	defaultAspectRatio   = "16:9"
	defaultResolution    = "720p"
	defaultDurationSec   = 5
	defaultRequestTimout = 30 * time.Second
)

type Config struct {
	BaseURL       string
	APIKey        string
	APIKeyEnv     string
	Model         string
	TimeoutSec    int
	DurationSec   int
	AspectRatio   string
	Resolution    string
	GenerateAudio bool
}

type ConfigProvider interface {
	VideoGenEnabled() bool
	VideoGenAPIKey() string
	VideoGenBaseURL() string
	VideoGenModel() string
	VideoGenTimeoutSec() int
	VideoGenDurationSec() int
	VideoGenAspectRatio() string
	VideoGenResolution() string
	VideoGenGenerateAudio() bool
}

type Client struct {
	baseURL       string
	apiKey        string
	apiKeyEnv     string
	model         string
	timeoutSec    int
	durationSec   int
	aspectRatio   string
	resolution    string
	generateAudio bool
	httpClient    *http.Client
	provider      ConfigProvider
}

type Options struct {
	Model         string
	ModelID       string
	Prompt        string
	Images        []ImageInput
	ReferenceURL  string
	DurationSec   int
	AspectRatio   string
	Resolution    string
	GenerateAudio *bool
	ExtraParams   map[string]any
	OnProgress    func(Result)
}

type ImageInput struct {
	URL  string `json:"url"`
	Name string `json:"name,omitempty"`
}

type Result struct {
	TaskID       string
	ModelID      string
	Status       string
	Progress     int
	ResultURL    string
	ErrorMessage string
	DurationMs   int64
}

type Model struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Type     string          `json:"type"`
	Raw      json.RawMessage `json:"-"`
	Disabled bool            `json:"disabled"`
	Enabled  *bool           `json:"enabled"`
}

type ProbeModel struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Type  string `json:"type"`
	Label string `json:"label"`
	Value string `json:"value"`
}

type Balance struct {
	Credits         int         `json:"credits"`
	RechargeBalance int         `json:"recharge_balance"`
	FreeQuotas      []FreeQuota `json:"free_quotas"`
	DurationMs      int64       `json:"duration_ms"`
}

type FreeQuota struct {
	ModelID        string `json:"model_id,omitempty"`
	ModelName      string `json:"model_name,omitempty"`
	RemainingCount int    `json:"remaining_count,omitempty"`
}

type generateResp struct {
	TaskID   string `json:"task_id"`
	ID       string `json:"id"`
	Status   string `json:"status"`
	Progress int    `json:"progress"`
}

type taskResp struct {
	ID           string `json:"id"`
	ModelID      string `json:"model_id"`
	Status       string `json:"status"`
	Progress     int    `json:"progress"`
	ResultURL    string `json:"result_url"`
	ErrorMessage string `json:"error_message"`
}

func NewClient(cfg Config) *Client {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	apiKeyEnv := strings.TrimSpace(cfg.APIKeyEnv)
	if apiKeyEnv == "" {
		apiKeyEnv = "AI_GEN_API_KEY"
	}
	timeoutSec := cfg.TimeoutSec
	if timeoutSec <= 0 {
		timeoutSec = defaultTimeoutSec
	}
	durationSec := cfg.DurationSec
	if durationSec <= 0 {
		durationSec = defaultDurationSec
	}
	return &Client{
		baseURL:       baseURL,
		apiKey:        strings.TrimSpace(cfg.APIKey),
		apiKeyEnv:     apiKeyEnv,
		model:         defaultString(cfg.Model, defaultModel),
		timeoutSec:    timeoutSec,
		durationSec:   durationSec,
		aspectRatio:   defaultString(cfg.AspectRatio, defaultAspectRatio),
		resolution:    defaultString(cfg.Resolution, defaultResolution),
		generateAudio: cfg.GenerateAudio,
		httpClient:    &http.Client{Timeout: defaultRequestTimout},
	}
}

func (c *Client) SetConfigProvider(p ConfigProvider) { c.provider = p }

func (c *Client) Enabled() bool {
	if c == nil {
		return false
	}
	cfg := c.runtimeConfig()
	return strings.TrimSpace(cfg.APIKey) != ""
}

func (c *Client) ListModels(ctx context.Context) ([]Model, error) {
	cfg := c.runtimeConfig()
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("videogen is disabled or api key is empty")
	}
	var models []Model
	if err := c.doJSON(ctx, http.MethodGet, cfg.BaseURL+"/models/", cfg.APIKey, nil, &models); err != nil {
		return nil, err
	}
	return models, nil
}

func (c *Client) Balance(ctx context.Context) (*Balance, error) {
	if c == nil {
		return nil, errors.New("videogen client not configured")
	}
	cfg := c.accountConfig()
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("videogen api key is empty")
	}
	start := time.Now()
	var balance Balance
	if err := c.doJSON(ctx, http.MethodGet, cfg.BaseURL+"/account/balance", cfg.APIKey, nil, &balance); err != nil {
		return nil, err
	}
	if balance.FreeQuotas == nil {
		balance.FreeQuotas = []FreeQuota{}
	}
	balance.DurationMs = time.Since(start).Milliseconds()
	return &balance, nil
}

func (c *Client) Generate(ctx context.Context, opt Options) (*Result, error) {
	if c == nil {
		return nil, errors.New("videogen client not configured")
	}
	cfg := c.runtimeConfig()
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("videogen is disabled or api key is empty")
	}
	if strings.TrimSpace(opt.Prompt) == "" {
		return nil, errors.New("prompt required")
	}
	ctx, cancel := ensureTimeout(ctx, time.Duration(cfg.TimeoutSec)*time.Second)
	defer cancel()

	start := time.Now()
	modelID, err := c.resolveModelID(ctx, cfg, opt)
	if err != nil {
		return nil, err
	}
	extra := map[string]any{}
	for k, v := range opt.ExtraParams {
		extra[k] = v
	}
	extra["duration"] = intDefault(opt.DurationSec, cfg.DurationSec)
	extra["aspect_ratio"] = defaultString(opt.AspectRatio, cfg.AspectRatio)
	extra["resolution"] = defaultString(opt.Resolution, cfg.Resolution)
	if opt.GenerateAudio != nil {
		extra["generate_audio"] = *opt.GenerateAudio
	} else {
		extra["generate_audio"] = cfg.GenerateAudio
	}
	payload := map[string]any{
		"model_id":     modelID,
		"prompt":       strings.TrimSpace(opt.Prompt),
		"extra_params": extra,
	}
	if len(opt.Images) > 0 {
		payload["images"] = opt.Images
	}
	if strings.TrimSpace(opt.ReferenceURL) != "" {
		payload["reference_image_url"] = strings.TrimSpace(opt.ReferenceURL)
	}

	var created generateResp
	if err := c.doJSON(ctx, http.MethodPost, cfg.BaseURL+"/generate/", cfg.APIKey, payload, &created); err != nil {
		return nil, err
	}
	taskID := firstNonEmpty(created.TaskID, created.ID)
	if taskID == "" {
		return nil, errors.New("videogen create task response missing task_id")
	}
	if opt.OnProgress != nil {
		opt.OnProgress(Result{
			TaskID:   taskID,
			ModelID:  modelID,
			Status:   "queued",
			Progress: 0,
		})
	}
	task, err := c.pollTask(ctx, cfg, taskID, opt.OnProgress)
	if err != nil {
		return nil, err
	}
	task.DurationMs = time.Since(start).Milliseconds()
	return task, nil
}

func (c *Client) Probe(ctx context.Context) (durationMs int64, modelCount int, modelName string, err error) {
	durationMs, modelCount, modelName, _, err = c.ProbeModels(ctx)
	return durationMs, modelCount, modelName, err
}

func (c *Client) GetTask(ctx context.Context, taskID string) (*Result, error) {
	if c == nil {
		return nil, errors.New("videogen client not configured")
	}
	cfg := c.runtimeConfig()
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("videogen is disabled or api key is empty")
	}
	var task taskResp
	if err := c.doJSON(ctx, http.MethodGet, cfg.BaseURL+"/generate/tasks/"+taskID, cfg.APIKey, nil, &task); err != nil {
		return nil, err
	}
	return &Result{
		TaskID:       firstNonEmpty(task.ID, taskID),
		ModelID:      task.ModelID,
		Status:       task.Status,
		Progress:     clampProgress(task.Progress),
		ResultURL:    strings.TrimSpace(task.ResultURL),
		ErrorMessage: task.ErrorMessage,
	}, nil
}

func (c *Client) ProbeModels(ctx context.Context) (durationMs int64, modelCount int, modelName string, videoModels []ProbeModel, err error) {
	start := time.Now()
	models, err := c.ListModels(ctx)
	if err != nil {
		return 0, 0, "", nil, err
	}
	firstVideoName := ""
	defaultVideoName := ""
	defaultVideoIndex := -1
	for _, m := range models {
		if m.Enabled != nil && !*m.Enabled {
			continue
		}
		if m.Disabled {
			continue
		}
		if !isVideoModel(m) {
			continue
		}
		if firstVideoName == "" {
			firstVideoName = strings.TrimSpace(m.Name)
		}
		if isDefaultModelAlias(m.ID) || isDefaultModelAlias(m.Name) {
			defaultVideoName = strings.TrimSpace(m.Name)
			defaultVideoIndex = len(videoModels)
		}
		videoModels = append(videoModels, ProbeModel{
			ID:    strings.TrimSpace(m.ID),
			Name:  strings.TrimSpace(m.Name),
			Type:  strings.TrimSpace(m.Type),
			Label: videoModelLabel(m),
			Value: probeModelValue(m),
		})
	}
	if defaultVideoName != "" {
		modelName = defaultVideoName
	} else {
		modelName = firstVideoName
	}
	if defaultVideoIndex > 0 && defaultVideoIndex < len(videoModels) {
		preferred := videoModels[defaultVideoIndex]
		copy(videoModels[1:defaultVideoIndex+1], videoModels[0:defaultVideoIndex])
		videoModels[0] = preferred
	}
	return time.Since(start).Milliseconds(), len(models), modelName, videoModels, nil
}

func (c *Client) pollTask(ctx context.Context, cfg Config, taskID string, onProgress func(Result)) (*Result, error) {
	ticker := time.NewTicker(defaultPollInterval)
	defer ticker.Stop()
	for {
		var task taskResp
		if err := c.doJSON(ctx, http.MethodGet, cfg.BaseURL+"/generate/tasks/"+taskID, cfg.APIKey, nil, &task); err != nil {
			return nil, err
		}
		status := strings.ToLower(strings.TrimSpace(task.Status))
		progress := clampProgress(task.Progress)
		progressResult := Result{
			TaskID:       firstNonEmpty(task.ID, taskID),
			ModelID:      task.ModelID,
			Status:       task.Status,
			Progress:     progress,
			ResultURL:    strings.TrimSpace(task.ResultURL),
			ErrorMessage: task.ErrorMessage,
		}
		if onProgress != nil {
			onProgress(progressResult)
		}
		switch status {
		case "completed":
			if strings.TrimSpace(task.ResultURL) == "" {
				return nil, errors.New("videogen completed without result_url")
			}
			progressResult.Progress = 100
			return &progressResult, nil
		case "failed":
			msg := strings.TrimSpace(task.ErrorMessage)
			if msg == "" {
				msg = "videogen task failed"
			}
			return nil, errors.New(msg)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func clampProgress(progress int) int {
	if progress < 0 {
		return 0
	}
	if progress > 100 {
		return 100
	}
	return progress
}

func (c *Client) resolveModelID(ctx context.Context, cfg Config, opt Options) (string, error) {
	if strings.TrimSpace(opt.ModelID) != "" {
		return strings.TrimSpace(opt.ModelID), nil
	}
	modelName := defaultString(opt.Model, cfg.Model)
	if isUUID(modelName) {
		return modelName, nil
	}
	models, err := c.ListModels(ctx)
	if err != nil {
		return "", err
	}
	var firstVideo string
	var defaultVideo string
	for _, m := range models {
		if m.Enabled != nil && !*m.Enabled {
			continue
		}
		if m.Disabled {
			continue
		}
		if !isVideoModel(m) {
			continue
		}
		if firstVideo == "" {
			firstVideo = m.ID
		}
		if isDefaultModelAlias(m.ID) || isDefaultModelAlias(m.Name) {
			defaultVideo = strings.TrimSpace(m.ID)
		}
		if strings.EqualFold(strings.TrimSpace(m.Name), strings.TrimSpace(modelName)) {
			return m.ID, nil
		}
	}
	if firstVideo != "" && strings.TrimSpace(modelName) == "" {
		return firstVideo, nil
	}
	if isDefaultModelAlias(modelName) {
		if defaultVideo != "" {
			return defaultVideo, nil
		}
		if firstVideo != "" {
			return firstVideo, nil
		}
	}
	return "", fmt.Errorf("videogen model not found: %s", modelName)
}

func (c *Client) doJSON(ctx context.Context, method, url, apiKey string, payload any, out any) error {
	var body io.Reader
	if payload != nil {
		data, _ := json.Marshal(payload)
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := c.httpClient
	if client == nil {
		client = &http.Client{Timeout: defaultRequestTimout}
	}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		data, _ := io.ReadAll(io.LimitReader(res.Body, 16*1024))
		return fmt.Errorf("videogen upstream %d: %s", res.StatusCode, strings.TrimSpace(string(data)))
	}
	if out == nil {
		io.Copy(io.Discard, res.Body)
		return nil
	}
	if err := json.NewDecoder(res.Body).Decode(out); err != nil {
		return fmt.Errorf("videogen decode: %w", err)
	}
	return nil
}

func (c *Client) runtimeConfig() Config {
	cfg := Config{
		BaseURL:       c.baseURL,
		APIKey:        c.key(),
		APIKeyEnv:     c.apiKeyEnv,
		Model:         c.model,
		TimeoutSec:    c.timeoutSec,
		DurationSec:   c.durationSec,
		AspectRatio:   c.aspectRatio,
		Resolution:    c.resolution,
		GenerateAudio: c.generateAudio,
	}
	if c.provider != nil {
		if !c.provider.VideoGenEnabled() {
			cfg.APIKey = ""
			return cfg
		}
		cfg.BaseURL = c.provider.VideoGenBaseURL()
		cfg.APIKey = c.provider.VideoGenAPIKey()
		cfg.Model = c.provider.VideoGenModel()
		cfg.TimeoutSec = c.provider.VideoGenTimeoutSec()
		cfg.DurationSec = c.provider.VideoGenDurationSec()
		cfg.AspectRatio = c.provider.VideoGenAspectRatio()
		cfg.Resolution = c.provider.VideoGenResolution()
		cfg.GenerateAudio = c.provider.VideoGenGenerateAudio()
	}
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultBaseURL
	}
	cfg.Model = defaultString(cfg.Model, defaultModel)
	if cfg.TimeoutSec <= 0 {
		cfg.TimeoutSec = defaultTimeoutSec
	}
	if cfg.DurationSec <= 0 {
		cfg.DurationSec = defaultDurationSec
	}
	cfg.AspectRatio = defaultString(cfg.AspectRatio, defaultAspectRatio)
	cfg.Resolution = defaultString(cfg.Resolution, defaultResolution)
	return cfg
}

func (c *Client) accountConfig() Config {
	cfg := Config{
		BaseURL: c.baseURL,
		APIKey:  c.key(),
	}
	if c.provider != nil {
		cfg.BaseURL = c.provider.VideoGenBaseURL()
		cfg.APIKey = c.provider.VideoGenAPIKey()
	}
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultBaseURL
	}
	return cfg
}

func (c *Client) key() string {
	if c == nil {
		return ""
	}
	if c.apiKey != "" {
		return c.apiKey
	}
	return strings.TrimSpace(os.Getenv(c.apiKeyEnv))
}

func ensureTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok || timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, timeout)
}

func isVideoModel(m Model) bool {
	t := strings.ToLower(strings.TrimSpace(m.Type))
	if strings.Contains(t, "video") {
		return true
	}
	name := strings.ToLower(strings.TrimSpace(m.Name))
	return strings.Contains(name, "seedance") || strings.Contains(name, "video")
}

func videoModelLabel(m Model) string {
	name := strings.TrimSpace(m.Name)
	if name == "" {
		name = strings.TrimSpace(m.ID)
	}
	if t := strings.TrimSpace(m.Type); t != "" {
		return name + " (" + t + ")"
	}
	return name
}

func probeModelValue(m Model) string {
	return firstNonEmpty(m.ID, m.Name)
}

func isUUID(s string) bool {
	_, err := uuid.Parse(strings.TrimSpace(s))
	return err == nil
}

func isDefaultModelAlias(s string) bool {
	s = strings.TrimSpace(s)
	return strings.EqualFold(s, defaultModelID) ||
		strings.EqualFold(s, defaultModelName) ||
		strings.EqualFold(s, "Seedance 2.0")
}

func defaultString(s, fallback string) string {
	if strings.TrimSpace(s) != "" {
		return strings.TrimSpace(s)
	}
	return fallback
}

func intDefault(n, fallback int) int {
	if n > 0 {
		return n
	}
	return fallback
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func GenerateTaskID() string {
	return "vid_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:24]
}
