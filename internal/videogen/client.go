// Package videogen calls the AI Gen Platform video generation API.
package videogen

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/432539/gpt2api/pkg/logger"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

const (
	ChannelEchoon          = "echoon"
	ChannelAPIYISeedance   = "apiyi_seedance2"
	ChannelAPIYIWan27      = "apiyi_wan27"
	ChannelAPIYIHappyHorse = "apiyi_happyhorse"

	defaultBaseURL       = "http://app.echoon.top/api/v1"
	defaultModelID       = "0e37fa2d-72b3-483a-81b4-ad595cd147c7"
	defaultModelName     = "Seedance-2.0-D-V"
	defaultModel         = defaultModelID
	defaultTimeoutSec    = 1800
	defaultPollInterval  = 3 * time.Second
	defaultAspectRatio   = "16:9"
	defaultResolution    = "720p"
	defaultDurationSec   = 5
	defaultRequestTimout = 60 * time.Second

	apiyiDefaultBaseURL       = "https://api.apiyi.com"
	apiyiDefaultFastModel     = "doubao-seedance-2-0-fast-260128"
	apiyiDefaultStandardModel = "doubao-seedance-2-0-260128"
	apiyiTaskPath             = "/seedance/api/v3/contents/generations/tasks"

	apiyiWanDefaultModel = "wan2.7-r2v"
	apiyiWanTextModel    = "wan2.7-t2v"
	apiyiWanImageModel   = "wan2.7-i2v"
	apiyiWanTaskPath     = "/wan/api/v1/services/aigc/video-generation/video-synthesis"
	apiyiWanQueryPath    = "/v1/tasks"

	apiyiHappyHorseDefaultModel = "happyhorse-1.0-r2v"
	apiyiHappyHorseTextModel    = "happyhorse-1.0-t2v"
	apiyiHappyHorseImageModel   = "happyhorse-1.0-i2v"
)

type Config struct {
	ChannelType   string
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
	VideoGenChannelType() string
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
	channelType   string
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
	Model             string
	ModelID           string
	Prompt            string
	Images            []ImageInput
	ReferenceURL      string
	ReferenceVideoURL string
	DurationSec       int
	AspectRatio       string
	Resolution        string
	GenerateAudio     *bool
	ExtraParams       map[string]any
	OnProgress        func(Result)
}

type ImageInput struct {
	URL  string `json:"url"`
	Name string `json:"name,omitempty"`
}

type Result struct {
	TaskID        string
	ModelID       string
	Status        string
	Progress      int
	ProgressKnown bool
	ResultURL     string
	CostType      string
	CostDetail    CostDetail
	ErrorMessage  string
	DurationMs    int64
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
	Supported       bool        `json:"supported"`
	Message         string      `json:"message,omitempty"`
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

type CostDetail struct {
	ModelName string          `json:"model_name,omitempty"`
	Price     float64         `json:"price,omitempty"`
	Raw       json.RawMessage `json:"-"`
}

type generateResp struct {
	TaskID   string `json:"task_id"`
	ID       string `json:"id"`
	Status   string `json:"status"`
	Progress int    `json:"progress"`
}

type taskResp struct {
	ID         string `json:"id"`
	ModelID    string `json:"model_id"`
	Status     string `json:"status"`
	Progress   int    `json:"progress"`
	Prompt     string `json:"prompt"`
	CostType   string `json:"cost_type"`
	CostDetail struct {
		ModelName string          `json:"model_name"`
		Price     json.RawMessage `json:"price"`
	} `json:"cost_detail"`
	ResultURL    string `json:"result_url"`
	ErrorMessage string `json:"error_message"`
}

type apiyiTaskResp struct {
	ID              string `json:"id"`
	TaskID          string `json:"task_id"`
	Model           string `json:"model"`
	ModelName       string `json:"model_name"`
	Status          string `json:"status"`
	TaskStatus      string `json:"task_status"`
	Progress        any    `json:"progress"`
	TaskProgress    any    `json:"task_progress"`
	ProgressPercent any    `json:"progress_percent"`
	Percent         any    `json:"percent"`
	ResultURL       string `json:"result_url"`
	Content         struct {
		VideoURL string `json:"video_url"`
	} `json:"content"`
	Data struct {
		ID              string `json:"id"`
		Model           string `json:"model"`
		ModelName       string `json:"model_name"`
		Status          string `json:"status"`
		TaskStatus      string `json:"task_status"`
		Progress        any    `json:"progress"`
		TaskProgress    any    `json:"task_progress"`
		ProgressPercent any    `json:"progress_percent"`
		Percent         any    `json:"percent"`
		ResultURL       string `json:"result_url"`
		Content         struct {
			VideoURL string `json:"video_url"`
		} `json:"content"`
	} `json:"data"`
	Output struct {
		ID              string `json:"id"`
		TaskID          string `json:"task_id"`
		Model           string `json:"model"`
		ModelName       string `json:"model_name"`
		Status          string `json:"status"`
		TaskStatus      string `json:"task_status"`
		Progress        any    `json:"progress"`
		TaskProgress    any    `json:"task_progress"`
		ProgressPercent any    `json:"progress_percent"`
		Percent         any    `json:"percent"`
		ResultURL       string `json:"result_url"`
		VideoURL        string `json:"video_url"`
		Content         struct {
			VideoURL string `json:"video_url"`
		} `json:"content"`
	} `json:"output"`
	Usage         json.RawMessage `json:"usage"`
	Error         any             `json:"error"`
	Resolution    string          `json:"resolution"`
	Ratio         string          `json:"ratio"`
	Duration      int             `json:"duration"`
	GenerateAudio bool            `json:"generate_audio"`
	raw           map[string]any
}

type apiyiWanCreateResp struct {
	ID     string `json:"id"`
	TaskID string `json:"task_id"`
	Output struct {
		TaskID     string `json:"task_id"`
		TaskStatus string `json:"task_status"`
	} `json:"output"`
	RequestID string `json:"request_id"`
}

type apiyiWanTaskResp struct {
	ID              string `json:"id"`
	TaskID          string `json:"task_id"`
	Model           string `json:"model"`
	ModelName       string `json:"model_name"`
	Status          string `json:"status"`
	TaskStatus      string `json:"task_status"`
	Progress        any    `json:"progress"`
	TaskProgress    any    `json:"task_progress"`
	ProgressPercent any    `json:"progress_percent"`
	Percent         any    `json:"percent"`
	ResultURL       string `json:"result_url"`
	Output          struct {
		TaskID          string `json:"task_id"`
		Model           string `json:"model"`
		ModelName       string `json:"model_name"`
		Status          string `json:"status"`
		TaskStatus      string `json:"task_status"`
		ResultURL       string `json:"result_url"`
		VideoURL        string `json:"video_url"`
		Progress        any    `json:"progress"`
		TaskProgress    any    `json:"task_progress"`
		ProgressPercent any    `json:"progress_percent"`
		Percent         any    `json:"percent"`
	} `json:"output"`
	Error      any             `json:"error"`
	FailReason string          `json:"fail_reason"`
	Message    string          `json:"message"`
	Usage      json.RawMessage `json:"usage"`
	raw        map[string]any
}

func (t *apiyiTaskResp) UnmarshalJSON(data []byte) error {
	type alias apiyiTaskResp
	var parsed alias
	if err := json.Unmarshal(data, &parsed); err != nil {
		return err
	}
	*t = apiyiTaskResp(parsed)
	_ = json.Unmarshal(data, &t.raw)
	return nil
}

func (t *apiyiWanTaskResp) UnmarshalJSON(data []byte) error {
	type alias apiyiWanTaskResp
	var parsed alias
	if err := json.Unmarshal(data, &parsed); err != nil {
		return err
	}
	*t = apiyiWanTaskResp(parsed)
	_ = json.Unmarshal(data, &t.raw)
	return nil
}

func NewClient(cfg Config) *Client {
	channelType := normalizeChannelType(cfg.ChannelType)
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultBaseURLForChannel(channelType)
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
		channelType:   channelType,
		baseURL:       baseURL,
		apiKey:        strings.TrimSpace(cfg.APIKey),
		apiKeyEnv:     apiKeyEnv,
		model:         defaultString(cfg.Model, defaultModelForChannel(channelType)),
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
	if isAPIYIChannel(cfg.ChannelType) {
		return apiYIModelsForChannel(cfg.ChannelType), nil
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
	return c.balance(ctx, c.accountConfig())
}

func (c *Client) BalanceForConfig(ctx context.Context, cfg Config) (*Balance, error) {
	if c == nil {
		return nil, errors.New("videogen client not configured")
	}
	return c.balance(ctx, c.normalizedAccountConfig(cfg))
}

func (c *Client) balance(ctx context.Context, cfg Config) (*Balance, error) {
	start := time.Now()
	if isAPIYIChannel(cfg.ChannelType) {
		return &Balance{
			Supported:  false,
			Message:    apiYIBalanceMessage(cfg.ChannelType),
			FreeQuotas: []FreeQuota{},
			DurationMs: time.Since(start).Milliseconds(),
		}, nil
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("videogen api key is empty")
	}
	var balance Balance
	if err := c.doJSON(ctx, http.MethodGet, cfg.BaseURL+"/account/balance", cfg.APIKey, nil, &balance); err != nil {
		return nil, err
	}
	if balance.FreeQuotas == nil {
		balance.FreeQuotas = []FreeQuota{}
	}
	balance.Supported = true
	balance.DurationMs = time.Since(start).Milliseconds()
	return &balance, nil
}

func (c *Client) Generate(ctx context.Context, opt Options) (*Result, error) {
	if c == nil {
		return nil, errors.New("videogen client not configured")
	}
	cfg := c.runtimeConfig()
	return c.generateWithConfig(ctx, cfg, opt)
}

func (c *Client) GenerateForConfig(ctx context.Context, cfg Config, opt Options) (*Result, error) {
	if c == nil {
		return nil, errors.New("videogen client not configured")
	}
	return c.generateWithConfig(ctx, c.normalizedRuntimeConfig(cfg), opt)
}

func (c *Client) generateWithConfig(ctx context.Context, cfg Config, opt Options) (*Result, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("videogen is disabled or api key is empty")
	}
	if strings.TrimSpace(opt.Prompt) == "" {
		return nil, errors.New("prompt required")
	}
	ctx, cancel := ensureTimeout(ctx, time.Duration(cfg.TimeoutSec)*time.Second)
	defer cancel()

	start := time.Now()
	switch cfg.ChannelType {
	case ChannelAPIYISeedance:
		return c.generateAPIYI(ctx, cfg, opt, start)
	case ChannelAPIYIWan27, ChannelAPIYIHappyHorse:
		return c.generateAPIYIWan(ctx, cfg, opt, start)
	}
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
	if strings.TrimSpace(opt.ReferenceVideoURL) != "" {
		payload["reference_video_url"] = strings.TrimSpace(opt.ReferenceVideoURL)
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
			TaskID:        taskID,
			ModelID:       modelID,
			Status:        "queued",
			Progress:      0,
			ProgressKnown: false,
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
	switch cfg.ChannelType {
	case ChannelAPIYISeedance:
		return c.getAPIYITask(ctx, cfg, taskID)
	case ChannelAPIYIWan27, ChannelAPIYIHappyHorse:
		return c.getAPIYIWanTask(ctx, cfg, taskID)
	}
	var task taskResp
	if err := c.doJSON(ctx, http.MethodGet, cfg.BaseURL+"/generate/tasks/"+taskID, cfg.APIKey, nil, &task); err != nil {
		return nil, err
	}
	return &Result{
		TaskID:        firstNonEmpty(task.ID, taskID),
		ModelID:       task.ModelID,
		Status:        task.Status,
		Progress:      clampProgress(task.Progress),
		ProgressKnown: true,
		ResultURL:     strings.TrimSpace(task.ResultURL),
		CostType:      strings.TrimSpace(task.CostType),
		CostDetail:    task.costDetail(),
		ErrorMessage:  task.ErrorMessage,
	}, nil
}

func (c *Client) ProbeModels(ctx context.Context) (durationMs int64, modelCount int, modelName string, videoModels []ProbeModel, err error) {
	start := time.Now()
	cfg := c.runtimeConfig()
	return c.probeModels(ctx, cfg, start)
}

func (c *Client) ProbeModelsForConfig(ctx context.Context, cfg Config) (durationMs int64, modelCount int, modelName string, videoModels []ProbeModel, err error) {
	if c == nil {
		return 0, 0, "", nil, errors.New("videogen client not configured")
	}
	start := time.Now()
	return c.probeModels(ctx, c.normalizedRuntimeConfig(cfg), start)
}

func (c *Client) probeModels(ctx context.Context, cfg Config, start time.Time) (durationMs int64, modelCount int, modelName string, videoModels []ProbeModel, err error) {
	if isAPIYIChannel(cfg.ChannelType) {
		models := apiYIProbeModelsForChannel(cfg.ChannelType)
		return time.Since(start).Milliseconds(), len(models), defaultModelForChannel(cfg.ChannelType), models, nil
	}
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

func (c *Client) generateAPIYI(ctx context.Context, cfg Config, opt Options, start time.Time) (*Result, error) {
	model := defaultString(firstNonEmpty(opt.ModelID, opt.Model, cfg.Model), apiyiDefaultFastModel)
	payload := c.apiyiPayload(cfg, opt, model)
	var created generateResp
	if err := c.doJSONAPIYICreate(ctx, cfg, payload, &created); err != nil {
		return nil, err
	}
	taskID := firstNonEmpty(created.ID, created.TaskID)
	if taskID == "" {
		return nil, errors.New("videogen create task response missing task_id")
	}
	if opt.OnProgress != nil {
		opt.OnProgress(Result{
			TaskID:        taskID,
			ModelID:       model,
			Status:        "queued",
			Progress:      0,
			ProgressKnown: false,
		})
	}
	task, err := c.pollAPIYITask(ctx, cfg, taskID, opt.OnProgress)
	if err != nil {
		return nil, err
	}
	task.DurationMs = time.Since(start).Milliseconds()
	return task, nil
}

func (c *Client) apiyiPayload(cfg Config, opt Options, model string) map[string]any {
	content := []map[string]any{}
	if prompt := strings.TrimSpace(opt.Prompt); prompt != "" {
		content = append(content, map[string]any{"type": "text", "text": prompt})
	}
	for _, img := range opt.Images {
		url := strings.TrimSpace(img.URL)
		if url == "" {
			continue
		}
		content = append(content, map[string]any{
			"type":      "image_url",
			"image_url": map[string]any{"url": url},
			"role":      "reference_image",
		})
	}
	if url := strings.TrimSpace(opt.ReferenceURL); url != "" {
		content = append(content, map[string]any{
			"type":      "image_url",
			"image_url": map[string]any{"url": url},
			"role":      "reference_image",
		})
	}
	if url := strings.TrimSpace(opt.ReferenceVideoURL); url != "" {
		content = append(content, map[string]any{
			"type":      "video_url",
			"video_url": map[string]any{"url": url},
			"role":      "reference_video",
		})
	}
	payload := map[string]any{}
	for k, v := range opt.ExtraParams {
		payload[k] = v
	}
	payload["model"] = model
	payload["content"] = content
	payload["resolution"] = defaultString(opt.Resolution, cfg.Resolution)
	payload["ratio"] = defaultString(opt.AspectRatio, cfg.AspectRatio)
	payload["duration"] = intDefault(opt.DurationSec, cfg.DurationSec)
	if opt.GenerateAudio != nil {
		payload["generate_audio"] = *opt.GenerateAudio
	} else {
		payload["generate_audio"] = cfg.GenerateAudio
	}
	return payload
}

func (c *Client) generateAPIYIWan(ctx context.Context, cfg Config, opt Options, start time.Time) (*Result, error) {
	model := c.apiyiWanModel(cfg, opt)
	payload := c.apiyiWanPayload(cfg, opt, model)
	var created apiyiWanCreateResp
	if err := c.doJSONAPIYIWanCreate(ctx, cfg, payload, &created); err != nil {
		return nil, err
	}
	taskID := firstNonEmpty(created.Output.TaskID, created.TaskID, created.ID)
	if taskID == "" {
		return nil, errors.New("videogen create task response missing task_id")
	}
	if opt.OnProgress != nil {
		opt.OnProgress(Result{
			TaskID:        taskID,
			ModelID:       model,
			Status:        "queued",
			Progress:      0,
			ProgressKnown: false,
		})
	}
	task, err := c.pollAPIYIWanTask(ctx, cfg, taskID, model, opt.OnProgress)
	if err != nil {
		return nil, err
	}
	task.DurationMs = time.Since(start).Milliseconds()
	return task, nil
}

func (c *Client) apiyiWanModel(cfg Config, opt Options) string {
	model := defaultString(firstNonEmpty(opt.ModelID, opt.Model, cfg.Model), defaultModelForChannel(cfg.ChannelType))
	if !hasWanMedia(opt) && !isDashScopeTextModel(model) {
		return dashScopeTextModelForChannel(cfg.ChannelType)
	}
	return model
}

func (c *Client) apiyiWanPayload(cfg Config, opt Options, model string) map[string]any {
	input := map[string]any{
		"prompt": strings.TrimSpace(opt.Prompt),
	}
	if !isDashScopeTextModel(model) {
		media := apiyiWanMedia(opt, model, cfg.ChannelType)
		if len(media) > 0 {
			input["media"] = media
		}
	}
	parameters := map[string]any{
		"resolution":    normalizeWanResolution(defaultString(opt.Resolution, cfg.Resolution)),
		"ratio":         defaultString(opt.AspectRatio, cfg.AspectRatio),
		"duration":      intDefault(opt.DurationSec, cfg.DurationSec),
		"prompt_extend": true,
		"watermark":     false,
	}
	payload := map[string]any{
		"model":      model,
		"input":      input,
		"parameters": parameters,
	}
	for k, v := range opt.ExtraParams {
		switch k {
		case "input":
			if extraInput, ok := v.(map[string]any); ok {
				for ik, iv := range extraInput {
					input[ik] = iv
				}
			}
		case "parameters":
			if extraParams, ok := v.(map[string]any); ok {
				for pk, pv := range extraParams {
					parameters[pk] = pv
				}
			}
		default:
			payload[k] = v
		}
	}
	return payload
}

func apiyiWanMedia(opt Options, model string, channelType string) []map[string]any {
	mediaType := "reference_image"
	if isDashScopeImageModel(model) {
		mediaType = "first_frame"
	}
	limit := dashScopeMediaLimit(channelType, model)
	media := make([]map[string]any, 0, len(opt.Images)+1)
	if url := strings.TrimSpace(opt.ReferenceVideoURL); url != "" {
		return []map[string]any{{
			"type": "reference_video",
			"url":  url,
		}}
	}
	for _, img := range opt.Images {
		url := strings.TrimSpace(img.URL)
		if url == "" {
			continue
		}
		media = append(media, map[string]any{
			"type": mediaType,
			"url":  url,
		})
		if mediaType == "first_frame" {
			return media
		}
		if len(media) >= limit {
			return media
		}
	}
	if url := strings.TrimSpace(opt.ReferenceURL); url != "" && len(media) < limit {
		media = append(media, map[string]any{
			"type": mediaType,
			"url":  url,
		})
	}
	return media
}

func hasWanMedia(opt Options) bool {
	if strings.TrimSpace(opt.ReferenceVideoURL) != "" {
		return true
	}
	if strings.TrimSpace(opt.ReferenceURL) != "" {
		return true
	}
	for _, img := range opt.Images {
		if strings.TrimSpace(img.URL) != "" {
			return true
		}
	}
	return false
}

func isDashScopeTextModel(model string) bool {
	model = strings.TrimSpace(model)
	return strings.EqualFold(model, apiyiWanTextModel) ||
		strings.EqualFold(model, apiyiHappyHorseTextModel)
}

func isDashScopeImageModel(model string) bool {
	model = strings.TrimSpace(model)
	return strings.EqualFold(model, apiyiWanImageModel) ||
		strings.EqualFold(model, apiyiHappyHorseImageModel)
}

func dashScopeTextModelForChannel(channelType string) string {
	switch normalizeChannelType(channelType) {
	case ChannelAPIYIHappyHorse:
		return apiyiHappyHorseTextModel
	default:
		return apiyiWanTextModel
	}
}

func dashScopeMediaLimit(channelType string, model string) int {
	if isDashScopeImageModel(model) {
		return 1
	}
	if normalizeChannelType(channelType) == ChannelAPIYIHappyHorse {
		return 9
	}
	return 5
}

func normalizeWanResolution(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "720P"
	}
	return strings.ToUpper(s)
}

func (c *Client) getAPIYITask(ctx context.Context, cfg Config, taskID string) (*Result, error) {
	var task apiyiTaskResp
	if err := c.doJSONAPIYI(ctx, http.MethodGet, cfg.BaseURL+apiyiTaskPath+"/"+taskID, cfg.APIKey, nil, &task); err != nil {
		return nil, err
	}
	result := task.toResult(taskID)
	logAPIYITaskProgress("videogen apiyi task status", taskID, task, result)
	return result, nil
}

func (c *Client) getAPIYIWanTask(ctx context.Context, cfg Config, taskID string) (*Result, error) {
	var task apiyiWanTaskResp
	if err := c.doJSONAPIYI(ctx, http.MethodGet, cfg.BaseURL+apiyiWanQueryPath+"/"+taskID, cfg.APIKey, nil, &task); err != nil {
		return nil, err
	}
	result := task.toResult(taskID, defaultModelForChannel(cfg.ChannelType))
	logAPIYIWanTaskProgress("videogen apiyi wan task status", taskID, task, result)
	return result, nil
}

func (c *Client) pollAPIYITask(ctx context.Context, cfg Config, taskID string, onProgress func(Result)) (*Result, error) {
	ticker := time.NewTicker(defaultPollInterval)
	defer ticker.Stop()
	for {
		var task apiyiTaskResp
		if err := c.doJSONAPIYI(ctx, http.MethodGet, cfg.BaseURL+apiyiTaskPath+"/"+taskID, cfg.APIKey, nil, &task); err != nil {
			return nil, err
		}
		progressResult := task.toResult(taskID)
		logAPIYITaskProgress("videogen apiyi task poll", taskID, task, progressResult)
		if onProgress != nil {
			onProgress(*progressResult)
		}
		switch strings.ToLower(strings.TrimSpace(progressResult.Status)) {
		case "completed":
			if strings.TrimSpace(progressResult.ResultURL) == "" {
				return nil, errors.New("videogen completed without result_url")
			}
			progressResult.Progress = 100
			progressResult.ProgressKnown = true
			return progressResult, nil
		case "failed":
			msg := task.errorMessage()
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

func (c *Client) pollAPIYIWanTask(ctx context.Context, cfg Config, taskID string, model string, onProgress func(Result)) (*Result, error) {
	ticker := time.NewTicker(defaultPollInterval)
	defer ticker.Stop()
	for {
		var task apiyiWanTaskResp
		if err := c.doJSONAPIYI(ctx, http.MethodGet, cfg.BaseURL+apiyiWanQueryPath+"/"+taskID, cfg.APIKey, nil, &task); err != nil {
			return nil, err
		}
		progressResult := task.toResult(taskID, model)
		logAPIYIWanTaskProgress("videogen apiyi wan task poll", taskID, task, progressResult)
		if onProgress != nil {
			onProgress(*progressResult)
		}
		switch strings.ToLower(strings.TrimSpace(progressResult.Status)) {
		case "completed":
			if strings.TrimSpace(progressResult.ResultURL) == "" {
				return nil, errors.New("videogen completed without result_url")
			}
			progressResult.Progress = 100
			progressResult.ProgressKnown = true
			return progressResult, nil
		case "failed":
			msg := task.errorMessage()
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

func (t apiyiTaskResp) toResult(fallbackID string) *Result {
	upstreamStatus := firstNonEmpty(t.Output.TaskStatus, t.Output.Status, t.Data.TaskStatus, t.Data.Status, t.TaskStatus, t.Status)
	status := mapAPIYIStatus(upstreamStatus)
	progressRaw := t.progressValue()
	progress := parseAPIYIProgress(progressRaw, status)
	model := firstNonEmpty(t.Output.ModelName, t.Output.Model, t.Data.ModelName, t.Data.Model, t.ModelName, t.Model)
	videoURL := firstNonEmpty(t.ResultURL, t.Data.ResultURL, t.Output.ResultURL, t.Output.VideoURL, t.Output.Content.VideoURL, t.Data.Content.VideoURL, t.Content.VideoURL)
	priceRaw := json.RawMessage(`1`)
	if len(t.Usage) > 0 && string(t.Usage) != "null" {
		priceRaw = append(json.RawMessage(nil), t.Usage...)
	}
	return &Result{
		TaskID:        firstNonEmpty(t.Output.TaskID, t.Output.ID, t.Data.ID, t.ID, t.TaskID, fallbackID),
		ModelID:       strings.TrimSpace(model),
		Status:        status,
		Progress:      progress,
		ProgressKnown: progressKnown(progressRaw, status),
		ResultURL:     strings.TrimSpace(videoURL),
		CostType:      "credits",
		CostDetail:    CostDetail{ModelName: strings.TrimSpace(model), Price: 1, Raw: priceRaw},
		ErrorMessage:  t.errorMessage(),
	}
}

func (t apiyiWanTaskResp) toResult(fallbackID string, fallbackModel string) *Result {
	status := mapAPIYIWanStatus(firstNonEmpty(t.Output.TaskStatus, t.Output.Status, t.TaskStatus, t.Status))
	progressRaw := t.progressValue()
	progress := parseAPIYIProgress(progressRaw, status)
	videoURL := firstNonEmpty(t.ResultURL, t.Output.ResultURL, t.Output.VideoURL)
	model := firstNonEmpty(t.Output.ModelName, t.Output.Model, t.ModelName, t.Model, fallbackModel)
	priceRaw := json.RawMessage(`1`)
	if len(t.Usage) > 0 && string(t.Usage) != "null" {
		priceRaw = append(json.RawMessage(nil), t.Usage...)
	}
	return &Result{
		TaskID:        firstNonEmpty(t.TaskID, t.Output.TaskID, t.ID, fallbackID),
		ModelID:       strings.TrimSpace(model),
		Status:        status,
		Progress:      progress,
		ProgressKnown: progressKnown(progressRaw, status),
		ResultURL:     strings.TrimSpace(videoURL),
		CostType:      "credits",
		CostDetail:    CostDetail{ModelName: strings.TrimSpace(model), Price: 1, Raw: priceRaw},
		ErrorMessage:  t.errorMessage(),
	}
}

func (t apiyiTaskResp) progressValue() any {
	return firstAPIYIProgressValue(
		t.Progress,
		t.TaskProgress,
		t.ProgressPercent,
		t.Percent,
		t.Data.Progress,
		t.Data.TaskProgress,
		t.Data.ProgressPercent,
		t.Data.Percent,
		t.Output.Progress,
		t.Output.TaskProgress,
		t.Output.ProgressPercent,
		t.Output.Percent,
		progressFromRawMap(t.raw),
	)
}

func (t apiyiWanTaskResp) progressValue() any {
	return firstAPIYIProgressValue(
		t.Progress,
		t.TaskProgress,
		t.ProgressPercent,
		t.Percent,
		t.Output.Progress,
		t.Output.TaskProgress,
		t.Output.ProgressPercent,
		t.Output.Percent,
		progressFromRawMap(t.raw),
	)
}

func (t apiyiWanTaskResp) errorMessage() string {
	if msg := strings.TrimSpace(t.FailReason); msg != "" {
		return msg
	}
	if msg := strings.TrimSpace(t.Message); msg != "" {
		return msg
	}
	switch v := t.Error.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case map[string]any:
		for _, key := range []string{"message", "msg", "error", "code"} {
			if s, ok := v[key].(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	data, _ := json.Marshal(t.Error)
	return strings.TrimSpace(string(data))
}

func (t apiyiTaskResp) errorMessage() string {
	switch v := t.Error.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case map[string]any:
		for _, key := range []string{"message", "msg", "error"} {
			if s, ok := v[key].(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	data, _ := json.Marshal(t.Error)
	return strings.TrimSpace(string(data))
}

func logAPIYITaskProgress(message, fallbackID string, task apiyiTaskResp, result *Result) {
	if result == nil {
		return
	}
	taskID := firstNonEmpty(result.TaskID, task.Output.TaskID, task.Output.ID, task.Data.ID, task.ID, task.TaskID, fallbackID)
	logger.L().Info(message,
		zap.String("task_id", taskID),
		zap.String("upstream_status", strings.TrimSpace(task.Status)),
		zap.String("data_status", strings.TrimSpace(task.Data.Status)),
		zap.String("output_status", strings.TrimSpace(firstNonEmpty(task.Output.TaskStatus, task.Output.Status))),
		zap.String("raw_progress", progressLogValue(task.progressValue())),
		zap.String("mapped_status", result.Status),
		zap.Int("mapped_progress", result.Progress),
		zap.Bool("has_result_url", strings.TrimSpace(result.ResultURL) != ""))
}

func logAPIYIWanTaskProgress(message, fallbackID string, task apiyiWanTaskResp, result *Result) {
	if result == nil {
		return
	}
	taskID := firstNonEmpty(result.TaskID, task.TaskID, task.Output.TaskID, task.ID, fallbackID)
	logger.L().Info(message,
		zap.String("task_id", taskID),
		zap.String("upstream_status", strings.TrimSpace(task.Status)),
		zap.String("output_status", strings.TrimSpace(task.Output.TaskStatus)),
		zap.String("raw_progress", progressLogValue(task.progressValue())),
		zap.String("mapped_status", result.Status),
		zap.Int("mapped_progress", result.Progress),
		zap.Bool("has_result_url", strings.TrimSpace(result.ResultURL) != ""))
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
			TaskID:        firstNonEmpty(task.ID, taskID),
			ModelID:       task.ModelID,
			Status:        task.Status,
			Progress:      progress,
			ProgressKnown: true,
			ResultURL:     strings.TrimSpace(task.ResultURL),
			CostType:      strings.TrimSpace(task.CostType),
			CostDetail:    task.costDetail(),
			ErrorMessage:  task.ErrorMessage,
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
			progressResult.ProgressKnown = true
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

func (t taskResp) costDetail() CostDetail {
	return CostDetail{
		ModelName: strings.TrimSpace(t.CostDetail.ModelName),
		Price:     parseJSONNumber(t.CostDetail.Price),
		Raw:       t.CostDetail.Price,
	}
}

func parseJSONNumber(raw json.RawMessage) float64 {
	if len(raw) == 0 || string(raw) == "null" {
		return 0
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
		return f
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
			return f
		}
	}
	return 0
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
	return c.doJSONWithHeaders(ctx, method, url, apiKey, payload, out, nil)
}

func (c *Client) doJSONAPIYI(ctx context.Context, method, url, apiKey string, payload any, out any) error {
	return c.doJSONWithHeaders(ctx, method, url, apiKey, payload, out, map[string]string{
		"Accept-Encoding": "identity",
	})
}

func (c *Client) doJSONAPIYICreate(ctx context.Context, cfg Config, payload any, out any) error {
	timeout := time.Duration(cfg.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = time.Duration(defaultTimeoutSec) * time.Second
	}
	return c.doJSONWithHeadersAndTimeout(ctx, http.MethodPost, cfg.BaseURL+apiyiTaskPath, cfg.APIKey, payload, out, map[string]string{
		"Accept-Encoding": "identity",
	}, timeout)
}

func (c *Client) doJSONAPIYIWanCreate(ctx context.Context, cfg Config, payload any, out any) error {
	timeout := time.Duration(cfg.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = time.Duration(defaultTimeoutSec) * time.Second
	}
	return c.doJSONWithHeadersAndTimeout(ctx, http.MethodPost, cfg.BaseURL+apiyiWanTaskPath, cfg.APIKey, payload, out, map[string]string{
		"Accept-Encoding":   "identity",
		"X-DashScope-Async": "enable",
	}, timeout)
}

func (c *Client) doJSONWithHeaders(ctx context.Context, method, url, apiKey string, payload any, out any, headers map[string]string) error {
	return c.doJSONWithHeadersAndTimeout(ctx, method, url, apiKey, payload, out, headers, 0)
}

func (c *Client) doJSONWithHeadersAndTimeout(ctx context.Context, method, url, apiKey string, payload any, out any, headers map[string]string, timeout time.Duration) error {
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
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	client := c.httpClient
	if client == nil {
		client = &http.Client{Timeout: defaultRequestTimout}
	}
	if timeout > 0 && client.Timeout != timeout {
		clone := *client
		clone.Timeout = timeout
		client = &clone
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
		ChannelType:   c.channelType,
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
		cfg.ChannelType = c.provider.VideoGenChannelType()
		cfg.BaseURL = c.provider.VideoGenBaseURL()
		cfg.APIKey = c.provider.VideoGenAPIKey()
		cfg.Model = c.provider.VideoGenModel()
		cfg.TimeoutSec = c.provider.VideoGenTimeoutSec()
		cfg.DurationSec = c.provider.VideoGenDurationSec()
		cfg.AspectRatio = c.provider.VideoGenAspectRatio()
		cfg.Resolution = c.provider.VideoGenResolution()
		cfg.GenerateAudio = c.provider.VideoGenGenerateAudio()
	}
	return c.normalizedRuntimeConfig(cfg)
}

func (c *Client) normalizedRuntimeConfig(cfg Config) Config {
	cfg.ChannelType = normalizeChannelType(cfg.ChannelType)
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if isAPIYIChannel(cfg.ChannelType) && isEchoonDefaultBaseURL(cfg.BaseURL) {
		cfg.BaseURL = ""
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultBaseURLForChannel(cfg.ChannelType)
	}
	if isAPIYIChannel(cfg.ChannelType) && isEchoonDefaultModel(cfg.Model) {
		cfg.Model = ""
	}
	cfg.Model = defaultString(cfg.Model, defaultModelForChannel(cfg.ChannelType))
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
		ChannelType: c.channelType,
		BaseURL:     c.baseURL,
		APIKey:      c.key(),
	}
	if c.provider != nil {
		cfg.ChannelType = c.provider.VideoGenChannelType()
		cfg.BaseURL = c.provider.VideoGenBaseURL()
		cfg.APIKey = c.provider.VideoGenAPIKey()
	}
	return c.normalizedAccountConfig(cfg)
}

func (c *Client) normalizedAccountConfig(cfg Config) Config {
	cfg.ChannelType = normalizeChannelType(cfg.ChannelType)
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if isAPIYIChannel(cfg.ChannelType) && isEchoonDefaultBaseURL(cfg.BaseURL) {
		cfg.BaseURL = ""
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultBaseURLForChannel(cfg.ChannelType)
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
		strings.EqualFold(s, "Seedance 2.0") ||
		strings.EqualFold(s, apiyiDefaultFastModel) ||
		strings.EqualFold(s, apiyiDefaultStandardModel) ||
		strings.EqualFold(s, apiyiWanDefaultModel) ||
		strings.EqualFold(s, apiyiWanTextModel) ||
		strings.EqualFold(s, apiyiWanImageModel) ||
		strings.EqualFold(s, apiyiHappyHorseDefaultModel) ||
		strings.EqualFold(s, apiyiHappyHorseTextModel) ||
		strings.EqualFold(s, apiyiHappyHorseImageModel)
}

func normalizeChannelType(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", ChannelEchoon:
		return ChannelEchoon
	case ChannelAPIYISeedance:
		return ChannelAPIYISeedance
	case ChannelAPIYIWan27:
		return ChannelAPIYIWan27
	case ChannelAPIYIHappyHorse:
		return ChannelAPIYIHappyHorse
	default:
		return ChannelEchoon
	}
}

func defaultBaseURLForChannel(channelType string) string {
	if isAPIYIChannel(channelType) {
		return apiyiDefaultBaseURL
	}
	return defaultBaseURL
}

func defaultModelForChannel(channelType string) string {
	switch normalizeChannelType(channelType) {
	case ChannelAPIYISeedance:
		return apiyiDefaultFastModel
	case ChannelAPIYIWan27:
		return apiyiWanDefaultModel
	case ChannelAPIYIHappyHorse:
		return apiyiHappyHorseDefaultModel
	}
	return defaultModel
}

func isEchoonDefaultBaseURL(s string) bool {
	s = strings.TrimRight(strings.TrimSpace(s), "/")
	return strings.EqualFold(s, defaultBaseURL)
}

func isEchoonDefaultModel(s string) bool {
	s = strings.TrimSpace(s)
	return strings.EqualFold(s, defaultModelID) ||
		strings.EqualFold(s, defaultModelName) ||
		strings.EqualFold(s, "Seedance 2.0")
}

func isAPIYIChannel(channelType string) bool {
	switch normalizeChannelType(channelType) {
	case ChannelAPIYISeedance, ChannelAPIYIWan27, ChannelAPIYIHappyHorse:
		return true
	default:
		return false
	}
}

func apiYIBalanceMessage(channelType string) string {
	switch normalizeChannelType(channelType) {
	case ChannelAPIYIHappyHorse:
		return "当前 API易 HappyHorse 渠道不支持余额查询"
	case ChannelAPIYIWan27:
		return "当前 API易 Wan2.7 渠道不支持余额查询"
	default:
		return "当前 API易 Seedance 2.0 渠道不支持余额查询"
	}
}

func apiyiModels() []Model {
	return []Model{
		{ID: apiyiDefaultFastModel, Name: "API易 Seedance 2.0 Fast", Type: "video"},
		{ID: apiyiDefaultStandardModel, Name: "API易 Seedance 2.0 Standard", Type: "video"},
	}
}

func apiyiProbeModels() []ProbeModel {
	models := apiyiModels()
	out := make([]ProbeModel, 0, len(models))
	for _, m := range models {
		out = append(out, ProbeModel{
			ID:    m.ID,
			Name:  m.Name,
			Type:  m.Type,
			Label: videoModelLabel(m),
			Value: m.ID,
		})
	}
	return out
}

func apiyiWanModels() []Model {
	return []Model{
		{ID: apiyiWanDefaultModel, Name: "API易 Wan2.7 参考图生视频", Type: "video"},
		{ID: apiyiWanTextModel, Name: "API易 Wan2.7 文生视频", Type: "video"},
		{ID: apiyiWanImageModel, Name: "API易 Wan2.7 图生视频", Type: "video"},
	}
}

func apiyiHappyHorseModels() []Model {
	return []Model{
		{ID: apiyiHappyHorseDefaultModel, Name: "API易 HappyHorse 参考图生视频", Type: "video"},
		{ID: apiyiHappyHorseTextModel, Name: "API易 HappyHorse 文生视频", Type: "video"},
		{ID: apiyiHappyHorseImageModel, Name: "API易 HappyHorse 图生视频", Type: "video"},
	}
}

func apiYIModelsForChannel(channelType string) []Model {
	switch normalizeChannelType(channelType) {
	case ChannelAPIYIHappyHorse:
		return apiyiHappyHorseModels()
	case ChannelAPIYIWan27:
		return apiyiWanModels()
	default:
		return apiyiModels()
	}
}

func apiYIProbeModelsForChannel(channelType string) []ProbeModel {
	models := apiYIModelsForChannel(channelType)
	out := make([]ProbeModel, 0, len(models))
	for _, m := range models {
		out = append(out, ProbeModel{
			ID:    m.ID,
			Name:  m.Name,
			Type:  m.Type,
			Label: videoModelLabel(m),
			Value: m.ID,
		})
	}
	return out
}

func mapAPIYIStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed":
		return "completed"
	case "succeeded":
		return "completed"
	case "failed", "expired":
		return "failed"
	case "queued":
		return "queued"
	case "running", "in_progress", "processing", "generating":
		return "running"
	default:
		return strings.TrimSpace(status)
	}
}

func mapAPIYIWanStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed", "succeeded":
		return "completed"
	case "failed", "expired", "canceled", "cancelled":
		return "failed"
	case "submitted", "pending", "queued":
		return "queued"
	case "in_progress", "running", "processing", "generating":
		return "running"
	default:
		return strings.TrimSpace(status)
	}
}

func apiyiProgress(status string) int {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed":
		return 100
	case "failed":
		return 100
	case "queued":
		return 5
	case "running":
		return 10
	default:
		return 0
	}
}

func parseAPIYIProgress(progress any, status string) int {
	switch v := progress.(type) {
	case nil:
		return apiyiProgress(status)
	case int:
		return clampProgress(v)
	case int64:
		return clampProgress(int(v))
	case int32:
		return clampProgress(int(v))
	case uint:
		return clampProgress(int(v))
	case uint64:
		if v > 100 {
			return 100
		}
		return int(v)
	case uint32:
		return clampProgress(int(v))
	case float64:
		return clampProgress(int(math.Round(v)))
	case float32:
		return clampProgress(int(math.Round(float64(v))))
	case string:
		s := strings.TrimSpace(strings.TrimSuffix(v, "%"))
		if s == "" {
			return apiyiProgress(status)
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return apiyiProgress(status)
		}
		return clampProgress(int(math.Round(f)))
	case json.Number:
		f, err := v.Float64()
		if err != nil {
			return apiyiProgress(status)
		}
		return clampProgress(int(math.Round(f)))
	default:
		return apiyiProgress(status)
	}
}

func progressKnown(progress any, status string) bool {
	if progress != nil {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed", "failed":
		return true
	default:
		return false
	}
}

func firstAPIYIProgressValue(values ...any) any {
	for _, v := range values {
		switch value := v.(type) {
		case nil:
			continue
		case string:
			if strings.TrimSpace(value) == "" {
				continue
			}
		case json.Number:
			if strings.TrimSpace(value.String()) == "" {
				continue
			}
		}
		return v
	}
	return nil
}

func progressFromRawMap(raw map[string]any) any {
	return progressFromRawMapDepth(raw, 0)
}

func progressFromRawMapDepth(raw map[string]any, depth int) any {
	if len(raw) == 0 || depth > 3 {
		return nil
	}
	for _, key := range []string{"progress", "task_progress", "progress_percent", "percent"} {
		if v, ok := raw[key]; ok {
			if progress := firstAPIYIProgressValue(v); progress != nil {
				return progress
			}
		}
	}
	for _, key := range []string{"data", "output", "result", "task", "body"} {
		child, ok := raw[key].(map[string]any)
		if !ok {
			continue
		}
		if progress := progressFromRawMapDepth(child, depth+1); progress != nil {
			return progress
		}
	}
	return nil
}

func progressLogValue(progress any) string {
	switch v := progress.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case json.Number:
		return v.String()
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(v), 'f', -1, 32)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case int32:
		return strconv.FormatInt(int64(v), 10)
	case uint:
		return strconv.FormatUint(uint64(v), 10)
	case uint64:
		return strconv.FormatUint(v, 10)
	case uint32:
		return strconv.FormatUint(uint64(v), 10)
	default:
		data, _ := json.Marshal(v)
		return strings.TrimSpace(string(data))
	}
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
