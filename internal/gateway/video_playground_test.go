package gateway

import (
	"bytes"
	"encoding/base64"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/432539/gpt2api/internal/settings"
	"github.com/432539/gpt2api/internal/videogen"
	"github.com/gin-gonic/gin"
)

func TestSaveVideoPlaygroundUploadImageReturnsDataURL(t *testing.T) {
	t.Setenv("GPT2API_SITE_ASSET_DIR", t.TempDir())
	fh := videoPlaygroundTestFileHeader(t, "image", "ref.jpg", []byte{0xff, 0xd8, 0xff, 0xdb, 0x00, 0x43, 0x00, 0xff, 0xd9})

	publicPath, dataURL, err := saveVideoPlaygroundUpload(fh, "image")
	if err != nil {
		t.Fatalf("saveVideoPlaygroundUpload error: %v", err)
	}
	if !strings.HasPrefix(publicPath, "/site-assets/videogen-play-image-") || !strings.HasSuffix(publicPath, ".jpg") {
		t.Fatalf("unexpected public path: %q", publicPath)
	}
	if !strings.HasPrefix(dataURL, "data:image/jpeg;base64,") {
		t.Fatalf("unexpected data url prefix: %q", dataURL)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(dataURL, "data:image/jpeg;base64,"))
	if err != nil {
		t.Fatalf("decode data url: %v", err)
	}
	if len(raw) != int(fh.Size) {
		t.Fatalf("data url bytes = %d, want %d", len(raw), fh.Size)
	}
	entries, err := os.ReadDir(os.Getenv("GPT2API_SITE_ASSET_DIR"))
	if err != nil {
		t.Fatalf("read upload dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("saved files = %d, want 1", len(entries))
	}
}

func TestSaveVideoPlaygroundUploadVideoDoesNotReturnDataURL(t *testing.T) {
	t.Setenv("GPT2API_SITE_ASSET_DIR", t.TempDir())
	fh := videoPlaygroundTestFileHeader(t, "video", "ref.mp4", []byte{
		0x00, 0x00, 0x00, 0x18, 'f', 't', 'y', 'p', 'm', 'p', '4', '2',
		0x00, 0x00, 0x00, 0x00, 'm', 'p', '4', '2', 'i', 's', 'o', 'm',
	})

	publicPath, dataURL, err := saveVideoPlaygroundUpload(fh, "video")
	if err != nil {
		t.Fatalf("saveVideoPlaygroundUpload error: %v", err)
	}
	if !strings.HasPrefix(publicPath, "/site-assets/videogen-play-video-") || !strings.HasSuffix(publicPath, ".mp4") {
		t.Fatalf("unexpected public path: %q", publicPath)
	}
	if dataURL != "" {
		t.Fatalf("video upload should not produce data url: %q", dataURL)
	}
}

func TestVideoPlaygroundImagePayloadURLPrefersDataURLForAPIYI(t *testing.T) {
	upload := videoPlaygroundUpload{
		PublicURL: "https://example.com/site-assets/ref.jpg",
		DataURL:   "data:image/jpeg;base64,abc",
	}
	if got := videoPlaygroundImagePayloadURL(videogen.ChannelAPIYIHappyHorse, upload); got != upload.DataURL {
		t.Fatalf("happyhorse payload url = %q, want data url", got)
	}
	if got := videoPlaygroundImagePayloadURL(videogen.ChannelEchoon, upload); got != upload.PublicURL {
		t.Fatalf("echoon payload url = %q, want public url", got)
	}
}

func TestVideoPlaygroundAbsoluteURLKeepsForwardedHostPort(t *testing.T) {
	c := videoPlaygroundTestContext(t, map[string]string{
		"X-Forwarded-Proto": "http",
		"X-Forwarded-Host":  "123.207.53.152:8080",
	})
	got := videoPlaygroundAbsoluteURL(c, nil, "/site-assets/ref.mp4")
	want := "http://123.207.53.152:8080/site-assets/ref.mp4"
	if got != want {
		t.Fatalf("absolute url = %q, want %q", got, want)
	}
}

func TestVideoPlaygroundAbsoluteURLAddsForwardedPort(t *testing.T) {
	c := videoPlaygroundTestContext(t, map[string]string{
		"X-Forwarded-Proto": "http",
		"X-Forwarded-Host":  "123.207.53.152",
		"X-Forwarded-Port":  "8080",
	})
	got := videoPlaygroundAbsoluteURL(c, nil, "/site-assets/ref.mp4")
	want := "http://123.207.53.152:8080/site-assets/ref.mp4"
	if got != want {
		t.Fatalf("absolute url = %q, want %q", got, want)
	}
}

func TestVideoPlaygroundAbsoluteURLUsesConfiguredBase(t *testing.T) {
	c := videoPlaygroundTestContext(t, map[string]string{
		"X-Forwarded-Host": "123.207.53.152:8080",
	})
	got := videoPlaygroundAbsoluteURL(c, videoPlaygroundTestSettings{
		settings.SiteAPIBaseURL: "https://cdn.example.com",
	}, "/site-assets/ref.mp4")
	want := "https://cdn.example.com/site-assets/ref.mp4"
	if got != want {
		t.Fatalf("absolute url = %q, want %q", got, want)
	}
}

func TestVideoPlaygroundSupportsReferenceVideoIncludesSeedance(t *testing.T) {
	for _, channelType := range []string{
		videogen.ChannelAPIYISeedance,
		videogen.ChannelAPIYIWan27,
		videogen.ChannelAPIYIHappyHorse,
	} {
		if !videoPlaygroundSupportsReferenceVideo(channelType) {
			t.Fatalf("channel %q should support reference video", channelType)
		}
	}
	if videoPlaygroundSupportsReferenceVideo(videogen.ChannelEchoon) {
		t.Fatal("echoon should not support reference video")
	}
}

func videoPlaygroundTestFileHeader(t *testing.T, field, filename string, data []byte) *multipart.FileHeader {
	t.Helper()
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	part, err := w.CreateFormFile(field, filename)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/", body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	if err := req.ParseMultipartForm(32 << 20); err != nil {
		t.Fatalf("ParseMultipartForm: %v", err)
	}
	files := req.MultipartForm.File[field]
	if len(files) != 1 {
		t.Fatalf("files[%s] len = %d, want 1", field, len(files))
	}
	return files[0]
}

func videoPlaygroundTestContext(t *testing.T, headers map[string]string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	req := httptest.NewRequest(http.MethodPost, "http://server/api/me/playground/video", nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = req
	return c
}

type videoPlaygroundTestSettings map[string]string

func (s videoPlaygroundTestSettings) VideoGenEnabled() bool { return true }

func (s videoPlaygroundTestSettings) VideoGenChannelType() string { return videogen.ChannelAPIYIWan27 }

func (s videoPlaygroundTestSettings) VideoGenConfigForChannel(channelType string) videogen.Config {
	return videogen.Config{ChannelType: channelType}
}

func (s videoPlaygroundTestSettings) VideoGenBillingRatio() float64 { return 10 }

func (s videoPlaygroundTestSettings) GetString(key string) string { return s[key] }
