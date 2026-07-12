package videoworkflow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestInputHash_LayoutIgnored(t *testing.T) {
	g := testGraph()
	g.Nodes[0].Config = []byte(`{"prompt":"same","nested":{"b":2,"a":1}}`)
	want, err := InputHash(g, "a", []AssetVersionRef{{NodeID: "up", VersionID: "v1"}})
	if err != nil {
		t.Fatal(err)
	}
	g.Nodes[0].Position = Position{X: 999, Y: 500}
	g.Nodes[0].Size = Size{Width: 300, Height: 100}
	g.Nodes[0].Collapsed = true
	g.Nodes[0].Config = []byte(`{ "nested": {"a":1, "b":2}, "prompt":"same" }`)
	got, err := InputHash(g, "a", []AssetVersionRef{{NodeID: "up", VersionID: "v1"}})
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("layout-only hash changed: %s != %s", got, want)
	}
}

func TestInputHash_SemanticChanges(t *testing.T) {
	base := testGraph()
	base.Nodes[0].Config = []byte(`{"prompt":"one"}`)
	upstream := []AssetVersionRef{{NodeID: "up", VersionID: "v1"}}
	want, _ := InputHash(base, "a", upstream)
	tests := map[string]func(*Graph, *[]AssetVersionRef){
		"prompt":       func(g *Graph, _ *[]AssetVersionRef) { g.Nodes[0].Config = []byte(`{"prompt":"two"}`) },
		"node version": func(g *Graph, _ *[]AssetVersionRef) { g.Nodes[0].Version = 2 },
		"model":        func(g *Graph, _ *[]AssetVersionRef) { g.Settings.TextModel = "other" },
		"role":         func(g *Graph, _ *[]AssetVersionRef) { g.Nodes[0].RoleID = "new-role" },
		"bound version": func(g *Graph, _ *[]AssetVersionRef) {
			g.Nodes[0].AssetID, g.Nodes[0].AssetVersionID = "asset", "version-2"
		},
		"asset version": func(_ *Graph, refs *[]AssetVersionRef) { (*refs)[0].VersionID = "v2" },
		"asset order": func(_ *Graph, refs *[]AssetVersionRef) {
			*refs = []AssetVersionRef{{NodeID: "two", VersionID: "v2"}, {NodeID: "one", VersionID: "v1"}}
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			g := base
			g.Nodes = append([]Node(nil), base.Nodes...)
			refs := append([]AssetVersionRef(nil), upstream...)
			mutate(&g, &refs)
			got, err := InputHash(g, "a", refs)
			if err != nil {
				t.Fatal(err)
			}
			if got == want {
				t.Fatal("semantic change did not change hash")
			}
		})
	}
}

func TestInputHash_NodeNotFound(t *testing.T) {
	if _, err := InputHash(testGraph(), "missing", nil); err == nil {
		t.Fatal("expected missing node error")
	}
}

func TestInputHash_InvalidConfigRemainsHashable(t *testing.T) {
	g := testGraph()
	g.Nodes[0].Config = []byte("not-json")
	if hash, err := InputHash(g, "a", nil); err != nil || len(hash) != 64 {
		t.Fatalf("hash=%q err=%v", hash, err)
	}
}

func TestVideoProviderSnapshotChangesInputHashAndCacheIdentity(t *testing.T) {
	graph := blankVideoCanvasTemplate().Graph
	upstream := []AssetVersionRef{{NodeID: "background_1", VersionID: "background-version"}, {NodeID: "scene_1", VersionID: "scene-version"}}
	fast := json.RawMessage(`{"channel_type":"apiyi_seedance2","base_url":"https://api.apiyi.com","model":"doubao-seedance-2-0-fast-260128","timeout_sec":1800,"duration_sec":15,"aspect_ratio":"9:16","resolution":"1080p","generate_audio":false}`)
	standard := json.RawMessage(`{"channel_type":"apiyi_seedance2","base_url":"https://api.apiyi.com","model":"doubao-seedance-2-0-260128","timeout_sec":1800,"duration_sec":15,"aspect_ratio":"9:16","resolution":"1080p","generate_audio":false}`)
	fastHash, err := InputHashWithProviderSnapshot(graph, "video_1", upstream, fast)
	if err != nil {
		t.Fatal(err)
	}
	standardHash, err := InputHashWithProviderSnapshot(graph, "video_1", upstream, standard)
	if err != nil {
		t.Fatal(err)
	}
	if fastHash == standardHash {
		t.Fatal("fast and standard provider snapshots produced the same input hash")
	}
	equivalentFast := json.RawMessage(`{"resolution":"1080P","model":"doubao-seedance-2-0-fast-260128","base_url":"https://api.apiyi.com/","channel_type":" APIYI_SEEDANCE2 ","duration_sec":15,"timeout_sec":1800,"aspect_ratio":"9:16","generate_audio":false,"api_key":"must-not-be-hashed"}`)
	equivalentHash, err := InputHashWithProviderSnapshot(graph, "video_1", upstream, equivalentFast)
	if err != nil {
		t.Fatal(err)
	}
	if equivalentHash != fastHash {
		t.Fatalf("equivalent snapshot hash=%s want=%s", equivalentHash, fastHash)
	}
	normalized, err := normalizeVideoProviderSnapshot(equivalentFast)
	if err != nil || bytes.Contains(normalized, []byte("api_key")) || bytes.Contains(normalized, []byte("must-not-be-hashed")) {
		t.Fatalf("normalized provider snapshot=%s err=%v", normalized, err)
	}

	store := newMemoryRuntimeStore()
	store.versions["fast-version"] = &AssetVersion{ID: "fast-version", OwnerUserID: 7, Status: AssetReady, InputHash: fastHash}
	store.cache[fastHash] = "fast-version"
	if _, err := store.FindCachedAssetVersion(context.Background(), 7, equivalentHash); err != nil {
		t.Fatalf("same provider snapshot missed cache: %v", err)
	}
	if _, err := store.FindCachedAssetVersion(context.Background(), 7, standardHash); !errors.Is(err, ErrNotFound) {
		t.Fatalf("standard provider snapshot reused fast cache: %v", err)
	}
}
