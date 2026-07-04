package ecommerce

import (
	"bytes"
	"encoding/base64"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildRequirementWithLibraryAssets(t *testing.T) {
	product := &LibraryAsset{
		AssetID:    "eal_product",
		Kind:       LibraryKindProduct,
		Name:       "防晒衣 SKU-A12",
		Code:       "SKU-A12",
		CoverURL:   "/ecommerce-assets/eal_product/cover.jpg",
		TagsJSON:   RawJSON(`["夏季","轻薄"]`),
		DetailJSON: RawJSON(`{"product":{"brand":"SunCo","selling_points":["UPF50+","冰感"]}}`),
	}
	model := &LibraryAsset{
		AssetID:    "eal_model",
		Kind:       LibraryKindModel,
		Name:       "户外真人模特",
		Code:       "M-01",
		DetailJSON: RawJSON(`{"model":{"model_type":"real","license":{"status":"approved"}}}`),
	}
	got := buildRequirementWithLibraryAssets("主需求", product, model)
	for _, want := range []string{"主需求", "【商品资产资料】", "防晒衣 SKU-A12", "UPF50+", "【模特资产资料】", "户外真人模特", "approved"} {
		if !strings.Contains(got, want) {
			t.Fatalf("requirement missing %q: %s", want, got)
		}
	}
}

func TestMergeTaskReferenceImagesUsesLibraryPriorityAndLimit(t *testing.T) {
	h := &Handler{runner: &Runner{}}
	product := &LibraryAsset{
		CoverURL:    "/product-cover.jpg",
		GalleryJSON: RawJSON(`[{"url":"/product-1.jpg"},{"url":"/product-2.jpg"}]`),
	}
	model := &LibraryAsset{
		CoverURL:    "/model-cover.jpg",
		GalleryJSON: RawJSON(`[{"url":"/model-1.jpg"}]`),
	}
	got := h.mergeTaskReferenceImages([]string{"/manual.jpg"}, product, model)
	want := []string{"/product-cover.jpg", "/product-1.jpg", "/product-2.jpg", "/model-cover.jpg"}
	if len(got) != len(want) {
		t.Fatalf("len=%d got=%v", len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ref[%d]=%q want %q; all=%v", i, got[i], want[i], got)
		}
	}
}

func TestAbsoluteAssetURLUsesAppBaseURL(t *testing.T) {
	h := &Handler{runner: &Runner{appBaseURL: "https://example.com/base"}}
	if got := h.absoluteAssetURL("/ecommerce-assets/a/cover.jpg"); got != "https://example.com/ecommerce-assets/a/cover.jpg" {
		t.Fatalf("absolute url = %q", got)
	}
	if got := h.absoluteAssetURL("data:image/png;base64,abc"); got != "data:image/png;base64,abc" {
		t.Fatalf("data url changed: %q", got)
	}
}

func TestSaveLibraryImageRejectsNonImage(t *testing.T) {
	t.Setenv("GPT2API_ECOMMERCE_ASSET_DIR", t.TempDir())
	if _, err := saveLibraryImage("eal_test", "bad.txt", strings.NewReader("hello")); err == nil {
		t.Fatal("expected non image to be rejected")
	}
}

func TestSaveLibraryImageStoresPNG(t *testing.T) {
	t.Setenv("GPT2API_ECOMMERCE_ASSET_DIR", t.TempDir())
	raw, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADUlEQVR4nGP8z8BQDwAFgwJ/lXbqVwAAAABJRU5ErkJggg==")
	if err != nil {
		t.Fatal(err)
	}
	saved, err := saveLibraryImage("eal_test", "cover.png", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if saved.URL == "" || saved.SHA256 == "" || saved.MIME != "image/png" || saved.Width != 1 || saved.Height != 1 {
		t.Fatalf("unexpected saved file: %+v", saved)
	}
}

func TestSaveVideoFromURLStoresMP4(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GPT2API_ECOMMERCE_ASSET_DIR", root)
	oldClient := libraryVideoHTTPClient
	libraryVideoHTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode:    http.StatusOK,
			Header:        http.Header{"Content-Type": []string{"video/mp4"}},
			Body:          io.NopCloser(strings.NewReader("mp4-video-data")),
			ContentLength: int64(len("mp4-video-data")),
			Request:       req,
		}, nil
	})}
	t.Cleanup(func() { libraryVideoHTTPClient = oldClient })

	saved, err := SaveVideoFromURL(t.Context(), "video/123", "https://cdn.example.com/result.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(saved.URL, "/ecommerce-assets/videos/video_123/") || !strings.HasSuffix(saved.URL, ".mp4") {
		t.Fatalf("unexpected video url: %s", saved.URL)
	}
	if saved.SHA256 == "" || saved.MIME != "video/mp4" || saved.SizeBytes != int64(len("mp4-video-data")) {
		t.Fatalf("unexpected saved video: %+v", saved)
	}
	rel := strings.TrimPrefix(saved.URL, "/ecommerce-assets/")
	if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
		t.Fatalf("saved video missing: %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestMergeLibraryGalleryPreservesURLItems(t *testing.T) {
	existing := RawJSON(`[{"url":"https://cdn.example.com/a.jpg","usage":"url"},{"url":"/ecommerce-assets/eal_test/old.jpg","sha256":"old"}]`)
	files := []LibraryAssetFile{{
		AssetID:    "eal_test",
		FileUsage:  "gallery",
		OriginName: "new.png",
		MIME:       "image/png",
		SizeBytes:  10,
		Width:      1,
		Height:     1,
		SHA256:     "new",
		URL:        "/ecommerce-assets/eal_test/new.png",
	}}
	merged := mergeLibraryGallery(existing, files)
	raw := string(merged.RawMessage())
	for _, want := range []string{"https://cdn.example.com/a.jpg", "/ecommerce-assets/eal_test/old.jpg", "/ecommerce-assets/eal_test/new.png"} {
		if !strings.Contains(raw, want) {
			t.Fatalf("merged gallery missing %q: %s", want, raw)
		}
	}
}
