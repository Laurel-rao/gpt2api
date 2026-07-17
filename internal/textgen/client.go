// Package textgen calls an AI Zero Token/OpenAI-compatible text gateway.
package textgen

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/432539/gpt2api/internal/upstream/adapter"
	"github.com/432539/gpt2api/internal/upstream/chatgpt"
)

const defaultBaseURL = "http://ai.reeko.net.cn/v1"
const defaultModel = "gpt-5.4"

type Config struct {
	BaseURL    string
	APIKey     string
	APIKeyEnv  string
	Model      string
	TimeoutSec int
}

type ConfigProvider interface {
	TextGenEnabled() bool
	TextGenAPIKey() string
	TextGenBaseURL() string
	TextGenModel() string
	TextGenTimeoutSec() int
}

type Client struct {
	baseURL    string
	apiKey     string
	apiKeyEnv  string
	model      string
	timeoutSec int
	provider   ConfigProvider
}

type Options struct {
	Model       string
	Messages    []chatgpt.ChatMessage
	Stream      bool
	Temperature float64
	TopP        float64
	MaxTokens   int
}

func NewClient(cfg Config) *Client {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	apiKeyEnv := strings.TrimSpace(cfg.APIKeyEnv)
	if apiKeyEnv == "" {
		apiKeyEnv = "AZT_API_KEY"
	}
	timeout := cfg.TimeoutSec
	if timeout <= 0 {
		timeout = 120
	}
	return &Client{
		baseURL:    baseURL,
		apiKey:     strings.TrimSpace(cfg.APIKey),
		apiKeyEnv:  apiKeyEnv,
		model:      normalizeModel(cfg.Model, defaultModel),
		timeoutSec: timeout,
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

func (c *Client) Chat(ctx context.Context, opt Options) (adapter.ChatStream, error) {
	if c == nil {
		return nil, errors.New("textgen client not configured")
	}
	cfg := c.runtimeConfig()
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("textgen is disabled or api key is empty")
	}
	ad := adapter.NewOpenAI(adapter.Params{
		BaseURL:  cfg.BaseURL,
		APIKey:   cfg.APIKey,
		TimeoutS: cfg.TimeoutSec,
	})
	model := normalizeModel(opt.Model, cfg.Model)
	return ad.Chat(ctx, model, &adapter.ChatRequest{
		Model:       model,
		Messages:    opt.Messages,
		Stream:      opt.Stream,
		Temperature: opt.Temperature,
		TopP:        opt.TopP,
		MaxTokens:   opt.MaxTokens,
	})
}

func (c *Client) Probe(ctx context.Context) (durationMs int64, content string, err error) {
	start := time.Now()
	stream, err := c.Chat(ctx, Options{
		Messages:  []chatgpt.ChatMessage{{Role: "user", Content: "只回复 OK"}},
		MaxTokens: 20,
	})
	if err != nil {
		return 0, "", err
	}
	var b strings.Builder
	for ch := range stream {
		if ch.Err != nil {
			return 0, "", ch.Err
		}
		b.WriteString(ch.Delta)
	}
	return time.Since(start).Milliseconds(), strings.TrimSpace(b.String()), nil
}

func (c *Client) runtimeConfig() Config {
	cfg := Config{
		BaseURL:    c.baseURL,
		APIKey:     c.key(),
		APIKeyEnv:  c.apiKeyEnv,
		Model:      c.model,
		TimeoutSec: c.timeoutSec,
	}
	if c.provider != nil {
		if !c.provider.TextGenEnabled() {
			cfg.APIKey = ""
			return cfg
		}
		cfg.BaseURL = c.provider.TextGenBaseURL()
		cfg.APIKey = c.provider.TextGenAPIKey()
		cfg.Model = c.provider.TextGenModel()
		cfg.TimeoutSec = c.provider.TextGenTimeoutSec()
	}
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultBaseURL
	}
	cfg.Model = normalizeModel(cfg.Model, defaultModel)
	if cfg.TimeoutSec <= 0 {
		cfg.TimeoutSec = 120
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

func normalizeModel(v, fallback string) string {
	v = strings.TrimSpace(v)
	switch strings.ToLower(v) {
	case "gpt-5.5", "gpt-5.4", "gpt-5.4-mini", "gpt-5.3-codex-spark":
		return strings.ToLower(v)
	case "gpt-5-5":
		return "gpt-5.5"
	case "gpt-5-4":
		return "gpt-5.4"
	case "gpt-5-4-mini":
		return "gpt-5.4-mini"
	}
	fallback = strings.TrimSpace(fallback)
	if fallback == "" || strings.EqualFold(fallback, v) {
		return defaultModel
	}
	out := normalizeModel(fallback, "")
	if out == "" {
		return defaultModel
	}
	return out
}
