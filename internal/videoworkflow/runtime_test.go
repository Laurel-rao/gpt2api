package videoworkflow

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRuntimeEstimateTokenAndSelection(t *testing.T) {
	store := newMemoryRuntimeStore()
	runtime, err := NewRuntime(RuntimeConfig{Store: store, EstimateSecret: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	graph := blankVideoCanvasTemplate().Graph
	estimate, err := runtime.EstimateFor(context.Background(), 7, 12, graph, RunModeNodeOnly, "background_1")
	if err != nil {
		t.Fatal(err)
	}
	if estimate.TotalCredits != 1 || estimate.Hash == "" || estimate.Token == "" {
		t.Fatalf("estimate=%+v", estimate)
	}
	if _, err := runtime.ValidateEstimate(context.Background(), 7, 12, graph, RunModeNodeOnly, "background_1", estimate.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.ValidateEstimate(context.Background(), 7, 13, graph, RunModeNodeOnly, "background_1", estimate.Token); !errors.Is(err, ErrInvalidEstimate) {
		t.Fatalf("revision mismatch error=%v", err)
	}
	graph.Settings.Resolution = Resolution1080p
	if _, err := runtime.ValidateEstimate(context.Background(), 7, 12, graph, RunModeNodeOnly, "background_1", estimate.Token); !errors.Is(err, ErrInvalidEstimate) {
		t.Fatalf("mutated graph token error=%v", err)
	}
	parts, err := selectedNodeIDs(blankVideoCanvasTemplate().Graph, RunModeNodeOnly, "background_1")
	if err != nil || len(parts) != 3 || !parts["brief"] || !parts["scene_1"] || !parts["background_1"] {
		t.Fatalf("node_only selection=%v err=%v", parts, err)
	}
}

func TestRuntimeReadinessLifecycle(t *testing.T) {
	runtime, err := NewRuntime(RuntimeConfig{Store: newMemoryRuntimeStore(), EstimateSecret: "secret", WorkerConcurrency: 2})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Ready() == nil {
		t.Fatal("runtime was ready before Start")
	}
	runtime.Start()
	if err := runtime.Ready(); err != nil {
		t.Fatal(err)
	}
	runtime.Close()
	if runtime.Ready() == nil {
		t.Fatal("runtime remained ready after Close")
	}
}

func TestRuntimeRejectsLocalhostPublicBaseURL(t *testing.T) {
	for _, value := range []string{"http://localhost:8080", "https://assets.localhost", "file:///tmp/media", "https://media.example.com/api", "https://media.example.com?sig=secret"} {
		if _, err := NewRuntime(RuntimeConfig{Store: newMemoryRuntimeStore(), EstimateSecret: "secret", PublicBaseURL: value}); err == nil {
			t.Fatalf("invalid public base URL accepted: %s", value)
		}
	}
}

func TestRuntimeSelectionSkipsDisabledSceneAndTimelineClip(t *testing.T) {
	templates, err := BuiltinTemplates()
	if err != nil {
		t.Fatal(err)
	}
	graph := templates[0].Graph
	graph.Groups[1].Enabled = false
	selected, err := selectedNodeIDs(graph, RunModeFull, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range graph.Groups[1].NodeIDs {
		if selected[id] {
			t.Fatalf("disabled scene node %s remained selected", id)
		}
	}
	if !selected["timeline"] || !selected["compose"] || countSelectedType(graph, selected, NodeVideo) != 3 {
		t.Fatalf("selection=%v", selected)
	}
	config, _ := json.Marshal(TimelineConfig{Clips: []TimelineClip{
		{ID: "clip-1", SourceNodeID: "video-1", SourcePort: "video", TrimOutMS: SceneDurationMS},
		{ID: "clip-2", SourceNodeID: "video-2", SourcePort: "video", TrimOutMS: SceneDurationMS},
	}})
	node := Node{ID: "timeline", Type: NodeTimeline, Config: config}
	result, err := executeTimelineNode(node, []Edge{{Source: "video-1"}}, map[string]runtimeNodeOutput{"video-1": {VersionID: "version-1"}})
	if err != nil || !bytes.Contains(result.Output, []byte("version-1")) || bytes.Contains(result.Output, []byte("video-2")) {
		t.Fatalf("timeline output=%s err=%v", result.Output, err)
	}
	blank := blankVideoCanvasTemplate().Graph
	disabled := false
	blank.Nodes[findNodeIndex(blank, "background_1")].Enabled = &disabled
	if _, err := selectedNodeIDs(blank, RunModeFull, ""); err == nil {
		t.Fatal("full run accepted graph with no enabled video scene")
	}
}

func TestRuntimeDAGImageGenerationAndCache(t *testing.T) {
	store := newMemoryRuntimeStore()
	imageGenerator := &fakeRuntimeImageGenerator{data: makeRuntimePNG(t)}
	runtime, err := NewRuntime(RuntimeConfig{
		Store: store, EstimateSecret: "secret", AssetRoot: t.TempDir(), ImageGenerator: imageGenerator,
	})
	if err != nil {
		t.Fatal(err)
	}
	graph := blankVideoCanvasTemplate().Graph
	first := &Run{ID: "run-1", UserID: 7, WorkflowID: "workflow", RequestID: "request-1", RunMode: RunModeNodeOnly, StartNodeID: "background_1", GraphSnapshot: graph, Status: RunRunning}
	store.runs[first.ID] = first
	if err := runtime.executeRun(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if first.Status != RunSucceeded || imageGenerator.calls != 1 {
		t.Fatalf("first status=%s calls=%d", first.Status, imageGenerator.calls)
	}
	background := store.nodeByRunAndNode("run-1", "background_1")
	if background == nil || background.OutputVersionID == "" || background.Status != NodeRunSucceeded {
		t.Fatalf("background node=%+v", background)
	}

	second := &Run{ID: "run-2", UserID: 7, WorkflowID: "workflow", RequestID: "request-2", RunMode: RunModeNodeOnly, StartNodeID: "background_1", GraphSnapshot: graph, Status: RunRunning}
	store.runs[second.ID] = second
	if err := runtime.executeRun(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	cached := store.nodeByRunAndNode("run-2", "background_1")
	if imageGenerator.calls != 1 || cached == nil || !cached.CacheHit || cached.CreditCost != 0 {
		t.Fatalf("cached calls=%d node=%+v", imageGenerator.calls, cached)
	}
}

func TestRuntimeCharacterApprovalUsesInputHashAndCandidate(t *testing.T) {
	templates, err := BuiltinTemplates()
	if err != nil {
		t.Fatal(err)
	}
	store := newMemoryRuntimeStore()
	imageGenerator := &fakeRuntimeImageGenerator{data: makeRuntimePNG(t)}
	runtime, err := NewRuntime(RuntimeConfig{Store: store, EstimateSecret: "secret", AssetRoot: t.TempDir(), ImageGenerator: imageGenerator})
	if err != nil {
		t.Fatal(err)
	}
	run := &Run{ID: "approval-run", UserID: 7, WorkflowID: "workflow", RequestID: "approval-request", RunMode: RunModeNodeOnly,
		StartNodeID: "role_heroine", GraphSnapshot: templates[0].Graph, Status: RunRunning}
	store.runs[run.ID] = run
	err = runtime.executeRun(context.Background(), run)
	if !errors.Is(err, errAwaitingApproval) || run.Status != RunAwaitingCharacterApproval {
		t.Fatalf("execute error=%v status=%s", err, run.Status)
	}
	node := store.nodeByRunAndNode(run.ID, "role_heroine")
	ids := outputVersionIDs(node.Output)
	if node == nil || node.Status != NodeRunAwaitingApproval || len(ids) != 2 {
		t.Fatalf("approval node=%+v candidates=%v", node, ids)
	}
	err = runtime.ApproveCharacters(context.Background(), run, CharacterApproval{Selections: []CharacterSelection{{
		NodeRunID: node.ID, InputHash: node.InputHash, SelectedVersionID: ids[0],
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if node = store.nodeByRunAndNode(run.ID, "role_heroine"); node.Status != NodeRunSucceeded || node.OutputVersionID != ids[0] || run.Status != RunRunning {
		t.Fatalf("approved run=%s node=%+v", run.Status, node)
	}
	store.mu.Lock()
	selectedAssetID := store.versions[ids[1]].AssetID
	currentVersionID := store.assets[selectedAssetID].CurrentVersionID
	store.runs[run.ID].Status = RunAwaitingStoryboardApproval
	run.Status = RunAwaitingStoryboardApproval
	store.mu.Unlock()
	if currentVersionID != ids[0] {
		t.Fatalf("generated candidate current version=%s want initially selected=%s", currentVersionID, ids[0])
	}
	if err := store.CreateAssetVersion(context.Background(), &AssetVersion{
		ID: "post-approval-transform", AssetID: selectedAssetID, OwnerUserID: run.UserID, Status: AssetReady,
		MIMEType: "image/png", FilePath: filepath.Join(t.TempDir(), "transform.png"), SourceType: "transform",
	}); err != nil {
		t.Fatal(err)
	}
	cachedVersion, err := store.FindCachedAssetVersion(context.Background(), run.UserID, node.InputHash)
	if err != nil || cachedVersion.ID != ids[0] {
		t.Fatalf("cache followed mutable asset current: cached=%+v err=%v", cachedVersion, err)
	}
	// 首次响应丢失后运行可能已推进到下一审批阶段；同一选择必须幂等成功。
	selection := CharacterApproval{Selections: []CharacterSelection{{NodeRunID: node.ID, InputHash: node.InputHash, SelectedVersionID: ids[0]}}}
	if err := runtime.ApproveCharacters(context.Background(), run, selection); err != nil {
		t.Fatalf("progressed idempotent approval: %v", err)
	}
	selection.Selections[0].SelectedVersionID = ids[1]
	if err := runtime.ApproveCharacters(context.Background(), run, selection); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("changed approval retry error=%v", err)
	}
}

func TestRuntimeNodeCommitFailureCleansMediaAndPreservesProviderCost(t *testing.T) {
	base := newMemoryRuntimeStore()
	store := &failNodeResultCommitStore{memoryRuntimeStore: base, failMediaCommit: true}
	root := t.TempDir()
	runtime, err := NewRuntime(RuntimeConfig{Store: store, WorkerID: "worker", EstimateSecret: "secret", AssetRoot: root,
		ImageGenerator: &fakeRuntimeImageGenerator{data: makeRuntimePNG(t)}})
	if err != nil {
		t.Fatal(err)
	}
	run := &Run{ID: "media-commit-failure", UserID: 7, WorkflowID: "workflow", RunMode: RunModeNodeOnly,
		StartNodeID: "background_1", GraphSnapshot: blankVideoCanvasTemplate().Graph, Status: RunRunning}
	base.runs[run.ID] = run
	if err := runtime.executeRun(context.Background(), run); err == nil {
		t.Fatal("media commit failure was ignored")
	}
	node := base.nodeByRunAndNode(run.ID, "background_1")
	if node == nil || node.CreditCost != runtime.config.ImageCredits || len(base.versions) != 0 || len(base.assets) != 0 {
		t.Fatalf("node=%+v assets=%d versions=%d", node, len(base.assets), len(base.versions))
	}
	files := 0
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			files++
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if files != 0 {
		t.Fatalf("uncommitted media files=%d", files)
	}
}

func TestRuntimeNodeCommitResponseErrorReconcilesAndKeepsReferencedMedia(t *testing.T) {
	base := newMemoryRuntimeStore()
	store := &failNodeResultCommitStore{memoryRuntimeStore: base, failMediaCommit: true, commitThenError: true}
	root := t.TempDir()
	runtime, err := NewRuntime(RuntimeConfig{Store: store, WorkerID: "worker", EstimateSecret: "secret", AssetRoot: root,
		ImageGenerator: &fakeRuntimeImageGenerator{data: makeRuntimePNG(t)}})
	if err != nil {
		t.Fatal(err)
	}
	run := &Run{ID: "media-commit-ambiguous", UserID: 7, WorkflowID: "workflow", RunMode: RunModeNodeOnly,
		StartNodeID: "background_1", GraphSnapshot: blankVideoCanvasTemplate().Graph, Status: RunRunning}
	base.runs[run.ID] = run
	if err := runtime.executeRun(context.Background(), run); err != nil {
		t.Fatalf("committed response error was not reconciled: %v", err)
	}
	node := base.nodeByRunAndNode(run.ID, "background_1")
	if run.Status != RunSucceeded || node == nil || node.Status != NodeRunSucceeded || node.OutputVersionID == "" {
		t.Fatalf("run=%s node=%+v", run.Status, node)
	}
	version, err := base.GetAssetVersion(context.Background(), run.UserID, node.OutputVersionID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(version.FilePath); err != nil {
		t.Fatalf("committed media was deleted: %v", err)
	}
}

func TestRuntimeNodeCommitUnknownReconciliationNeverFinalizesFailed(t *testing.T) {
	base := newMemoryRuntimeStore()
	store := &failNodeResultCommitStore{memoryRuntimeStore: base, failMediaCommit: true, commitThenError: true, failGetNodeRuns: 1}
	runtime, err := NewRuntime(RuntimeConfig{Store: store, WorkerID: "worker", EstimateSecret: "secret", AssetRoot: t.TempDir(),
		ImageGenerator: &fakeRuntimeImageGenerator{data: makeRuntimePNG(t)}})
	if err != nil {
		t.Fatal(err)
	}
	run := &Run{ID: "media-commit-unknown", UserID: 7, RunMode: RunModeNodeOnly, StartNodeID: "background_1", GraphSnapshot: blankVideoCanvasTemplate().Graph, Status: RunRunning}
	base.runs[run.ID] = run
	runtime.executeClaimed(run)
	node := base.nodeByRunAndNode(run.ID, "background_1")
	if run.Status != RunRunning || node == nil || node.Status != NodeRunSucceeded {
		t.Fatalf("run=%s node=%+v", run.Status, node)
	}
	runtime.executeClaimed(run)
	if run.Status != RunSucceeded {
		t.Fatalf("recovered status=%s", run.Status)
	}
}

func TestRuntimeCachedNodeCommitResponseErrorReconciles(t *testing.T) {
	base := newMemoryRuntimeStore()
	store := &failNodeResultCommitStore{memoryRuntimeStore: base}
	generator := &fakeRuntimeImageGenerator{data: makeRuntimePNG(t)}
	runtime, err := NewRuntime(RuntimeConfig{Store: store, WorkerID: "worker", EstimateSecret: "secret", AssetRoot: t.TempDir(), ImageGenerator: generator})
	if err != nil {
		t.Fatal(err)
	}
	graph := blankVideoCanvasTemplate().Graph
	first := &Run{ID: "cache-source", UserID: 7, RunMode: RunModeNodeOnly, StartNodeID: "background_1", GraphSnapshot: graph, Status: RunRunning}
	base.runs[first.ID] = first
	if err := runtime.executeRun(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	store.failCachedCommit, store.commitThenError = true, true
	second := &Run{ID: "cache-ambiguous", UserID: 7, RunMode: RunModeNodeOnly, StartNodeID: "background_1", GraphSnapshot: graph, Status: RunRunning}
	base.runs[second.ID] = second
	if err := runtime.executeRun(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	node := base.nodeByRunAndNode(second.ID, "background_1")
	if second.Status != RunSucceeded || node == nil || node.Status != NodeRunSucceeded || !node.CacheHit || generator.calls != 1 {
		t.Fatalf("run=%s node=%+v generator_calls=%d", second.Status, node, generator.calls)
	}
}

func TestRuntimeFencedProviderPhasesRecoverWithoutDuplicateSubmission(t *testing.T) {
	t.Run("start commit acknowledgement", func(t *testing.T) {
		base := newMemoryRuntimeStore()
		store := &ambiguousFencedStore{memoryRuntimeStore: base, failStart: true, startCommitThenError: true}
		generator := &fakeRuntimeImageGenerator{data: makeRuntimePNG(t)}
		runtime, err := NewRuntime(RuntimeConfig{Store: store, WorkerID: "worker", EstimateSecret: "secret", AssetRoot: t.TempDir(), ImageGenerator: generator})
		if err != nil {
			t.Fatal(err)
		}
		run := &Run{ID: "start-ack", UserID: 7, RunMode: RunModeNodeOnly, StartNodeID: "background_1", GraphSnapshot: blankVideoCanvasTemplate().Graph, Status: RunRunning}
		base.runs[run.ID] = run
		if err := runtime.executeRun(context.Background(), run); err != nil {
			t.Fatal(err)
		}
		if run.Status != RunSucceeded || generator.calls != 1 {
			t.Fatalf("run=%s calls=%d", run.Status, generator.calls)
		}
	})

	t.Run("not submitted recovery", func(t *testing.T) {
		base := newMemoryRuntimeStore()
		store := &ambiguousFencedStore{memoryRuntimeStore: base, failSubmitting: true}
		generator := &fakeRuntimeImageGenerator{data: makeRuntimePNG(t)}
		runtime, err := NewRuntime(RuntimeConfig{Store: store, WorkerID: "worker", EstimateSecret: "secret", AssetRoot: t.TempDir(), ImageGenerator: generator})
		if err != nil {
			t.Fatal(err)
		}
		run := &Run{ID: "not-submitted-recovery", UserID: 7, RunMode: RunModeNodeOnly, StartNodeID: "background_1", GraphSnapshot: blankVideoCanvasTemplate().Graph, Status: RunRunning}
		base.runs[run.ID] = run
		err = runtime.executeRun(context.Background(), run)
		if !errors.Is(err, errRecoverableRuntimePersistence) || generator.calls != 0 {
			t.Fatalf("first err=%v calls=%d", err, generator.calls)
		}
		node := base.nodeByRunAndNode(run.ID, "background_1")
		if node == nil || node.Status != NodeRunRunning || providerStatePhase(node.ProviderState) != "not_submitted" {
			t.Fatalf("node=%+v phase=%s", node, providerStatePhase(node.ProviderState))
		}
		if err := runtime.executeRun(context.Background(), run); err != nil {
			t.Fatal(err)
		}
		if generator.calls != 1 || run.Status != RunSucceeded {
			t.Fatalf("recovery calls=%d run=%s", generator.calls, run.Status)
		}
	})

	t.Run("submitting without task", func(t *testing.T) {
		base := newMemoryRuntimeStore()
		store := &ambiguousFencedStore{memoryRuntimeStore: base, failSubmitting: true}
		generator := &fakeRuntimeImageGenerator{data: makeRuntimePNG(t)}
		runtime, err := NewRuntime(RuntimeConfig{Store: store, WorkerID: "worker", EstimateSecret: "secret", AssetRoot: t.TempDir(), ImageGenerator: generator})
		if err != nil {
			t.Fatal(err)
		}
		run := &Run{ID: "submitting-no-task", UserID: 7, RunMode: RunModeNodeOnly, StartNodeID: "background_1", GraphSnapshot: blankVideoCanvasTemplate().Graph, Status: RunRunning}
		base.runs[run.ID] = run
		_ = runtime.executeRun(context.Background(), run)
		base.mu.Lock()
		for _, node := range base.nodes {
			if node.RunID == run.ID && node.NodeID == "background_1" {
				node.ProviderState = json.RawMessage(`{"phase":"submitting"}`)
			}
		}
		base.mu.Unlock()
		err = runtime.executeRun(context.Background(), run)
		if !errors.Is(err, ErrProviderSubmissionUnknown) || generator.calls != 0 {
			t.Fatalf("err=%v calls=%d", err, generator.calls)
		}
	})
}

func TestRuntimeCacheReadErrorAndInflightProviderNeverGenerateOrOverwriteCost(t *testing.T) {
	t.Run("cache database error", func(t *testing.T) {
		store := newMemoryRuntimeStore()
		store.findCacheErr = errors.New("cache database unavailable")
		generator := &fakeRuntimeImageGenerator{data: makeRuntimePNG(t)}
		runtime, err := NewRuntime(RuntimeConfig{Store: store, WorkerID: "worker", EstimateSecret: "secret", AssetRoot: t.TempDir(), ImageGenerator: generator})
		if err != nil {
			t.Fatal(err)
		}
		run := &Run{ID: "cache-read-error", UserID: 7, RunMode: RunModeNodeOnly, StartNodeID: "background_1", GraphSnapshot: blankVideoCanvasTemplate().Graph, Status: RunRunning}
		store.runs[run.ID] = run
		runtime.executeClaimed(run)
		if run.Status != RunRunning || generator.calls != 0 {
			t.Fatalf("run=%s calls=%d", run.Status, generator.calls)
		}
	})

	t.Run("inflight provider ignores late cache", func(t *testing.T) {
		base := newMemoryRuntimeStore()
		store := &ambiguousFencedStore{memoryRuntimeStore: base, failSubmitting: true}
		generator := &fakeRuntimeImageGenerator{data: makeRuntimePNG(t)}
		runtime, err := NewRuntime(RuntimeConfig{Store: store, WorkerID: "worker", EstimateSecret: "secret", AssetRoot: t.TempDir(), ImageGenerator: generator})
		if err != nil {
			t.Fatal(err)
		}
		run := &Run{ID: "inflight-cache", UserID: 7, RunMode: RunModeNodeOnly, StartNodeID: "background_1", GraphSnapshot: blankVideoCanvasTemplate().Graph, Status: RunRunning}
		base.runs[run.ID] = run
		_ = runtime.executeRun(context.Background(), run)
		node := base.nodeByRunAndNode(run.ID, "background_1")
		if node == nil {
			t.Fatal("background node was not created")
		}
		// provider 已进入提交阶段且产生费用后，同哈希缓存才到达。
		base.mu.Lock()
		storedNode := base.nodes[node.ID]
		storedNode.Status = NodeRunRunning
		storedNode.ProviderState = json.RawMessage(`{"phase":"submitting"}`)
		storedNode.CreditCost = 7
		cacheNode := &NodeRun{ID: "late-cache-node", RunID: "other-run", NodeID: "background_1", InputHash: node.InputHash, Status: NodeRunSucceeded, OutputVersionID: "late-cache-version"}
		base.nodes[cacheNode.ID] = cacheNode
		base.assets["late-cache-asset"] = &Asset{ID: "late-cache-asset", OwnerUserID: 7, Status: AssetReady, CurrentVersionID: "late-cache-version"}
		base.versions["late-cache-version"] = &AssetVersion{ID: "late-cache-version", AssetID: "late-cache-asset", OwnerUserID: 7, Status: AssetReady,
			InputHash: node.InputHash, CreatedByRunID: cacheNode.RunID, CreatedByNodeRunID: cacheNode.ID}
		base.cache[node.InputHash] = "late-cache-version"
		base.mu.Unlock()
		err = runtime.executeRun(context.Background(), run)
		latest := base.nodeByRunAndNode(run.ID, "background_1")
		if !errors.Is(err, ErrProviderSubmissionUnknown) || latest.CacheHit || latest.CreditCost != 7 {
			t.Fatalf("err=%v node=%+v", err, latest)
		}
	})
}

func TestRuntimeMissingUpstreamMediaStopsProviders(t *testing.T) {
	store := newMemoryRuntimeStore()
	missing := filepath.Join(t.TempDir(), "missing.png")
	store.versions["missing-image"] = &AssetVersion{ID: "missing-image", OwnerUserID: 7, Status: AssetReady, MIMEType: "image/png", FilePath: missing}
	run := &Run{ID: "missing-upstream", UserID: 7, Status: RunRunning}
	imageState := &NodeRun{ID: "image-node", RunID: run.ID, NodeID: "background", InputHash: "image-hash", Status: NodeRunRunning, ProviderState: json.RawMessage(`{"phase":"not_submitted"}`)}
	videoState := &NodeRun{ID: "video-node", RunID: run.ID, NodeID: "video", InputHash: "video-hash", Status: NodeRunRunning, ProviderState: json.RawMessage(`{"phase":"not_submitted"}`)}
	store.runs[run.ID] = run
	store.nodes[imageState.ID], store.nodes[videoState.ID] = imageState, videoState
	imageGenerator := &fakeRuntimeImageGenerator{data: makeRuntimePNG(t)}
	videoGenerator := &fakeRuntimeVideoGenerator{}
	runtime, err := NewRuntime(RuntimeConfig{Store: store, WorkerID: "worker", EstimateSecret: "secret", AssetRoot: t.TempDir(),
		ImageGenerator: imageGenerator, VideoGenerator: videoGenerator, MediaSigner: NewMediaSigner("secret"), PublicBaseURL: "https://media.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	incoming := []Edge{{Source: "reference"}}
	outputs := map[string]runtimeNodeOutput{"reference": {VersionID: "missing-image"}}
	if _, err := runtime.executeImageNode(context.Background(), run, Node{ID: "background", Type: NodeBackground}, imageState, incoming, outputs); !errors.Is(err, ErrNotFound) {
		t.Fatalf("image error=%v", err)
	}
	if _, err := runtime.executeVideoNode(context.Background(), run, Node{ID: "video", Type: NodeVideo}, videoState, incoming, outputs); !errors.Is(err, ErrNotFound) {
		t.Fatalf("video error=%v", err)
	}
	if imageGenerator.calls != 0 || videoGenerator.calls != 0 {
		t.Fatalf("image calls=%d video calls=%d", imageGenerator.calls, videoGenerator.calls)
	}
}

func TestRuntimeApprovalPauseErrorsStayRecoverable(t *testing.T) {
	for _, tt := range []struct {
		name            string
		commitThenError bool
		wantStatus      RunStatus
	}{
		{name: "not committed", wantStatus: RunRunning},
		{name: "committed response error", commitThenError: true, wantStatus: RunAwaitingCharacterApproval},
	} {
		t.Run(tt.name, func(t *testing.T) {
			base := newMemoryRuntimeStore()
			store := &ambiguousPauseStore{memoryRuntimeStore: base, fail: true, commitThenError: tt.commitThenError}
			runtime, err := NewRuntime(RuntimeConfig{Store: store, WorkerID: "worker", EstimateSecret: "secret", AssetRoot: t.TempDir(), ImageGenerator: &fakeRuntimeImageGenerator{data: makeRuntimePNG(t)}})
			if err != nil {
				t.Fatal(err)
			}
			templates, err := BuiltinTemplates()
			if err != nil {
				t.Fatal(err)
			}
			run := &Run{ID: "pause-" + tt.name, UserID: 7, RunMode: RunModeNodeOnly, StartNodeID: "role_heroine", GraphSnapshot: templates[0].Graph, Status: RunRunning}
			base.runs[run.ID] = run
			runtime.executeClaimed(run)
			if run.Status != tt.wantStatus || run.Status == RunFailed {
				t.Fatalf("status=%s want=%s", run.Status, tt.wantStatus)
			}
		})
	}
}

func TestRuntimeSuccessfulFinalizationErrorsNeverDowngradeRun(t *testing.T) {
	for _, tt := range []struct {
		name            string
		commitThenError bool
		wantStatus      RunStatus
	}{
		{name: "not committed", wantStatus: RunRunning},
		{name: "committed response error", commitThenError: true, wantStatus: RunSucceeded},
	} {
		t.Run(tt.name, func(t *testing.T) {
			base := newMemoryRuntimeStore()
			store := &ambiguousFinalizeStore{memoryRuntimeStore: base, fail: true, commitThenError: tt.commitThenError}
			runtime, err := NewRuntime(RuntimeConfig{Store: store, WorkerID: "worker", EstimateSecret: "secret", AssetRoot: t.TempDir(), ImageGenerator: &fakeRuntimeImageGenerator{data: makeRuntimePNG(t)}})
			if err != nil {
				t.Fatal(err)
			}
			run := &Run{ID: "finalize-" + tt.name, UserID: 7, RunMode: RunModeNodeOnly, StartNodeID: "background_1", GraphSnapshot: blankVideoCanvasTemplate().Graph, Status: RunRunning}
			base.runs[run.ID] = run
			runtime.executeClaimed(run)
			if run.Status != tt.wantStatus || run.Status == RunFailed {
				t.Fatalf("status=%s want=%s", run.Status, tt.wantStatus)
			}
		})
	}
}

func TestRuntimeProviderCostAcknowledgementIsReconciled(t *testing.T) {
	for _, tt := range []struct {
		name            string
		failures        int
		commitThenError bool
		readFailures    int
		wantSuccess     bool
	}{
		{name: "committed response error", failures: 1, commitThenError: true, wantSuccess: true},
		{name: "definitely not committed", failures: 3},
		{name: "reconciliation read failure", failures: 1, readFailures: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			base := newMemoryRuntimeStore()
			store := &ambiguousCostStore{memoryRuntimeStore: base, failures: tt.failures, commitThenError: tt.commitThenError, readFailures: tt.readFailures}
			generator := &fakeRuntimeImageGenerator{data: makeRuntimePNG(t)}
			runtime, err := NewRuntime(RuntimeConfig{Store: store, WorkerID: "worker", EstimateSecret: "secret", AssetRoot: t.TempDir(), ImageGenerator: generator})
			if err != nil {
				t.Fatal(err)
			}
			run := &Run{ID: "cost-" + tt.name, UserID: 7, RunMode: RunModeNodeOnly, StartNodeID: "background_1", GraphSnapshot: blankVideoCanvasTemplate().Graph, Status: RunRunning}
			base.runs[run.ID] = run
			runtime.executeClaimed(run)
			node := base.nodeByRunAndNode(run.ID, "background_1")
			if tt.wantSuccess {
				if run.Status != RunSucceeded || node == nil || node.CreditCost != runtime.config.ImageCredits {
					t.Fatalf("run=%s node=%+v", run.Status, node)
				}
				return
			}
			if run.Status != RunRunning || node == nil || node.Status != NodeRunRunning || generator.calls != 1 {
				t.Fatalf("run=%s node=%+v calls=%d", run.Status, node, generator.calls)
			}
		})
	}
}

func TestRuntimeSlotAcquireErrorsNeverFailNodeBeforeProvider(t *testing.T) {
	for _, tt := range []struct {
		name            string
		commitThenError bool
	}{
		{name: "transient"},
		{name: "committed response error", commitThenError: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			base := newMemoryRuntimeStore()
			store := &ambiguousSlotStore{memoryRuntimeStore: base, fail: true, commitThenError: tt.commitThenError}
			generator := &fakeRuntimeImageGenerator{data: makeRuntimePNG(t)}
			runtime, err := NewRuntime(RuntimeConfig{Store: store, WorkerID: "worker", EstimateSecret: "secret", AssetRoot: t.TempDir(), ImageGenerator: generator})
			if err != nil {
				t.Fatal(err)
			}
			run := &Run{ID: "slot-error-" + tt.name, UserID: 7, RunMode: RunModeNodeOnly, StartNodeID: "background_1", GraphSnapshot: blankVideoCanvasTemplate().Graph, Status: RunRunning}
			base.runs[run.ID] = run
			runtime.executeClaimed(run)
			node := base.nodeByRunAndNode(run.ID, "background_1")
			if run.Status != RunRunning || node == nil || node.Status != NodeRunRunning || generator.calls != 0 {
				t.Fatalf("run=%s node=%+v calls=%d", run.Status, node, generator.calls)
			}
		})
	}
}

func TestRuntimeNodeRunPersistenceErrorsStayRecoverable(t *testing.T) {
	t.Run("list node runs", func(t *testing.T) {
		store := newMemoryRuntimeStore()
		store.listNodeRunsErr = errors.New("node list unavailable")
		runtime, err := NewRuntime(RuntimeConfig{Store: store, WorkerID: "worker", EstimateSecret: "secret"})
		if err != nil {
			t.Fatal(err)
		}
		run := &Run{ID: "list-node-error", UserID: 7, RunMode: RunModeNodeOnly, StartNodeID: "background_1", GraphSnapshot: blankVideoCanvasTemplate().Graph, Status: RunRunning}
		store.runs[run.ID] = run
		runtime.executeClaimed(run)
		if run.Status != RunRunning {
			t.Fatalf("status=%s", run.Status)
		}
	})

	for _, tt := range []struct {
		name            string
		commitThenError bool
		findError       bool
		wantSuccess     bool
	}{
		{name: "committed response error", commitThenError: true, wantSuccess: true},
		{name: "not committed and find unavailable", findError: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			base := newMemoryRuntimeStore()
			store := &ambiguousCreateNodeStore{memoryRuntimeStore: base, fail: true, targetNodeID: "brief", commitThenError: tt.commitThenError, findError: tt.findError}
			generator := &fakeRuntimeImageGenerator{data: makeRuntimePNG(t)}
			runtime, err := NewRuntime(RuntimeConfig{Store: store, WorkerID: "worker", EstimateSecret: "secret", AssetRoot: t.TempDir(), ImageGenerator: generator})
			if err != nil {
				t.Fatal(err)
			}
			run := &Run{ID: "create-node-" + tt.name, UserID: 7, RunMode: RunModeNodeOnly, StartNodeID: "background_1", GraphSnapshot: blankVideoCanvasTemplate().Graph, Status: RunRunning}
			base.runs[run.ID] = run
			runtime.executeClaimed(run)
			if tt.wantSuccess {
				if run.Status != RunSucceeded || generator.calls != 1 {
					t.Fatalf("status=%s calls=%d", run.Status, generator.calls)
				}
				return
			}
			if run.Status != RunRunning || generator.calls != 0 {
				t.Fatalf("status=%s calls=%d", run.Status, generator.calls)
			}
		})
	}
}

func TestApprovalDecisionBatchIsAtomicAndProgressRetryIsIdempotent(t *testing.T) {
	store := newMemoryRuntimeStore()
	run := &Run{ID: "batch-approval", Status: RunAwaitingCharacterApproval}
	store.runs[run.ID] = run
	decisions := make([]ApprovalNodeDecision, 0, 2)
	for i := 1; i <= 2; i++ {
		nodeID := fmt.Sprintf("role-%d", i)
		nodeRunID := fmt.Sprintf("node-%d", i)
		hash := fmt.Sprintf("hash-%d", i)
		store.nodes[nodeRunID] = &NodeRun{ID: nodeRunID, RunID: run.ID, NodeID: nodeID, InputHash: hash, Status: NodeRunAwaitingApproval}
		approval := &Approval{ID: fmt.Sprintf("approval-%d", i), RunID: run.ID, NodeRunID: nodeRunID, NodeID: nodeID,
			InputHash: hash, Type: "characters", Status: "pending", ExpiresAt: time.Now().Add(time.Hour)}
		store.approvals[approval.ID] = approval
		payload := json.RawMessage(fmt.Sprintf(`{"node_run_id":%q,"selected_version_id":%q}`, nodeRunID, fmt.Sprintf("version-%d", i)))
		output := json.RawMessage(fmt.Sprintf(`{"selected_version_id":%q}`, fmt.Sprintf("version-%d", i)))
		decisions = append(decisions, ApprovalNodeDecision{NodeRunID: nodeRunID, NodeID: nodeID, InputHash: hash,
			ApprovalType: "characters", DecisionPayload: payload, Output: output, OutputVersionID: fmt.Sprintf("version-%d", i)})
	}
	if err := store.CommitApprovalDecision(context.Background(), &ApprovalDecisionCommit{
		RunID: run.ID, ExpectedStatus: RunAwaitingCharacterApproval, Decisions: decisions[:1],
	}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("partial character batch error=%v", err)
	}
	if run.Status != RunAwaitingCharacterApproval || store.nodes["node-1"].Status != NodeRunAwaitingApproval || store.approvals["approval-1"].Status != "pending" {
		t.Fatalf("partial batch mutated state: run=%s node=%s approval=%s", run.Status, store.nodes["node-1"].Status, store.approvals["approval-1"].Status)
	}
	commit := &ApprovalDecisionCommit{RunID: run.ID, ExpectedStatus: RunAwaitingCharacterApproval, Decisions: decisions}
	if err := store.CommitApprovalDecision(context.Background(), commit); err != nil {
		t.Fatal(err)
	}
	run.Status = RunAwaitingStoryboardApproval
	if err := store.CommitApprovalDecision(context.Background(), commit); err != nil {
		t.Fatalf("progressed retry: %v", err)
	}
	changed := *commit
	changed.Decisions = append([]ApprovalNodeDecision(nil), decisions...)
	changed.Decisions[0].DecisionPayload = json.RawMessage(`{"changed":true}`)
	if err := store.CommitApprovalDecision(context.Background(), &changed); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("changed retry error=%v", err)
	}
}

func TestRuntimeBoundImageVersionSkipsGenerationAndSignsVideoReference(t *testing.T) {
	legacyNode := Node{Config: json.RawMessage(`{"asset_id":"legacy-asset","asset_version_id":"legacy-version"}`)}
	if assetID, versionID := nodeBoundAsset(legacyNode); assetID != "legacy-asset" || versionID != "legacy-version" {
		t.Fatalf("legacy binding=%s/%s", assetID, versionID)
	}
	store := newMemoryRuntimeStore()
	root := t.TempDir()
	path := root + "/bound.png"
	if err := os.WriteFile(path, makeRuntimePNG(t), 0o640); err != nil {
		t.Fatal(err)
	}
	store.assets["asset-1"] = &Asset{ID: "asset-1", OwnerUserID: 7, Kind: MediaKindImage, Status: AssetReady}
	store.versions["version-1"] = &AssetVersion{ID: "version-1", AssetID: "asset-1", OwnerUserID: 7, Status: AssetReady, MIMEType: "image/png", FilePath: path}
	imageGenerator := &fakeRuntimeImageGenerator{data: makeRuntimePNG(t)}
	signer := NewMediaSigner("secret")
	runtime, err := NewRuntime(RuntimeConfig{Store: store, EstimateSecret: "secret", AssetRoot: root, ImageGenerator: imageGenerator,
		MediaSigner: signer, PublicBaseURL: "https://media.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	graph := blankVideoCanvasTemplate().Graph
	index := findNodeIndex(graph, "background_1")
	graph.Nodes[index].AssetID, graph.Nodes[index].AssetVersionID = "asset-1", "version-1"
	run := &Run{ID: "bound-run", UserID: 7, WorkflowID: "workflow", RequestID: "bound-request", RunMode: RunModeNodeOnly,
		StartNodeID: "background_1", GraphSnapshot: graph, Status: RunRunning}
	store.runs[run.ID] = run
	if err := runtime.executeRun(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	node := store.nodeByRunAndNode(run.ID, "background_1")
	if imageGenerator.calls != 0 || node.OutputVersionID != "version-1" || node.CreditCost != 0 || len(store.refs) != 1 {
		t.Fatalf("calls=%d node=%+v refs=%v", imageGenerator.calls, node, store.refs)
	}
	urls, err := runtime.upstreamReferenceURLs(context.Background(), 7, []Edge{{Source: "background_1"}}, map[string]runtimeNodeOutput{"background_1": {VersionID: "version-1"}})
	if err != nil || len(urls) != 1 || !strings.HasPrefix(urls[0], "https://media.example.com/p/vwf/version-1?") {
		t.Fatalf("reference urls=%v err=%v", urls, err)
	}

	store.versions["foreign-version"] = &AssetVersion{ID: "foreign-version", AssetID: "foreign-asset", OwnerUserID: 8, Status: AssetReady, MIMEType: "image/png", FilePath: path}
	graph.Nodes[index].AssetID, graph.Nodes[index].AssetVersionID = "foreign-asset", "foreign-version"
	foreign := &Run{ID: "foreign-run", UserID: 7, WorkflowID: "workflow", RequestID: "foreign-request", RunMode: RunModeNodeOnly,
		StartNodeID: "background_1", GraphSnapshot: graph, Status: RunRunning}
	store.runs[foreign.ID] = foreign
	if err := runtime.executeRun(context.Background(), foreign); err == nil {
		t.Fatal("cross-user bound asset was accepted")
	}
}

func TestRuntimeBoundImageReferenceWriteErrorStaysRecoverable(t *testing.T) {
	store := newMemoryRuntimeStore()
	store.addReferenceErr = errors.New("asset reference database unavailable")
	path := filepath.Join(t.TempDir(), "bound.png")
	if err := os.WriteFile(path, makeRuntimePNG(t), 0o640); err != nil {
		t.Fatal(err)
	}
	store.assets["asset-1"] = &Asset{ID: "asset-1", OwnerUserID: 7, Kind: MediaKindImage, Status: AssetReady}
	store.versions["version-1"] = &AssetVersion{ID: "version-1", AssetID: "asset-1", OwnerUserID: 7, Status: AssetReady, MIMEType: "image/png", FilePath: path}
	graph := blankVideoCanvasTemplate().Graph
	index := findNodeIndex(graph, "background_1")
	graph.Nodes[index].AssetID, graph.Nodes[index].AssetVersionID = "asset-1", "version-1"
	run := &Run{ID: "bound-reference-error", UserID: 7, RunMode: RunModeNodeOnly, StartNodeID: "background_1", GraphSnapshot: graph, Status: RunRunning}
	store.runs[run.ID] = run
	runtime, err := NewRuntime(RuntimeConfig{Store: store, WorkerID: "worker", EstimateSecret: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	runtime.executeClaimed(run)
	node := store.nodeByRunAndNode(run.ID, "background_1")
	if run.Status != RunRunning || node == nil || node.Status != NodeRunRunning || len(store.refs) != 0 {
		t.Fatalf("run=%s node=%+v refs=%v", run.Status, node, store.refs)
	}
}

func TestRuntimeBoundImageVersionReadErrorStaysRecoverable(t *testing.T) {
	store := newMemoryRuntimeStore()
	store.getVersionErr = errors.New("asset version database unavailable")
	graph := blankVideoCanvasTemplate().Graph
	index := findNodeIndex(graph, "background_1")
	graph.Nodes[index].AssetID, graph.Nodes[index].AssetVersionID = "asset-1", "version-1"
	run := &Run{ID: "bound-version-read-error", UserID: 7, RunMode: RunModeNodeOnly, StartNodeID: "background_1", GraphSnapshot: graph, Status: RunRunning}
	store.runs[run.ID] = run
	runtime, err := NewRuntime(RuntimeConfig{Store: store, WorkerID: "worker", EstimateSecret: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	runtime.executeClaimed(run)
	node := store.nodeByRunAndNode(run.ID, "background_1")
	if run.Status != RunRunning || node == nil || node.Status != NodeRunRunning {
		t.Fatalf("run=%s node=%+v", run.Status, node)
	}
}

func TestRuntimeComposeVersionReadErrorStaysRecoverable(t *testing.T) {
	store := newMemoryRuntimeStore()
	store.getVersionErr = errors.New("asset version database unavailable")
	run := &Run{ID: "compose-version-read-error", UserID: 7, Status: RunRunning}
	nodeRun := &NodeRun{ID: "compose-node-run", RunID: run.ID, NodeID: "compose", InputHash: "compose-hash", Status: NodeRunRunning}
	store.runs[run.ID], store.nodes[nodeRun.ID] = run, nodeRun
	runtime, err := NewRuntime(RuntimeConfig{Store: store, WorkerID: "worker", EstimateSecret: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	output := runtimeNodeOutput{JSON: json.RawMessage(`{"clips":[{"id":"clip-1","version_id":"version-1","trim_in_ms":0,"trim_out_ms":1000}]}`)}
	_, err = runtime.executeComposeNode(context.Background(), run, Node{ID: "compose", Type: NodeCompose}, []Edge{{Source: "timeline"}}, map[string]runtimeNodeOutput{"timeline": output}, nodeRun)
	if !errors.Is(err, errRecoverableRuntimePersistence) || nodeRun.Status != NodeRunRunning || run.Status != RunRunning {
		t.Fatalf("error=%v run=%s node=%s", err, run.Status, nodeRun.Status)
	}
}

func TestIncomingEdgesKeepVideoReferencesInDeclaredPortOrder(t *testing.T) {
	templates, err := BuiltinTemplates()
	if err != nil {
		t.Fatal(err)
	}
	graph := templates[0].Graph
	for left, right := 0, len(graph.Edges)-1; left < right; left, right = left+1, right-1 {
		graph.Edges[left], graph.Edges[right] = graph.Edges[right], graph.Edges[left]
	}
	var replacement Edge
	filtered := graph.Edges[:0]
	for _, edge := range graph.Edges {
		if edge.Target == "video_1" && edge.TargetPort == "heroine" {
			replacement = edge
			continue
		}
		filtered = append(filtered, edge)
	}
	if replacement.ID == "" {
		t.Fatal("video heroine edge not found")
	}
	graph.Edges = append(filtered, replacement)
	selected := make(map[string]bool, len(graph.Nodes))
	for _, node := range graph.Nodes {
		selected[node.ID] = true
	}
	edges := incomingEdges(graph, selected)["video_1"]
	wantPorts := []string{"scene", "background", "heroine", "hero", "cousin"}
	if len(edges) != len(wantPorts) {
		t.Fatalf("video incoming edges=%v", edges)
	}
	gotPorts := make([]string, len(edges))
	for index := range edges {
		gotPorts[index] = edges[index].TargetPort
	}
	for index, port := range wantPorts {
		if edges[index].TargetPort != port {
			t.Fatalf("video port order=%v, want=%v", gotPorts, wantPorts)
		}
	}

	store := newMemoryRuntimeStore()
	outputs := make(map[string]runtimeNodeOutput)
	wantVersions := []string{"version-background", "version-heroine", "version-hero", "version-cousin"}
	referencePath := filepath.Join(t.TempDir(), "reference.png")
	if err := os.WriteFile(referencePath, makeRuntimePNG(t), 0o640); err != nil {
		t.Fatal(err)
	}
	for index, source := range []string{"background_1", "role_heroine", "role_hero", "role_cousin"} {
		versionID := wantVersions[index]
		store.versions[versionID] = &AssetVersion{ID: versionID, OwnerUserID: 7, Status: AssetReady, MIMEType: "image/png", FilePath: referencePath}
		outputs[source] = runtimeNodeOutput{VersionID: versionID, JSON: json.RawMessage(`{"reference":"` + source + `"}`)}
	}
	runtime, err := NewRuntime(RuntimeConfig{Store: store, EstimateSecret: "secret", MediaSigner: NewMediaSigner("sign-secret"), PublicBaseURL: "https://media.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	urls, err := runtime.upstreamReferenceURLs(context.Background(), 7, edges, outputs)
	if err != nil {
		t.Fatal(err)
	}
	if len(urls) != len(wantVersions) {
		t.Fatalf("reference URLs=%v", urls)
	}
	for index, versionID := range wantVersions {
		if !strings.Contains(urls[index], "/p/vwf/"+versionID+"?") {
			t.Fatalf("reference URL order=%v, want versions=%v", urls, wantVersions)
		}
	}
	videoNode := graph.Nodes[findNodeIndex(graph, "video_1")]
	prompt := buildNodePrompt(videoNode, edges, outputs)
	lastPosition := -1
	for _, source := range []string{"background_1", "role_heroine", "role_hero", "role_cousin"} {
		position := strings.Index(prompt, source+":")
		if position <= lastPosition {
			t.Fatalf("video prompt reference order is unstable: %s", prompt)
		}
		lastPosition = position
	}
}

func TestIncomingEdgesUseSourceAndPortAsStableTieBreakers(t *testing.T) {
	graph := Graph{
		Nodes: []Node{
			{ID: "source-a"},
			{ID: "source-z"},
			{ID: "target", Inputs: []Port{{ID: "input"}}},
		},
		Edges: []Edge{
			{ID: "third", Source: "source-z", SourcePort: "a", Target: "target", TargetPort: "input"},
			{ID: "second", Source: "source-a", SourcePort: "z", Target: "target", TargetPort: "input"},
			{ID: "first", Source: "source-a", SourcePort: "a", Target: "target", TargetPort: "input"},
		},
	}
	edges := incomingEdges(graph, map[string]bool{"source-a": true, "source-z": true, "target": true})["target"]
	if len(edges) != 3 || edges[0].ID != "first" || edges[1].ID != "second" || edges[2].ID != "third" {
		t.Fatalf("same-port edge order=%v", edges)
	}
}

func TestSwappedVideoTargetPortsChangeInputHashAndMissOldCache(t *testing.T) {
	templates, err := BuiltinTemplates()
	if err != nil {
		t.Fatal(err)
	}
	graph := templates[0].Graph
	selected := make(map[string]bool, len(graph.Nodes))
	for _, node := range graph.Nodes {
		selected[node.ID] = true
	}
	outputs := map[string]runtimeNodeOutput{
		"scene_1":      {VersionID: "scene-version"},
		"background_1": {VersionID: "background-version"},
		"role_heroine": {VersionID: "heroine-version"},
		"role_hero":    {VersionID: "hero-version"},
		"role_cousin":  {VersionID: "cousin-version"},
	}
	provider := json.RawMessage(`{"channel_type":"apiyi_wan27","base_url":"https://api.apiyi.com","model":"wan2.7-r2v","duration_sec":15,"aspect_ratio":"9:16","resolution":"1080p"}`)
	baseRefs := buildUpstreamVersionRefs(incomingEdges(graph, selected)["video_1"], outputs)
	baseHash, err := InputHashWithProviderSnapshot(graph, "video_1", baseRefs, provider)
	if err != nil {
		t.Fatal(err)
	}

	heroineEdge, heroEdge := -1, -1
	for index := range graph.Edges {
		edge := graph.Edges[index]
		if edge.Target != "video_1" {
			continue
		}
		switch edge.TargetPort {
		case "heroine":
			heroineEdge = index
		case "hero":
			heroEdge = index
		}
	}
	if heroineEdge < 0 || heroEdge < 0 {
		t.Fatalf("video character edges not found: heroine=%d hero=%d", heroineEdge, heroEdge)
	}
	graph.Edges[heroineEdge].TargetPort, graph.Edges[heroEdge].TargetPort = graph.Edges[heroEdge].TargetPort, graph.Edges[heroineEdge].TargetPort
	swappedRefs := buildUpstreamVersionRefs(incomingEdges(graph, selected)["video_1"], outputs)
	swappedHash, err := InputHashWithProviderSnapshot(graph, "video_1", swappedRefs, provider)
	if err != nil {
		t.Fatal(err)
	}
	if baseHash == swappedHash {
		t.Fatalf("swapping target ports kept input hash=%s; base refs=%v swapped refs=%v", baseHash, baseRefs, swappedRefs)
	}

	store := newMemoryRuntimeStore()
	store.versions["old-cache-version"] = &AssetVersion{ID: "old-cache-version", OwnerUserID: 7, Status: AssetReady, InputHash: baseHash}
	store.cache[baseHash] = "old-cache-version"
	if _, err := store.FindCachedAssetVersion(context.Background(), 7, baseHash); err != nil {
		t.Fatalf("base hash missed cache: %v", err)
	}
	if _, err := store.FindCachedAssetVersion(context.Background(), 7, swappedHash); !errors.Is(err, ErrNotFound) {
		t.Fatalf("swapped target ports reused old cache: %v", err)
	}
}

func TestRuntimeVideoSubmissionPersistenceFailureNeverResubmits(t *testing.T) {
	base := newMemoryRuntimeStore()
	store := &failSubmittedStore{memoryRuntimeStore: base, failSubmitted: true}
	root := t.TempDir()
	path := root + "/bound.png"
	if err := os.WriteFile(path, makeRuntimePNG(t), 0o640); err != nil {
		t.Fatal(err)
	}
	base.assets["asset"] = &Asset{ID: "asset", OwnerUserID: 7, Kind: MediaKindImage, Status: AssetReady}
	base.versions["version"] = &AssetVersion{ID: "version", AssetID: "asset", OwnerUserID: 7, Status: AssetReady, MIMEType: "image/png", FilePath: path}
	video := &fakeRuntimeVideoGenerator{snapshot: json.RawMessage(`{"channel_type":"old","base_url":"https://old.example.com","model":"old-model"}`)}
	runtime, err := NewRuntime(RuntimeConfig{Store: store, EstimateSecret: "secret", AssetRoot: root, VideoGenerator: video,
		MediaSigner: NewMediaSigner("secret"), PublicBaseURL: "https://media.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	graph := blankVideoCanvasTemplate().Graph
	background := findNodeIndex(graph, "background_1")
	graph.Nodes[background].AssetID, graph.Nodes[background].AssetVersionID = "asset", "version"
	run := &Run{ID: "submission-run", UserID: 7, WorkflowID: "workflow", RequestID: "request", RunMode: RunModeNodeOnly,
		StartNodeID: "video_1", GraphSnapshot: graph, Status: RunRunning}
	base.runs[run.ID] = run
	err = runtime.executeRun(context.Background(), run)
	if !errors.Is(err, errRecoverableProviderSubmission) || video.calls != 1 {
		t.Fatalf("first err=%v calls=%d", err, video.calls)
	}
	node := base.nodeByRunAndNode(run.ID, "video_1")
	if node == nil || node.Status != NodeRunRunning || node.UpstreamTaskID != "" || !bytes.Contains(node.ModelSnapshot, []byte("old.example.com")) {
		t.Fatalf("node after failed persistence=%+v", node)
	}
	err = runtime.executeRun(context.Background(), run)
	if !errors.Is(err, ErrProviderSubmissionUnknown) || video.calls != 1 {
		t.Fatalf("restart err=%v calls=%d", err, video.calls)
	}
}

func TestRuntimeVideoSubmissionTransientPersistenceFailureResumesDurableTask(t *testing.T) {
	base := newMemoryRuntimeStore()
	store := &failSubmittedStore{memoryRuntimeStore: base, remainingFailures: 1}
	root := t.TempDir()
	path := root + "/bound.png"
	if err := os.WriteFile(path, makeRuntimePNG(t), 0o640); err != nil {
		t.Fatal(err)
	}
	base.assets["asset"] = &Asset{ID: "asset", OwnerUserID: 7, Kind: MediaKindImage, Status: AssetReady}
	base.versions["version"] = &AssetVersion{ID: "version", AssetID: "asset", OwnerUserID: 7, Status: AssetReady, MIMEType: "image/png", FilePath: path}
	video := &fakeRuntimeVideoGenerator{snapshot: json.RawMessage(`{"channel_type":"old","base_url":"https://old.example.com","model":"old-model"}`)}
	runtime, err := NewRuntime(RuntimeConfig{Store: store, EstimateSecret: "secret", AssetRoot: root, VideoGenerator: video,
		MediaSigner: NewMediaSigner("secret"), PublicBaseURL: "https://media.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	graph := blankVideoCanvasTemplate().Graph
	background := findNodeIndex(graph, "background_1")
	graph.Nodes[background].AssetID, graph.Nodes[background].AssetVersionID = "asset", "version"
	run := &Run{ID: "transient-submission-run", UserID: 7, WorkflowID: "workflow", RequestID: "request", RunMode: RunModeNodeOnly,
		StartNodeID: "video_1", GraphSnapshot: graph, Status: RunRunning}
	base.runs[run.ID] = run
	err = runtime.executeRun(context.Background(), run)
	node := base.nodeByRunAndNode(run.ID, "video_1")
	if !errors.Is(err, errRecoverableProviderSubmission) || node == nil || node.UpstreamTaskID != "provider-task" || video.submissions != 1 {
		t.Fatalf("first err=%v node=%+v submissions=%d", err, node, video.submissions)
	}
	_ = runtime.executeRun(context.Background(), run)
	if video.submissions != 1 || video.resumes != 1 || video.lastTaskID != "provider-task" {
		t.Fatalf("submissions=%d resumes=%d task=%q", video.submissions, video.resumes, video.lastTaskID)
	}
}

func TestRuntimeCanceledSubmittedVideoStaysRunningAndResumesOnNewWorker(t *testing.T) {
	store := newMemoryRuntimeStore()
	root := t.TempDir()
	path := root + "/bound.png"
	if err := os.WriteFile(path, makeRuntimePNG(t), 0o640); err != nil {
		t.Fatal(err)
	}
	store.assets["asset"] = &Asset{ID: "asset", OwnerUserID: 7, Kind: MediaKindImage, Status: AssetReady}
	store.versions["version"] = &AssetVersion{ID: "version", AssetID: "asset", OwnerUserID: 7, Status: AssetReady, MIMEType: "image/png", FilePath: path}
	graph := blankVideoCanvasTemplate().Graph
	background := findNodeIndex(graph, "background_1")
	graph.Nodes[background].AssetID, graph.Nodes[background].AssetVersionID = "asset", "version"
	run := &Run{ID: "canceled-submitted-video", UserID: 7, RunMode: RunModeNodeOnly, StartNodeID: "video_1", GraphSnapshot: graph, Status: RunRunning}
	store.runs[run.ID] = run
	firstGenerator := &cancelAfterSubmittedVideoGenerator{}
	first, err := NewRuntime(RuntimeConfig{Store: store, WorkerID: "worker-a", EstimateSecret: "secret", AssetRoot: root, VideoGenerator: firstGenerator,
		MediaSigner: NewMediaSigner("secret"), PublicBaseURL: "https://media.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := first.executeRun(context.Background(), run); !errors.Is(err, context.Canceled) {
		t.Fatalf("first error=%v", err)
	}
	node := store.nodeByRunAndNode(run.ID, "video_1")
	if node == nil || node.Status != NodeRunRunning || node.UpstreamTaskID != "durable-task" {
		t.Fatalf("node=%+v", node)
	}
	store.mu.Lock()
	store.runs[run.ID].LeaseOwner = "worker-b"
	lease := time.Now().Add(time.Minute)
	store.runs[run.ID].LeaseExpiresAt = &lease
	store.mu.Unlock()
	secondGenerator := &cancelAfterSubmittedVideoGenerator{}
	second, err := NewRuntime(RuntimeConfig{Store: store, WorkerID: "worker-b", EstimateSecret: "secret", AssetRoot: root, VideoGenerator: secondGenerator,
		MediaSigner: NewMediaSigner("secret"), PublicBaseURL: "https://media.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := second.executeRun(context.Background(), run); !errors.Is(err, context.Canceled) {
		t.Fatalf("resume error=%v", err)
	}
	if secondGenerator.resumes != 1 || store.nodeByRunAndNode(run.ID, "video_1").Status != NodeRunRunning {
		t.Fatalf("resumes=%d node=%+v", secondGenerator.resumes, store.nodeByRunAndNode(run.ID, "video_1"))
	}
}

func TestRuntimeVideoRecoveryUsesDurableTaskAndProviderSnapshot(t *testing.T) {
	store := newMemoryRuntimeStore()
	root := t.TempDir()
	path := root + "/reference.png"
	if err := os.WriteFile(path, makeRuntimePNG(t), 0o640); err != nil {
		t.Fatal(err)
	}
	store.versions["reference-version"] = &AssetVersion{ID: "reference-version", OwnerUserID: 7, Status: AssetReady, MIMEType: "image/png", FilePath: path}
	video := &fakeRuntimeVideoGenerator{}
	runtime, err := NewRuntime(RuntimeConfig{Store: store, EstimateSecret: "secret", AssetRoot: root, VideoGenerator: video,
		MediaSigner: NewMediaSigner("secret"), PublicBaseURL: "https://media.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := json.RawMessage(`{"text":"","image":"","video":"old-model","video_provider":{"channel_type":"old","base_url":"https://old.example.com","model":"old-model"}}`)
	state := &NodeRun{ID: "video-node-run", RunID: "resume-run", NodeID: "video_1", InputHash: "hash", Status: NodeRunRunning, UpstreamTaskID: "durable-task", ModelSnapshot: snapshot}
	store.nodes[state.ID] = state
	run := &Run{ID: "resume-run", UserID: 7, GraphSnapshot: blankVideoCanvasTemplate().Graph, Status: RunRunning}
	store.runs[run.ID] = run
	_, err = runtime.executeVideoNode(context.Background(), run, Node{ID: "video_1", Type: NodeVideo}, state,
		[]Edge{{Source: "background_1"}}, map[string]runtimeNodeOutput{"background_1": {VersionID: "reference-version"}})
	if err == nil || video.calls != 1 || video.lastTaskID != "durable-task" || !bytes.Contains(video.lastProviderConfig, []byte("old.example.com")) {
		t.Fatalf("err=%v calls=%d task=%q provider=%s", err, video.calls, video.lastTaskID, video.lastProviderConfig)
	}
}

func TestRuntimeVideoRecoveryAcceptsLegacyNodeIDSortedHashes(t *testing.T) {
	templates, err := BuiltinTemplates()
	if err != nil {
		t.Fatal(err)
	}
	graph := templates[0].Graph
	for left, right := 0, len(graph.Edges)-1; left < right; left, right = left+1, right-1 {
		graph.Edges[left], graph.Edges[right] = graph.Edges[right], graph.Edges[left]
	}
	selected, err := selectedNodeIDs(graph, RunModeNodeOnly, "video_1")
	if err != nil {
		t.Fatal(err)
	}
	sceneOutput := json.RawMessage(`{"scene":"雨夜重逢"}`)
	outputs := map[string]runtimeNodeOutput{
		"scene_1":      {JSON: sceneOutput},
		"background_1": {VersionID: "version-background"},
		"role_heroine": {VersionID: "version-heroine"},
		"role_hero":    {VersionID: "version-hero"},
		"role_cousin":  {VersionID: "version-cousin"},
	}
	canonicalRefs := buildUpstreamVersionRefs(incomingEdges(graph, selected)["video_1"], outputs)
	legacyRefs := legacyNodeIDSortedRefs(canonicalRefs)
	wantLegacyOrder := []string{"background_1", "role_cousin", "role_hero", "role_heroine", "scene_1"}
	if len(legacyRefs) != len(wantLegacyOrder) {
		t.Fatalf("legacy refs=%v", legacyRefs)
	}
	for index, nodeID := range wantLegacyOrder {
		if legacyRefs[index].NodeID != nodeID {
			t.Fatalf("legacy refs=%v, want order=%v", legacyRefs, wantLegacyOrder)
		}
	}
	persistedProvider, err := normalizeVideoProviderSnapshot(json.RawMessage(`{"channel_type":"apiyi_wan27","base_url":"https://persisted.example.com","model":"wan2.7-r2v","timeout_sec":1800,"duration_sec":15,"aspect_ratio":"9:16","resolution":"1080p"}`))
	if err != nil {
		t.Fatal(err)
	}
	newProvider := json.RawMessage(`{"channel_type":"apiyi_wan27","base_url":"https://new.example.com","model":"wan2.7-r2v","timeout_sec":1800,"duration_sec":15,"aspect_ratio":"9:16","resolution":"1080p"}`)
	modelSnapshot, _ := json.Marshal(map[string]any{"text": "default", "image": "gpt-image-2", "video": "wan2.7-r2v", "video_provider": persistedProvider})

	tests := map[string]func() (string, error){
		"provider-aware": func() (string, error) {
			return InputHashWithProviderSnapshot(graph, "video_1", legacyRefs, persistedProvider)
		},
		"no-provider": func() (string, error) {
			return InputHash(graph, "video_1", legacyRefs)
		},
	}
	for name, legacyHash := range tests {
		t.Run(name, func(t *testing.T) {
			inputHash, err := legacyHash()
			if err != nil {
				t.Fatal(err)
			}
			store := newMemoryRuntimeStore()
			referencePath := filepath.Join(t.TempDir(), "reference.png")
			if err := os.WriteFile(referencePath, makeRuntimePNG(t), 0o640); err != nil {
				t.Fatal(err)
			}
			run := &Run{ID: "legacy-" + name, UserID: 7, RunMode: RunModeNodeOnly, StartNodeID: "video_1", GraphSnapshot: graph, Status: RunRunning}
			store.runs[run.ID] = run
			store.nodes["brief"] = &NodeRun{ID: "brief", RunID: run.ID, NodeID: "brief", NodeType: NodeStoryBrief, Status: NodeRunSucceeded, Output: json.RawMessage(`{"prompt":"雨夜重逢"}`)}
			store.nodes["role-heroine"] = &NodeRun{ID: "role-heroine", RunID: run.ID, NodeID: "role_heroine", NodeType: NodeCharacter, Status: NodeRunSucceeded, OutputVersionID: "version-heroine"}
			store.nodes["role-hero"] = &NodeRun{ID: "role-hero", RunID: run.ID, NodeID: "role_hero", NodeType: NodeCharacter, Status: NodeRunSucceeded, OutputVersionID: "version-hero"}
			store.nodes["role-cousin"] = &NodeRun{ID: "role-cousin", RunID: run.ID, NodeID: "role_cousin", NodeType: NodeCharacter, Status: NodeRunSucceeded, OutputVersionID: "version-cousin"}
			store.nodes["script"] = &NodeRun{ID: "script", RunID: run.ID, NodeID: "script", NodeType: NodeScript, Status: NodeRunSucceeded, Output: json.RawMessage(`{"scenes":[{"scene":"雨夜重逢"}]}`)}
			store.nodes["scene"] = &NodeRun{ID: "scene", RunID: run.ID, NodeID: "scene_1", NodeType: NodeScene, Status: NodeRunSucceeded, Output: sceneOutput}
			store.nodes["background"] = &NodeRun{ID: "background", RunID: run.ID, NodeID: "background_1", NodeType: NodeBackground, Status: NodeRunSucceeded, OutputVersionID: "version-background"}
			store.nodes["video"] = &NodeRun{ID: "video", RunID: run.ID, NodeID: "video_1", NodeType: NodeVideo, InputHash: inputHash,
				Status: NodeRunRunning, UpstreamTaskID: "durable-task", ModelSnapshot: modelSnapshot}
			for _, versionID := range []string{"version-background", "version-heroine", "version-hero", "version-cousin"} {
				store.versions[versionID] = &AssetVersion{ID: versionID, OwnerUserID: 7, Status: AssetReady, MIMEType: "image/png", FilePath: referencePath}
			}
			store.versions["cached-version"] = &AssetVersion{ID: "cached-version", OwnerUserID: 7, Status: AssetReady, InputHash: inputHash}
			store.cache[inputHash] = "cached-version"
			video := &fakeRuntimeVideoGenerator{snapshot: newProvider}
			runtime, err := NewRuntime(RuntimeConfig{Store: store, EstimateSecret: "secret", VideoGenerator: video,
				MediaSigner: NewMediaSigner("secret"), PublicBaseURL: "https://media.example.com"})
			if err != nil {
				t.Fatal(err)
			}
			err = runtime.executeRun(context.Background(), run)
			if err == nil || strings.Contains(err.Error(), "input hash changed") || video.snapshotCalls != 0 || video.resumes != 1 ||
				video.lastTaskID != "durable-task" || !bytes.Contains(video.lastProviderConfig, []byte("persisted.example.com")) || bytes.Contains(video.lastProviderConfig, []byte("new.example.com")) {
				t.Fatalf("err=%v snapshot_calls=%d resumes=%d task=%q provider=%s", err, video.snapshotCalls, video.resumes, video.lastTaskID, video.lastProviderConfig)
			}
		})
	}
}

func TestRuntimeNonVideoHashKeepsLegacyNodeIDOrder(t *testing.T) {
	templates, err := BuiltinTemplates()
	if err != nil {
		t.Fatal(err)
	}
	graph := templates[0].Graph
	selected, err := selectedNodeIDs(graph, RunModeNodeOnly, "script")
	if err != nil {
		t.Fatal(err)
	}
	outputs := map[string]runtimeNodeOutput{
		"brief":        {JSON: json.RawMessage(`{"prompt":"雨夜重逢"}`)},
		"role_heroine": {VersionID: "version-heroine"},
		"role_hero":    {VersionID: "version-hero"},
		"role_cousin":  {VersionID: "version-cousin"},
	}
	legacyRefs := legacyNodeIDSortedRefs(buildUpstreamVersionRefs(incomingEdges(graph, selected)["script"], outputs))
	inputHash, err := InputHash(graph, "script", legacyRefs)
	if err != nil {
		t.Fatal(err)
	}
	store := newMemoryRuntimeStore()
	run := &Run{ID: "legacy-script", UserID: 7, RunMode: RunModeNodeOnly, StartNodeID: "script", GraphSnapshot: graph, Status: RunRunning}
	store.runs[run.ID] = run
	store.nodes["brief"] = &NodeRun{ID: "brief", RunID: run.ID, NodeID: "brief", NodeType: NodeStoryBrief, Status: NodeRunSucceeded, Output: outputs["brief"].JSON}
	store.nodes["role-heroine"] = &NodeRun{ID: "role-heroine", RunID: run.ID, NodeID: "role_heroine", NodeType: NodeCharacter, Status: NodeRunSucceeded, OutputVersionID: "version-heroine"}
	store.nodes["role-hero"] = &NodeRun{ID: "role-hero", RunID: run.ID, NodeID: "role_hero", NodeType: NodeCharacter, Status: NodeRunSucceeded, OutputVersionID: "version-hero"}
	store.nodes["role-cousin"] = &NodeRun{ID: "role-cousin", RunID: run.ID, NodeID: "role_cousin", NodeType: NodeCharacter, Status: NodeRunSucceeded, OutputVersionID: "version-cousin"}
	store.nodes["script"] = &NodeRun{ID: "script", RunID: run.ID, NodeID: "script", NodeType: NodeScript, InputHash: inputHash, Status: NodeRunRunning}
	runtime, err := NewRuntime(RuntimeConfig{Store: store, EstimateSecret: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	err = runtime.executeRun(context.Background(), run)
	if !errors.Is(err, ErrProviderSubmissionUnknown) || strings.Contains(err.Error(), "input hash changed") {
		t.Fatalf("legacy non-video hash recovery error=%v", err)
	}
}

func TestRuntimeRunningImageWithoutDurableTaskFailsClosed(t *testing.T) {
	store := newMemoryRuntimeStore()
	imageGenerator := &fakeRuntimeImageGenerator{data: makeRuntimePNG(t)}
	runtime, _ := NewRuntime(RuntimeConfig{Store: store, EstimateSecret: "secret", AssetRoot: t.TempDir(), ImageGenerator: imageGenerator})
	graph := blankVideoCanvasTemplate().Graph
	run := &Run{ID: "image-recovery", UserID: 7, RunMode: RunModeNodeOnly, StartNodeID: "background_1", GraphSnapshot: graph, Status: RunRunning}
	store.runs[run.ID] = run
	briefOutput := json.RawMessage(`{"prompt":"x"}`)
	sceneOutput := json.RawMessage(`{"scene":"x"}`)
	store.nodes["brief"] = &NodeRun{ID: "brief", RunID: run.ID, NodeID: "brief", NodeType: NodeStoryBrief, Status: NodeRunSucceeded, Output: briefOutput}
	store.nodes["scene"] = &NodeRun{ID: "scene", RunID: run.ID, NodeID: "scene_1", NodeType: NodeScene, Status: NodeRunSucceeded, Output: sceneOutput}
	inputHash, _ := InputHash(graph, "background_1", []AssetVersionRef{{NodeID: "scene_1", VersionID: "json:" + hashJSONForTest(sceneOutput)}})
	store.nodes["background"] = &NodeRun{ID: "background", RunID: run.ID, NodeID: "background_1", NodeType: NodeBackground, InputHash: inputHash, Status: NodeRunRunning}
	err := runtime.executeRun(context.Background(), run)
	if !errors.Is(err, ErrProviderSubmissionUnknown) || imageGenerator.calls != 0 {
		t.Fatalf("err=%v calls=%d", err, imageGenerator.calls)
	}
}

func TestRawVideoDurationTolerance(t *testing.T) {
	tests := []struct {
		durationMS int64
		valid      bool
	}{
		{durationMS: 14_900, valid: true},
		{durationMS: 15_093, valid: true},
		{durationMS: 14_000, valid: true},
		{durationMS: 16_000, valid: true},
		{durationMS: 13_999, valid: false},
		{durationMS: 16_001, valid: false},
	}
	for _, tt := range tests {
		err := validateRawVideoDuration(tt.durationMS)
		if (err == nil) != tt.valid {
			t.Fatalf("duration=%d valid=%t err=%v", tt.durationMS, tt.valid, err)
		}
	}
}

func TestRuntimeReservationReconcilesFreezeAfterAmbiguousPreDeduct(t *testing.T) {
	store := newMemoryRuntimeStore()
	billing := &fakeRuntimeBilling{store: store}
	runtime, err := NewRuntime(RuntimeConfig{Store: store, EstimateSecret: "secret", Billing: billing})
	if err != nil {
		t.Fatal(err)
	}
	run := &Run{ID: "ambiguous-charge", UserID: 7, EstimatedCost: 9, Status: RunRunning}
	store.runs[run.ID] = run
	if err := runtime.ensureReservation(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	charge := store.chargeByKey("video-workflow:" + run.ID)
	if billing.calls != 0 || charge == nil || charge.Status != ChargeReserved || charge.BillingRef != "reserved" {
		t.Fatalf("legacy billing calls=%d charge=%+v", billing.calls, charge)
	}
	if err := runtime.ensureReservation(context.Background(), run); err != nil || billing.calls != 0 {
		t.Fatalf("idempotent retry err=%v calls=%d", err, billing.calls)
	}
}

func TestRuntimeReservationTransientFailureStaysRetryable(t *testing.T) {
	store := newMemoryRuntimeStore()
	store.reserveErrs = []error{errors.New("billing unavailable")}
	billing := &fakeRuntimeBilling{store: store}
	runtime, err := NewRuntime(RuntimeConfig{Store: store, EstimateSecret: "secret", Billing: billing})
	if err != nil {
		t.Fatal(err)
	}
	run := &Run{ID: "retry-charge", UserID: 7, EstimatedCost: 11, Status: RunRunning}
	store.runs[run.ID] = run
	err = runtime.ensureReservation(context.Background(), run)
	charge := store.chargeByKey("video-workflow:" + run.ID)
	if !errors.Is(err, ErrReservationPending) || charge == nil || charge.Status != ChargeReserved || charge.BillingRef != "pending" {
		t.Fatalf("first err=%v charge=%+v", err, charge)
	}
	if err := runtime.ensureReservation(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	charge = store.chargeByKey("video-workflow:" + run.ID)
	if billing.calls != 0 || charge.Status != ChargeReserved || charge.BillingRef != "reserved" {
		t.Fatalf("calls=%d charge=%+v", billing.calls, charge)
	}
}

func TestRuntimeReservationCASPreventsCrossWorkerDoubleFreeze(t *testing.T) {
	store := newMemoryRuntimeStore()
	store.reserveStarted = make(chan struct{}, 1)
	store.reserveRelease = make(chan struct{})
	run := &Run{ID: "cross-worker-reserve", UserID: 7, EstimatedCost: 13, Status: RunRunning, LeaseOwner: "worker-a"}
	lease := time.Now().Add(time.Minute)
	run.LeaseExpiresAt = &lease
	store.runs[run.ID] = run
	billing := &fakeRuntimeBilling{store: store}
	first, err := NewRuntime(RuntimeConfig{Store: store, WorkerID: "worker-a", EstimateSecret: "secret", Billing: billing})
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewRuntime(RuntimeConfig{Store: store, WorkerID: "worker-b", EstimateSecret: "secret", Billing: billing})
	if err != nil {
		t.Fatal(err)
	}
	firstErr := make(chan error, 1)
	go func() { firstErr <- first.ensureReservation(context.Background(), run) }()
	select {
	case <-store.reserveStarted:
	case <-time.After(time.Second):
		t.Fatal("reservation transaction did not start")
	}
	if err := second.ensureReservation(context.Background(), run); !errors.Is(err, ErrReservationPending) {
		t.Fatalf("fresh reservation lock error=%v", err)
	}
	close(store.reserveRelease)
	if err := <-firstErr; err != nil {
		t.Fatal(err)
	}
	if store.reserveTransactions != 1 {
		t.Fatalf("freeze transactions=%d", store.reserveTransactions)
	}
}

func TestRuntimeReservationRecoversExpiredLockFromLedger(t *testing.T) {
	store := newMemoryRuntimeStore()
	run := &Run{ID: "stale-reserve", UserID: 7, EstimatedCost: 8, Status: RunRunning, LeaseOwner: "replacement"}
	lease := time.Now().Add(time.Minute)
	run.LeaseExpiresAt = &lease
	store.runs[run.ID] = run
	key := "video-workflow:" + run.ID
	store.charges[key] = &ChargeReservation{ID: "charge", UserID: 7, RunID: run.ID, IdempotencyKey: key, Amount: 8,
		Status: ChargeReserved, BillingRef: "reserving:dead", UpdatedAt: time.Now().Add(-2 * RuntimeBillingLockLease)}
	runtime, err := NewRuntime(RuntimeConfig{Store: store, WorkerID: "replacement", EstimateSecret: "secret", Billing: &fakeRuntimeBilling{store: store}})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.ensureReservation(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	if charge := store.chargeByKey(key); charge.BillingRef != "reserved" || store.reserveTransactions != 1 {
		t.Fatalf("charge=%+v freeze transactions=%d", charge, store.reserveTransactions)
	}
}

func TestRuntimeReservationAllowsRecoveryAfterSettlement(t *testing.T) {
	store := newMemoryRuntimeStore()
	run := &Run{ID: "settled-charge", UserID: 7, EstimatedCost: 11}
	key := "video-workflow:" + run.ID
	store.charges[key] = &ChargeReservation{ID: "charge", UserID: 7, RunID: run.ID, IdempotencyKey: key, Amount: 11, Status: ChargeSettled, BillingRef: "reserved"}
	billing := &fakeRuntimeBilling{store: store}
	runtime, err := NewRuntime(RuntimeConfig{Store: store, EstimateSecret: "secret", Billing: billing})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.ensureReservation(context.Background(), run); err != nil || billing.calls != 0 {
		t.Fatalf("err=%v calls=%d", err, billing.calls)
	}
}

func TestFinishRunReadErrorsDoNotCommitTerminalState(t *testing.T) {
	t.Run("node costs", func(t *testing.T) {
		store := newMemoryRuntimeStore()
		store.listNodeRunsErr = errors.New("node read unavailable")
		stored := &Run{ID: "read-node-error", UserID: 7, Status: RunRunning}
		store.runs[stored.ID] = stored
		runtime, err := NewRuntime(RuntimeConfig{Store: store, WorkerID: "worker", EstimateSecret: "secret"})
		if err != nil {
			t.Fatal(err)
		}
		run := *stored
		if err := runtime.finishRun(context.Background(), &run, RunSucceeded, "", "", ""); err == nil {
			t.Fatal("node cost read failure was ignored")
		}
		if stored.Status != RunRunning {
			t.Fatalf("run terminalized after node read error: %s", stored.Status)
		}
	})

	t.Run("charge", func(t *testing.T) {
		store := newMemoryRuntimeStore()
		store.getChargeErr = errors.New("charge read unavailable")
		stored := &Run{ID: "read-charge-error", UserID: 7, Status: RunRunning, EstimatedCost: 10}
		store.runs[stored.ID] = stored
		billing := &fakeRuntimeBilling{store: store}
		runtime, err := NewRuntime(RuntimeConfig{Store: store, WorkerID: "worker", EstimateSecret: "secret", Billing: billing})
		if err != nil {
			t.Fatal(err)
		}
		run := *stored
		if err := runtime.finishRun(context.Background(), &run, RunSucceeded, "", "", ""); err == nil {
			t.Fatal("charge read failure was ignored")
		}
		if stored.Status != RunRunning {
			t.Fatalf("run terminalized after charge read error: %s", stored.Status)
		}
	})
}

func TestFinishRunLostLeaseCannotSettle(t *testing.T) {
	store := newMemoryRuntimeStore()
	lease := time.Now().Add(time.Minute)
	stored := &Run{ID: "lost-finalization-lease", UserID: 7, Status: RunRunning, EstimatedCost: 10, LeaseOwner: "new-worker", LeaseExpiresAt: &lease}
	store.runs[stored.ID] = stored
	store.nodes["node"] = &NodeRun{ID: "node", RunID: stored.ID, Status: NodeRunSucceeded, CreditCost: 4}
	key := "video-workflow:" + stored.ID
	store.charges[key] = &ChargeReservation{ID: "charge", UserID: 7, RunID: stored.ID, IdempotencyKey: key, Amount: 10, Status: ChargeReserved, BillingRef: "reserved"}
	store.recordBillingTransaction(7, key, "freeze")
	runtime, err := NewRuntime(RuntimeConfig{Store: store, WorkerID: "old-worker", EstimateSecret: "secret", Billing: &fakeRuntimeBilling{store: store}})
	if err != nil {
		t.Fatal(err)
	}
	copy := *stored
	if err := runtime.finishRun(context.Background(), &copy, RunSucceeded, "", "", ""); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("lost worker finalization error=%v", err)
	}
	if stored.Status != RunRunning || store.finalizeSettlements != 0 || store.ledger[billingLedgerKey(7, key, "unfreeze")] {
		t.Fatalf("status=%s settlements=%d ledger=%v", stored.Status, store.finalizeSettlements, store.ledger)
	}
}

func TestFinishRunConcurrentSettlementCallsBillingOnce(t *testing.T) {
	store := newMemoryRuntimeStore()
	stored := &Run{ID: "concurrent-settle", UserID: 7, Status: RunRunning, EstimatedCost: 10}
	store.runs[stored.ID] = stored
	store.nodes["node"] = &NodeRun{ID: "node", RunID: stored.ID, Status: NodeRunSucceeded, CreditCost: 6}
	key := "video-workflow:" + stored.ID
	store.charges[key] = &ChargeReservation{
		ID: "charge", UserID: stored.UserID, RunID: stored.ID, IdempotencyKey: key,
		Amount: stored.EstimatedCost, Status: ChargeReserved, BillingRef: "reserved", UpdatedAt: time.Now(),
	}
	store.recordBillingTransaction(stored.UserID, key, "freeze")
	billing := &fakeRuntimeBilling{store: store}
	runtime, err := NewRuntime(RuntimeConfig{Store: store, WorkerID: "worker", EstimateSecret: "secret", Billing: billing})
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		copy := *stored
		go func() {
			<-start
			errs <- runtime.finishRun(context.Background(), &copy, RunSucceeded, "", "", "")
		}()
	}
	close(start)
	succeeded, conflicted := 0, 0
	for i := 0; i < 2; i++ {
		if err := <-errs; err == nil {
			succeeded++
		} else if errors.Is(err, ErrInvalidState) {
			conflicted++
		} else {
			t.Fatalf("unexpected finalization error: %v", err)
		}
	}
	if succeeded != 1 || conflicted != 1 || store.finalizeSettlements != 1 {
		t.Fatalf("succeeded=%d conflicted=%d settlements=%d", succeeded, conflicted, store.finalizeSettlements)
	}
	charge := store.chargeByKey(key)
	if charge == nil || charge.Status != ChargeSettled || stored.Status != RunSucceeded {
		t.Fatalf("charge=%+v run=%s", charge, stored.Status)
	}
}

func TestFinishFailedRunAfterReservationWasRefunded(t *testing.T) {
	store := newMemoryRuntimeStore()
	stored := &Run{ID: "insufficient-failed", UserID: 7, Status: RunRunning, EstimatedCost: 10}
	store.runs[stored.ID] = stored
	key := "video-workflow:" + stored.ID
	store.charges[key] = &ChargeReservation{
		ID: "charge", UserID: stored.UserID, RunID: stored.ID, IdempotencyKey: key,
		Amount: stored.EstimatedCost, Status: ChargeRefunded, BillingRef: "pending", UpdatedAt: time.Now(),
	}
	billing := &fakeRuntimeBilling{store: store}
	runtime, err := NewRuntime(RuntimeConfig{Store: store, WorkerID: "worker", EstimateSecret: "secret", Billing: billing})
	if err != nil {
		t.Fatal(err)
	}
	run := *stored
	if err := runtime.finishRun(context.Background(), &run, RunFailed, "insufficient", "余额不足", ""); err != nil {
		t.Fatal(err)
	}
	_, settleCalls := billing.callCounts()
	if stored.Status != RunFailed || settleCalls != 0 {
		t.Fatalf("run=%s settle_calls=%d", stored.Status, settleCalls)
	}
}

func TestCancelPendingFinalizationRetriesAfterReadFailure(t *testing.T) {
	store := newMemoryRuntimeStore()
	stored := &Run{ID: "cancel-retry", UserID: 7, Status: RunCancelPending}
	store.runs[stored.ID] = stored
	store.nodes["node"] = &NodeRun{ID: "node", RunID: stored.ID, Status: NodeRunRunning}
	store.listNodeRunsErr = errors.New("temporary read failure")
	runtime, err := NewRuntime(RuntimeConfig{Store: store, WorkerID: "worker", EstimateSecret: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	first := *stored
	runtime.executeClaimed(&first)
	if stored.Status != RunCancelPending {
		t.Fatalf("cancel read failure became terminal: %s", stored.Status)
	}
	store.mu.Lock()
	store.listNodeRunsErr = nil
	store.mu.Unlock()
	second := *stored
	runtime.executeClaimed(&second)
	if stored.Status != RunCanceled {
		t.Fatalf("recovered cancellation status=%s", stored.Status)
	}
	store.mu.Lock()
	nodeStatus := store.nodes["node"].Status
	store.mu.Unlock()
	if nodeStatus != NodeRunCanceled {
		t.Fatalf("node status=%s", nodeStatus)
	}
	if err := store.UpdateNodeRunOutput(context.Background(), "node", json.RawMessage(`{"late":true}`), 9, false); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("late canceled-node output error=%v", err)
	}
	store.mu.Lock()
	lateCost := store.nodes["node"].CreditCost
	store.mu.Unlock()
	if lateCost != 0 {
		t.Fatalf("late canceled-node cost=%d", lateCost)
	}
}

func TestCancelPendingRecoversStaleSettlementLock(t *testing.T) {
	store := newMemoryRuntimeStore()
	stored := &Run{ID: "cancel-stale-settlement", UserID: 7, Status: RunCancelPending, EstimatedCost: 10}
	store.runs[stored.ID] = stored
	key := "video-workflow:" + stored.ID
	store.charges[key] = &ChargeReservation{
		ID: "charge", UserID: stored.UserID, RunID: stored.ID, IdempotencyKey: key,
		Amount: stored.EstimatedCost, Status: ChargeReserved, BillingRef: "settling:dead-worker",
		UpdatedAt: time.Now().Add(-2 * RuntimeLeaseDuration),
	}
	store.recordBillingTransaction(stored.UserID, key, "freeze")
	billing := &fakeRuntimeBilling{store: store}
	runtime, err := NewRuntime(RuntimeConfig{Store: store, WorkerID: "replacement-worker", EstimateSecret: "secret", Billing: billing})
	if err != nil {
		t.Fatal(err)
	}
	run := *stored
	runtime.executeClaimed(&run)
	_, settleCalls := billing.callCounts()
	charge := store.chargeByKey(key)
	if stored.Status != RunCanceled || settleCalls != 0 || charge == nil || charge.Status != ChargeSettled {
		t.Fatalf("run=%s settle_calls=%d charge=%+v", stored.Status, settleCalls, charge)
	}
}

func TestCancelPendingBeforeReservationDoesNotPreDeduct(t *testing.T) {
	store := newMemoryRuntimeStore()
	stored := &Run{ID: "cancel-before-reservation", UserID: 7, Status: RunCancelPending, EstimatedCost: 10}
	store.runs[stored.ID] = stored
	billing := &fakeRuntimeBilling{store: store}
	runtime, err := NewRuntime(RuntimeConfig{Store: store, WorkerID: "worker", EstimateSecret: "secret", Billing: billing})
	if err != nil {
		t.Fatal(err)
	}
	run := *stored
	runtime.executeClaimed(&run)
	preDeductCalls, settleCalls := billing.callCounts()
	if stored.Status != RunCanceled || preDeductCalls != 0 || settleCalls != 0 {
		t.Fatalf("run=%s pre_deduct=%d settle=%d", stored.Status, preDeductCalls, settleCalls)
	}
}

func TestMemoryRuntimeStoreSerializesAssetQuota(t *testing.T) {
	store := newMemoryRuntimeStore()
	const attempts = 24
	versionBytes := AssetQuotaBytes / 3
	start := make(chan struct{})
	results := make(chan error, attempts)
	for i := 0; i < attempts; i++ {
		go func(index int) {
			<-start
			results <- store.CreateAssetVersion(context.Background(), &AssetVersion{
				ID: fmt.Sprintf("version-%d", index), AssetID: fmt.Sprintf("asset-%d", index),
				OwnerUserID: 7, SizeBytes: versionBytes, Status: AssetReady,
			})
		}(i)
	}
	close(start)
	succeeded := 0
	for i := 0; i < attempts; i++ {
		err := <-results
		if err == nil {
			succeeded++
			continue
		}
		if !errors.Is(err, ErrAssetQuotaExceeded) {
			t.Fatalf("unexpected quota error: %v", err)
		}
	}
	used, err := store.UsedAssetBytes(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if succeeded != 3 || used > AssetQuotaBytes {
		t.Fatalf("succeeded=%d used=%d quota=%d", succeeded, used, AssetQuotaBytes)
	}
}

func TestRuntimeGlobalSlotsCoordinateInstancesAndRecoverExpiry(t *testing.T) {
	store := newMemoryRuntimeStore()
	lease := time.Now().Add(time.Minute)
	runA := &Run{ID: "slot-run-a", Status: RunRunning, LeaseOwner: "worker-a", LeaseExpiresAt: &lease}
	runB := &Run{ID: "slot-run-b", Status: RunRunning, LeaseOwner: "worker-b", LeaseExpiresAt: &lease}
	nodeA := &NodeRun{ID: "slot-node-a", RunID: runA.ID, Status: NodeRunRunning}
	nodeB := &NodeRun{ID: "slot-node-b", RunID: runB.ID, Status: NodeRunRunning}
	store.runs[runA.ID], store.runs[runB.ID] = runA, runB
	store.nodes[nodeA.ID], store.nodes[nodeB.ID] = nodeA, nodeB
	runtimeA, err := NewRuntime(RuntimeConfig{Store: store, WorkerID: "worker-a", EstimateSecret: "secret", ImageConcurrency: 1})
	if err != nil {
		t.Fatal(err)
	}
	runtimeB, err := NewRuntime(RuntimeConfig{Store: store, WorkerID: "worker-b", EstimateSecret: "secret", ImageConcurrency: 1})
	if err != nil {
		t.Fatal(err)
	}
	_, releaseA, err := runtimeA.acquireNodeExecutionSlot(context.Background(), runA, nodeA, "image", runtimeA.imageSlots, 1)
	if err != nil {
		t.Fatal(err)
	}
	type acquiredSlot struct {
		release func()
		err     error
	}
	acquired := make(chan acquiredSlot, 1)
	go func() {
		_, release, err := runtimeB.acquireNodeExecutionSlot(context.Background(), runB, nodeB, "image", runtimeB.imageSlots, 1)
		acquired <- acquiredSlot{release: release, err: err}
	}()
	select {
	case result := <-acquired:
		if result.release != nil {
			result.release()
		}
		t.Fatalf("second instance bypassed global slot: %v", result.err)
	case <-time.After(100 * time.Millisecond):
	}
	releaseA()
	select {
	case result := <-acquired:
		if result.err != nil {
			t.Fatal(result.err)
		}
		result.release()
	case <-time.After(time.Second):
		t.Fatal("second instance did not acquire released slot")
	}

	if _, err := store.AcquireRuntimeSlot(context.Background(), "video", 1, runA.ID, nodeA.ID, "worker-a", "expired-token", 20*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(40 * time.Millisecond)
	index, err := store.AcquireRuntimeSlot(context.Background(), "video", 1, runB.ID, nodeB.ID, "worker-b", "replacement-token", time.Minute)
	if err != nil || index != 0 {
		t.Fatalf("expired slot recovery index=%d err=%v", index, err)
	}
	if err := store.ReleaseRuntimeSlot(context.Background(), "video", index, "expired-token"); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("stale token released replacement slot: %v", err)
	}
	if err := store.ReleaseRuntimeSlot(context.Background(), "video", index, "replacement-token"); err != nil {
		t.Fatal(err)
	}
}

type fakeRuntimeImageGenerator struct {
	mu    sync.Mutex
	calls int
	data  []byte
}

type fakeRuntimeVideoGenerator struct {
	calls              int
	snapshotCalls      int
	submissions        int
	resumes            int
	snapshot           json.RawMessage
	lastTaskID         string
	lastProviderConfig json.RawMessage
}

type cancelAfterSubmittedVideoGenerator struct {
	calls   int
	resumes int
}

func (*cancelAfterSubmittedVideoGenerator) VideoConfigSnapshot() json.RawMessage {
	return json.RawMessage(`{"channel_type":"test","base_url":"https://provider.example.com","model":"test-video"}`)
}

func (f *cancelAfterSubmittedVideoGenerator) GenerateVideo(_ context.Context, request VideoGenerationRequest) (*GeneratedVideo, error) {
	f.calls++
	if request.ProviderTaskID != "" {
		f.resumes++
		return nil, context.Canceled
	}
	state := json.RawMessage(`{"phase":"submitted","task_id":"durable-task"}`)
	if err := request.OnSubmitted("durable-task", state); err != nil {
		return nil, err
	}
	return nil, context.Canceled
}

type billingOutcome struct {
	err         error
	writeFreeze bool
}

type fakeRuntimeBilling struct {
	mu            sync.Mutex
	store         *memoryRuntimeStore
	outcomes      []billingOutcome
	calls         int
	settleCalls   int
	settleStarted chan struct{}
	settleRelease chan struct{}
}

func (f *fakeRuntimeBilling) PreDeduct(_ context.Context, userID, _ uint64, _ int64, refID, _ string) error {
	f.mu.Lock()
	f.calls++
	var outcome billingOutcome
	if len(f.outcomes) > 0 {
		outcome = f.outcomes[0]
		f.outcomes = f.outcomes[1:]
	}
	f.mu.Unlock()
	if outcome.writeFreeze {
		f.store.recordBillingTransaction(userID, refID, "freeze")
	}
	return outcome.err
}

func (f *fakeRuntimeBilling) Settle(ctx context.Context, userID, _ uint64, _, _ int64, refID, _ string) error {
	f.mu.Lock()
	f.settleCalls++
	started, release := f.settleStarted, f.settleRelease
	f.mu.Unlock()
	if started != nil {
		select {
		case started <- struct{}{}:
		default:
		}
	}
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	f.store.recordBillingTransaction(userID, refID, "unfreeze")
	return nil
}

func (*fakeRuntimeBilling) Refund(context.Context, uint64, uint64, int64, string, string) error {
	return nil
}

func (f *fakeRuntimeBilling) callCounts() (preDeduct, settle int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls, f.settleCalls
}

func (f *fakeRuntimeVideoGenerator) VideoConfigSnapshot() json.RawMessage {
	f.snapshotCalls++
	return append(json.RawMessage(nil), f.snapshot...)
}
func (f *fakeRuntimeVideoGenerator) GenerateVideo(_ context.Context, request VideoGenerationRequest) (*GeneratedVideo, error) {
	f.calls++
	f.lastTaskID = request.ProviderTaskID
	f.lastProviderConfig = append(json.RawMessage(nil), request.ProviderConfig...)
	if request.ProviderTaskID != "" {
		f.resumes++
		return nil, errors.New("resume stopped for test")
	}
	f.submissions++
	state, _ := json.Marshal(map[string]any{"phase": "submitted", "task_id": "provider-task"})
	if request.OnSubmitted != nil {
		if err := request.OnSubmitted("provider-task", state); err != nil {
			return nil, err
		}
	}
	return nil, errors.New("unexpected")
}

func hashJSONForTest(raw json.RawMessage) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (f *fakeRuntimeImageGenerator) GenerateImage(_ context.Context, request ImageGenerationRequest) ([]GeneratedImage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	out := make([]GeneratedImage, request.Count)
	for i := range out {
		out[i] = GeneratedImage{Data: append([]byte(nil), f.data...), MIMEType: "image/png", TaskID: request.TaskID}
	}
	return out, nil
}

func makeRuntimePNG(t *testing.T) []byte {
	t.Helper()
	value := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	value.Set(0, 0, color.NRGBA{R: 255, A: 255})
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, value); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

type memoryRuntimeStore struct {
	RuntimeStore
	mu                  sync.Mutex
	runs                map[string]*Run
	nodes               map[string]*NodeRun
	assets              map[string]*Asset
	versions            map[string]*AssetVersion
	cache               map[string]string
	approvals           map[string]*Approval
	charges             map[string]*ChargeReservation
	ledger              map[string]bool
	refs                []AssetReference
	listNodeRunsErr     error
	getChargeErr        error
	findCacheErr        error
	getVersionErr       error
	addReferenceErr     error
	slots               map[string]*memoryRuntimeSlot
	reserveErrs         []error
	finalizeSettlements int
	reserveTransactions int
	reserveStarted      chan struct{}
	reserveRelease      chan struct{}
}

type memoryRuntimeSlot struct {
	kind      string
	index     int
	runID     string
	nodeRunID string
	workerID  string
	token     string
	expiresAt time.Time
}

func newMemoryRuntimeStore() *memoryRuntimeStore {
	return &memoryRuntimeStore{runs: map[string]*Run{}, nodes: map[string]*NodeRun{}, assets: map[string]*Asset{}, versions: map[string]*AssetVersion{}, cache: map[string]string{}, approvals: map[string]*Approval{}, charges: map[string]*ChargeReservation{}, ledger: map[string]bool{}, slots: map[string]*memoryRuntimeSlot{}}
}

func (s *memoryRuntimeStore) ListNodeRuns(_ context.Context, runID string) ([]NodeRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listNodeRunsErr != nil {
		return nil, s.listNodeRunsErr
	}
	var out []NodeRun
	for _, node := range s.nodes {
		if node.RunID == runID {
			out = append(out, *node)
		}
	}
	return out, nil
}

func (s *memoryRuntimeStore) CreateNodeRun(_ context.Context, node *NodeRun, nodeType NodeType, attempt int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.nodes {
		if existing.RunID == node.RunID && existing.NodeID == node.NodeID && existing.Attempt == attempt {
			return errors.New("duplicate")
		}
	}
	copy := *node
	copy.Attempt = attempt
	copy.NodeType = nodeType
	s.nodes[node.ID] = &copy
	return nil
}

func (s *memoryRuntimeStore) FindNodeRun(_ context.Context, runID, nodeID string) (*NodeRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, node := range s.nodes {
		if node.RunID == runID && node.NodeID == nodeID {
			copy := *node
			return &copy, nil
		}
	}
	return nil, ErrNotFound
}

func (s *memoryRuntimeStore) GetNodeRun(_ context.Context, id string) (*NodeRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	node := s.nodes[id]
	if node == nil {
		return nil, ErrNotFound
	}
	copy := *node
	return &copy, nil
}

func (s *memoryRuntimeStore) UpdateNodeRunStatus(_ context.Context, id string, from, to NodeRunStatus, upstreamID, versionID, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	node := s.nodes[id]
	if node == nil || node.Status != from {
		return ErrInvalidState
	}
	node.Status, node.ErrorMessage = to, message
	if upstreamID != "" {
		node.UpstreamTaskID = upstreamID
	}
	if versionID != "" {
		node.OutputVersionID = versionID
	}
	return nil
}

func (s *memoryRuntimeStore) UpdateNodeRunStatusFenced(_ context.Context, transition *FencedNodeRunTransition) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, node := s.runs[transition.RunID], s.nodes[transition.NodeRunID]
	if run == nil || node == nil || run.Status != RunRunning || node.RunID != transition.RunID || node.NodeID != transition.NodeID ||
		node.InputHash != transition.InputHash || node.Status != transition.From {
		return ErrInvalidState
	}
	if run.LeaseOwner == "" {
		run.LeaseOwner = transition.WorkerID
		until := time.Now().Add(RuntimeLeaseDuration)
		run.LeaseExpiresAt = &until
	}
	if run.LeaseOwner != transition.WorkerID || run.LeaseExpiresAt == nil || run.LeaseExpiresAt.Before(time.Now()) {
		return ErrInvalidState
	}
	node.Status, node.ErrorMessage = transition.To, transition.ErrorMessage
	if transition.UpstreamTaskID != "" {
		node.UpstreamTaskID = transition.UpstreamTaskID
	}
	if transition.OutputVersionID != "" {
		node.OutputVersionID = transition.OutputVersionID
	}
	if len(transition.ProviderState) > 0 {
		node.ProviderState = append(json.RawMessage(nil), transition.ProviderState...)
	}
	return nil
}

func (s *memoryRuntimeStore) UpdateNodeRunOutput(_ context.Context, id string, output json.RawMessage, cost int64, cacheHit bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	node := s.nodes[id]
	if node == nil || (node.Status != NodeRunRunning && node.Status != NodeRunAwaitingApproval) {
		return ErrInvalidState
	}
	node.Output, node.CreditCost, node.CacheHit = append(json.RawMessage(nil), output...), cost, cacheHit
	return nil
}

func (s *memoryRuntimeStore) RecordNodeRunCost(_ context.Context, id string, cost int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	node := s.nodes[id]
	if node == nil || cost < 0 || (node.Status != NodeRunRunning && node.Status != NodeRunAwaitingApproval && node.Status != NodeRunCancelPending) {
		return ErrInvalidState
	}
	if cost > node.CreditCost {
		node.CreditCost = cost
	}
	return nil
}

func (s *memoryRuntimeStore) CommitNodeResult(_ context.Context, commit *NodeResultCommit) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if commit == nil {
		return ErrInvalidState
	}
	run := s.runs[commit.RunID]
	node := s.nodes[commit.NodeRunID]
	if run == nil || node == nil || run.Status != commit.ExpectedRunStatus || node.RunID != commit.RunID || node.NodeID != commit.NodeID ||
		node.InputHash != commit.InputHash || node.Status != commit.From {
		return ErrInvalidState
	}
	if run.LeaseOwner == "" {
		run.LeaseOwner = commit.WorkerID
		until := time.Now().Add(RuntimeLeaseDuration)
		run.LeaseExpiresAt = &until
	}
	if run.LeaseOwner != commit.WorkerID || run.LeaseExpiresAt == nil || run.LeaseExpiresAt.Before(time.Now()) {
		return ErrInvalidState
	}
	var used, added int64
	for _, version := range s.versions {
		if version.DeletedAt == nil {
			used += version.SizeBytes
		}
	}
	for i := range commit.Versions {
		added += commit.Versions[i].SizeBytes
	}
	if used < 0 || added < 0 || added > AssetQuotaBytes-used {
		return ErrAssetQuotaExceeded
	}
	for i := range commit.Assets {
		asset := commit.Assets[i]
		copy := asset
		s.assets[asset.ID] = &copy
	}
	for i := range commit.Versions {
		version := commit.Versions[i]
		if version.Version == 0 {
			version.Version = uint64(i + 1)
		}
		copy := version
		s.versions[version.ID] = &copy
		s.cache[version.InputHash] = version.ID
		asset := s.assets[version.AssetID]
		if asset != nil {
			asset.Status = AssetReady
			asset.CurrentVersionID = version.ID
		}
	}
	for i := range commit.Versions {
		if commit.Versions[i].ID == commit.OutputVersionID {
			if asset := s.assets[commit.Versions[i].AssetID]; asset != nil {
				asset.CurrentVersionID = commit.OutputVersionID
			}
		}
	}
	for i := range commit.References {
		s.refs = append(s.refs, commit.References[i])
	}
	node.Status, node.OutputVersionID = commit.To, commit.OutputVersionID
	node.Output, node.CreditCost, node.CacheHit = append(json.RawMessage(nil), commit.Output...), commit.CreditCost, commit.CacheHit
	if commit.UpstreamTaskID != "" {
		node.UpstreamTaskID = commit.UpstreamTaskID
	}
	if commit.Approval != nil {
		copy := *commit.Approval
		s.approvals[copy.ID] = &copy
	}
	return nil
}

func (s *memoryRuntimeStore) UpdateNodeRunProgress(_ context.Context, id string, progress int, taskID string, provider json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	node := s.nodes[id]
	if node == nil {
		return ErrNotFound
	}
	node.Progress = progress
	if taskID != "" {
		node.UpstreamTaskID = taskID
	}
	if len(provider) > 0 {
		node.ProviderState = append(json.RawMessage(nil), provider...)
	}
	return nil
}

func (s *memoryRuntimeStore) UpdateNodeRunProgressFenced(_ context.Context, runID, workerID, nodeRunID, inputHash string, progress int, taskID string, provider json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, node := s.runs[runID], s.nodes[nodeRunID]
	if run == nil || node == nil || run.Status != RunRunning || node.RunID != runID || node.InputHash != inputHash || node.Status != NodeRunRunning {
		return ErrInvalidState
	}
	if run.LeaseOwner == "" {
		run.LeaseOwner = workerID
		until := time.Now().Add(RuntimeLeaseDuration)
		run.LeaseExpiresAt = &until
	}
	if run.LeaseOwner != workerID || run.LeaseExpiresAt == nil || run.LeaseExpiresAt.Before(time.Now()) {
		return ErrInvalidState
	}
	node.Progress = progress
	if taskID != "" {
		node.UpstreamTaskID = taskID
	}
	if len(provider) > 0 {
		node.ProviderState = append(json.RawMessage(nil), provider...)
	}
	return nil
}
func (s *memoryRuntimeStore) UpdateRunProgress(_ context.Context, id string, progress int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runs[id] != nil {
		s.runs[id].Progress = progress
	}
	return nil
}

func (s *memoryRuntimeStore) UpdateRunProgressFenced(_ context.Context, id, workerID string, progress int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	run := s.runs[id]
	if run == nil || run.Status != RunRunning {
		return ErrInvalidState
	}
	if run.LeaseOwner == "" {
		run.LeaseOwner = workerID
		until := time.Now().Add(RuntimeLeaseDuration)
		run.LeaseExpiresAt = &until
	}
	if run.LeaseOwner != workerID || run.LeaseExpiresAt == nil || run.LeaseExpiresAt.Before(time.Now()) {
		return ErrInvalidState
	}
	if progress > run.Progress {
		run.Progress = progress
	}
	return nil
}

func (s *memoryRuntimeStore) CompleteRun(_ context.Context, id, _ string, from, to RunStatus, actual int64, output, code, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	run := s.runs[id]
	if run == nil || run.Status != from {
		return ErrInvalidState
	}
	run.Status, run.ActualCost, run.OutputVersionID, run.ErrorCode, run.ErrorMessage = to, actual, output, code, message
	return nil
}

func (s *memoryRuntimeStore) UpdateRunStatus(_ context.Context, id string, from, to RunStatus, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	run := s.runs[id]
	if run == nil || run.Status != from {
		return ErrInvalidState
	}
	run.Status, run.ErrorMessage = to, message
	return nil
}

func (s *memoryRuntimeStore) UpdateRunStatusFenced(_ context.Context, id, workerID string, from, to RunStatus, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	run := s.runs[id]
	if run == nil || run.Status != from || from != RunRunning {
		return ErrInvalidState
	}
	if run.LeaseOwner == "" {
		run.LeaseOwner = workerID
		until := time.Now().Add(RuntimeLeaseDuration)
		run.LeaseExpiresAt = &until
	}
	if run.LeaseOwner != workerID || run.LeaseExpiresAt == nil || run.LeaseExpiresAt.Before(time.Now()) {
		return ErrInvalidState
	}
	run.Status, run.ErrorMessage, run.LeaseOwner, run.LeaseExpiresAt = to, message, "", nil
	return nil
}

func (s *memoryRuntimeStore) CreateAsset(_ context.Context, asset *Asset) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	copy := *asset
	s.assets[asset.ID] = &copy
	return nil
}

func (s *memoryRuntimeStore) CreateAssetVersion(_ context.Context, version *AssetVersion) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var used int64
	for _, existing := range s.versions {
		if existing.OwnerUserID == version.OwnerUserID && existing.DeletedAt == nil {
			used += existing.SizeBytes
		}
	}
	if version.SizeBytes < 0 || used < 0 || version.SizeBytes > AssetQuotaBytes-used {
		return ErrAssetQuotaExceeded
	}
	copy := *version
	copy.Version = 1
	s.versions[version.ID] = &copy
	s.cache[version.InputHash] = version.ID
	if asset := s.assets[version.AssetID]; asset != nil {
		asset.Status, asset.CurrentVersionID = AssetReady, version.ID
	}
	return nil
}

func (s *memoryRuntimeStore) FindCachedAssetVersion(_ context.Context, userID uint64, hash string) (*AssetVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.findCacheErr != nil {
		return nil, s.findCacheErr
	}
	version := s.versions[s.cache[hash]]
	if version == nil || version.OwnerUserID != userID || version.DeletedAt != nil {
		return nil, ErrNotFound
	}
	if asset := s.assets[version.AssetID]; asset != nil {
		if asset.DeletedAt != nil {
			return nil, ErrNotFound
		}
		if version.CreatedByNodeRunID == "" && asset.CurrentVersionID != "" && asset.CurrentVersionID != version.ID {
			version = s.versions[asset.CurrentVersionID]
			if version == nil || version.InputHash != hash {
				return nil, ErrNotFound
			}
		}
	}
	if version.CreatedByNodeRunID != "" {
		creator := s.nodes[version.CreatedByNodeRunID]
		if creator == nil || creator.RunID != version.CreatedByRunID || creator.Status != NodeRunSucceeded {
			return nil, ErrNotFound
		}
		if creator.OutputVersionID != version.ID {
			version = s.versions[creator.OutputVersionID]
			if version == nil || version.InputHash != hash || version.CreatedByNodeRunID != creator.ID {
				return nil, ErrNotFound
			}
		}
	}
	if version.Status != AssetReady {
		return nil, ErrNotFound
	}
	copy := *version
	return &copy, nil
}

func (s *memoryRuntimeStore) GetAssetVersion(_ context.Context, userID uint64, id string) (*AssetVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.getVersionErr != nil {
		return nil, s.getVersionErr
	}
	version := s.versions[id]
	if version == nil || version.OwnerUserID != userID {
		return nil, ErrNotFound
	}
	copy := *version
	return &copy, nil
}

func (s *memoryRuntimeStore) UsedAssetBytes(_ context.Context, userID uint64) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var total int64
	for _, version := range s.versions {
		if version.OwnerUserID == userID {
			total += version.SizeBytes
		}
	}
	return total, nil
}

func (s *memoryRuntimeStore) AddAssetReference(_ context.Context, ref *AssetReference, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.addReferenceErr != nil {
		return s.addReferenceErr
	}
	s.refs = append(s.refs, *ref)
	return nil
}

func (s *memoryRuntimeStore) CreateCharge(_ context.Context, charge *ChargeReservation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.charges[charge.IdempotencyKey] != nil {
		return errors.New("duplicate charge")
	}
	copy := *charge
	copy.CreatedAt = time.Now()
	copy.UpdatedAt = copy.CreatedAt
	s.charges[charge.IdempotencyKey] = &copy
	return nil
}

func (s *memoryRuntimeStore) GetChargeByIdempotencyKey(_ context.Context, key string) (*ChargeReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.getChargeErr != nil {
		return nil, s.getChargeErr
	}
	charge := s.charges[key]
	if charge == nil {
		return nil, ErrNotFound
	}
	copy := *charge
	return &copy, nil
}

func (s *memoryRuntimeStore) UpdateChargeStatus(_ context.Context, id string, from, to ChargeStatus, actual int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, charge := range s.charges {
		if charge.ID == id {
			if charge.Status != from {
				return ErrInvalidState
			}
			charge.Status, charge.ActualAmount = to, actual
			charge.UpdatedAt = time.Now()
			return nil
		}
	}
	return ErrNotFound
}

func (s *memoryRuntimeStore) SetChargeBillingRef(_ context.Context, id, ref string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, charge := range s.charges {
		if charge.ID == id {
			charge.BillingRef = ref
			charge.UpdatedAt = time.Now()
			return nil
		}
	}
	return ErrNotFound
}

func (s *memoryRuntimeStore) CompareAndSwapChargeBillingRef(_ context.Context, id, from, to string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, charge := range s.charges {
		if charge.ID != id {
			continue
		}
		if charge.Status != ChargeReserved || charge.BillingRef != from {
			return ErrInvalidState
		}
		charge.BillingRef = to
		charge.UpdatedAt = time.Now()
		return nil
	}
	return ErrNotFound
}

func (s *memoryRuntimeStore) HasBillingTransaction(_ context.Context, userID uint64, refID string, kinds ...string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, kind := range kinds {
		if s.ledger[billingLedgerKey(userID, refID, kind)] {
			return true, nil
		}
	}
	return false, nil
}

func (s *memoryRuntimeStore) ReserveRunCredits(_ context.Context, reservation *RunCreditReservation) error {
	if s.reserveStarted != nil {
		select {
		case s.reserveStarted <- struct{}{}:
		default:
		}
	}
	if s.reserveRelease != nil {
		<-s.reserveRelease
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.reserveErrs) > 0 {
		err := s.reserveErrs[0]
		s.reserveErrs = s.reserveErrs[1:]
		return err
	}
	run := s.runs[reservation.RunID]
	charge := s.charges[reservation.IdempotencyKey]
	if run == nil || charge == nil || run.Status != reservation.ExpectedStatus || charge.ID != reservation.ChargeID ||
		charge.Status != ChargeReserved || charge.BillingRef != reservation.LockRef {
		return ErrInvalidState
	}
	if run.LeaseOwner == "" {
		run.LeaseOwner = reservation.WorkerID
		until := time.Now().Add(RuntimeLeaseDuration)
		run.LeaseExpiresAt = &until
	}
	if run.LeaseOwner != reservation.WorkerID || run.LeaseExpiresAt == nil || run.LeaseExpiresAt.Before(time.Now()) {
		return ErrInvalidState
	}
	s.ledger[billingLedgerKey(reservation.UserID, reservation.IdempotencyKey, "freeze")] = true
	charge.BillingRef, charge.UpdatedAt = "reserved", time.Now()
	s.reserveTransactions++
	return nil
}

func (s *memoryRuntimeStore) FinalizeRun(_ context.Context, finalization *RunFinalization) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listNodeRunsErr != nil {
		return 0, s.listNodeRunsErr
	}
	if s.getChargeErr != nil && finalization.BillingEnabled && finalization.EstimatedCost > 0 {
		return 0, s.getChargeErr
	}
	run := s.runs[finalization.RunID]
	if run == nil || run.Status != finalization.ExpectedStatus || run.LeaseOwner != finalization.WorkerID ||
		run.LeaseExpiresAt == nil || run.LeaseExpiresAt.Before(time.Now()) {
		return 0, ErrInvalidState
	}
	var actual int64
	for _, node := range s.nodes {
		if node.RunID == finalization.RunID {
			actual += node.CreditCost
		}
	}
	if finalization.BillingEnabled && finalization.EstimatedCost > 0 {
		key := "video-workflow:" + finalization.RunID
		charge := s.charges[key]
		if charge == nil {
			if finalization.TargetStatus != RunCanceled || actual != 0 {
				return 0, ErrNotFound
			}
		} else if charge.Status == ChargeRefunded {
			if finalization.TargetStatus == RunSucceeded || actual != 0 {
				return 0, ErrInvalidState
			}
		} else if charge.Status == ChargeSettled {
			if charge.ActualAmount != actual {
				return 0, ErrInvalidState
			}
		} else if charge.Status == ChargeReserved {
			frozen := s.ledger[billingLedgerKey(finalization.UserID, key, "freeze")]
			if !frozen && finalization.TargetStatus == RunCanceled && actual == 0 {
				charge.Status, charge.ActualAmount = ChargeRefunded, 0
			} else if !frozen {
				return 0, ErrInvalidState
			} else {
				s.ledger[billingLedgerKey(finalization.UserID, key, "unfreeze")] = true
				if actual > 0 {
					s.ledger[billingLedgerKey(finalization.UserID, key, "consume")] = true
				}
				charge.Status, charge.ActualAmount = ChargeSettled, actual
				s.finalizeSettlements++
			}
		} else {
			return 0, ErrInvalidState
		}
	}
	run.Status, run.ActualCost, run.OutputVersionID = finalization.TargetStatus, actual, finalization.OutputVersionID
	run.ErrorCode, run.ErrorMessage, run.LeaseOwner, run.LeaseExpiresAt = finalization.ErrorCode, finalization.ErrorMessage, "", nil
	return actual, nil
}

func (s *memoryRuntimeStore) recordBillingTransaction(userID uint64, refID, kind string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ledger[billingLedgerKey(userID, refID, kind)] = true
}

func (s *memoryRuntimeStore) chargeByKey(key string) *ChargeReservation {
	s.mu.Lock()
	defer s.mu.Unlock()
	if charge := s.charges[key]; charge != nil {
		copy := *charge
		return &copy
	}
	return nil
}

func billingLedgerKey(userID uint64, refID, kind string) string {
	return fmt.Sprintf("%d/%s/%s", userID, refID, kind)
}

func (s *memoryRuntimeStore) CreateApproval(_ context.Context, approval *Approval) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	copy := *approval
	s.approvals[approval.ID] = &copy
	return nil
}

func (s *memoryRuntimeStore) CommitApprovalDecision(_ context.Context, commit *ApprovalDecisionCommit) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	run := s.runs[commit.RunID]
	if run == nil {
		return ErrNotFound
	}
	if !isApprovalDecisionSuccessor(commit.ExpectedStatus, run.Status) {
		return ErrInvalidState
	}
	idempotent := run.Status != commit.ExpectedStatus
	type decisionRows struct {
		decision *ApprovalNodeDecision
		node     *NodeRun
		approval *Approval
	}
	rows := make([]decisionRows, 0, len(commit.Decisions))
	seen := make(map[string]bool, len(commit.Decisions))
	for i := range commit.Decisions {
		decision := &commit.Decisions[i]
		if seen[decision.NodeRunID] {
			return ErrInvalidState
		}
		seen[decision.NodeRunID] = true
		node := s.nodes[decision.NodeRunID]
		var approval *Approval
		for _, candidate := range s.approvals {
			if candidate.RunID == commit.RunID && candidate.NodeRunID == decision.NodeRunID && candidate.InputHash == decision.InputHash && candidate.Type == decision.ApprovalType {
				approval = candidate
				break
			}
		}
		if node == nil || approval == nil {
			return ErrInvalidState
		}
		if idempotent {
			if approval.Status != "approved" || node.Status != NodeRunSucceeded || node.OutputVersionID != decision.OutputVersionID ||
				!bytes.Equal(normalizeJSON(approval.Payload), normalizeJSON(decision.DecisionPayload)) ||
				!bytes.Equal(normalizeJSON(node.Output), normalizeJSON(decision.Output)) {
				return ErrInvalidState
			}
		} else if approval.Status != "pending" || node.Status != NodeRunAwaitingApproval {
			return ErrInvalidState
		}
		rows = append(rows, decisionRows{decision: decision, node: node, approval: approval})
	}
	if !idempotent {
		approvalCount := 0
		for _, approval := range s.approvals {
			if approval.RunID == commit.RunID && approval.Type == commit.Decisions[0].ApprovalType {
				approvalCount++
				if !seen[approval.NodeRunID] {
					return ErrInvalidState
				}
			}
		}
		if approvalCount != len(commit.Decisions) {
			return ErrInvalidState
		}
		for i := range rows {
			row := &rows[i]
			row.approval.Status, row.approval.Payload = "approved", append(json.RawMessage(nil), row.decision.DecisionPayload...)
			row.node.Status, row.node.OutputVersionID = NodeRunSucceeded, row.decision.OutputVersionID
			row.node.Output = append(json.RawMessage(nil), row.decision.Output...)
		}
		run.Status, run.LeaseOwner, run.LeaseExpiresAt = RunRunning, "", nil
	}
	return nil
}

func (s *memoryRuntimeStore) DecideApproval(_ context.Context, runID, nodeRunID, hash, kind string, payload json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, approval := range s.approvals {
		if approval.RunID == runID && approval.NodeRunID == nodeRunID && approval.InputHash == hash && approval.Type == kind && approval.Status == "pending" {
			approval.Status, approval.Payload = "approved", append(json.RawMessage(nil), payload...)
			return nil
		}
	}
	return ErrInvalidState
}

func (s *memoryRuntimeStore) GetApprovalByNode(_ context.Context, runID, nodeRunID, hash, kind string) (*Approval, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, approval := range s.approvals {
		if approval.RunID == runID && approval.NodeRunID == nodeRunID && approval.InputHash == hash && approval.Type == kind {
			copy := *approval
			copy.Payload = append(json.RawMessage(nil), approval.Payload...)
			return &copy, nil
		}
	}
	return nil, ErrNotFound
}

func (s *memoryRuntimeStore) nodeByRunAndNode(runID, nodeID string) *NodeRun {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, node := range s.nodes {
		if node.RunID == runID && node.NodeID == nodeID {
			copy := *node
			return &copy
		}
	}
	return nil
}

func (s *memoryRuntimeStore) CancelExpiredApprovals(context.Context) ([]string, error) {
	return nil, nil
}
func (s *memoryRuntimeStore) GetRunByID(_ context.Context, id string) (*Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if run := s.runs[id]; run != nil {
		copy := *run
		return &copy, nil
	}
	return nil, ErrNotFound
}
func (s *memoryRuntimeStore) ClaimNextRun(context.Context, string, time.Duration) (*Run, error) {
	return nil, ErrNotFound
}
func (s *memoryRuntimeStore) HeartbeatRun(context.Context, string, string, RunStatus, time.Duration) error {
	return nil
}

func (s *memoryRuntimeStore) RenewRunFinalizationLease(_ context.Context, runID, workerID string, status RunStatus, lease time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	run := s.runs[runID]
	if run == nil || run.Status != status {
		return ErrInvalidState
	}
	if run.LeaseOwner == "" {
		run.LeaseOwner = workerID
		until := time.Now().Add(RuntimeLeaseDuration)
		run.LeaseExpiresAt = &until
	}
	if run.LeaseOwner != workerID || run.LeaseExpiresAt == nil || run.LeaseExpiresAt.Before(time.Now()) {
		return ErrInvalidState
	}
	until := time.Now().Add(lease)
	run.LeaseExpiresAt = &until
	return nil
}

func (s *memoryRuntimeStore) AcquireRuntimeSlot(_ context.Context, kind string, capacity int, runID, nodeRunID, workerID, token string, lease time.Duration) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, node := s.runs[runID], s.nodes[nodeRunID]
	if run == nil || node == nil || run.Status != RunRunning || node.RunID != runID || node.Status != NodeRunRunning {
		return 0, ErrInvalidState
	}
	if run.LeaseOwner == "" {
		run.LeaseOwner = workerID
		until := time.Now().Add(RuntimeLeaseDuration)
		run.LeaseExpiresAt = &until
	}
	if run.LeaseOwner != workerID || run.LeaseExpiresAt == nil || run.LeaseExpiresAt.Before(time.Now()) {
		return 0, ErrInvalidState
	}
	now := time.Now()
	for index := 0; index < capacity; index++ {
		key := fmt.Sprintf("%s/%d", kind, index)
		slot := s.slots[key]
		if slot != nil && slot.token != "" && slot.expiresAt.After(now) {
			continue
		}
		s.slots[key] = &memoryRuntimeSlot{kind: kind, index: index, runID: runID, nodeRunID: nodeRunID, workerID: workerID, token: token, expiresAt: now.Add(lease)}
		return index, nil
	}
	return 0, ErrNotFound
}

func (s *memoryRuntimeStore) RenewRuntimeSlot(_ context.Context, kind string, index int, runID, nodeRunID, workerID, token string, lease time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	slot := s.slots[fmt.Sprintf("%s/%d", kind, index)]
	run := s.runs[runID]
	if slot == nil || run == nil || slot.token != token || slot.runID != runID || slot.nodeRunID != nodeRunID || slot.workerID != workerID ||
		slot.expiresAt.Before(time.Now()) || run.Status != RunRunning || run.LeaseOwner != workerID || run.LeaseExpiresAt == nil || run.LeaseExpiresAt.Before(time.Now()) {
		return ErrInvalidState
	}
	slot.expiresAt = time.Now().Add(lease)
	return nil
}

func (s *memoryRuntimeStore) ReleaseRuntimeSlot(_ context.Context, kind string, index int, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := fmt.Sprintf("%s/%d", kind, index)
	slot := s.slots[key]
	if slot == nil || slot.token != token {
		return ErrInvalidState
	}
	delete(s.slots, key)
	return nil
}

type failSubmittedStore struct {
	*memoryRuntimeStore
	failSubmitted     bool
	remainingFailures int
}

type failNodeResultCommitStore struct {
	*memoryRuntimeStore
	failMediaCommit  bool
	failCachedCommit bool
	commitThenError  bool
	failGetNodeRuns  int
}

type ambiguousFencedStore struct {
	*memoryRuntimeStore
	failStart                 bool
	startCommitThenError      bool
	failSubmitting            bool
	submittingCommitThenError bool
}

func (s *ambiguousFencedStore) UpdateNodeRunStatusFenced(ctx context.Context, transition *FencedNodeRunTransition) error {
	if s.failStart && transition.From == NodeRunQueued && transition.To == NodeRunRunning {
		s.failStart = false
		if s.startCommitThenError {
			if err := s.memoryRuntimeStore.UpdateNodeRunStatusFenced(ctx, transition); err != nil {
				return err
			}
		}
		return errors.New("start acknowledgement lost")
	}
	return s.memoryRuntimeStore.UpdateNodeRunStatusFenced(ctx, transition)
}

func (s *ambiguousFencedStore) UpdateNodeRunProgressFenced(ctx context.Context, runID, workerID, nodeRunID, inputHash string, progress int, taskID string, provider json.RawMessage) error {
	if s.failSubmitting && providerStatePhase(provider) == "submitting" {
		s.failSubmitting = false
		if s.submittingCommitThenError {
			if err := s.memoryRuntimeStore.UpdateNodeRunProgressFenced(ctx, runID, workerID, nodeRunID, inputHash, progress, taskID, provider); err != nil {
				return err
			}
		}
		return errors.New("submitting acknowledgement lost")
	}
	return s.memoryRuntimeStore.UpdateNodeRunProgressFenced(ctx, runID, workerID, nodeRunID, inputHash, progress, taskID, provider)
}

type ambiguousPauseStore struct {
	*memoryRuntimeStore
	fail            bool
	commitThenError bool
}

func (s *ambiguousPauseStore) UpdateRunStatusFenced(ctx context.Context, id, workerID string, from, to RunStatus, message string) error {
	if s.fail {
		s.fail = false
		if s.commitThenError {
			if err := s.memoryRuntimeStore.UpdateRunStatusFenced(ctx, id, workerID, from, to, message); err != nil {
				return err
			}
		}
		return errors.New("pause acknowledgement lost")
	}
	return s.memoryRuntimeStore.UpdateRunStatusFenced(ctx, id, workerID, from, to, message)
}

type ambiguousFinalizeStore struct {
	*memoryRuntimeStore
	fail            bool
	commitThenError bool
}

type ambiguousCostStore struct {
	*memoryRuntimeStore
	failures        int
	commitThenError bool
	readFailures    int
}

type ambiguousSlotStore struct {
	*memoryRuntimeStore
	fail            bool
	commitThenError bool
}

type ambiguousCreateNodeStore struct {
	*memoryRuntimeStore
	fail            bool
	targetNodeID    string
	commitThenError bool
	findError       bool
}

func (s *ambiguousCreateNodeStore) CreateNodeRun(ctx context.Context, node *NodeRun, nodeType NodeType, attempt int) error {
	if s.fail && node.NodeID == s.targetNodeID {
		s.fail = false
		if s.commitThenError {
			if err := s.memoryRuntimeStore.CreateNodeRun(ctx, node, nodeType, attempt); err != nil {
				return err
			}
		}
		return errors.New("create node acknowledgement lost")
	}
	return s.memoryRuntimeStore.CreateNodeRun(ctx, node, nodeType, attempt)
}

func (s *ambiguousCreateNodeStore) FindNodeRun(ctx context.Context, runID, nodeID string) (*NodeRun, error) {
	if s.findError && nodeID == s.targetNodeID {
		s.findError = false
		return nil, errors.New("find node unavailable")
	}
	return s.memoryRuntimeStore.FindNodeRun(ctx, runID, nodeID)
}

func (s *ambiguousSlotStore) AcquireRuntimeSlot(ctx context.Context, kind string, capacity int, runID, nodeRunID, workerID, token string, lease time.Duration) (int, error) {
	if s.fail {
		s.fail = false
		if s.commitThenError {
			index, err := s.memoryRuntimeStore.AcquireRuntimeSlot(ctx, kind, capacity, runID, nodeRunID, workerID, token, lease)
			if err != nil {
				return 0, err
			}
			return index, errors.New("slot acknowledgement lost")
		}
		return 0, errors.New("slot database unavailable")
	}
	return s.memoryRuntimeStore.AcquireRuntimeSlot(ctx, kind, capacity, runID, nodeRunID, workerID, token, lease)
}

func (s *ambiguousCostStore) RecordNodeRunCost(ctx context.Context, id string, cost int64) error {
	if s.failures > 0 {
		s.failures--
		if s.commitThenError {
			s.commitThenError = false
			if err := s.memoryRuntimeStore.RecordNodeRunCost(ctx, id, cost); err != nil {
				return err
			}
		}
		return errors.New("cost acknowledgement lost")
	}
	return s.memoryRuntimeStore.RecordNodeRunCost(ctx, id, cost)
}

func (s *ambiguousCostStore) GetNodeRun(ctx context.Context, id string) (*NodeRun, error) {
	if s.readFailures > 0 {
		s.readFailures--
		return nil, errors.New("cost reconciliation unavailable")
	}
	return s.memoryRuntimeStore.GetNodeRun(ctx, id)
}

func (s *ambiguousFinalizeStore) FinalizeRun(ctx context.Context, finalization *RunFinalization) (int64, error) {
	if s.fail {
		s.fail = false
		if s.commitThenError {
			actual, err := s.memoryRuntimeStore.FinalizeRun(ctx, finalization)
			if err != nil {
				return 0, err
			}
			return actual, errors.New("finalization acknowledgement lost")
		}
		return 0, errors.New("finalization unavailable")
	}
	return s.memoryRuntimeStore.FinalizeRun(ctx, finalization)
}

func (s *failNodeResultCommitStore) CommitNodeResult(ctx context.Context, commit *NodeResultCommit) error {
	if (s.failMediaCommit && len(commit.Versions) > 0) || (s.failCachedCommit && commit.CacheHit) {
		s.failMediaCommit, s.failCachedCommit = false, false
		if s.commitThenError {
			if err := s.memoryRuntimeStore.CommitNodeResult(ctx, commit); err != nil {
				return err
			}
		}
		return errors.New("node result transaction unavailable")
	}
	return s.memoryRuntimeStore.CommitNodeResult(ctx, commit)
}

func (s *failNodeResultCommitStore) GetNodeRun(ctx context.Context, id string) (*NodeRun, error) {
	if s.failGetNodeRuns > 0 {
		s.failGetNodeRuns--
		return nil, errors.New("node reconciliation unavailable")
	}
	return s.memoryRuntimeStore.GetNodeRun(ctx, id)
}

func (s *failSubmittedStore) UpdateNodeRunProgress(ctx context.Context, id string, progress int, taskID string, provider json.RawMessage) error {
	if taskID != "" && (s.failSubmitted || s.remainingFailures > 0) {
		if s.remainingFailures > 0 {
			s.remainingFailures--
		}
		return errors.New("database unavailable")
	}
	return s.memoryRuntimeStore.UpdateNodeRunProgress(ctx, id, progress, taskID, provider)
}

func (s *failSubmittedStore) UpdateNodeRunProgressFenced(ctx context.Context, runID, workerID, nodeRunID, inputHash string, progress int, taskID string, provider json.RawMessage) error {
	if taskID != "" && (s.failSubmitted || s.remainingFailures > 0) {
		if s.remainingFailures > 0 {
			s.remainingFailures--
		}
		return errors.New("database unavailable")
	}
	return s.memoryRuntimeStore.UpdateNodeRunProgressFenced(ctx, runID, workerID, nodeRunID, inputHash, progress, taskID, provider)
}
