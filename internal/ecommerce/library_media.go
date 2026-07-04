package ecommerce

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "golang.org/x/image/webp"
)

const maxLibraryAssetFileBytes = 20 * 1024 * 1024
const maxLibraryVideoFileBytes = 300 * 1024 * 1024

var libraryVideoHTTPClient = http.DefaultClient

func LibraryAssetDir() string {
	if d := strings.TrimSpace(os.Getenv("GPT2API_ECOMMERCE_ASSET_DIR")); d != "" {
		return d
	}
	if wd, err := os.Getwd(); err == nil {
		return filepath.Join(wd, "data", "ecommerce-assets")
	}
	return "./data/ecommerce-assets"
}

type savedLibraryFile struct {
	URL        string
	SHA256     string
	MIME       string
	SizeBytes  int64
	Width      int
	Height     int
	OriginName string
}

func saveLibraryImage(assetID, originName string, r io.Reader) (*savedLibraryFile, error) {
	if strings.TrimSpace(assetID) == "" {
		return nil, fmt.Errorf("asset id required")
	}
	limited := io.LimitReader(r, maxLibraryAssetFileBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("empty file")
	}
	if len(data) > maxLibraryAssetFileBytes {
		return nil, fmt.Errorf("file too large: max 20MB")
	}
	mime := http.DetectContentType(data[:minInt(len(data), 512)])
	ext := libraryImageExt(mime, originName)
	if ext == "" {
		return nil, fmt.Errorf("unsupported file type")
	}
	width, height := decodeImageSize(data)
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	dir := filepath.Join(LibraryAssetDir(), assetID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	filename := hash + ext
	dst := filepath.Join(dir, filename)
	if _, err := os.Stat(dst); os.IsNotExist(err) {
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	return &savedLibraryFile{
		URL:        "/ecommerce-assets/" + assetID + "/" + filename,
		SHA256:     hash,
		MIME:       mime,
		SizeBytes:  int64(len(data)),
		Width:      width,
		Height:     height,
		OriginName: originName,
	}, nil
}

func SaveVideoFromURL(ctx context.Context, assetID, sourceURL string) (*savedLibraryFile, error) {
	assetID = sanitizeLibraryPathSegment(assetID)
	if assetID == "" {
		return nil, fmt.Errorf("asset id required")
	}
	sourceURL = strings.TrimSpace(sourceURL)
	if sourceURL == "" {
		return nil, fmt.Errorf("video url required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := libraryVideoHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("download video: http %d", resp.StatusCode)
	}
	if resp.ContentLength > maxLibraryVideoFileBytes {
		return nil, fmt.Errorf("video too large: max 300MB")
	}
	limited := io.LimitReader(resp.Body, maxLibraryVideoFileBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("empty video")
	}
	if len(data) > maxLibraryVideoFileBytes {
		return nil, fmt.Errorf("video too large: max 300MB")
	}
	ext := libraryVideoExt(resp.Header.Get("Content-Type"), sourceURL)
	if ext == "" {
		return nil, fmt.Errorf("unsupported video type")
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	dir := filepath.Join(LibraryAssetDir(), "videos", assetID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	filename := hash + ext
	dst := filepath.Join(dir, filename)
	if _, err := os.Stat(dst); os.IsNotExist(err) {
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	mimeType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if parsed, _, err := mime.ParseMediaType(mimeType); err == nil {
		mimeType = parsed
	}
	if mimeType == "" {
		mimeType = "video/mp4"
	}
	return &savedLibraryFile{
		URL:        "/ecommerce-assets/videos/" + assetID + "/" + filename,
		SHA256:     hash,
		MIME:       mimeType,
		SizeBytes:  int64(len(data)),
		OriginName: filename,
	}, nil
}

func libraryImageExt(mime, filename string) string {
	switch strings.ToLower(strings.TrimSpace(mime)) {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	}
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".jpg", ".jpeg":
		return ".jpg"
	case ".png", ".webp":
		return ext
	default:
		return ""
	}
}

func libraryVideoExt(contentType, sourceURL string) string {
	if mt, _, err := mime.ParseMediaType(strings.TrimSpace(contentType)); err == nil {
		switch strings.ToLower(mt) {
		case "video/mp4":
			return ".mp4"
		case "video/webm":
			return ".webm"
		case "video/quicktime":
			return ".mov"
		case "", "application/octet-stream", "binary/octet-stream":
		default:
			return ""
		}
	}
	if u, err := url.Parse(sourceURL); err == nil {
		switch strings.ToLower(filepath.Ext(u.Path)) {
		case ".mp4", ".webm", ".mov":
			return strings.ToLower(filepath.Ext(u.Path))
		}
	}
	return ".mp4"
}

func sanitizeLibraryPathSegment(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "._-")
}

func decodeImageSize(data []byte) (int, int) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0
	}
	return cfg.Width, cfg.Height
}

func rawJSONFromValue(v interface{}) RawJSON {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil || string(b) == "null" {
		return nil
	}
	return RawJSON(b)
}

func libraryGalleryFromFiles(files []LibraryAssetFile) RawJSON {
	items := make([]map[string]interface{}, 0, len(files))
	for _, f := range files {
		items = append(items, map[string]interface{}{
			"url":        f.URL,
			"usage":      f.FileUsage,
			"origin":     f.OriginName,
			"mime":       f.MIME,
			"size_bytes": f.SizeBytes,
			"width":      f.Width,
			"height":     f.Height,
			"sha256":     f.SHA256,
		})
	}
	return rawJSONFromValue(items)
}

func mergeLibraryGallery(existing RawJSON, files []LibraryAssetFile) RawJSON {
	items := make([]map[string]interface{}, 0, len(files))
	seen := map[string]struct{}{}
	var existingItems []map[string]interface{}
	_ = json.Unmarshal(existing.RawMessage(), &existingItems)
	for _, item := range existingItems {
		rawURL, _ := item["url"].(string)
		url := strings.TrimSpace(rawURL)
		if url == "" {
			continue
		}
		if _, ok := seen[url]; ok {
			continue
		}
		item["url"] = url
		items = append(items, item)
		seen[url] = struct{}{}
	}
	var fileItems []map[string]interface{}
	_ = json.Unmarshal(libraryGalleryFromFiles(files).RawMessage(), &fileItems)
	for _, item := range fileItems {
		rawURL, _ := item["url"].(string)
		url := strings.TrimSpace(rawURL)
		if url == "" {
			continue
		}
		if _, ok := seen[url]; ok {
			continue
		}
		item["url"] = url
		items = append(items, item)
		seen[url] = struct{}{}
	}
	return rawJSONFromValue(items)
}

func libraryReferenceImages(asset *LibraryAsset) []string {
	if asset == nil {
		return nil
	}
	out := make([]string, 0, 4)
	seen := map[string]struct{}{}
	add := func(u string) {
		u = strings.TrimSpace(u)
		if u == "" {
			return
		}
		if _, ok := seen[u]; ok {
			return
		}
		seen[u] = struct{}{}
		out = append(out, u)
	}
	add(asset.CoverURL)
	var gallery []struct {
		URL string `json:"url"`
	}
	_ = json.Unmarshal(asset.GalleryJSON.RawMessage(), &gallery)
	for _, item := range gallery {
		add(item.URL)
	}
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
