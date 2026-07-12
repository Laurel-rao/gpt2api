package config

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"

	"github.com/spf13/viper"
)

type Config struct {
	App           AppConfig           `mapstructure:"app"`
	Log           LogConfig           `mapstructure:"log"`
	MySQL         MySQLConfig         `mapstructure:"mysql"`
	Redis         RedisConfig         `mapstructure:"redis"`
	JWT           JWTConfig           `mapstructure:"jwt"`
	Crypto        CryptoConfig        `mapstructure:"crypto"`
	Security      SecurityConfig      `mapstructure:"security"`
	Scheduler     SchedulerConfig     `mapstructure:"scheduler"`
	Upstream      UpstreamConfig      `mapstructure:"upstream"`
	ImageGen      ImageGenConfig      `mapstructure:"imagegen"`
	TextGen       TextGenConfig       `mapstructure:"textgen"`
	VideoGen      VideoGenConfig      `mapstructure:"videogen"`
	VideoWorkflow VideoWorkflowConfig `mapstructure:"video_workflow"`
	Ecommerce     EcommerceConfig     `mapstructure:"ecommerce"`
	EPay          EPayConfig          `mapstructure:"epay"`
	Backup        BackupConfig        `mapstructure:"backup"`
	SMTP          SMTPConfig          `mapstructure:"smtp"`
}

type AppConfig struct {
	Name    string `mapstructure:"name"`
	Env     string `mapstructure:"env"`
	Listen  string `mapstructure:"listen"`
	BaseURL string `mapstructure:"base_url"`
}

type LogConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
	Output string `mapstructure:"output"`
}

type MySQLConfig struct {
	DSN                string `mapstructure:"dsn"`
	MaxOpenConns       int    `mapstructure:"max_open_conns"`
	MaxIdleConns       int    `mapstructure:"max_idle_conns"`
	ConnMaxLifetimeSec int    `mapstructure:"conn_max_lifetime_sec"`
}

type RedisConfig struct {
	Addr     string `mapstructure:"addr"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
	PoolSize int    `mapstructure:"pool_size"`
}

type JWTConfig struct {
	Secret        string `mapstructure:"secret"`
	AccessTTLSec  int    `mapstructure:"access_ttl_sec"`
	RefreshTTLSec int    `mapstructure:"refresh_ttl_sec"`
	Issuer        string `mapstructure:"issuer"`
}

type CryptoConfig struct {
	AESKey string `mapstructure:"aes_key"`
}

type SecurityConfig struct {
	BcryptCost  int      `mapstructure:"bcrypt_cost"`
	CORSOrigins []string `mapstructure:"cors_origins"`
}

type SchedulerConfig struct {
	MinIntervalSec     int     `mapstructure:"min_interval_sec"`
	DailyUsageRatio    float64 `mapstructure:"daily_usage_ratio"`
	LockTTLSec         int     `mapstructure:"lock_ttl_sec"`
	AccountConcurrency int     `mapstructure:"account_concurrency"`
	Cooldown429Sec     int     `mapstructure:"cooldown_429_sec"`
	WarnedPauseHours   int     `mapstructure:"warned_pause_hours"`
}

type UpstreamConfig struct {
	BaseURL           string `mapstructure:"base_url"`
	RequestTimeoutSec int    `mapstructure:"request_timeout_sec"`
	SSEReadTimeoutSec int    `mapstructure:"sse_read_timeout_sec"`
}

type ImageGenConfig struct {
	BaseURL        string `mapstructure:"base_url"`
	APIKey         string `mapstructure:"api_key"`
	APIKeyEnv      string `mapstructure:"api_key_env"`
	TimeoutSec     int    `mapstructure:"timeout_sec"`
	Quality        string `mapstructure:"quality"`
	Background     string `mapstructure:"background"`
	OutputFormat   string `mapstructure:"output_format"`
	ResponseFormat string `mapstructure:"response_format"`
}

type TextGenConfig struct {
	BaseURL    string `mapstructure:"base_url"`
	APIKey     string `mapstructure:"api_key"`
	APIKeyEnv  string `mapstructure:"api_key_env"`
	Model      string `mapstructure:"model"`
	TimeoutSec int    `mapstructure:"timeout_sec"`
}

type VideoGenConfig struct {
	ChannelType string `mapstructure:"channel_type"`
	BaseURL     string `mapstructure:"base_url"`
	APIKey      string `mapstructure:"api_key"`
	APIKeyEnv   string `mapstructure:"api_key_env"`
	Model       string `mapstructure:"model"`
	APIYI       struct {
		BaseURL string `mapstructure:"base_url"`
		APIKey  string `mapstructure:"api_key"`
		Model   string `mapstructure:"model"`
	} `mapstructure:"apiyi_seedance2"`
	APIYIWan27 struct {
		BaseURL string `mapstructure:"base_url"`
		APIKey  string `mapstructure:"api_key"`
		Model   string `mapstructure:"model"`
	} `mapstructure:"apiyi_wan27"`
	APIYIHappyHorse struct {
		BaseURL string `mapstructure:"base_url"`
		APIKey  string `mapstructure:"api_key"`
		Model   string `mapstructure:"model"`
	} `mapstructure:"apiyi_happyhorse"`
	TimeoutSec    int    `mapstructure:"timeout_sec"`
	DurationSec   int    `mapstructure:"duration_sec"`
	AspectRatio   string `mapstructure:"aspect_ratio"`
	Resolution    string `mapstructure:"resolution"`
	GenerateAudio bool   `mapstructure:"generate_audio"`
}

// VideoWorkflowConfig 控制视频画布素材、签名链接和持久化执行器。
type VideoWorkflowConfig struct {
	Enabled            bool   `mapstructure:"enabled"`
	AcceptNewRuns      bool   `mapstructure:"accept_new_runs"`
	AssetDir           string `mapstructure:"asset_dir"`
	SigningSecret      string `mapstructure:"signing_secret"`
	PublicBaseURL      string `mapstructure:"public_base_url"`
	WorkerConcurrency  int    `mapstructure:"worker_concurrency"`
	ImageConcurrency   int    `mapstructure:"image_concurrency"`
	VideoConcurrency   int    `mapstructure:"video_concurrency"`
	ComposeConcurrency int    `mapstructure:"compose_concurrency"`
	HeartbeatSec       int    `mapstructure:"heartbeat_sec"`
	LeaseTTLSec        int    `mapstructure:"lease_ttl_sec"`
	RecoveryScanSec    int    `mapstructure:"recovery_scan_sec"`
	ApprovalTTLSec     int    `mapstructure:"approval_ttl_sec"`
	PreviewURLTTLSec   int    `mapstructure:"preview_url_ttl_sec"`
	DownloadURLTTLSec  int    `mapstructure:"download_url_ttl_sec"`
	ProviderURLTTLSec  int    `mapstructure:"provider_url_ttl_sec"`
	EstimateTTLSec     int    `mapstructure:"estimate_ttl_sec"`
	UserQuotaBytes     int64  `mapstructure:"user_quota_bytes"`
	ImageMaxBytes      int64  `mapstructure:"image_max_bytes"`
	VideoMaxBytes      int64  `mapstructure:"video_max_bytes"`
	ImageCredits       int64  `mapstructure:"image_credit_per_output"`
	TextCredits        int64  `mapstructure:"text_credit_per_node"`
	VideoCredits       int64  `mapstructure:"video_credit_per_clip"`
	FFmpegBin          string `mapstructure:"ffmpeg_bin"`
	FFprobeBin         string `mapstructure:"ffprobe_bin"`
}

type EcommerceConfig struct {
	ImageConcurrency int `mapstructure:"image_concurrency"`
}

// BackupConfig 数据库备份配置。
type BackupConfig struct {
	Dir          string `mapstructure:"dir"`           // 备份落盘目录,默认 /app/data/backups
	Retention    int    `mapstructure:"retention"`     // 保留最近 N 个(>0),0 表示不自动清理
	MysqldumpBin string `mapstructure:"mysqldump_bin"` // 默认 mysqldump
	MysqlBin     string `mapstructure:"mysql_bin"`     // 恢复用,默认 mysql
	MaxUploadMB  int    `mapstructure:"max_upload_mb"` // 上传 .sql.gz 上限,默认 512
	AllowRestore bool   `mapstructure:"allow_restore"` // 是否允许 /restore 端点(生产强烈建议 false 手动切)
}

type EPayConfig struct {
	// GatewayURL 形如 https://pay.example.com/submit.php
	// 空字符串时整个充值通道被视为未启用,前端 list 会提示运维未配置。
	GatewayURL string `mapstructure:"gateway_url"`
	PID        string `mapstructure:"pid"`
	Key        string `mapstructure:"key"`
	// NotifyURL 后端异步回调(必填完整 https,不要带 query)
	NotifyURL string `mapstructure:"notify_url"`
	// ReturnURL 支付成功浏览器跳回(前端路由页,如 /billing)
	ReturnURL string `mapstructure:"return_url"`
	// SignType 目前只支持 MD5,保留扩展位。
	SignType string `mapstructure:"sign_type"`
	// Expires 订单默认有效期(分钟),0 取默认 30
	ExpiresMin int `mapstructure:"expires_min"`
}

// SMTPConfig 用于注册欢迎 / 充值到账 邮件通知。
// Host 为空时邮件通道整体关闭,不影响主流程。
type SMTPConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
	From     string `mapstructure:"from"`      // 显示的 From 地址
	FromName string `mapstructure:"from_name"` // 显示名
	UseTLS   bool   `mapstructure:"use_tls"`   // true 隐式 TLS(465),false STARTTLS(587)
}

var (
	global *Config
	once   sync.Once
)

func Load(path string) (*Config, error) {
	var loadErr error
	once.Do(func() {
		v := viper.New()
		v.SetConfigFile(path)
		v.SetEnvPrefix("GPT2API")
		v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
		v.AutomaticEnv()
		setVideoWorkflowDefaults(v)
		if err := v.ReadInConfig(); err != nil {
			loadErr = fmt.Errorf("read config: %w", err)
			return
		}
		var c Config
		if err := v.Unmarshal(&c); err != nil {
			loadErr = fmt.Errorf("unmarshal config: %w", err)
			return
		}
		normalizeVideoWorkflowConfig(&c.VideoWorkflow)
		if err := validateVideoWorkflowConfig(&c); err != nil {
			loadErr = err
			return
		}
		global = &c
	})
	return global, loadErr
}

func validateVideoWorkflowConfig(c *Config) error {
	if c == nil || !strings.EqualFold(strings.TrimSpace(c.App.Env), "prod") {
		return nil
	}
	jwtSecret := strings.TrimSpace(c.JWT.Secret)
	if len([]byte(jwtSecret)) < 32 || isProductionPlaceholder(jwtSecret) {
		return errors.New("jwt.secret must contain at least 32 bytes and must not use a placeholder in prod")
	}
	aesKey := strings.TrimSpace(c.Crypto.AESKey)
	decodedAESKey, err := hex.DecodeString(aesKey)
	if err != nil || len(decodedAESKey) != 32 || isProductionPlaceholder(aesKey) || strings.EqualFold(aesKey, "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef") {
		return errors.New("crypto.aes_key must be a non-placeholder 64-character hexadecimal AES-256 key in prod")
	}
	if !c.VideoWorkflow.Enabled {
		return nil
	}
	signingSecret := strings.TrimSpace(c.VideoWorkflow.SigningSecret)
	if len([]byte(signingSecret)) < 32 || isProductionPlaceholder(signingSecret) {
		return errors.New("video_workflow.signing_secret must contain at least 32 bytes and must not use a placeholder in prod")
	}
	if signingSecret == jwtSecret {
		return errors.New("video_workflow.signing_secret must not reuse jwt.secret in prod")
	}
	if err := validatePublicBaseURL(c.VideoWorkflow.PublicBaseURL); err != nil {
		return err
	}
	return nil
}

func isProductionPlaceholder(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return true
	}
	normalized := strings.NewReplacer("-", "_", " ", "_").Replace(value)
	for _, marker := range []string{
		"change_me", "changeme", "please_change", "placeholder", "replace_me",
		"your_secret", "test_secret",
	} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func validatePublicBaseURL(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("video_workflow.public_base_url is required in prod")
	}
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.EscapedPath() != "" && parsed.EscapedPath() != "/") {
		return errors.New("video_workflow.public_base_url must be a public http(s) origin without credentials, query or fragment")
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return errors.New("video_workflow.public_base_url must not use localhost")
	}
	if host == "example.com" || strings.HasSuffix(host, ".example.com") {
		return errors.New("video_workflow.public_base_url must not use example.com")
	}
	if ip := net.ParseIP(host); ip != nil && (!ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast()) {
		return errors.New("video_workflow.public_base_url must use a public host")
	}
	return nil
}

func setVideoWorkflowDefaults(v *viper.Viper) {
	v.SetDefault("video_workflow.enabled", true)
	v.SetDefault("video_workflow.accept_new_runs", true)
	v.SetDefault("video_workflow.asset_dir", "/app/data/video-workflow-assets")
	v.SetDefault("video_workflow.signing_secret", "")
	_ = v.BindEnv("video_workflow.signing_secret")
	v.SetDefault("video_workflow.public_base_url", "")
	_ = v.BindEnv("video_workflow.public_base_url")
	v.SetDefault("video_workflow.worker_concurrency", 4)
	v.SetDefault("video_workflow.image_concurrency", 2)
	v.SetDefault("video_workflow.video_concurrency", 2)
	v.SetDefault("video_workflow.compose_concurrency", 1)
	v.SetDefault("video_workflow.heartbeat_sec", 10)
	v.SetDefault("video_workflow.lease_ttl_sec", 60)
	v.SetDefault("video_workflow.recovery_scan_sec", 30)
	v.SetDefault("video_workflow.approval_ttl_sec", 72*60*60)
	v.SetDefault("video_workflow.preview_url_ttl_sec", 15*60)
	v.SetDefault("video_workflow.download_url_ttl_sec", 5*60)
	v.SetDefault("video_workflow.provider_url_ttl_sec", 60*60)
	v.SetDefault("video_workflow.estimate_ttl_sec", 5*60)
	v.SetDefault("video_workflow.user_quota_bytes", int64(5*1024*1024*1024))
	v.SetDefault("video_workflow.image_max_bytes", int64(20*1024*1024))
	v.SetDefault("video_workflow.video_max_bytes", int64(300*1024*1024))
	v.SetDefault("video_workflow.image_credit_per_output", int64(500000))
	v.SetDefault("video_workflow.text_credit_per_node", int64(10000))
	v.SetDefault("video_workflow.video_credit_per_clip", int64(100000))
	v.SetDefault("video_workflow.ffmpeg_bin", "ffmpeg")
	v.SetDefault("video_workflow.ffprobe_bin", "ffprobe")
}

func normalizeVideoWorkflowConfig(c *VideoWorkflowConfig) {
	c.PublicBaseURL = strings.TrimRight(strings.TrimSpace(c.PublicBaseURL), "/")
	if c.AssetDir == "" {
		c.AssetDir = "/app/data/video-workflow-assets"
	}
	if c.WorkerConcurrency <= 0 {
		c.WorkerConcurrency = 4
	}
	if c.ImageConcurrency <= 0 {
		c.ImageConcurrency = 2
	}
	if c.VideoConcurrency <= 0 {
		c.VideoConcurrency = 2
	}
	if c.ComposeConcurrency <= 0 {
		c.ComposeConcurrency = 1
	}
	if c.HeartbeatSec <= 0 {
		c.HeartbeatSec = 10
	}
	if c.LeaseTTLSec <= 0 {
		c.LeaseTTLSec = 60
	}
	if c.RecoveryScanSec <= 0 {
		c.RecoveryScanSec = 30
	}
	if c.ApprovalTTLSec <= 0 {
		c.ApprovalTTLSec = 72 * 60 * 60
	}
	if c.PreviewURLTTLSec <= 0 {
		c.PreviewURLTTLSec = 15 * 60
	}
	if c.DownloadURLTTLSec <= 0 {
		c.DownloadURLTTLSec = 5 * 60
	}
	if c.ProviderURLTTLSec <= 0 {
		c.ProviderURLTTLSec = 60 * 60
	}
	if c.EstimateTTLSec <= 0 {
		c.EstimateTTLSec = 5 * 60
	}
	if c.UserQuotaBytes <= 0 {
		c.UserQuotaBytes = 5 * 1024 * 1024 * 1024
	}
	if c.ImageMaxBytes <= 0 {
		c.ImageMaxBytes = 20 * 1024 * 1024
	}
	if c.VideoMaxBytes <= 0 {
		c.VideoMaxBytes = 300 * 1024 * 1024
	}
	if c.ImageCredits <= 0 {
		c.ImageCredits = 500000
	}
	if c.TextCredits <= 0 {
		c.TextCredits = 10000
	}
	if c.VideoCredits <= 0 {
		c.VideoCredits = 100000
	}
	if c.FFmpegBin == "" {
		c.FFmpegBin = "ffmpeg"
	}
	if c.FFprobeBin == "" {
		c.FFprobeBin = "ffprobe"
	}
}

// Get 返回全局配置,仅在 Load 之后调用。
func Get() *Config {
	if global == nil {
		panic("config not loaded; call config.Load first")
	}
	return global
}
