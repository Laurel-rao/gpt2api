package videoworkflow

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/432539/gpt2api/internal/middleware"
)

func TestUploadVideoAssetWritesPreviewFrame(t *testing.T) {
	composer := NewComposer()
	if !composer.Available() {
		t.Skip("ffmpeg unavailable")
	}

	store := newAmbiguousAssetStore(t)
	root := t.TempDir()
	service := NewService(store)
	service.ConfigureMedia(root, "preview-secret", composer)

	videoPath := filepath.Join(t.TempDir(), "clip.mp4")
	makeFixture(t, videoPath, "blue", "320x240", 24, false)
	data, err := os.ReadFile(videoPath)
	if err != nil {
		t.Fatal(err)
	}

	asset, err := service.UploadAsset(context.Background(), AssetUploadInput{
		UserID: 7, Kind: MediaKindVideo, Name: "clip", FileName: "clip.mp4", Reader: bytes.NewReader(data),
	})
	if err != nil || asset == nil || len(asset.Versions) != 1 {
		t.Fatalf("asset=%+v err=%v", asset, err)
	}
	version := asset.Versions[0]
	if version.PreviewURL == "" {
		t.Fatal("expected preview_url after video upload")
	}
	if !videoPreviewSidecarExists(version.FilePath) {
		t.Fatalf("missing preview sidecar for %s", version.FilePath)
	}
	if !strings.Contains(version.PreviewURL, "purpose=preview") {
		t.Fatalf("unexpected preview url: %s", version.PreviewURL)
	}

	expires := time.Now().Add(PreviewSignTTL)
	sig, err := service.mediaSigner.Sign(version.ID, MediaPurposePreview, expires)
	if err != nil {
		t.Fatal(err)
	}
	opened, file, err := service.OpenSignedMedia(context.Background(), version.ID, MediaPurposePreview, expires.Unix(), sig, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if opened.MIMEType != "image/jpeg" {
		t.Fatalf("preview mime=%s want image/jpeg", opened.MIMEType)
	}
	head := make([]byte, 3)
	if _, err := file.Read(head); err != nil || head[0] != 0xff || head[1] != 0xd8 {
		t.Fatalf("preview is not jpeg: %v %v", head, err)
	}
}

func TestWriteVideoPreviewFrameFailureDoesNotBlockUpload(t *testing.T) {
	root := t.TempDir()
	service := NewService(newAmbiguousAssetStore(t))
	service.ConfigureMedia(root, "preview-secret", &Composer{
		FFmpegPath:  filepath.Join(root, "missing-ffmpeg"),
		FFprobePath: filepath.Join(root, "missing-ffprobe"),
	})

	savedPath := filepath.Join(root, "7", "ab", "clip.mp4")
	if err := os.MkdirAll(filepath.Dir(savedPath), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(savedPath, []byte("not-a-real-video"), 0o640); err != nil {
		t.Fatal(err)
	}
	service.writeVideoPreviewFrame(context.Background(), MediaKindVideo, savedPath)
	if videoPreviewSidecarExists(savedPath) {
		t.Fatal("failed extract should not leave a preview sidecar")
	}
	if _, err := os.Stat(savedPath); err != nil {
		t.Fatalf("source video should remain after preview failure: %v", err)
	}
}

func TestServeSignedMediaPreviewReturnsJPEGForVideo(t *testing.T) {
	composer := NewComposer()
	if !composer.Available() {
		t.Skip("ffmpeg unavailable")
	}
	store := newAmbiguousAssetStore(t)
	root := t.TempDir()
	service := NewService(store)
	service.ConfigureMedia(root, "preview-secret", composer)

	videoPath := filepath.Join(t.TempDir(), "serve.mp4")
	makeFixture(t, videoPath, "red", "320x240", 24, false)
	data, err := os.ReadFile(videoPath)
	if err != nil {
		t.Fatal(err)
	}
	asset, err := service.UploadAsset(context.Background(), AssetUploadInput{
		UserID: 9, Kind: MediaKindVideo, Name: "serve", FileName: "serve.mp4", Reader: bytes.NewReader(data),
	})
	if err != nil || asset == nil || len(asset.Versions) != 1 || asset.Versions[0].PreviewURL == "" {
		t.Fatalf("asset=%+v err=%v", asset, err)
	}

	gin.SetMode(gin.TestMode)
	handler := NewHandler(service)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(middleware.CtxUserID, uint64(9)); c.Next() })
	router.GET("/p/vwf/:version_id", handler.ServeSignedMedia)

	request := httptest.NewRequest(http.MethodGet, asset.Versions[0].PreviewURL, nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if ct := response.Header().Get("Content-Type"); !strings.HasPrefix(ct, "image/jpeg") {
		t.Fatalf("content-type=%s want image/jpeg", ct)
	}
	body := response.Body.Bytes()
	if len(body) < 3 || body[0] != 0xff || body[1] != 0xd8 {
		t.Fatalf("response is not jpeg (%d bytes)", len(body))
	}
}
