package videoworkflow

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	PreviewSignTTL  = 15 * time.Minute
	DownloadSignTTL = 5 * time.Minute
	SeedanceSignTTL = 60 * time.Minute
)

const (
	MediaPurposePreview    = "preview"
	MediaPurposeDownload   = "download"
	MediaPurposeSeedance   = "seedance"
	MediaKindImage         = "image"
	MediaKindVideo         = "video"
	MediaKindStoryboard    = "storyboard"
	MediaKindComposedVideo = "composed_video"
)

var (
	ErrInvalidMediaPurpose = errors.New("invalid media purpose")
	ErrInvalidMediaSign    = errors.New("invalid or expired media signature")
	ErrAssetQuotaExceeded  = errors.New("video workflow asset quota exceeded")
	ErrUnsupportedMedia    = errors.New("unsupported media type")
	ErrRemoteAddressDenied = errors.New("remote media address denied")
)

type MediaSigner struct {
	key []byte
}

func NewMediaSigner(secret string) *MediaSigner {
	sum := sha256.Sum256([]byte("gpt2api-video-workflow:" + secret))
	return &MediaSigner{key: sum[:]}
}

func (s *MediaSigner) Sign(versionID string, purpose string, expiresAt time.Time) (string, error) {
	if !validMediaPurpose(purpose) {
		return "", ErrInvalidMediaPurpose
	}
	if strings.TrimSpace(versionID) == "" || expiresAt.IsZero() {
		return "", ErrInvalidMediaSign
	}
	mac := hmac.New(sha256.New, s.key)
	_, _ = io.WriteString(mac, mediaSignaturePayload(versionID, purpose, expiresAt.Unix()))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func (s *MediaSigner) Verify(versionID string, purpose string, expiresUnix int64, signature string, now time.Time) error {
	if !validMediaPurpose(purpose) || strings.TrimSpace(versionID) == "" || expiresUnix <= now.Unix() {
		return ErrInvalidMediaSign
	}
	want, err := s.Sign(versionID, purpose, time.Unix(expiresUnix, 0))
	if err != nil {
		return ErrInvalidMediaSign
	}
	got, err := hex.DecodeString(strings.TrimSpace(signature))
	if err != nil || !hmac.Equal(got, mustDecodeHex(want)) {
		return ErrInvalidMediaSign
	}
	return nil
}

func SignedMediaPath(versionID string, purpose string, expiresAt time.Time, signature string) string {
	query := url.Values{}
	query.Set("exp", strconv.FormatInt(expiresAt.Unix(), 10))
	query.Set("purpose", purpose)
	query.Set("sig", signature)
	return fmt.Sprintf("/p/vwf/%s?%s", url.PathEscape(versionID), query.Encode())
}

func mediaSignaturePayload(versionID string, purpose string, expiresUnix int64) string {
	return fmt.Sprintf("%s\n%s\n%d", versionID, purpose, expiresUnix)
}

func validMediaPurpose(purpose string) bool {
	switch purpose {
	case MediaPurposePreview, MediaPurposeDownload, MediaPurposeSeedance:
		return true
	default:
		return false
	}
}

func mustDecodeHex(value string) []byte {
	b, _ := hex.DecodeString(value)
	return b
}

func AssetRoot() string {
	if dir := strings.TrimSpace(os.Getenv("GPT2API_VIDEO_WORKFLOW_ASSET_DIR")); dir != "" {
		return dir
	}
	wd, err := os.Getwd()
	if err == nil {
		return filepath.Join(wd, "data", "video-workflow-assets")
	}
	return "./data/video-workflow-assets"
}

type SavedMedia struct {
	Path       string
	MIME       string
	SizeBytes  int64
	SHA256     string
	OriginName string
	Created    bool
}

func SaveMedia(ctx context.Context, root string, ownerID uint64, kind, originName string, src io.Reader, usedBytes int64) (*SavedMedia, error) {
	if usedBytes < 0 || usedBytes > AssetQuotaBytes {
		return nil, ErrAssetQuotaExceeded
	}
	maxBytes := maxBytesForKind(kind)
	if maxBytes == 0 {
		return nil, ErrUnsupportedMedia
	}
	tempDir := filepath.Join(root, ".tmp")
	if err := os.MkdirAll(tempDir, 0o750); err != nil {
		return nil, err
	}
	temp, err := os.CreateTemp(tempDir, "upload-*")
	if err != nil {
		return nil, err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	hash := sha256.New()
	written, err := copyContext(ctx, io.MultiWriter(temp, hash), io.LimitReader(src, maxBytes+1))
	closeErr := temp.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if written == 0 || written > maxBytes {
		return nil, fmt.Errorf("media exceeds %d bytes", maxBytes)
	}
	if usedBytes+written > AssetQuotaBytes {
		return nil, ErrAssetQuotaExceeded
	}
	file, err := os.Open(tempPath)
	if err != nil {
		return nil, err
	}
	head := make([]byte, 512)
	n, _ := io.ReadFull(file, head)
	_ = file.Close()
	mimeType := http.DetectContentType(head[:n])
	ext, ok := mediaExtension(kind, mimeType)
	if !ok {
		return nil, ErrUnsupportedMedia
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	dir := filepath.Join(root, strconv.FormatUint(ownerID, 10), digest[:2])
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	finalPath := filepath.Join(dir, digest+ext)
	// 临时文件与最终文件位于同一素材根目录，os.Link 仅用于原子竞争 canonical
	// 路径。同摘要复用者将自己的临时文件 rename 到唯一普通文件，避免 tar 将
	// 持久化素材识别为硬链接，同时保证每个请求都能独立安全清理。
	created := false
	ownedPath := finalPath
	if err := os.Link(tempPath, finalPath); err == nil {
		created = true
	} else if errors.Is(err, os.ErrExist) {
		ownedPath = strings.TrimSuffix(finalPath, ext) + ".ref-" + uuid.NewString() + ext
		if err := os.Rename(tempPath, ownedPath); err != nil {
			return nil, err
		}
	} else {
		return nil, err
	}
	return &SavedMedia{Path: ownedPath, MIME: mimeType, SizeBytes: written, SHA256: digest, OriginName: filepath.Base(originName), Created: created}, nil
}

func maxBytesForKind(kind string) int64 {
	switch kind {
	case MediaKindImage:
		return MaxImageBytes
	case MediaKindVideo, MediaKindComposedVideo:
		return MaxVideoBytes
	case MediaKindStoryboard:
		return 2 * 1024 * 1024
	default:
		return 0
	}
}

func mediaExtension(kind, mimeType string) (string, bool) {
	if kind == MediaKindImage {
		switch mimeType {
		case "image/jpeg":
			return ".jpg", true
		case "image/png":
			return ".png", true
		case "image/webp":
			return ".webp", true
		}
	}
	if kind == MediaKindVideo || kind == MediaKindComposedVideo {
		switch mimeType {
		case "video/mp4", "application/mp4":
			return ".mp4", true
		case "video/webm":
			return ".webm", true
		case "video/quicktime":
			return ".mov", true
		}
	}
	if kind == MediaKindStoryboard && (mimeType == "application/json" || mimeType == "text/plain; charset=utf-8") {
		return ".json", true
	}
	return "", false
}

func copyContext(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	buf := make([]byte, 64*1024)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		n, readErr := src.Read(buf)
		if n > 0 {
			written, writeErr := dst.Write(buf[:n])
			total += int64(written)
			if writeErr != nil {
				return total, writeErr
			}
		}
		if readErr == io.EOF {
			return total, nil
		}
		if readErr != nil {
			return total, readErr
		}
	}
}

func NewRestrictedHTTPClient(timeout time.Duration) *http.Client {
	return NewRestrictedHTTPClientWithAllowedHosts(timeout, nil)
}

// NewRestrictedHTTPClientWithAllowedHosts 创建不读取 HTTP(S)_PROXY 的下载客户端。
// allowedHosts 为空时允许任意公网 HTTPS 域名；非空时仅允许精确域名或其子域名。
func NewRestrictedHTTPClientWithAllowedHosts(timeout time.Duration, allowedHosts []string) *http.Client {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			if !allowedRemoteHost(host, allowedHosts) {
				return nil, ErrRemoteAddressDenied
			}
			ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
			if err != nil {
				return nil, err
			}
			if len(ips) == 0 {
				return nil, ErrRemoteAddressDenied
			}
			for _, ip := range ips {
				if deniedIP(ip) {
					return nil, ErrRemoteAddressDenied
				}
			}
			// 使用已经校验的 IP 建连，避免校验后再次 DNS 解析产生重绑定。
			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
		},
	}
	client := &http.Client{Timeout: timeout, Transport: transport}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return errors.New("too many redirects")
		}
		if req.URL.Scheme != "https" || req.URL.User != nil || !allowedRemoteHost(req.URL.Hostname(), allowedHosts) {
			return ErrRemoteAddressDenied
		}
		return nil
	}
	return client
}

func allowedRemoteHost(host string, allowedHosts []string) bool {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if host == "" {
		return false
	}
	if len(allowedHosts) == 0 {
		return true
	}
	for _, allowed := range allowedHosts {
		allowed = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(allowed), "."))
		if host == allowed || strings.HasSuffix(host, "."+allowed) {
			return true
		}
	}
	return false
}

func DownloadRemoteMedia(ctx context.Context, client *http.Client, rawURL string, maxBytes int64) ([]byte, string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Hostname() == "" {
		return nil, "", ErrRemoteAddressDenied
	}
	if client == nil {
		client = NewRestrictedHTTPClient(60 * time.Second)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, "", err
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, "", fmt.Errorf("remote media status %d", response.StatusCode)
	}
	if maxBytes <= 0 {
		maxBytes = MaxVideoBytes
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if err != nil {
		return nil, "", err
	}
	if len(data) == 0 || int64(len(data)) > maxBytes {
		return nil, "", fmt.Errorf("remote media exceeds %d bytes", maxBytes)
	}
	mimeType := http.DetectContentType(data[:minInt(len(data), 512)])
	return data, mimeType, nil
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func deniedIP(ip net.IP) bool {
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalMulticast() || ip.IsLinkLocalUnicast() || ip.IsMulticast() {
		return true
	}
	address, ok := netip.AddrFromSlice(ip)
	if !ok {
		return true
	}
	address = address.Unmap()
	if !address.IsGlobalUnicast() {
		return true
	}
	for _, prefix := range deniedRemotePrefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

var deniedRemotePrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
}
