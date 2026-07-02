package account

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"golang.org/x/net/publicsuffix"

	"github.com/432539/gpt2api/internal/upstream/chatgpt"
)

// QuotaSettings 热更新参数。
type QuotaSettings interface {
	AccountQuotaProbeEnabled() bool
	AccountQuotaProbeIntervalSec() int
	AccountRefreshConcurrency() int // 复用刷新并发上限
}

// QuotaResult 探测结果。
type QuotaResult struct {
	AccountID       uint64    `json:"account_id"`
	Email           string    `json:"email"`
	OK              bool      `json:"ok"`
	Remaining       int       `json:"remaining"`
	Total           int       `json:"total"`
	ResetAt         time.Time `json:"reset_at,omitempty"`
	DefaultModel    string    `json:"default_model,omitempty"`    // 如 gpt-5-3
	BlockedFeatures []string  `json:"blocked_features,omitempty"` // 被风控限制的功能列表
	Error           string    `json:"error,omitempty"`
	Debug           string    `json:"debug,omitempty"` // 脱敏诊断信息,供后台手动探测排查
}

// QuotaProber 后台定期探测账号图片剩余额度。
type QuotaProber struct {
	svc      *Service
	settings QuotaSettings
	log      *zap.Logger
	client   *http.Client

	proxyResolver AccountProxyResolver

	kick chan struct{}
}

func NewQuotaProber(svc *Service, settings QuotaSettings, logger *zap.Logger) *QuotaProber {
	return &QuotaProber{
		svc:      svc,
		settings: settings,
		log:      logger,
		// client 仅作为"没代理也没 uTLS transport 构造失败"的退路,一般不会走到
		client: &http.Client{
			Timeout: 20 * time.Second,
		},
		kick: make(chan struct{}, 1),
	}
}

// SetProxyResolver 注入账号代理解析器;未注入则直连。
func (q *QuotaProber) SetProxyResolver(pr AccountProxyResolver) { q.proxyResolver = pr }

// clientFor 返回一个带 uTLS(伪 Chrome ClientHello)的 http.Client。
//
// 历史背景:探测额度的落点是 POST /backend-api/conversation/init,这条路径和
// 生图走的 f/conversation 同一道 Cloudflare 指纹校验。用 Go 默认 net/http 的
// 标准 TLS 指纹发请求,JA3/JA4 立刻对不上 Chrome,被 403 挡住;而生图之所以
// 没事是因为它走的是 chatgpt.NewUTLSTransport(内部 parrot 成 Chrome)。
// 这里统一复用同一套 uTLS transport,保持整个"后台对 chatgpt.com 的触碰"指纹一致。
func (q *QuotaProber) clientFor(ctx context.Context, accountID uint64) *http.Client {
	proxyURL := ""
	if q.proxyResolver != nil {
		proxyURL = q.proxyResolver.ProxyURLForAccount(ctx, accountID)
	}
	tr, err := chatgpt.NewUTLSTransport(proxyURL, 30*time.Second)
	if err != nil {
		q.log.Warn("build utls transport for quota probe failed, fallback std http",
			zap.Uint64("account_id", accountID), zap.Error(err))
		jar, _ := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
		return &http.Client{Timeout: q.client.Timeout, Jar: jar}
	}
	jar, _ := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	return &http.Client{Transport: tr, Timeout: q.client.Timeout, Jar: jar}
}

func (q *QuotaProber) Kick() {
	select {
	case q.kick <- struct{}{}:
	default:
	}
}

// Run 后台循环。
func (q *QuotaProber) Run(ctx context.Context) {
	q.log.Info("account quota prober started")
	defer q.log.Info("account quota prober stopped")

	select {
	case <-ctx.Done():
		return
	case <-time.After(10 * time.Second):
	}

	// 扫描循环固定 60s 一轮。注意"扫描周期"和"账号探测最小间隔"是两件事:
	//   - 扫描周期 = prober goroutine 多久检查一次 DB,有没有候选要打;
	//   - 探测最小间隔 = 同一账号两次探测之间的最短间隔(5h,由 DAO SQL 决定)。
	// 绑定后者会让 5h 场景下每 100 分钟才扫一次 → "额度=0 补探"分支最长延迟 100 分钟,
	// 达不到用户想要的"归零后尽快更新"。固定 60s 扫描 + SQL WHERE 过滤几乎零成本。
	const scanInterval = 60 * time.Second

	for {
		if q.settings.AccountQuotaProbeEnabled() {
			q.scanOnce(ctx)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(scanInterval):
		case <-q.kick:
		}
	}
}

func (q *QuotaProber) scanOnce(ctx context.Context) {
	minInterval := q.settings.AccountQuotaProbeIntervalSec()
	conc := q.settings.AccountRefreshConcurrency()

	rows, err := q.svc.dao.ListNeedProbeQuota(ctx, minInterval, 256)
	if err != nil {
		q.log.Warn("list quota probe candidates failed", zap.Error(err))
		return
	}
	if len(rows) == 0 {
		return
	}

	sem := make(chan struct{}, conc)
	var wg sync.WaitGroup
	for _, a := range rows {
		a := a
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			_, _ = q.ProbeOne(ctx, a)
		}()
	}
	wg.Wait()
}

// ProbeByID 指定账号探测。
func (q *QuotaProber) ProbeByID(ctx context.Context, id uint64) (*QuotaResult, error) {
	a, err := q.svc.dao.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return q.ProbeOne(ctx, a)
}

// ProbeOne 执行一次探测。
// 访问 https://chatgpt.com/backend-api/rate_limits(需要 AT),挑选 image 相关条目汇总。
func (q *QuotaProber) ProbeOne(ctx context.Context, a *Account) (*QuotaResult, error) {
	res := &QuotaResult{AccountID: a.ID, Email: a.Email}
	at, err := q.svc.cipher.DecryptString(a.AuthTokenEnc)
	if err != nil || at == "" {
		res.Error = "AT 解密失败"
		_ = q.svc.dao.ApplyQuotaResult(ctx, a.ID, -1, -1, nil)
		return res, errors.New(res.Error)
	}

	probe, probeErr := q.doProbe(ctx, a, at)
	if probeErr != nil {
		res.Error = friendlyProbeErr(probeErr)
		if pe := asProbeHTTPError(probeErr); pe != nil {
			res.Debug = pe.DebugString()
		}
		_ = q.svc.dao.ApplyQuotaResult(ctx, a.ID, -1, -1, nil)
		return res, probeErr
	}

	var resetPtr *time.Time
	if !probe.resetAt.IsZero() {
		resetPtr = &probe.resetAt
	}
	if err := q.svc.dao.ApplyQuotaResult(ctx, a.ID, probe.remaining, probe.total, resetPtr); err != nil {
		res.Error = "写库失败:" + err.Error()
		return res, err
	}
	res.OK = true
	res.Remaining = probe.remaining
	res.Total = probe.total
	res.ResetAt = probe.resetAt
	res.DefaultModel = probe.defaultModel
	res.BlockedFeatures = probe.blockedFeatures
	return res, nil
}

func (q *QuotaProber) bootstrapBrowserCookies(ctx context.Context, hc *http.Client, a *Account, proxyLabel string) {
	bootCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(bootCtx, http.MethodGet, chatgpt.BaseURL+"/", nil)
	if err != nil {
		return
	}
	req.Header.Set("User-Agent", chatgpt.DefaultUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8,en-GB;q=0.7,en-US;q=0.6")
	req.Header.Set("Sec-Ch-Ua", `"Microsoft Edge";v="143", "Chromium";v="143", "Not A(Brand";v="24"`)
	req.Header.Set("Sec-Ch-Ua-Mobile", "?0")
	req.Header.Set("Sec-Ch-Ua-Platform", `"Windows"`)
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "none")
	req.Header.Set("Sec-Fetch-User", "?1")
	req.Header.Set("Upgrade-Insecure-Requests", "1")

	start := time.Now()
	resp, err := hc.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		q.log.Warn("quota probe bootstrap failed",
			zap.Uint64("account_id", a.ID),
			zap.String("email", a.Email),
			zap.String("proxy", proxyLabel),
			zap.Duration("elapsed", elapsed),
			zap.Error(err),
		)
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 400 {
		q.log.Warn("quota probe bootstrap returned non-2xx",
			zap.Uint64("account_id", a.ID),
			zap.String("email", a.Email),
			zap.Int("http_status", resp.StatusCode),
			zap.String("proxy", proxyLabel),
			zap.Duration("elapsed", elapsed),
			zap.String("content_type", resp.Header.Get("Content-Type")),
			zap.String("server", resp.Header.Get("Server")),
			zap.String("cf_ray", resp.Header.Get("Cf-Ray")),
		)
	}
}

type probeOutcome struct {
	remaining       int
	total           int
	resetAt         time.Time
	defaultModel    string
	blockedFeatures []string
}

type probeHTTPError struct {
	Stage       string
	Method      string
	URL         string
	Status      int
	Body        string
	AccountID   uint64
	Email       string
	Proxy       string
	DeviceID    string
	SessionID   string
	TokenTail   string
	Elapsed     time.Duration
	ContentType string
	Server      string
	CFRay       string
}

func (e *probeHTTPError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("%s http=%d body=%s", e.Stage, e.Status, e.Body)
}

func (e *probeHTTPError) DebugString() string {
	if e == nil {
		return ""
	}
	parts := []string{
		fmt.Sprintf("stage=%s", e.Stage),
		fmt.Sprintf("method=%s", e.Method),
		fmt.Sprintf("url=%s", e.URL),
		fmt.Sprintf("http=%d", e.Status),
		fmt.Sprintf("elapsed=%s", e.Elapsed.Round(time.Millisecond)),
		fmt.Sprintf("proxy=%s", e.Proxy),
		fmt.Sprintf("device_id=%s", maskMiddle(e.DeviceID)),
		fmt.Sprintf("has_session_id=%t", strings.TrimSpace(e.SessionID) != ""),
		fmt.Sprintf("session_id=%s", maskMiddle(e.SessionID)),
		fmt.Sprintf("token_tail=%s", e.TokenTail),
	}
	if e.ContentType != "" {
		parts = append(parts, "content_type="+e.ContentType)
	}
	if e.Server != "" {
		parts = append(parts, "server="+e.Server)
	}
	if e.CFRay != "" {
		parts = append(parts, "cf_ray="+e.CFRay)
	}
	if e.Body != "" {
		parts = append(parts, "body="+e.Body)
	}
	return strings.Join(parts, " | ")
}

func asProbeHTTPError(err error) *probeHTTPError {
	var pe *probeHTTPError
	if errors.As(err, &pe) {
		return pe
	}
	return nil
}

// doProbe 调 /backend-api/conversation/init。
//
// 这是 ChatGPT 网页左下角「今日还剩 XX 张图」的数据源,官方不会把这次调用计入额度消耗,
// 适合用于后台定时探测。
//
// 请求 body 参照抓包样例;响应关心的字段是:
//   - limits_progress[].feature_name == "image_gen" → remaining / reset_after
//   - default_model_slug  → 账号默认模型
//   - blocked_features    → 被风控限制的功能;非空需要关注
//
// 指纹注意事项(曾在这里踩过 403):
//   - TLS ClientHello 必须是 Chrome parrot(uTLS),由 q.clientFor 返回的 transport 提供;
//   - HTTP 头部必须对齐 chatgpt.Client.commonHeaders(全套 sec-ch-ua-* / sec-fetch-*
//     / Oai-Device-Id / Oai-Client-Version),少任何一项都可能被 Cloudflare / 业务风控
//     当成脚本直接 403;
//   - User-Agent / sec-ch-ua 两者的版本号必须一致(都是 Edge 143),否则"指纹冲突"。
func (q *QuotaProber) doProbe(ctx context.Context, a *Account, accessToken string) (out probeOutcome, err error) {
	out.remaining = -1
	out.total = -1

	// timezone_offset_min: 跟 UI 一致发 -480(北京时间),非关键
	reqBody := []byte(`{"gizmo_id":null,"requested_default_model":null,"conversation_id":null,"timezone_offset_min":-480,"system_hints":["picture_v2"]}`)

	req, err := http.NewRequestWithContext(ctx, "POST",
		"https://chatgpt.com/backend-api/conversation/init", bytes.NewReader(reqBody))
	if err != nil {
		return
	}

	// ── 基础头 ──
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Origin", chatgpt.BaseURL)
	req.Header.Set("Referer", chatgpt.BaseURL+"/")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8,en-GB;q=0.7,en-US;q=0.6")
	req.Header.Set("User-Agent", chatgpt.DefaultUserAgent)

	// ── Client-Hints(Edge 143 @ Windows 11,必须跟 UA 版本一致)──
	req.Header.Set("Sec-Ch-Ua", `"Microsoft Edge";v="143", "Chromium";v="143", "Not A(Brand";v="24"`)
	req.Header.Set("Sec-Ch-Ua-Arch", `"x86"`)
	req.Header.Set("Sec-Ch-Ua-Bitness", `"64"`)
	req.Header.Set("Sec-Ch-Ua-Full-Version", `"143.0.3650.96"`)
	req.Header.Set("Sec-Ch-Ua-Full-Version-List",
		`"Microsoft Edge";v="143.0.3650.96", "Chromium";v="143.0.7499.147", "Not A(Brand";v="24.0.0.0"`)
	req.Header.Set("Sec-Ch-Ua-Mobile", "?0")
	req.Header.Set("Sec-Ch-Ua-Model", `""`)
	req.Header.Set("Sec-Ch-Ua-Platform", `"Windows"`)
	req.Header.Set("Sec-Ch-Ua-Platform-Version", `"19.0.0"`)

	// ── Fetch 元数据(同源 XHR)──
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Site", "same-origin")

	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Pragma", "no-cache")
	req.Header.Set("Priority", "u=1, i")

	// ── Oai-* 业务指纹(chatgpt.com 自己埋的)──
	// DeviceID 缺失会触发降权/频繁 403,这里用账号绑定的;实在没存就退回随机填充,
	// 起码让请求长得像浏览器。
	deviceID := strings.TrimSpace(a.OAIDeviceID)
	if deviceID == "" {
		deviceID = fallbackDeviceID(a.ID)
	}
	req.Header.Set("Oai-Device-Id", deviceID)
	if sid := strings.TrimSpace(a.OAISessionID); sid != "" {
		req.Header.Set("Oai-Session-Id", sid)
	}
	sessionID := strings.TrimSpace(a.OAISessionID)
	req.Header.Set("Oai-Language", chatgpt.DefaultLanguage)
	req.Header.Set("Oai-Client-Version", chatgpt.DefaultClientVersion)
	req.Header.Set("Oai-Client-Build-Number", chatgpt.DefaultClientBuildNum)

	// ── X-Openai-Target-* (chatgpt web 每请求必带,值就是 URL path)──
	req.Header.Set("X-Openai-Target-Path", req.URL.Path)
	req.Header.Set("X-Openai-Target-Route", req.URL.Path)

	proxyLabel := "direct"
	if q.proxyResolver != nil {
		proxyLabel = sanitizeProxyURL(q.proxyResolver.ProxyURLForAccount(ctx, a.ID))
		if proxyLabel == "" {
			proxyLabel = "direct"
		}
	}
	hc := q.clientFor(ctx, a.ID)
	q.bootstrapBrowserCookies(ctx, hc, a, proxyLabel)
	start := time.Now()
	resp, e := hc.Do(req)
	elapsed := time.Since(start)
	if e != nil {
		q.log.Warn("quota probe request failed",
			zap.Uint64("account_id", a.ID),
			zap.String("email", a.Email),
			zap.String("stage", "conversation/init"),
			zap.String("method", req.Method),
			zap.String("url", req.URL.String()),
			zap.String("proxy", proxyLabel),
			zap.String("device_id", maskMiddle(deviceID)),
			zap.Bool("has_session_id", sessionID != ""),
			zap.Duration("elapsed", elapsed),
			zap.Error(e),
		)
		err = e
		return
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		pe := &probeHTTPError{
			Stage:       "conversation/init",
			Method:      req.Method,
			URL:         req.URL.String(),
			Status:      resp.StatusCode,
			Body:        summarizeProbeBody(string(data), resp.Header.Get("Content-Type"), resp.Header.Get("Server")),
			AccountID:   a.ID,
			Email:       a.Email,
			Proxy:       proxyLabel,
			DeviceID:    deviceID,
			SessionID:   sessionID,
			TokenTail:   tokenTail(accessToken),
			Elapsed:     elapsed,
			ContentType: resp.Header.Get("Content-Type"),
			Server:      resp.Header.Get("Server"),
			CFRay:       resp.Header.Get("Cf-Ray"),
		}
		q.log.Warn("quota probe upstream rejected",
			zap.Uint64("account_id", pe.AccountID),
			zap.String("email", pe.Email),
			zap.String("stage", pe.Stage),
			zap.String("method", pe.Method),
			zap.String("url", pe.URL),
			zap.Int("http_status", pe.Status),
			zap.String("proxy", pe.Proxy),
			zap.String("device_id", maskMiddle(pe.DeviceID)),
			zap.Bool("has_session_id", pe.SessionID != ""),
			zap.String("session_id", maskMiddle(pe.SessionID)),
			zap.String("token_tail", pe.TokenTail),
			zap.Duration("elapsed", pe.Elapsed),
			zap.String("content_type", pe.ContentType),
			zap.String("server", pe.Server),
			zap.String("cf_ray", pe.CFRay),
			zap.String("body", pe.Body),
		)
		err = pe
		return
	}

	var payload struct {
		Type             string   `json:"type"`
		BlockedFeatures  []string `json:"blocked_features"`
		DefaultModelSlug string   `json:"default_model_slug"`
		LimitsProgress   []struct {
			FeatureName string `json:"feature_name"`
			Remaining   *int   `json:"remaining"`
			ResetAfter  string `json:"reset_after"`
		} `json:"limits_progress"`
	}
	if err = json.Unmarshal(data, &payload); err != nil {
		return
	}
	out.defaultModel = payload.DefaultModelSlug
	out.blockedFeatures = payload.BlockedFeatures

	for _, item := range payload.LimitsProgress {
		if !isImageFeature(item.FeatureName) {
			continue
		}
		if item.Remaining != nil {
			if out.remaining < 0 || *item.Remaining < out.remaining {
				out.remaining = *item.Remaining
			}
		}
		if item.ResetAfter != "" {
			if t, e := time.Parse(time.RFC3339, item.ResetAfter); e == nil {
				if out.resetAt.IsZero() || t.Before(out.resetAt) {
					out.resetAt = t
				}
			}
		}
	}
	return
}

// fallbackDeviceID 兜底 Oai-Device-Id:用账号 ID 拼一个固定的 uuid-like 字符串,
// 保证同一账号每次探测都发一样的 device-id(这跟"用户在浏览器里的固定 LocalStorage"
// 语义一致),避免被风控标成"跳跃式设备"。生产环境建议在首次登录时就把真实
// device-id 写进 oai_accounts.oai_device_id。
func fallbackDeviceID(accountID uint64) string {
	// 形如 00000000-0000-4000-8000-xxxxxxxxxxxx(变体位正确,RFC 4122 v4)
	return fmt.Sprintf("00000000-0000-4000-8000-%012d", accountID%1_000_000_000_000)
}

func isImageFeature(name string) bool {
	n := strings.ToLower(name)
	switch n {
	case "image_gen", "image_generation", "image_edit", "img_gen":
		return true
	}
	return strings.Contains(n, "image_gen") || strings.Contains(n, "img_gen")
}

func sanitizeProxyURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "configured"
	}
	if u.User != nil {
		username := u.User.Username()
		if username != "" {
			u.User = url.UserPassword(maskMiddle(username), "***")
		} else {
			u.User = url.UserPassword("***", "***")
		}
	}
	return u.String()
}

func tokenTail(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	if len(token) <= 8 {
		return "***" + token
	}
	return "***" + token[len(token)-8:]
}

func maskMiddle(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if len(s) <= 8 {
		return "***"
	}
	return s[:4] + "..." + s[len(s)-4:]
}

func summarizeProbeBody(body, contentType, server string) string {
	s := strings.TrimSpace(body)
	if s == "" {
		return ""
	}
	lowBody := strings.ToLower(s)
	lowCT := strings.ToLower(contentType)
	lowServer := strings.ToLower(server)
	if strings.Contains(lowCT, "text/html") || strings.HasPrefix(lowBody, "<!doctype html") || strings.HasPrefix(lowBody, "<html") || strings.HasPrefix(lowBody, "<meta ") {
		label := "html"
		if strings.Contains(lowServer, "cloudflare") || strings.Contains(lowBody, "cloudflare") || strings.Contains(lowBody, "cf-browser-verification") {
			label = "cloudflare html challenge"
		}
		title := extractHTMLTitle(s)
		if title != "" {
			return label + ": title=" + title
		}
		return label + ": " + truncate(compactWhitespace(s), 180)
	}
	return truncate(compactWhitespace(s), 500)
}

func extractHTMLTitle(s string) string {
	low := strings.ToLower(s)
	start := strings.Index(low, "<title>")
	end := strings.Index(low, "</title>")
	if start < 0 || end <= start {
		return ""
	}
	return truncate(compactWhitespace(s[start+len("<title>"):end]), 120)
}

func compactWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func friendlyProbeErr(err error) string {
	if err == nil {
		return ""
	}
	if pe := asProbeHTTPError(err); pe != nil {
		switch pe.Status {
		case http.StatusUnauthorized:
			return "AT 已过期,无法探测额度:" + pe.DebugString()
		case http.StatusForbidden:
			return "上游拒绝访问(403):" + pe.DebugString()
		case http.StatusTooManyRequests:
			return "上游速率限制(429):" + pe.DebugString()
		default:
			return fmt.Sprintf("上游返回异常(%d):%s", pe.Status, pe.DebugString())
		}
	}
	s := err.Error()
	low := strings.ToLower(s)
	switch {
	case strings.Contains(low, "http=401"):
		return "AT 已过期,无法探测额度"
	case strings.Contains(low, "http=403"):
		return "上游拒绝访问(403)"
	case strings.Contains(low, "http=429"):
		return "上游速率限制(429)"
	case strings.Contains(low, "timeout"), strings.Contains(low, "deadline exceeded"):
		return "探测超时"
	case strings.Contains(low, "connection refused"), strings.Contains(low, "no such host"):
		return "网络不通"
	default:
		if len(s) > 160 {
			s = s[:160] + "…"
		}
		return "探测失败:" + s
	}
}
