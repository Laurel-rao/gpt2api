package videoworkflow

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	_ "golang.org/x/image/webp"
	stdimage "image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type AssetStore interface {
	CreateAsset(context.Context, *Asset) error
	CreateAssetVersion(context.Context, *AssetVersion) error
	ListAssets(context.Context, uint64, string, int, int) ([]Asset, int64, error)
	GetAsset(context.Context, uint64, string) (*Asset, error)
	GetAssetVersion(context.Context, uint64, string) (*AssetVersion, error)
	GetAssetVersionPublic(context.Context, string) (*AssetVersion, error)
	UsedAssetBytes(context.Context, uint64) (int64, error)
	SoftDeleteAsset(context.Context, uint64, string) error
}

type AssetUploadInput struct {
	UserID   uint64
	Kind     string
	Name     string
	FileName string
	Reader   io.Reader
}

type SignedAssetVersion struct {
	VersionID string    `json:"version_id"`
	Purpose   string    `json:"purpose"`
	ExpiresAt time.Time `json:"expires_at"`
	URL       string    `json:"url"`
}

func (s *Service) ConfigureMedia(root, signingSecret string, composer *Composer) {
	root = strings.TrimSpace(root)
	if root == "" {
		root = AssetRoot()
	}
	s.assetRoot = root
	s.mediaSigner = NewMediaSigner(signingSecret)
	if composer == nil {
		composer = NewComposer()
	}
	s.composer = composer
}

func (s *Service) assetStore() (AssetStore, error) {
	store, ok := s.store.(AssetStore)
	if !ok {
		return nil, errors.New("videoworkflow: asset store is not configured")
	}
	return store, nil
}

func (s *Service) ListAssets(ctx context.Context, userID uint64, kind string, limit, offset int) ([]Asset, int64, error) {
	store, err := s.assetStore()
	if err != nil {
		return nil, 0, err
	}
	kind = strings.TrimSpace(kind)
	if kind != "" && maxBytesForKind(kind) == 0 {
		return nil, 0, ErrUnsupportedMedia
	}
	assets, total, err := store.ListAssets(ctx, userID, kind, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	for i := range assets {
		s.decorateAssetVersions(assets[i].Versions, time.Now())
	}
	return assets, total, nil
}

func (s *Service) UploadAsset(ctx context.Context, input AssetUploadInput) (*Asset, error) {
	store, err := s.assetStore()
	if err != nil {
		return nil, err
	}
	if input.UserID == 0 || input.Reader == nil {
		return nil, errors.New("videoworkflow: user and file are required")
	}
	if maxBytesForKind(input.Kind) == 0 || input.Kind == MediaKindStoryboard || input.Kind == MediaKindComposedVideo {
		return nil, ErrUnsupportedMedia
	}
	used, err := store.UsedAssetBytes(ctx, input.UserID)
	if err != nil {
		return nil, err
	}
	root := s.assetRoot
	if root == "" {
		root = AssetRoot()
	}
	saved, err := SaveMedia(ctx, root, input.UserID, input.Kind, input.FileName, input.Reader, used)
	if err != nil {
		return nil, err
	}
	width, height, durationMS, err := s.inspectUploadedMedia(ctx, input.Kind, saved.Path)
	if err != nil {
		removeNewMedia(saved)
		return nil, err
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = saved.OriginName
	}
	asset := &Asset{ID: NewAssetID(), OwnerUserID: input.UserID, Kind: input.Kind, Name: name, Status: AssetPending}
	if err := store.CreateAsset(ctx, asset); err != nil {
		removeNewMedia(saved)
		return nil, err
	}
	version := &AssetVersion{
		ID: NewVersionID(), AssetID: asset.ID, OwnerUserID: input.UserID, Status: AssetReady,
		MIMEType: saved.MIME, StorageKey: storageKey(root, saved.Path), FilePath: saved.Path, SizeBytes: saved.SizeBytes, SHA256: saved.SHA256, SourceType: "upload",
		Width: width, Height: height, DurationMS: durationMS,
	}
	s.writeVideoPreviewFrame(ctx, input.Kind, saved.Path)
	if err := store.CreateAssetVersion(ctx, version); err != nil {
		originalErr := err
		committed, definitelyNotCommitted := reconcileCreatedAssetVersion(store, input.UserID, asset.ID, version, true)
		if !committed {
			if definitelyNotCommitted {
				_ = store.SoftDeleteAsset(context.Background(), input.UserID, asset.ID)
				removeNewMedia(saved)
			}
			return nil, originalErr
		}
	}
	asset.Status = AssetReady
	asset.CurrentVersionID = version.ID
	asset.Versions = []AssetVersion{*version}
	s.decorateAssetVersions(asset.Versions, time.Now())
	return asset, nil
}

func reconcileCreatedAssetVersion(store AssetStore, userID uint64, assetID string, expected *AssetVersion, requireCurrent bool) (bool, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), RuntimeBillingTimeout)
	defer cancel()
	actual, err := store.GetAssetVersion(ctx, userID, expected.ID)
	if errors.Is(err, ErrNotFound) {
		return false, true
	}
	if err != nil || !assetVersionsExactlyMatch(actual, expected) {
		return false, false
	}
	if requireCurrent {
		asset, err := store.GetAsset(ctx, userID, assetID)
		if err != nil || asset.CurrentVersionID != expected.ID || asset.Status != AssetReady {
			return false, false
		}
	}
	return true, false
}

func assetVersionsExactlyMatch(actual, expected *AssetVersion) bool {
	if actual == nil || expected == nil {
		return false
	}
	return actual.ID == expected.ID && actual.AssetID == expected.AssetID && actual.OwnerUserID == expected.OwnerUserID &&
		(expected.Version == 0 || actual.Version == expected.Version) && actual.Status == expected.Status && actual.MIMEType == expected.MIMEType &&
		actual.StorageKey == expected.StorageKey && actual.FilePath == expected.FilePath && actual.ParentVersionID == expected.ParentVersionID &&
		actual.SourceType == expected.SourceType && bytes.Equal(normalizeJSON(actual.Metadata), normalizeJSON(expected.Metadata)) &&
		actual.CreatedByRunID == expected.CreatedByRunID && actual.CreatedByNodeRunID == expected.CreatedByNodeRunID &&
		actual.SizeBytes == expected.SizeBytes && actual.SHA256 == expected.SHA256 && actual.Width == expected.Width &&
		actual.Height == expected.Height && actual.DurationMS == expected.DurationMS && actual.InputHash == expected.InputHash
}

func (s *Service) decorateAssetVersions(versions []AssetVersion, now time.Time) {
	if s.mediaSigner == nil {
		return
	}
	expires := now.Add(PreviewSignTTL)
	for i := range versions {
		if versions[i].Status != AssetReady {
			continue
		}
		// 视频封面依赖 sidecar；无封面时不暴露 preview_url，避免前端把视频当图片加载。
		if isVideoMIME(versions[i].MIMEType) && !videoPreviewSidecarExists(versions[i].FilePath) {
			continue
		}
		signature, err := s.mediaSigner.Sign(versions[i].ID, MediaPurposePreview, expires)
		if err == nil {
			versions[i].PreviewURL = SignedMediaPath(versions[i].ID, MediaPurposePreview, expires, signature)
		}
	}
}

func removeNewMedia(saved *SavedMedia) {
	// 每个 SaveMedia 调用持有独立普通文件：首个请求持有 canonical，复用者
	// 持有唯一文件；删除本请求路径不会影响并发请求已经写入数据库的版本。
	if saved != nil && strings.TrimSpace(saved.Path) != "" {
		_ = os.Remove(saved.Path)
		_ = os.Remove(videoPreviewSidecarPath(saved.Path))
	}
}

func videoPreviewSidecarPath(videoPath string) string {
	return videoPath + ".preview.jpg"
}

func videoPreviewSidecarExists(videoPath string) bool {
	info, err := os.Stat(videoPreviewSidecarPath(videoPath))
	return err == nil && info.Size() > 0
}

func isVideoMIME(mime string) bool {
	return strings.HasPrefix(strings.TrimSpace(mime), "video/")
}

func kindNeedsVideoPreview(kind string) bool {
	return kind == MediaKindVideo || kind == MediaKindComposedVideo
}

// writeVideoPreviewFrame 尝试抽取视频封面；失败只记日志，不阻断上传/生成。
func (s *Service) writeVideoPreviewFrame(ctx context.Context, kind, videoPath string) {
	if !kindNeedsVideoPreview(kind) || strings.TrimSpace(videoPath) == "" {
		return
	}
	composer := s.composer
	if composer == nil {
		composer = NewComposer()
	}
	previewPath := videoPreviewSidecarPath(videoPath)
	if err := composer.ExtractPreviewFrame(ctx, videoPath, previewPath); err != nil {
		log.Printf("videoworkflow: extract video preview failed path=%s: %v", videoPath, err)
		_ = os.Remove(previewPath)
	}
}

func (s *Service) inspectUploadedMedia(ctx context.Context, kind, path string) (int, int, int64, error) {
	if kind == MediaKindImage {
		file, err := os.Open(path)
		if err != nil {
			return 0, 0, 0, err
		}
		defer file.Close()
		config, _, err := stdimage.DecodeConfig(file)
		if err != nil || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > maxDecodedImagePixels {
			return 0, 0, 0, ErrUnsupportedMedia
		}
		return config.Width, config.Height, 0, nil
	}
	composer := s.composer
	if composer == nil {
		composer = NewComposer()
	}
	if !composer.Available() {
		return 0, 0, 0, ErrFFmpegUnavailable
	}
	probe, err := composer.probe(ctx, path)
	if err != nil || !hasStream(probe, "video") {
		return 0, 0, 0, ErrUnsupportedMedia
	}
	width, height := 0, 0
	for _, stream := range probe.Streams {
		if stream.CodecType == "video" {
			width, height = stream.Width, stream.Height
			break
		}
	}
	duration, err := probeDurationMS(probe)
	if err != nil || duration <= 0 {
		return 0, 0, 0, ErrUnsupportedMedia
	}
	return width, height, duration, nil
}

func (s *Service) DeleteAsset(ctx context.Context, userID uint64, assetID string) error {
	store, err := s.assetStore()
	if err != nil {
		return err
	}
	return store.SoftDeleteAsset(ctx, userID, assetID)
}

func (s *Service) SignAssetVersion(ctx context.Context, userID uint64, assetID, versionID, purpose string, now time.Time) (*SignedAssetVersion, error) {
	store, err := s.assetStore()
	if err != nil {
		return nil, err
	}
	asset, err := store.GetAsset(ctx, userID, assetID)
	if err != nil {
		return nil, err
	}
	version, err := store.GetAssetVersion(ctx, userID, versionID)
	if err != nil || version.AssetID != asset.ID || version.Status != AssetReady {
		return nil, ErrNotFound
	}
	if purpose == "" {
		purpose = MediaPurposePreview
	}
	ttl := PreviewSignTTL
	switch purpose {
	case MediaPurposePreview:
	case MediaPurposeDownload:
		ttl = DownloadSignTTL
	case MediaPurposeSeedance:
		ttl = SeedanceSignTTL
	default:
		return nil, ErrInvalidMediaPurpose
	}
	signer := s.mediaSigner
	if signer == nil {
		return nil, errors.New("videoworkflow: media signer is not configured")
	}
	expires := now.Add(ttl)
	signature, err := signer.Sign(version.ID, purpose, expires)
	if err != nil {
		return nil, err
	}
	return &SignedAssetVersion{VersionID: version.ID, Purpose: purpose, ExpiresAt: expires, URL: SignedMediaPath(version.ID, purpose, expires, signature)}, nil
}

func (s *Service) OpenSignedMedia(ctx context.Context, versionID, purpose string, expires int64, signature string, now time.Time) (*AssetVersion, *os.File, error) {
	if s.mediaSigner == nil || s.mediaSigner.Verify(versionID, purpose, expires, signature, now) != nil {
		return nil, nil, ErrNotFound
	}
	store, err := s.assetStore()
	if err != nil {
		return nil, nil, err
	}
	version, err := store.GetAssetVersionPublic(ctx, versionID)
	if err != nil {
		return nil, nil, ErrNotFound
	}
	clean := filepath.Clean(version.FilePath)
	root := filepath.Clean(s.assetRoot)
	if root == "." || (clean != root && !strings.HasPrefix(clean, root+string(os.PathSeparator))) {
		return nil, nil, ErrNotFound
	}
	out := *version
	openPath := clean
	if purpose == MediaPurposePreview {
		preview := filepath.Clean(videoPreviewSidecarPath(clean))
		if preview != root && strings.HasPrefix(preview, root+string(os.PathSeparator)) && videoPreviewSidecarExists(clean) {
			openPath = preview
			out.FilePath = preview
			out.MIMEType = "image/jpeg"
		}
	}
	file, err := os.Open(openPath)
	if err != nil {
		return nil, nil, ErrNotFound
	}
	return &out, file, nil
}

func probeDurationMS(probe mediaProbe) (int64, error) {
	var seconds float64
	if _, err := fmt.Sscanf(probe.Format.Duration, "%f", &seconds); err != nil {
		return 0, err
	}
	return int64(seconds*1000 + 0.5), nil
}
