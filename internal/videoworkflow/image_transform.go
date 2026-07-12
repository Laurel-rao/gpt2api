package videoworkflow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"time"
)

const maxDecodedImagePixels = 50_000_000

var ErrInvalidImageTransform = errors.New("videoworkflow: invalid image transform")

func (s *Service) TransformAssetVersion(ctx context.Context, userID uint64, assetID, versionID string, transform ImageTransform) (*AssetVersion, error) {
	store, err := s.assetStore()
	if err != nil {
		return nil, err
	}
	asset, err := store.GetAsset(ctx, userID, assetID)
	if err != nil || asset.Kind != MediaKindImage {
		return nil, ErrNotFound
	}
	parent, err := store.GetAssetVersion(ctx, userID, versionID)
	if err != nil || parent.AssetID != asset.ID || parent.Status != AssetReady {
		return nil, ErrNotFound
	}
	if err := validateImageTransform(transform); err != nil {
		return nil, err
	}
	file, err := os.Open(parent.FilePath)
	if err != nil {
		return nil, ErrNotFound
	}
	config, _, err := image.DecodeConfig(file)
	if err != nil || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > maxDecodedImagePixels {
		_ = file.Close()
		return nil, ErrUnsupportedMedia
	}
	if _, err := file.Seek(0, 0); err != nil {
		_ = file.Close()
		return nil, err
	}
	decoded, _, err := image.Decode(file)
	_ = file.Close()
	if err != nil {
		return nil, ErrUnsupportedMedia
	}
	bounds := decoded.Bounds()
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 || int64(bounds.Dx())*int64(bounds.Dy()) > maxDecodedImagePixels {
		return nil, ErrUnsupportedMedia
	}
	result := applyImageTransform(decoded, transform)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, result); err != nil {
		return nil, err
	}
	used, err := store.UsedAssetBytes(ctx, userID)
	if err != nil {
		return nil, err
	}
	root := s.assetRoot
	if root == "" {
		root = AssetRoot()
	}
	saved, err := SaveMedia(ctx, root, userID, MediaKindImage, "transform.png", bytes.NewReader(encoded.Bytes()), used)
	if err != nil {
		return nil, err
	}
	metadata, _ := json.Marshal(map[string]any{"image_transform": transform})
	version := &AssetVersion{
		ID: NewVersionID(), AssetID: assetID, OwnerUserID: userID, Status: AssetReady,
		MIMEType: "image/png", StorageKey: storageKey(root, saved.Path), FilePath: saved.Path,
		SizeBytes: saved.SizeBytes, SHA256: saved.SHA256, Width: result.Bounds().Dx(), Height: result.Bounds().Dy(),
		ParentVersionID: parent.ID, SourceType: "transform", Metadata: metadata,
	}
	if err := store.CreateAssetVersion(ctx, version); err != nil {
		originalErr := err
		committed, definitelyNotCommitted := reconcileCreatedAssetVersion(store, userID, assetID, version, false)
		if !committed {
			if definitelyNotCommitted {
				removeNewMedia(saved)
			}
			return nil, originalErr
		}
	}
	decorated := []AssetVersion{*version}
	s.decorateAssetVersions(decorated, time.Now())
	*version = decorated[0]
	return version, nil
}

func validateImageTransform(transform ImageTransform) error {
	if transform.Rotation != 0 && transform.Rotation != 90 && transform.Rotation != 180 && transform.Rotation != 270 {
		return fmt.Errorf("%w: rotation must be 0, 90, 180 or 270", ErrInvalidImageTransform)
	}
	crop := transform.Crop
	if crop.Width == 0 && crop.Height == 0 {
		return nil
	}
	values := []float64{crop.X, crop.Y, crop.Width, crop.Height}
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1 {
			return fmt.Errorf("%w: crop values must be within 0..1", ErrInvalidImageTransform)
		}
	}
	if crop.Width <= 0 || crop.Height <= 0 || crop.X+crop.Width > 1.0000001 || crop.Y+crop.Height > 1.0000001 {
		return fmt.Errorf("%w: invalid crop rectangle", ErrInvalidImageTransform)
	}
	return nil
}

func applyImageTransform(source image.Image, transform ImageTransform) *image.NRGBA {
	bounds := source.Bounds()
	crop := transform.Crop
	if crop.Width == 0 && crop.Height == 0 {
		crop = NormalizedCrop{Width: 1, Height: 1}
	}
	x0 := bounds.Min.X + int(math.Round(crop.X*float64(bounds.Dx())))
	y0 := bounds.Min.Y + int(math.Round(crop.Y*float64(bounds.Dy())))
	x1 := bounds.Min.X + int(math.Round((crop.X+crop.Width)*float64(bounds.Dx())))
	y1 := bounds.Min.Y + int(math.Round((crop.Y+crop.Height)*float64(bounds.Dy())))
	if x1 <= x0 {
		x1 = x0 + 1
	}
	if y1 <= y0 {
		y1 = y0 + 1
	}
	cropped := image.NewNRGBA(image.Rect(0, 0, x1-x0, y1-y0))
	draw.Draw(cropped, cropped.Bounds(), source, image.Point{X: x0, Y: y0}, draw.Src)
	flipped := image.NewNRGBA(cropped.Bounds())
	w, h := cropped.Bounds().Dx(), cropped.Bounds().Dy()
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			sx, sy := x, y
			if transform.FlipHorizontal {
				sx = w - 1 - x
			}
			if transform.FlipVertical {
				sy = h - 1 - y
			}
			flipped.Set(x, y, cropped.At(sx, sy))
		}
	}
	switch transform.Rotation {
	case 90:
		out := image.NewNRGBA(image.Rect(0, 0, h, w))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				out.Set(h-1-y, x, flipped.At(x, y))
			}
		}
		return out
	case 180:
		out := image.NewNRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				out.Set(w-1-x, h-1-y, flipped.At(x, y))
			}
		}
		return out
	case 270:
		out := image.NewNRGBA(image.Rect(0, 0, h, w))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				out.Set(y, w-1-x, flipped.At(x, y))
			}
		}
		return out
	default:
		return flipped
	}
}

func storageKey(root, path string) string {
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == "." || relative == ".." || len(relative) >= 3 && relative[:3] == "../" {
		return filepath.Base(path)
	}
	return filepath.ToSlash(relative)
}
