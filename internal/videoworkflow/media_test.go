package videoworkflow

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestSignedMedia_Purpose(t *testing.T) {
	signer := NewMediaSigner("secret")
	now := time.Unix(1_800_000_000, 0)
	expires := now.Add(PreviewSignTTL)
	sig, err := signer.Sign("vwv_42", MediaPurposePreview, expires)
	if err != nil {
		t.Fatal(err)
	}
	if err := signer.Verify("vwv_42", MediaPurposePreview, expires.Unix(), sig, now); err != nil {
		t.Fatal(err)
	}
	if err := signer.Verify("vwv_42", MediaPurposeSeedance, expires.Unix(), sig, now); !errors.Is(err, ErrInvalidMediaSign) {
		t.Fatalf("purpose substitution got %v", err)
	}
	if err := signer.Verify("vwv_43", MediaPurposePreview, expires.Unix(), sig, now); !errors.Is(err, ErrInvalidMediaSign) {
		t.Fatalf("version substitution got %v", err)
	}
	if err := signer.Verify("vwv_42", MediaPurposePreview, expires.Unix(), sig, expires); !errors.Is(err, ErrInvalidMediaSign) {
		t.Fatalf("expired signature got %v", err)
	}
	path := SignedMediaPath("vwv_42", MediaPurposePreview, expires, sig)
	if !contains(path, "/p/vwf/vwv_42?") || !contains(path, "purpose=preview") {
		t.Fatalf("unexpected signed path %q", path)
	}
}

func TestSignedMedia_InvalidInputs(t *testing.T) {
	signer := NewMediaSigner("secret")
	if _, err := signer.Sign("", MediaPurposePreview, time.Now()); !errors.Is(err, ErrInvalidMediaSign) {
		t.Fatalf("empty version error = %v", err)
	}
	if _, err := signer.Sign("version", "unknown", time.Now()); !errors.Is(err, ErrInvalidMediaPurpose) {
		t.Fatalf("purpose error = %v", err)
	}
	if err := signer.Verify("version", MediaPurposePreview, time.Now().Add(time.Minute).Unix(), "not-hex", time.Now()); !errors.Is(err, ErrInvalidMediaSign) {
		t.Fatalf("signature error = %v", err)
	}
}

func TestAssetQuota_ExactBoundary(t *testing.T) {
	root := t.TempDir()
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 64)...)
	saved, err := SaveMedia(context.Background(), root, 7, MediaKindImage, "face.png", bytes.NewReader(png), AssetQuotaBytes-int64(len(png)))
	if err != nil {
		t.Fatal(err)
	}
	if saved.SizeBytes != int64(len(png)) || saved.MIME != "image/png" {
		t.Fatalf("unexpected saved media %#v", saved)
	}
	if filepath.Base(saved.Path) != saved.SHA256+".png" {
		t.Fatalf("path %q does not use content hash", saved.Path)
	}
	if _, err := os.Stat(saved.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveMedia(context.Background(), root, 7, MediaKindImage, "same.png", bytes.NewReader(png), 0); err != nil {
		t.Fatalf("content-addressed duplicate failed: %v", err)
	}
	_, err = SaveMedia(context.Background(), root, 7, MediaKindImage, "next.png", bytes.NewReader(png[:1]), AssetQuotaBytes)
	if !errors.Is(err, ErrAssetQuotaExceeded) {
		t.Fatalf("got %v, want quota exceeded", err)
	}
}

func TestSaveMediaConcurrentPublishIsNoReplaceAndCleanupSafe(t *testing.T) {
	root := t.TempDir()
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{7}, 1024)...)
	const workers = 32
	start := make(chan struct{})
	results := make(chan *SavedMedia, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			saved, err := SaveMedia(context.Background(), root, 7, MediaKindImage, "same.png", bytes.NewReader(png), 0)
			if err != nil {
				errs <- err
				return
			}
			results <- saved
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	created := 0
	var creator *SavedMedia
	var reused *SavedMedia
	for saved := range results {
		if saved.Created {
			created++
			creator = saved
		} else if reused == nil {
			reused = saved
		}
	}
	if created != 1 {
		t.Fatalf("created=%d, want exactly 1", created)
	}
	if creator == nil || reused == nil || creator.Path == reused.Path {
		t.Fatalf("creator=%+v reused=%+v", creator, reused)
	}
	creatorInfo, err := os.Stat(creator.Path)
	if err != nil {
		t.Fatal(err)
	}
	reusedInfo, err := os.Stat(reused.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !creatorInfo.Mode().IsRegular() || !reusedInfo.Mode().IsRegular() || os.SameFile(creatorInfo, reusedInfo) {
		t.Fatal("published files must be independent regular files accepted by backup tar validation")
	}
	// canonical 创建者后续数据库失败并清理时，复用者的独立文件仍可读取。
	removeNewMedia(creator)
	if _, err := os.Stat(reused.Path); err != nil {
		t.Fatalf("reused published file was removed with creator: %v", err)
	}
}

func TestUpload_MIMEAndSize(t *testing.T) {
	_, err := SaveMedia(context.Background(), t.TempDir(), 1, MediaKindImage, "fake.png", bytes.NewReader([]byte("plain text")), 0)
	if !errors.Is(err, ErrUnsupportedMedia) {
		t.Fatalf("got %v, want unsupported media", err)
	}
	_, err = SaveMedia(context.Background(), t.TempDir(), 1, "binary", "file.bin", bytes.NewReader([]byte{1}), 0)
	if !errors.Is(err, ErrUnsupportedMedia) {
		t.Fatalf("got %v, want unsupported kind", err)
	}
	_, err = SaveMedia(context.Background(), t.TempDir(), 1, MediaKindImage, "empty.png", bytes.NewReader(nil), 0)
	if err == nil {
		t.Fatal("empty media was accepted")
	}
	_, err = SaveMedia(context.Background(), t.TempDir(), 1, MediaKindImage, "bad.png", bytes.NewReader([]byte{1}), -1)
	if !errors.Is(err, ErrAssetQuotaExceeded) {
		t.Fatalf("negative used bytes error = %v", err)
	}
}

func TestMediaHelpers(t *testing.T) {
	t.Setenv("GPT2API_VIDEO_WORKFLOW_ASSET_DIR", "/tmp/video-assets")
	if got := AssetRoot(); got != "/tmp/video-assets" {
		t.Fatalf("asset root = %q", got)
	}
	if maxBytesForKind(MediaKindStoryboard) != 2*1024*1024 || maxBytesForKind("bad") != 0 {
		t.Fatal("unexpected media byte limits")
	}
	for _, test := range []struct{ kind, mime, ext string }{
		{MediaKindImage, "image/jpeg", ".jpg"}, {MediaKindImage, "image/webp", ".webp"},
		{MediaKindVideo, "video/mp4", ".mp4"}, {MediaKindVideo, "video/webm", ".webm"},
		{MediaKindVideo, "video/quicktime", ".mov"}, {MediaKindStoryboard, "application/json", ".json"},
	} {
		if ext, ok := mediaExtension(test.kind, test.mime); !ok || ext != test.ext {
			t.Fatalf("extension %s/%s = %q,%v", test.kind, test.mime, ext, ok)
		}
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := copyContext(canceled, io.Discard, bytes.NewReader([]byte("x"))); !errors.Is(err, context.Canceled) {
		t.Fatalf("copy cancellation error = %v", err)
	}
}

func TestRestrictedHTTPClient(t *testing.T) {
	client := NewRestrictedHTTPClient(0)
	if client.Timeout != 60*time.Second {
		t.Fatalf("timeout = %s", client.Timeout)
	}
	request, _ := http.NewRequest(http.MethodGet, "file:///tmp/a", nil)
	if err := client.CheckRedirect(request, nil); !errors.Is(err, ErrRemoteAddressDenied) {
		t.Fatalf("redirect scheme error = %v", err)
	}
	request, _ = http.NewRequest(http.MethodGet, "https://example.com", nil)
	if err := client.CheckRedirect(request, make([]*http.Request, 3)); err == nil {
		t.Fatal("redirect limit was not enforced")
	}
	if got := NewRestrictedHTTPClient(3 * time.Second).Timeout; got != 3*time.Second {
		t.Fatalf("explicit timeout = %s", got)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.Proxy != nil {
		t.Fatal("restricted client must disable environment proxies")
	}
	request, _ = http.NewRequest(http.MethodGet, "http://example.com/video.mp4", nil)
	if err := client.CheckRedirect(request, nil); !errors.Is(err, ErrRemoteAddressDenied) {
		t.Fatalf("plain HTTP redirect error = %v", err)
	}
}

func TestRestrictedHTTPClientAllowedHosts(t *testing.T) {
	client := NewRestrictedHTTPClientWithAllowedHosts(time.Second, []string{"media.example.com"})
	allowed, _ := http.NewRequest(http.MethodGet, "https://cdn.media.example.com/a.mp4", nil)
	if err := client.CheckRedirect(allowed, nil); err != nil {
		t.Fatalf("allowed subdomain rejected: %v", err)
	}
	denied, _ := http.NewRequest(http.MethodGet, "https://media.example.com.evil.test/a.mp4", nil)
	if err := client.CheckRedirect(denied, nil); !errors.Is(err, ErrRemoteAddressDenied) {
		t.Fatalf("suffix-confusion host error = %v", err)
	}
	if _, _, err := DownloadRemoteMedia(context.Background(), client, "http://media.example.com/a.mp4", 10); !errors.Is(err, ErrRemoteAddressDenied) {
		t.Fatalf("plain HTTP initial URL error = %v", err)
	}
}

func TestRemoteDownload_SSRFAddressRules(t *testing.T) {
	denied := []string{
		"127.0.0.1", "10.0.0.1", "172.16.0.1", "192.168.1.1", "169.254.1.1", "::1", "fc00::1",
		"100.100.100.200", "100.64.0.1", "198.18.0.1", "192.0.0.8", "192.0.2.1", "198.51.100.1", "203.0.113.1", "240.0.0.1", "2001:db8::1",
	}
	for _, value := range denied {
		if !deniedIP(net.ParseIP(value)) {
			t.Fatalf("%s should be denied", value)
		}
	}
	if deniedIP(net.ParseIP("8.8.8.8")) {
		t.Fatal("public address should be allowed")
	}
}
