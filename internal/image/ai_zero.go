package image

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/432539/gpt2api/pkg/logger"
)

const defaultAIZeroBaseURL = "http://123.207.53.152/v1"

// AIZeroConfig 是 AI Zero Token/OpenAI-compatible 生图网关配置。
type AIZeroConfig struct {
	BaseURL        string
	APIKey         string
	APIKeyEnv      string
	TimeoutSec     int
	Quality        string
	Background     string
	OutputFormat   string
	ResponseFormat string
}

type AIZeroConfigProvider interface {
	ImageGenEnabled() bool
	ImageGenAPIKey() string
	ImageGenBaseURL() string
	ImageGenQuality() string
	ImageGenBackground() string
	ImageGenOutputFormat() string
	ImageGenResponseFormat() string
	ImageGenTimeoutSec() int
}

// AIZeroClient 调用 AI Zero Token/OpenAI-compatible 图片接口。
type AIZeroClient struct {
	baseURL        string
	apiKey         string
	apiKeyEnv      string
	quality        string
	background     string
	outputFormat   string
	responseFormat string
	httpClient     *http.Client
	provider       AIZeroConfigProvider
}

func NewAIZeroClient(cfg AIZeroConfig) *AIZeroClient {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultAIZeroBaseURL
	}
	apiKeyEnv := strings.TrimSpace(cfg.APIKeyEnv)
	if apiKeyEnv == "" {
		apiKeyEnv = "AZT_API_KEY"
	}
	timeout := time.Duration(cfg.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 420 * time.Second
	}
	return &AIZeroClient{
		baseURL:        baseURL,
		apiKey:         strings.TrimSpace(cfg.APIKey),
		apiKeyEnv:      apiKeyEnv,
		quality:        defaultString(cfg.Quality, "low"),
		background:     defaultString(cfg.Background, "auto"),
		outputFormat:   defaultString(cfg.OutputFormat, "png"),
		responseFormat: normalizeAIZeroResponseFormat(cfg.ResponseFormat),
		httpClient:     &http.Client{Timeout: timeout},
	}
}

func (c *AIZeroClient) SetConfigProvider(p AIZeroConfigProvider) {
	c.provider = p
}

func (c *AIZeroClient) Enabled() bool {
	return c != nil && strings.TrimSpace(c.key()) != ""
}

func (c *AIZeroClient) key() string {
	if c == nil {
		return ""
	}
	if c.apiKey != "" {
		return c.apiKey
	}
	return strings.TrimSpace(os.Getenv(c.apiKeyEnv))
}

func (c *AIZeroClient) runtimeConfig() AIZeroConfig {
	cfg := AIZeroConfig{
		BaseURL:        c.baseURL,
		APIKey:         c.key(),
		APIKeyEnv:      c.apiKeyEnv,
		Quality:        c.quality,
		Background:     c.background,
		OutputFormat:   c.outputFormat,
		ResponseFormat: c.responseFormat,
	}
	if c.httpClient != nil && c.httpClient.Timeout > 0 {
		cfg.TimeoutSec = int(c.httpClient.Timeout / time.Second)
	}
	if c.provider != nil {
		if !c.provider.ImageGenEnabled() {
			cfg.APIKey = ""
			return cfg
		}
		cfg.APIKey = c.provider.ImageGenAPIKey()
		cfg.BaseURL = c.provider.ImageGenBaseURL()
		cfg.Quality = c.provider.ImageGenQuality()
		cfg.Background = c.provider.ImageGenBackground()
		cfg.OutputFormat = c.provider.ImageGenOutputFormat()
		cfg.ResponseFormat = c.provider.ImageGenResponseFormat()
		cfg.TimeoutSec = c.provider.ImageGenTimeoutSec()
	}
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultAIZeroBaseURL
	}
	cfg.Quality = defaultString(cfg.Quality, "low")
	cfg.Background = defaultString(cfg.Background, "auto")
	cfg.OutputFormat = defaultString(cfg.OutputFormat, "png")
	cfg.ResponseFormat = normalizeAIZeroResponseFormat(cfg.ResponseFormat)
	if cfg.TimeoutSec <= 0 {
		cfg.TimeoutSec = 420
	}
	return cfg
}

type aiZeroImageData struct {
	URL           string `json:"url"`
	B64JSON       string `json:"b64_json"`
	RevisedPrompt string `json:"revised_prompt"`
}

type aiZeroImageResp struct {
	Created    int64             `json:"created"`
	Data       []aiZeroImageData `json:"data"`
	ID         string            `json:"id"`
	TaskID     string            `json:"task_id"`
	HistoryURL string            `json:"history_url"`
}

func (r *Runner) runAIZero(ctx context.Context, opt RunOptions, start time.Time) *RunResult {
	result := &RunResult{Status: StatusFailed, ErrorCode: ErrUnknown}

	if r.dao != nil && opt.TaskID != "" {
		_ = r.dao.MarkRunning(ctx, opt.TaskID, 0)
	}

	attemptCtx, cancel := context.WithTimeout(ctx, opt.PerAttemptTimeout)
	defer cancel()

	resp, err := r.aiZero.generate(attemptCtx, opt)
	if err != nil {
		result.ErrorCode = classifyAIZeroErr(err)
		result.ErrorMessage = err.Error()
		result.DurationMs = time.Since(start).Milliseconds()
		if r.dao != nil && opt.TaskID != "" {
			_ = r.dao.MarkFailed(ctx, opt.TaskID, result.ErrorCode)
		}
		return result
	}

	cfg := r.aiZero.runtimeConfig()
	urls, contentTypes, err := normalizeAIZeroImages(resp, cfg.OutputFormat)
	if err != nil {
		result.ErrorCode = ErrInvalidResponse
		result.ErrorMessage = err.Error()
		result.DurationMs = time.Since(start).Milliseconds()
		if r.dao != nil && opt.TaskID != "" {
			_ = r.dao.MarkFailed(ctx, opt.TaskID, result.ErrorCode)
		}
		return result
	}
	if opt.N > 0 && len(urls) > opt.N {
		urls = urls[:opt.N]
		contentTypes = contentTypes[:opt.N]
	}

	fileIDs := make([]string, 0, len(urls))
	for i := range urls {
		fileIDs = append(fileIDs, fmt.Sprintf("azt:%d", i))
	}

	if opt.ReturnImageBytes {
		for i, u := range urls {
			body, ct, err := fetchImageURL(attemptCtx, r.aiZero.httpClient, u, 32*1024*1024)
			if err != nil {
				result.ErrorCode = ErrDownload
				result.ErrorMessage = err.Error()
				result.DurationMs = time.Since(start).Milliseconds()
				if r.dao != nil && opt.TaskID != "" {
					_ = r.dao.MarkFailed(ctx, opt.TaskID, result.ErrorCode)
				}
				return result
			}
			result.ImageBytes = append(result.ImageBytes, body)
			if ct != "" {
				contentTypes[i] = ct
			}
		}
	}

	result.Status = StatusSuccess
	result.ConversationID = firstNonEmpty(resp.TaskID, resp.ID, resp.HistoryURL)
	result.FileIDs = fileIDs
	result.SignedURLs = urls
	result.ContentTypes = contentTypes
	result.Attempts = 1
	result.DurationMs = time.Since(start).Milliseconds()

	if r.dao != nil && opt.TaskID != "" {
		_ = r.dao.MarkSuccess(ctx, opt.TaskID, result.ConversationID, fileIDs, urls, 0)
	}

	logger.L().Info("ai-zero image runner done",
		zap.String("task_id", opt.TaskID),
		zap.Int("count", len(urls)),
		zap.String("upstream_task", result.ConversationID),
	)
	return result
}

func (c *AIZeroClient) generate(ctx context.Context, opt RunOptions) (*aiZeroImageResp, error) {
	if c == nil {
		return nil, errors.New("ai-zero client not configured")
	}
	cfg := c.runtimeConfig()
	key := strings.TrimSpace(cfg.APIKey)
	if key == "" {
		return nil, errors.New("ai-zero imagegen is disabled or api key is empty")
	}
	client := c.httpClient
	if cfg.TimeoutSec > 0 {
		client = &http.Client{Timeout: time.Duration(cfg.TimeoutSec) * time.Second}
	}
	model := strings.TrimSpace(opt.UpstreamModel)
	model = normalizeAIZeroModel(model)
	n := opt.N
	if n <= 0 {
		n = 1
	}
	size := normalizeAIZeroSize(opt.Size)
	if size == "" {
		size = "1024x1024"
	}

	payload := map[string]any{
		"model":           model,
		"prompt":          strings.TrimSpace(opt.Prompt),
		"n":               n,
		"size":            size,
		"quality":         defaultString(opt.Quality, cfg.Quality),
		"background":      defaultString(opt.Background, cfg.Background),
		"output_format":   defaultString(opt.OutputFormat, cfg.OutputFormat),
		"response_format": normalizeAIZeroResponseFormat(firstNonEmpty(opt.ResponseFormat, cfg.ResponseFormat)),
	}

	path := "/images/generations"
	if len(opt.References) > 0 {
		path = "/images/edits"
		refs := make([]string, 0, len(opt.References))
		for _, ref := range opt.References {
			dataURL, err := referenceToDataURL(ref)
			if err != nil {
				return nil, err
			}
			refs = append(refs, dataURL)
		}
		payload["images"] = refs
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)

	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ai-zero image request: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		data, _ := io.ReadAll(io.LimitReader(res.Body, 16*1024))
		logger.L().Warn("ai-zero image upstream error",
			zap.Int("status", res.StatusCode),
			zap.String("path", path),
			zap.String("model", model),
			zap.String("size", size),
			zap.String("body", strings.TrimSpace(string(data))),
		)
		return nil, fmt.Errorf("ai-zero upstream %d: %s", res.StatusCode, strings.TrimSpace(string(data)))
	}

	var out aiZeroImageResp
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("ai-zero image decode: %w", err)
	}
	return &out, nil
}

func (c *AIZeroClient) Probe(ctx context.Context) (*RunResult, error) {
	start := time.Now()
	res, err := c.generate(ctx, RunOptions{
		UpstreamModel:  "gpt-image-2",
		Prompt:         "simple connectivity test image, a small blue dot on white background",
		N:              1,
		Size:           "1024x1024",
		Quality:        "low",
		ResponseFormat: "b64_json",
	})
	if err != nil {
		return nil, err
	}
	urls, cts, err := normalizeAIZeroImages(res, c.runtimeConfig().OutputFormat)
	if err != nil {
		return nil, err
	}
	return &RunResult{
		Status:         StatusSuccess,
		ConversationID: firstNonEmpty(res.TaskID, res.ID, res.HistoryURL),
		SignedURLs:     urls,
		ContentTypes:   cts,
		Attempts:       1,
		DurationMs:     time.Since(start).Milliseconds(),
	}, nil
}

func normalizeAIZeroResponseFormat(v string) string {
	if strings.EqualFold(strings.TrimSpace(v), "b64_json") {
		return "b64_json"
	}
	return "b64_json"
}

func normalizeAIZeroModel(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "gpt-image-1", "gpt-image-1-mini", "gpt-image-1.5", "gpt-image-2":
		return strings.ToLower(strings.TrimSpace(v))
	default:
		return "gpt-image-2"
	}
}

func normalizeAIZeroImages(resp *aiZeroImageResp, outputFormat string) ([]string, []string, error) {
	if resp == nil || len(resp.Data) == 0 {
		return nil, nil, errors.New("ai-zero empty image response")
	}
	urls := make([]string, 0, len(resp.Data))
	cts := make([]string, 0, len(resp.Data))
	for _, item := range resp.Data {
		if strings.TrimSpace(item.URL) != "" {
			urls = append(urls, strings.TrimSpace(item.URL))
			cts = append(cts, contentTypeForFormat(outputFormat))
			continue
		}
		if strings.TrimSpace(item.B64JSON) != "" {
			ct := contentTypeForFormat(outputFormat)
			urls = append(urls, "data:"+ct+";base64,"+strings.TrimSpace(item.B64JSON))
			cts = append(cts, ct)
		}
	}
	if len(urls) == 0 {
		return nil, nil, errors.New("ai-zero response has no url or b64_json")
	}
	return urls, cts, nil
}

func referenceToDataURL(ref ReferenceImage) (string, error) {
	if len(ref.Data) == 0 {
		return "", errors.New("empty reference image")
	}
	ct := http.DetectContentType(ref.Data)
	if !strings.HasPrefix(ct, "image/") {
		ct = contentTypeForFile(ref.FileName)
	}
	if ct == "" || !strings.HasPrefix(ct, "image/") {
		ct = "image/png"
	}
	return "data:" + ct + ";base64," + base64.StdEncoding.EncodeToString(ref.Data), nil
}

func fetchImageURL(ctx context.Context, client *http.Client, u string, limit int64) ([]byte, string, error) {
	if strings.HasPrefix(strings.ToLower(u), "data:") {
		return decodeDataURL(u)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, "", err
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		return nil, "", fmt.Errorf("fetch image %d", res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(body)) > limit {
		return nil, "", fmt.Errorf("image exceeds %d bytes", limit)
	}
	return body, res.Header.Get("Content-Type"), nil
}

func decodeDataURL(s string) ([]byte, string, error) {
	comma := strings.IndexByte(s, ',')
	if comma < 0 {
		return nil, "", errors.New("invalid data url")
	}
	meta := s[5:comma]
	raw := s[comma+1:]
	ct := strings.Split(meta, ";")[0]
	data, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, "", err
	}
	return data, ct, nil
}

func classifyAIZeroErr(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "401"), strings.Contains(msg, "403"):
		return ErrAuthRequired
	case strings.Contains(msg, "429"):
		return ErrRateLimited
	case strings.Contains(msg, "deadline exceeded"), strings.Contains(msg, "timeout"):
		return ErrPollTimeout
	case strings.Contains(msg, "empty image response"), strings.Contains(msg, "no url"):
		return ErrInvalidResponse
	default:
		return ErrUpstream
	}
}

func normalizeAIZeroSize(size string) string {
	size = strings.ToLower(strings.TrimSpace(size))
	size = strings.ReplaceAll(size, "*", "x")
	size = strings.ReplaceAll(size, "×", "x")
	switch size {
	case "1024x1024", "1536x1024", "1024x1536":
		return size
	case "1792x1024", "1365x1024", "1280x1024":
		return "1536x1024"
	case "1024x1792", "1024x1365", "1024x1280":
		return "1024x1536"
	default:
		return size
	}
}

func contentTypeForFormat(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "jpeg", "jpg":
		return "image/jpeg"
	case "webp":
		return "image/webp"
	default:
		return "image/png"
	}
}

func contentTypeForFile(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	default:
		return ""
	}
}

func defaultString(s, fallback string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return fallback
	}
	return s
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
