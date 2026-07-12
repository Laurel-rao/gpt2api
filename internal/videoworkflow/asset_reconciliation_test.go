package videoworkflow

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

type ambiguousAssetStore struct {
	*fakeStore
	assets          map[string]*Asset
	versions        map[string]*AssetVersion
	commitThenError bool
	failCreate      bool
	lastAttempt     *AssetVersion
	softDeletes     int
}

func newAmbiguousAssetStore(t *testing.T) *ambiguousAssetStore {
	return &ambiguousAssetStore{fakeStore: newFakeStore(t), assets: map[string]*Asset{}, versions: map[string]*AssetVersion{}}
}

func (s *ambiguousAssetStore) CreateAsset(_ context.Context, asset *Asset) error {
	copy := *asset
	s.assets[asset.ID] = &copy
	return nil
}

func (s *ambiguousAssetStore) CreateAssetVersion(_ context.Context, version *AssetVersion) error {
	copy := *version
	if copy.Version == 0 {
		copy.Version = 1
		version.Version = 1
	}
	s.lastAttempt = &copy
	if !s.failCreate || s.commitThenError {
		s.versions[copy.ID] = &copy
		if asset := s.assets[copy.AssetID]; asset != nil {
			asset.Status, asset.CurrentVersionID = AssetReady, copy.ID
		}
	}
	if s.failCreate {
		return errors.New("create version acknowledgement lost")
	}
	return nil
}

func (s *ambiguousAssetStore) ListAssets(_ context.Context, userID uint64, kind string, _, _ int) ([]Asset, int64, error) {
	var out []Asset
	for _, asset := range s.assets {
		if asset.OwnerUserID == userID && (kind == "" || asset.Kind == kind) && asset.DeletedAt == nil {
			out = append(out, *asset)
		}
	}
	return out, int64(len(out)), nil
}

func (s *ambiguousAssetStore) GetAsset(_ context.Context, userID uint64, assetID string) (*Asset, error) {
	asset := s.assets[assetID]
	if asset == nil || asset.OwnerUserID != userID || asset.DeletedAt != nil {
		return nil, ErrNotFound
	}
	copy := *asset
	return &copy, nil
}

func (s *ambiguousAssetStore) GetAssetVersion(_ context.Context, userID uint64, versionID string) (*AssetVersion, error) {
	version := s.versions[versionID]
	if version == nil || version.OwnerUserID != userID || version.DeletedAt != nil {
		return nil, ErrNotFound
	}
	copy := *version
	return &copy, nil
}

func (s *ambiguousAssetStore) GetAssetVersionPublic(ctx context.Context, versionID string) (*AssetVersion, error) {
	for _, version := range s.versions {
		if version.ID == versionID {
			return s.GetAssetVersion(ctx, version.OwnerUserID, versionID)
		}
	}
	return nil, ErrNotFound
}

func (s *ambiguousAssetStore) UsedAssetBytes(_ context.Context, userID uint64) (int64, error) {
	var used int64
	for _, version := range s.versions {
		if version.OwnerUserID == userID && version.DeletedAt == nil {
			used += version.SizeBytes
		}
	}
	return used, nil
}

func (s *ambiguousAssetStore) SoftDeleteAsset(_ context.Context, userID uint64, assetID string) error {
	asset := s.assets[assetID]
	if asset == nil || asset.OwnerUserID != userID {
		return ErrNotFound
	}
	now := time.Now()
	asset.DeletedAt, asset.Status = &now, AssetDeleted
	s.softDeletes++
	return nil
}

func TestUploadAssetReconcilesCreateVersionAcknowledgement(t *testing.T) {
	for _, tt := range []struct {
		name            string
		commitThenError bool
		wantSuccess     bool
	}{
		{name: "committed", commitThenError: true, wantSuccess: true},
		{name: "not committed"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store := newAmbiguousAssetStore(t)
			store.failCreate, store.commitThenError = true, tt.commitThenError
			service := NewService(store)
			service.ConfigureMedia(t.TempDir(), "secret", nil)
			asset, err := service.UploadAsset(context.Background(), AssetUploadInput{
				UserID: 7, Kind: MediaKindImage, FileName: "upload.png", Reader: bytes.NewReader(makeRuntimePNG(t)),
			})
			if tt.wantSuccess {
				if err != nil || asset == nil || len(asset.Versions) != 1 {
					t.Fatalf("asset=%+v err=%v", asset, err)
				}
				if _, err := os.Stat(store.lastAttempt.FilePath); err != nil {
					t.Fatalf("committed upload was deleted: %v", err)
				}
				return
			}
			if err == nil || store.lastAttempt == nil || store.softDeletes != 1 {
				t.Fatalf("err=%v attempt=%+v softDeletes=%d", err, store.lastAttempt, store.softDeletes)
			}
			if _, statErr := os.Stat(store.lastAttempt.FilePath); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("uncommitted upload file remains: %v", statErr)
			}
		})
	}
}

func TestTransformAssetReconcilesCreateVersionAcknowledgement(t *testing.T) {
	for _, tt := range []struct {
		name            string
		commitThenError bool
		wantSuccess     bool
	}{
		{name: "committed", commitThenError: true, wantSuccess: true},
		{name: "not committed"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store := newAmbiguousAssetStore(t)
			root := t.TempDir()
			parentPath := root + "/parent.png"
			if err := os.WriteFile(parentPath, makeRuntimePNG(t), 0o640); err != nil {
				t.Fatal(err)
			}
			store.assets["asset"] = &Asset{ID: "asset", OwnerUserID: 7, Kind: MediaKindImage, Status: AssetReady, CurrentVersionID: "parent"}
			store.versions["parent"] = &AssetVersion{ID: "parent", AssetID: "asset", OwnerUserID: 7, Version: 1, Status: AssetReady,
				MIMEType: "image/png", FilePath: parentPath, StorageKey: "parent.png", SourceType: "upload"}
			store.failCreate, store.commitThenError = true, tt.commitThenError
			service := NewService(store)
			service.ConfigureMedia(root, "secret", nil)
			version, err := service.TransformAssetVersion(context.Background(), 7, "asset", "parent", ImageTransform{Rotation: 90})
			if tt.wantSuccess {
				if err != nil || version == nil {
					t.Fatalf("version=%+v err=%v", version, err)
				}
				if _, err := os.Stat(store.lastAttempt.FilePath); err != nil {
					t.Fatalf("committed transform was deleted: %v", err)
				}
				return
			}
			if err == nil || store.lastAttempt == nil {
				t.Fatalf("version=%+v err=%v", version, err)
			}
			if _, statErr := os.Stat(store.lastAttempt.FilePath); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("uncommitted transform file remains: %v", statErr)
			}
		})
	}
}
