package config

import (
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestNormalizeVideoWorkflowConfig(t *testing.T) {
	cfg := VideoWorkflowConfig{}
	normalizeVideoWorkflowConfig(&cfg)

	if cfg.AssetDir != "/app/data/video-workflow-assets" {
		t.Fatalf("unexpected asset dir: %q", cfg.AssetDir)
	}
	if cfg.PublicBaseURL != "" {
		t.Fatalf("unexpected public base URL: %q", cfg.PublicBaseURL)
	}
	if cfg.WorkerConcurrency != 4 || cfg.TextConcurrency != 2 || cfg.ImageConcurrency != 2 || cfg.VideoConcurrency != 2 || cfg.ComposeConcurrency != 1 {
		t.Fatalf("unexpected concurrency defaults: %+v", cfg)
	}
	if cfg.HeartbeatSec != 10 || cfg.LeaseTTLSec != 60 || cfg.RecoveryScanSec != 30 {
		t.Fatalf("unexpected lease defaults: %+v", cfg)
	}
	if cfg.ApprovalTTLSec != 259200 || cfg.PreviewURLTTLSec != 900 || cfg.DownloadURLTTLSec != 300 || cfg.ProviderURLTTLSec != 3600 || cfg.EstimateTTLSec != 300 {
		t.Fatalf("unexpected ttl defaults: %+v", cfg)
	}
	if cfg.UserQuotaBytes != 5368709120 || cfg.ImageMaxBytes != 20971520 || cfg.VideoMaxBytes != 314572800 {
		t.Fatalf("unexpected quota defaults: %+v", cfg)
	}
	if cfg.ImageCredits != 500000 || cfg.TextCredits != 10000 || cfg.VideoCredits != 100000 {
		t.Fatalf("unexpected billing defaults: %+v", cfg)
	}
	if cfg.FFmpegBin != "ffmpeg" || cfg.FFprobeBin != "ffprobe" {
		t.Fatalf("unexpected media tools: %+v", cfg)
	}
}

func TestNormalizeVideoWorkflowConfigPreservesOverrides(t *testing.T) {
	cfg := VideoWorkflowConfig{
		AssetDir:           "/tmp/assets",
		PublicBaseURL:      "https://media.acme.cn/",
		WorkerConcurrency:  8,
		TextConcurrency:    5,
		ImageConcurrency:   3,
		VideoConcurrency:   4,
		ComposeConcurrency: 2,
		HeartbeatSec:       5,
		LeaseTTLSec:        30,
		RecoveryScanSec:    15,
		ApprovalTTLSec:     60,
		PreviewURLTTLSec:   30,
		DownloadURLTTLSec:  20,
		ProviderURLTTLSec:  45,
		EstimateTTLSec:     10,
		UserQuotaBytes:     1000,
		ImageMaxBytes:      100,
		VideoMaxBytes:      500,
		ImageCredits:       11,
		TextCredits:        12,
		VideoCredits:       13,
		FFmpegBin:          "/bin/ffmpeg",
		FFprobeBin:         "/bin/ffprobe",
	}
	normalizeVideoWorkflowConfig(&cfg)

	if cfg.AssetDir != "/tmp/assets" || cfg.PublicBaseURL != "https://media.acme.cn" || cfg.WorkerConcurrency != 8 || cfg.TextConcurrency != 5 || cfg.UserQuotaBytes != 1000 || cfg.ImageCredits != 11 || cfg.TextCredits != 12 || cfg.VideoCredits != 13 || cfg.FFmpegBin != "/bin/ffmpeg" {
		t.Fatalf("overrides were replaced: %+v", cfg)
	}
}

func TestVideoWorkflowEnvironmentOverrides(t *testing.T) {
	t.Setenv("GPT2API_VIDEO_WORKFLOW_TEXT_CONCURRENCY", "4")
	t.Setenv("GPT2API_VIDEO_WORKFLOW_IMAGE_CONCURRENCY", "6")
	t.Setenv("GPT2API_VIDEO_WORKFLOW_ASSET_DIR", "/env/assets")
	t.Setenv("GPT2API_VIDEO_WORKFLOW_SIGNING_SECRET", "env-only-signing-secret-32-bytes!")
	t.Setenv("GPT2API_VIDEO_WORKFLOW_PUBLIC_BASE_URL", "https://media.acme.cn")
	v := viper.New()
	v.SetEnvPrefix("GPT2API")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	setVideoWorkflowDefaults(v)

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.VideoWorkflow.TextConcurrency != 4 || cfg.VideoWorkflow.ImageConcurrency != 6 || cfg.VideoWorkflow.AssetDir != "/env/assets" || cfg.VideoWorkflow.SigningSecret != "env-only-signing-secret-32-bytes!" || cfg.VideoWorkflow.PublicBaseURL != "https://media.acme.cn" {
		t.Fatalf("environment overrides were not decoded: %+v", cfg.VideoWorkflow)
	}
}

func TestValidateVideoWorkflowSigningSecretInProduction(t *testing.T) {
	cfg := validProductionConfig()
	cfg.VideoWorkflow.SigningSecret = ""
	if err := validateVideoWorkflowConfig(cfg); err == nil {
		t.Fatal("expected a production signing-secret error")
	}
	cfg.VideoWorkflow.SigningSecret = "production-video-signature-7f9a2c4e6b8d"
	if err := validateVideoWorkflowConfig(cfg); err != nil {
		t.Fatalf("valid signing secret rejected: %v", err)
	}
	// 开发环境允许由启动装配层派生本地密钥，避免阻断无容器开发。
	cfg.App.Env = "dev"
	cfg.VideoWorkflow.SigningSecret = ""
	cfg.VideoWorkflow.PublicBaseURL = ""
	if err := validateVideoWorkflowConfig(cfg); err != nil {
		t.Fatalf("development config rejected: %v", err)
	}
}

func TestValidateProductionSecurityRejectsDefaultsAndPlaceholders(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{name: "repository jwt", mutate: func(c *Config) { c.JWT.Secret = "CHANGE_ME_TO_RANDOM_32_BYTES_SECRET" }},
		{name: "compose jwt", mutate: func(c *Config) { c.JWT.Secret = "dev_secret_change_me_in_production_please_32b" }},
		{name: "placeholder jwt", mutate: func(c *Config) { c.JWT.Secret = "placeholder-jwt-secret-with-32-characters" }},
		{name: "hyphenated placeholder jwt", mutate: func(c *Config) { c.JWT.Secret = "please-change-me-jwt-secret-with-32-chars" }},
		{name: "repository aes", mutate: func(c *Config) { c.Crypto.AESKey = "CHANGE_ME_TO_64_HEX_CHARS_AES_256_KEY_000000000000000000000000" }},
		{name: "compose aes", mutate: func(c *Config) { c.Crypto.AESKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" }},
		{name: "invalid aes", mutate: func(c *Config) { c.Crypto.AESKey = strings.Repeat("z", 64) }},
		{name: "placeholder video signing", mutate: func(c *Config) { c.VideoWorkflow.SigningSecret = "please_change_video_workflow_signing_secret_32b" }},
		{name: "reused jwt signing secret", mutate: func(c *Config) { c.VideoWorkflow.SigningSecret = c.JWT.Secret }},
		{name: "example public url", mutate: func(c *Config) { c.VideoWorkflow.PublicBaseURL = "https://api.example.com" }},
		{name: "localhost public url", mutate: func(c *Config) { c.VideoWorkflow.PublicBaseURL = "http://localhost:8080" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validProductionConfig()
			tt.mutate(cfg)
			if err := validateVideoWorkflowConfig(cfg); err == nil {
				t.Fatal("unsafe production configuration was accepted")
			}
		})
	}
	if err := validateVideoWorkflowConfig(validProductionConfig()); err != nil {
		t.Fatalf("valid production configuration rejected: %v", err)
	}
}

func TestValidateVideoWorkflowPublicBaseURL(t *testing.T) {
	for _, value := range []string{"", "localhost:8080", "http://localhost:8080", "https://api.localhost", "http://127.0.0.1", "ftp://media.acme.cn", "https://user:pass@media.acme.cn", "https://media.acme.cn?sig=secret", "https://media.acme.cn/api", "https://example.com", "https://api.example.com"} {
		if err := validatePublicBaseURL(value); err == nil {
			t.Fatalf("invalid public base URL accepted: %q", value)
		}
	}
	for _, value := range []string{"https://media.acme.cn", "http://8.8.8.8:8080"} {
		if err := validatePublicBaseURL(value); err != nil {
			t.Fatalf("public base URL %q rejected: %v", value, err)
		}
	}
}

func validProductionConfig() *Config {
	return &Config{
		App:    AppConfig{Env: "prod"},
		JWT:    JWTConfig{Secret: "production-jwt-secret-7f9a2c4e6b8d0f1a"},
		Crypto: CryptoConfig{AESKey: strings.Repeat("ab", 32)},
		VideoWorkflow: VideoWorkflowConfig{
			Enabled: true, SigningSecret: "production-video-signature-7f9a2c4e6b8d",
			PublicBaseURL: "https://media.acme.cn",
		},
	}
}
