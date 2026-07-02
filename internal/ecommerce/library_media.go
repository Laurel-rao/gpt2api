package ecommerce

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	_ "golang.org/x/image/webp"
)

const maxLibraryAssetFileBytes = 20 * 1024 * 1024

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
