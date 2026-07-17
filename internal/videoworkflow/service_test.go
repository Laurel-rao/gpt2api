package videoworkflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestService_CreateWorkflowClonesTemplate(t *testing.T) {
	store := newFakeStore(t)
	service := NewService(store)
	workflow, err := service.CreateWorkflow(context.Background(), CreateWorkflowInput{UserID: 7, TemplateID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if workflow.UserID != 7 || workflow.Revision != 1 || workflow.TemplateVersion != 1 {
		t.Fatalf("unexpected workflow: %+v", workflow)
	}
	workflow.Graph.Nodes[0].ID = "mutated"
	if store.template.Graph.Nodes[0].ID == "mutated" {
		t.Fatal("workflow graph aliases template graph")
	}
}

func TestService_EnsureTemplateAndEstimate(t *testing.T) {
	store := newFakeStore(t)
	runtime := &fakeRuntime{}
	service := NewService(store)
	service.SetRuntime(runtime)
	if err := service.EnsureBuiltinTemplate(context.Background()); err != nil {
		t.Fatal(err)
	}
	workflow, _ := service.CreateWorkflow(context.Background(), CreateWorkflowInput{UserID: 7, TemplateID: store.template.ID})
	estimate, err := service.EstimateRun(context.Background(), 7, workflow.ID)
	if err != nil {
		t.Fatal(err)
	}
	if estimate.Token != "token" || estimate.TotalCredits != 1 {
		t.Fatalf("estimate = %+v", estimate)
	}
}

func TestService_UpdateRuntimeSettingsAppliesRuntimeConcurrency(t *testing.T) {
	service := NewService(newFakeStore(t))
	runtime := &fakeRuntime{}
	service.SetRuntime(runtime)
	next, err := service.UpdateRuntimeSettings(context.Background(), RuntimeConcurrency{
		WorkerConcurrency:  6,
		TextConcurrency:    3,
		ImageConcurrency:   4,
		VideoConcurrency:   5,
		ComposeConcurrency: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if next.TextConcurrency != 3 || next.ImageConcurrency != 4 || next.VideoConcurrency != 5 || next.ComposeConcurrency != 2 || next.WorkerConcurrency != 6 {
		t.Fatalf("unexpected concurrency: %+v", next)
	}
	if runtime.concurrency != next {
		t.Fatalf("runtime concurrency not updated: %+v", runtime.concurrency)
	}
}

func TestService_ReadDeleteAndValidationWrappers(t *testing.T) {
	store := newFakeStore(t)
	service := NewService(store)
	workflow, _ := service.CreateWorkflow(context.Background(), CreateWorkflowInput{UserID: 7, TemplateID: 1})
	if templates, err := service.ListTemplates(context.Background()); err != nil || len(templates) != 1 {
		t.Fatalf("templates=%v err=%v", templates, err)
	}
	if rows, total, err := service.ListWorkflows(context.Background(), 7, 20, 0); err != nil || total != 1 || len(rows) != 1 {
		t.Fatalf("workflows=%v total=%d err=%v", rows, total, err)
	}
	if _, err := service.GetWorkflow(context.Background(), 7, workflow.ID); err != nil {
		t.Fatal(err)
	}
	if errs, err := service.ValidateWorkflow(context.Background(), 7, workflow.ID); err != nil || len(errs) != 0 {
		t.Fatalf("validation=%v err=%v", errs, err)
	}
	if err := service.DeleteWorkflow(context.Background(), 7, workflow.ID); err != nil {
		t.Fatal(err)
	}
}

func TestService_ValidateWorkflowGraphUsesRequestBody(t *testing.T) {
	store := newFakeStore(t)
	service := NewService(store)
	workflow, err := service.CreateWorkflow(context.Background(), CreateWorkflowInput{UserID: 7, TemplateID: 1, Name: "校验"})
	if err != nil {
		t.Fatal(err)
	}

	if errs, err := service.ValidateWorkflow(context.Background(), 7, workflow.ID); err != nil || len(errs) != 0 {
		t.Fatalf("empty-path validation=%v err=%v", errs, err)
	}

	invalid, err := CloneGraph(workflow.Graph)
	if err != nil {
		t.Fatal(err)
	}
	invalid.Edges[0].TargetPort = "missing"
	errs, err := service.ValidateWorkflowGraph(context.Background(), 7, workflow.ID, invalid)
	if err != nil {
		t.Fatal(err)
	}
	if !errs.Has(ValidationPortNotFound) {
		t.Fatalf("expected port_not_found, got %#v", errs)
	}

	saved, err := service.ValidateWorkflow(context.Background(), 7, workflow.ID)
	if err != nil || len(saved) != 0 {
		t.Fatalf("saved graph should still be valid: errs=%v err=%v", saved, err)
	}

	if _, err := service.ValidateWorkflowGraph(context.Background(), 8, workflow.ID, workflow.Graph); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other user error=%v", err)
	}
	if _, err := service.ValidateWorkflow(context.Background(), 7, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing workflow error=%v", err)
	}
	if _, err := service.ValidateWorkflowGraph(context.Background(), 7, "missing", workflow.Graph); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing workflow with graph error=%v", err)
	}

	store.workflow.Graph.Edges = nil
	if errs, err := service.ValidateWorkflow(context.Background(), 7, workflow.ID); err != nil || len(errs) == 0 {
		t.Fatalf("expected saved-graph validation failures, errs=%v err=%v", errs, err)
	}
}

func TestService_RuntimeRequiredAndTerminalCancelRejected(t *testing.T) {
	store := newFakeStore(t)
	service := NewService(store)
	workflow, _ := service.CreateWorkflow(context.Background(), CreateWorkflowInput{UserID: 7, TemplateID: 1})
	if _, err := service.EstimateRun(context.Background(), 7, workflow.ID); err == nil {
		t.Fatal("estimate accepted without runtime")
	}
	store.run = &Run{ID: "run", UserID: 7, Status: RunSucceeded}
	if err := service.CancelRun(context.Background(), 7, "run"); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("terminal cancel error = %v", err)
	}
	store.run.Status = RunAwaitingCharacterApproval
	if err := service.ApproveCharacters(context.Background(), 7, "run", CharacterApproval{}); err == nil {
		t.Fatal("empty character approval was accepted")
	}
	validCharacters := CharacterApproval{Selections: []CharacterSelection{{NodeRunID: "node", InputHash: "hash", SelectedVersionID: "version"}}}
	if err := service.ApproveCharacters(context.Background(), 7, "run", validCharacters); err == nil {
		t.Fatal("approval accepted without runtime")
	}
	store.run.Status = RunAwaitingStoryboardApproval
	validStoryboard := StoryboardApproval{NodeRunID: "node", InputHash: "hash", Script: []byte(`{"scenes":[]}`)}
	if err := service.ApproveStoryboard(context.Background(), 7, "run", validStoryboard); err == nil {
		t.Fatal("storyboard accepted without runtime")
	}
	store.run.Status = RunRunning
	if err := service.CancelRun(context.Background(), 7, "run"); err != nil || store.run.Status != RunCancelPending {
		t.Fatalf("cancel without runtime status=%s err=%v", store.run.Status, err)
	}
}

func TestService_DrainRejectsOnlyNewRuns(t *testing.T) {
	store := newFakeStore(t)
	service := NewService(store)
	workflow, _ := service.CreateWorkflow(context.Background(), CreateWorkflowInput{UserID: 7, TemplateID: 1})
	service.SetAcceptNewRuns(false)
	if _, err := service.StartRun(context.Background(), 7, workflow.ID); !errors.Is(err, ErrRunsDraining) {
		t.Fatalf("draining start error=%v", err)
	}
	if _, err := service.GetWorkflow(context.Background(), 7, workflow.ID); err != nil {
		t.Fatalf("draining blocked read: %v", err)
	}
	service.SetAcceptNewRuns(true)
	if !service.AcceptNewRuns() {
		t.Fatal("accept_new_runs did not reopen")
	}
}

func TestService_RunDetailDecoratesCandidatesAndOutput(t *testing.T) {
	base := newFakeStore(t)
	base.run = &Run{ID: "run", UserID: 7, Status: RunAwaitingCharacterApproval, OutputVersionID: "final"}
	store := &detailTestStore{fakeStore: base, versions: map[string]AssetVersion{
		"candidate-1": {ID: "candidate-1", AssetID: "characters", OwnerUserID: 7, Status: AssetReady},
		"candidate-2": {ID: "candidate-2", AssetID: "characters", OwnerUserID: 7, Status: AssetReady},
		"final":       {ID: "final", AssetID: "output", OwnerUserID: 7, Status: AssetReady},
	}}
	candidateOutput, _ := json.Marshal(map[string]any{"candidate_version_ids": []string{"candidate-1", "candidate-2"}})
	store.nodes = []NodeRun{{ID: "character-node", RunID: "run", NodeID: "role", NodeType: NodeCharacter, Status: NodeRunAwaitingApproval, Output: candidateOutput}}
	service := NewService(store)
	service.ConfigureMedia(t.TempDir(), "secret", NewComposer())
	detail, err := service.GetRunDetail(context.Background(), 7, "run")
	if err != nil {
		t.Fatal(err)
	}
	var output struct {
		Candidates []struct {
			VersionID  string `json:"version_id"`
			PreviewURL string `json:"preview_url"`
		} `json:"candidates"`
	}
	if json.Unmarshal(detail.NodeRuns[0].Output, &output) != nil || len(output.Candidates) != 2 || output.Candidates[0].PreviewURL == "" {
		t.Fatalf("decorated output=%s", detail.NodeRuns[0].Output)
	}
	if detail.OutputURL == "" || detail.Output == nil || detail.Output.PreviewURL != detail.OutputURL {
		t.Fatalf("detail output url=%q output=%+v", detail.OutputURL, detail.Output)
	}
}

func TestService_CreateAndUpdateInputErrors(t *testing.T) {
	store := newFakeStore(t)
	service := NewService(store)
	if _, err := service.CreateWorkflow(context.Background(), CreateWorkflowInput{UserID: 7, TemplateID: 999}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing template error = %v", err)
	}
	workflow, _ := service.CreateWorkflow(context.Background(), CreateWorkflowInput{UserID: 7, TemplateID: 1})
	if _, err := service.UpdateWorkflow(context.Background(), UpdateWorkflowInput{UserID: 7, WorkflowID: workflow.ID, ExpectedRevision: 1, Graph: workflow.Graph}); err == nil {
		t.Fatal("empty workflow name was accepted")
	}
}

func TestService_UpdateWorkflowRevisionAndDraft(t *testing.T) {
	store := newFakeStore(t)
	service := NewService(store)
	workflow, err := service.CreateWorkflow(context.Background(), CreateWorkflowInput{UserID: 7, TemplateID: 1, Name: "原名"})
	if err != nil {
		t.Fatal(err)
	}
	draft := workflow.Graph
	draft.Edges = nil
	updated, err := service.UpdateWorkflow(context.Background(), UpdateWorkflowInput{
		UserID: 7, WorkflowID: workflow.ID, ExpectedRevision: 1, Name: "草稿", Graph: draft,
	})
	if err != nil {
		t.Fatalf("incomplete draft should save: %v", err)
	}
	if updated.Revision != 2 {
		t.Fatalf("revision = %d", updated.Revision)
	}
	_, err = service.UpdateWorkflow(context.Background(), UpdateWorkflowInput{
		UserID: 7, WorkflowID: workflow.ID, ExpectedRevision: 1, Name: "冲突", Graph: draft,
	})
	if !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale revision error = %v", err)
	}
}

func TestService_StartRunFreezesSnapshot(t *testing.T) {
	store := newFakeStore(t)
	service := NewService(store)
	workflow, _ := service.CreateWorkflow(context.Background(), CreateWorkflowInput{UserID: 7, TemplateID: 1})
	dispatcher := &fakeRuntime{}
	service.SetRuntime(dispatcher)
	run, err := service.StartRun(context.Background(), 7, workflow.ID)
	if err != nil {
		t.Fatal(err)
	}
	if dispatcher.dispatched != run || run.Status != RunQueued || run.Revision != workflow.Revision {
		t.Fatalf("run was not dispatched correctly: %+v", run)
	}
	store.workflow.Graph.Nodes[0].ID = "changed-after-run"
	if run.GraphSnapshot.Nodes[0].ID == "changed-after-run" {
		t.Fatal("run snapshot aliases workflow graph")
	}
}

func TestService_StartRunRejectsIncompleteGraph(t *testing.T) {
	store := newFakeStore(t)
	service := NewService(store)
	workflow, _ := service.CreateWorkflow(context.Background(), CreateWorkflowInput{UserID: 7, TemplateID: 1})
	store.workflow.Graph.Edges = nil
	_, err := service.StartRun(context.Background(), 7, workflow.ID)
	var validation *GraphValidationError
	if !errors.As(err, &validation) || !validation.Errors.Has(ValidationRequiredInputMissing) {
		t.Fatalf("start error = %v", err)
	}
}

func TestService_RuntimeV2RequiresEstimateToken(t *testing.T) {
	store := newFakeStore(t)
	service := NewService(store)
	workflow, _ := service.CreateWorkflow(context.Background(), CreateWorkflowInput{UserID: 7, TemplateID: 1})
	runtime := &strictFakeRuntime{fakeRuntime: fakeRuntime{}}
	service.SetRuntime(runtime)
	input := StartRunInput{Revision: workflow.Revision, RunMode: RunModeFull, RequestID: "request-1"}
	if _, err := service.StartRunWithInput(context.Background(), 7, workflow.ID, input); !errors.Is(err, ErrInvalidEstimate) {
		t.Fatalf("missing token error=%v", err)
	}
	input.EstimateToken = "valid"
	if _, err := service.StartRunWithInput(context.Background(), 7, workflow.ID, input); err != nil {
		t.Fatal(err)
	}
}

func TestService_StartRunRequestIDIsIdempotentUnderConcurrency(t *testing.T) {
	base := newFakeStore(t)
	store := &idempotentRunStore{fakeStore: base, byRequest: make(map[string]*Run)}
	service := NewService(store)
	workflow, _ := service.CreateWorkflow(context.Background(), CreateWorkflowInput{UserID: 7, TemplateID: 1})
	runtime := &idempotentRuntime{}
	service.SetRuntime(runtime)
	input := StartRunInput{Revision: workflow.Revision, RunMode: RunModeFull, EstimateToken: "valid", RequestID: "same-request"}
	var wg sync.WaitGroup
	errorsCh := make(chan error, 10)
	ids := make(chan string, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			run, err := service.StartRunWithInput(context.Background(), 7, workflow.ID, input)
			if err != nil {
				errorsCh <- err
				return
			}
			ids <- run.ID
		}()
	}
	wg.Wait()
	close(errorsCh)
	close(ids)
	for err := range errorsCh {
		t.Fatal(err)
	}
	unique := make(map[string]bool)
	for id := range ids {
		unique[id] = true
	}
	if len(unique) != 1 || store.createCount != 1 || runtime.dispatches.Load() != 1 {
		t.Fatalf("ids=%v creates=%d dispatches=%d", unique, store.createCount, runtime.dispatches.Load())
	}
}

func TestService_StartRunRetryUsesRequestIDBeforeDrainWorkflowAndTokenExpiry(t *testing.T) {
	base := newFakeStore(t)
	store := &idempotentRunStore{fakeStore: base, byRequest: make(map[string]*Run)}
	runtime := &retryRuntime{}
	service := NewService(store)
	service.SetRuntime(runtime)
	workflow, _ := service.CreateWorkflow(context.Background(), CreateWorkflowInput{UserID: 7, TemplateID: 1})
	input := StartRunInput{Revision: workflow.Revision, RunMode: RunModeFull, EstimateToken: "original", RequestID: "stable-request"}
	created, err := service.StartRunWithInput(context.Background(), 7, workflow.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.validates.Load() != 1 || runtime.dispatches.Load() != 1 {
		t.Fatalf("validates=%d dispatches=%d", runtime.validates.Load(), runtime.dispatches.Load())
	}
	runtime.rejectOriginal.Store(true)
	service.SetAcceptNewRuns(false)
	base.workflow = nil
	retried, err := service.StartRunWithInput(context.Background(), 7, workflow.ID, input)
	if err != nil || retried.ID != created.ID {
		t.Fatalf("retry=%+v err=%v", retried, err)
	}
	if runtime.validates.Load() != 1 || runtime.dispatches.Load() != 1 {
		t.Fatalf("expired exact retry revalidated or dispatched: validates=%d dispatches=%d", runtime.validates.Load(), runtime.dispatches.Load())
	}

	renewed := input
	renewed.EstimateToken = "renewed"
	if retried, err = service.StartRunWithInput(context.Background(), 7, workflow.ID, renewed); err != nil || retried.ID != created.ID {
		t.Fatalf("renewed retry=%+v err=%v", retried, err)
	}
	if runtime.validates.Load() != 2 || runtime.dispatches.Load() != 1 {
		t.Fatalf("renewed validates=%d dispatches=%d", runtime.validates.Load(), runtime.dispatches.Load())
	}
}

func TestService_StartRunRequestIDRejectsSemanticConflicts(t *testing.T) {
	base := newFakeStore(t)
	store := &idempotentRunStore{fakeStore: base, byRequest: make(map[string]*Run)}
	runtime := &retryRuntime{}
	service := NewService(store)
	service.SetRuntime(runtime)
	workflow, _ := service.CreateWorkflow(context.Background(), CreateWorkflowInput{UserID: 7, TemplateID: 1})
	input := StartRunInput{Revision: workflow.Revision, RunMode: RunModeDownstream, StartNodeID: "background_1", EstimateToken: "original", RequestID: "semantic-request"}
	if _, err := service.StartRunWithInput(context.Background(), 7, workflow.ID, input); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name       string
		workflowID string
		mutate     func(*StartRunInput)
	}{
		{name: "workflow", workflowID: "different-workflow"},
		{name: "revision", mutate: func(in *StartRunInput) { in.Revision++ }},
		{name: "mode", mutate: func(in *StartRunInput) { in.RunMode = RunModeNodeOnly }},
		{name: "start", mutate: func(in *StartRunInput) { in.StartNodeID = "scene_1" }},
		{name: "estimate hash", mutate: func(in *StartRunInput) { in.EstimateToken = "different-hash" }},
		{name: "estimate credits", mutate: func(in *StartRunInput) { in.EstimateToken = "different-cost" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidate := input
			if tt.mutate != nil {
				tt.mutate(&candidate)
			}
			workflowID := workflow.ID
			if tt.workflowID != "" {
				workflowID = tt.workflowID
			}
			if _, err := service.StartRunWithInput(context.Background(), 7, workflowID, candidate); !errors.Is(err, ErrRequestConflict) {
				t.Fatalf("error=%v", err)
			}
		})
	}
	store.mu.Lock()
	stored := store.byRequest[fmt.Sprintf("%d/%s", uint64(7), input.RequestID)]
	stored.EstimateHash = ""
	store.mu.Unlock()
	if _, err := service.StartRunWithInput(context.Background(), 7, workflow.ID, input); !errors.Is(err, ErrRequestConflict) {
		t.Fatalf("missing estimate hash error=%v", err)
	}
	if runtime.dispatches.Load() != 1 || store.createCount != 1 {
		t.Fatalf("dispatches=%d creates=%d", runtime.dispatches.Load(), store.createCount)
	}
}

func TestService_StartRunInvalidParametersAreTyped(t *testing.T) {
	service := NewService(newFakeStore(t))
	for _, input := range []StartRunInput{
		{},
		{RequestID: "request", RunMode: "unknown"},
		{RequestID: "request", RunMode: RunModeNodeOnly},
		{RequestID: "request", RunMode: RunModeFull, StartNodeID: "node"},
	} {
		if _, err := service.StartRunWithInput(context.Background(), 7, "workflow", input); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("input=%+v error=%v", input, err)
		}
	}
}

func TestService_DispatchFailureMarksRunFailed(t *testing.T) {
	store := newFakeStore(t)
	service := NewService(store)
	workflow, _ := service.CreateWorkflow(context.Background(), CreateWorkflowInput{UserID: 7, TemplateID: 1})
	service.SetDispatcher(errorDispatcher{})
	run, err := service.StartRun(context.Background(), 7, workflow.ID)
	if err == nil || run.Status != RunFailed || store.run.Status != RunFailed {
		t.Fatalf("run=%+v err=%v stored=%+v", run, err, store.run)
	}
}

func TestService_CancelRunUsesCancelPending(t *testing.T) {
	store := newFakeStore(t)
	leaseUntil := time.Now().Add(time.Minute)
	store.run = &Run{ID: "run", UserID: 7, Status: RunRunning, LeaseOwner: "active-worker", LeaseExpiresAt: &leaseUntil}
	runtime := &fakeRuntime{}
	service := NewService(store)
	service.SetRuntime(runtime)
	if err := service.CancelRun(context.Background(), 7, "run"); err != nil {
		t.Fatal(err)
	}
	if store.run.Status != RunCancelPending || runtime.canceled == nil {
		t.Fatalf("stored=%s runtime canceled=%v", store.run.Status, runtime.canceled)
	}
	if store.run.LeaseOwner != "active-worker" || store.run.LeaseExpiresAt == nil || !store.run.LeaseExpiresAt.Equal(leaseUntil) {
		t.Fatalf("cancel transition changed active lease: owner=%q expires=%v", store.run.LeaseOwner, store.run.LeaseExpiresAt)
	}
	if err := service.CancelRun(context.Background(), 7, "run"); err != nil || store.run.Status != RunCancelPending {
		t.Fatalf("repeated cancel err=%v status=%s", err, store.run.Status)
	}

	store.run.Status = RunQueued
	if err := service.CancelRun(context.Background(), 7, "run"); err != nil {
		t.Fatal(err)
	}
	if store.run.Status != RunCanceled {
		t.Fatalf("queued cancellation ended at %s", store.run.Status)
	}
}

func TestService_ApprovalStateGuards(t *testing.T) {
	store := newFakeStore(t)
	runtime := &fakeRuntime{}
	service := NewService(store)
	service.SetRuntime(runtime)
	store.run = &Run{ID: "run", UserID: 7, Status: RunAwaitingCharacterApproval}
	approval := CharacterApproval{Selections: []CharacterSelection{{NodeRunID: "node", InputHash: "hash", SelectedVersionID: "version"}}}
	if err := service.ApproveCharacters(context.Background(), 7, "run", approval); err != nil {
		t.Fatal(err)
	}
	if len(runtime.characters.Selections) != 1 {
		t.Fatal("character approval was not forwarded")
	}
	if err := service.ApproveStoryboard(context.Background(), 7, "run", StoryboardApproval{}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("wrong approval gate error = %v", err)
	}
	store.run.Status = RunAwaitingStoryboardApproval
	if err := service.ApproveStoryboard(context.Background(), 7, "run", StoryboardApproval{NodeRunID: "node", InputHash: "hash", Script: []byte("invalid")}); err == nil {
		t.Fatal("invalid storyboard JSON was accepted")
	}
	storyboard := StoryboardApproval{NodeRunID: "node", InputHash: "hash", Script: []byte(`{"scenes":[]}`)}
	if err := service.ApproveStoryboard(context.Background(), 7, "run", storyboard); err != nil {
		t.Fatal(err)
	}
	if runtime.storyboard.NodeRunID != "node" {
		t.Fatal("storyboard approval was not forwarded")
	}
}

type fakeStore struct {
	template  Template
	workflow  *Workflow
	run       *Run
	revisions map[uint64]*WorkflowRevision
}

type detailTestStore struct {
	*fakeStore
	nodes    []NodeRun
	versions map[string]AssetVersion
}

type idempotentRunStore struct {
	*fakeStore
	mu          sync.Mutex
	byRequest   map[string]*Run
	createCount int
}

func (s *idempotentRunStore) CreateRun(_ context.Context, run *Run) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := fmt.Sprintf("%d/%s", run.UserID, run.RequestID)
	if s.byRequest[key] != nil {
		return errors.New("duplicate request")
	}
	copy := *run
	s.byRequest[key] = &copy
	s.run = &copy
	s.createCount++
	return nil
}

func (s *idempotentRunStore) GetRunByRequestID(_ context.Context, userID uint64, requestID string) (*Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	run := s.byRequest[fmt.Sprintf("%d/%s", userID, requestID)]
	if run == nil {
		return nil, ErrNotFound
	}
	copy := *run
	return &copy, nil
}

func (s *detailTestStore) ListNodeRuns(context.Context, string) ([]NodeRun, error) {
	return append([]NodeRun(nil), s.nodes...), nil
}

func (s *detailTestStore) GetAssetVersion(_ context.Context, userID uint64, versionID string) (*AssetVersion, error) {
	version, ok := s.versions[versionID]
	if !ok || version.OwnerUserID != userID {
		return nil, ErrNotFound
	}
	return &version, nil
}

func newFakeStore(t *testing.T) *fakeStore {
	t.Helper()
	template := MustBuiltinTemplateV1()
	template.ID = 1
	return &fakeStore{template: template}
}

func (f *fakeStore) EnsureTemplate(_ context.Context, template *Template) error {
	f.template = *template
	return nil
}

func (f *fakeStore) ListTemplates(context.Context) ([]Template, error) {
	return []Template{f.template}, nil
}

func (f *fakeStore) GetTemplate(_ context.Context, id uint64) (*Template, error) {
	if id != f.template.ID {
		return nil, ErrNotFound
	}
	clone := f.template
	return &clone, nil
}

func (f *fakeStore) CreateWorkflow(_ context.Context, workflow *Workflow) error {
	copy := *workflow
	f.workflow = &copy
	f.rememberRevision(&copy)
	return nil
}

func (f *fakeStore) ListWorkflows(_ context.Context, userID uint64, _, _ int) ([]Workflow, int64, error) {
	if f.workflow == nil || f.workflow.UserID != userID {
		return nil, 0, nil
	}
	return []Workflow{*f.workflow}, 1, nil
}

func (f *fakeStore) GetWorkflow(_ context.Context, userID uint64, id string) (*Workflow, error) {
	if f.workflow == nil || f.workflow.UserID != userID || f.workflow.ID != id {
		return nil, ErrNotFound
	}
	copy := *f.workflow
	return &copy, nil
}

func (f *fakeStore) UpdateWorkflow(_ context.Context, workflow *Workflow, expected uint64) error {
	if f.workflow == nil {
		return ErrNotFound
	}
	if f.workflow.Revision != expected {
		return ErrRevisionConflict
	}
	copy := *workflow
	copy.Revision = expected + 1
	f.workflow = &copy
	workflow.Revision = copy.Revision
	f.rememberRevision(&copy)
	return nil
}

func (f *fakeStore) DeleteWorkflow(_ context.Context, userID uint64, id string) error {
	if f.workflow == nil || f.workflow.UserID != userID || f.workflow.ID != id {
		return ErrNotFound
	}
	f.workflow = nil
	f.revisions = nil
	return nil
}

func (f *fakeStore) rememberRevision(workflow *Workflow) {
	if workflow == nil {
		return
	}
	if f.revisions == nil {
		f.revisions = map[uint64]*WorkflowRevision{}
	}
	f.revisions[workflow.Revision] = &WorkflowRevision{
		WorkflowID: workflow.ID,
		UserID:     workflow.UserID,
		Revision:   workflow.Revision,
		Name:       workflow.Name,
		Graph:      workflow.Graph,
		NodeCount:  len(workflow.Graph.Nodes),
		EdgeCount:  len(workflow.Graph.Edges),
		CreatedAt:  time.Now().UTC(),
	}
}

func (f *fakeStore) ListWorkflowRevisions(_ context.Context, userID uint64, workflowID string, limit, offset int) ([]WorkflowRevisionListItem, int64, error) {
	if f.workflow == nil || f.workflow.UserID != userID || f.workflow.ID != workflowID {
		return nil, 0, ErrNotFound
	}
	items := make([]WorkflowRevisionListItem, 0, len(f.revisions))
	for _, revision := range f.revisions {
		items = append(items, WorkflowRevisionListItem{
			WorkflowID: revision.WorkflowID,
			Revision:   revision.Revision,
			Name:       revision.Name,
			NodeCount:  revision.NodeCount,
			EdgeCount:  revision.EdgeCount,
			CreatedAt:  revision.CreatedAt,
		})
	}
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			if items[i].Revision < items[j].Revision {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
	total := int64(len(items))
	if offset >= len(items) || limit <= 0 {
		return nil, total, nil
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	return items[offset:end], total, nil
}

func (f *fakeStore) GetWorkflowRevision(_ context.Context, userID uint64, workflowID string, revision uint64) (*WorkflowRevision, error) {
	if f.workflow == nil || f.workflow.UserID != userID || f.workflow.ID != workflowID {
		return nil, ErrNotFound
	}
	item := f.revisions[revision]
	if item == nil {
		return nil, ErrNotFound
	}
	copy := *item
	return &copy, nil
}

func (f *fakeStore) CreateRun(_ context.Context, run *Run) error {
	copy := *run
	f.run = &copy
	return nil
}

func (f *fakeStore) ListRuns(_ context.Context, userID uint64, workflowID string, limit, offset int) ([]RunListItem, int64, error) {
	if f.run == nil || f.run.UserID != userID || f.run.WorkflowID != workflowID || offset > 0 || limit <= 0 {
		return nil, 0, nil
	}
	run := f.run
	return []RunListItem{{
		ID: run.ID, WorkflowID: run.WorkflowID, Revision: run.Revision, Status: run.Status,
		Progress: run.Progress, RunMode: run.RunMode, StartNodeID: run.StartNodeID,
		EstimatedCost: run.EstimatedCost, ActualCost: run.ActualCost, OutputVersionID: run.OutputVersionID,
		ErrorCode: run.ErrorCode, ErrorMessage: run.ErrorMessage, CreatedAt: run.CreatedAt,
		StartedAt: run.StartedAt, FinishedAt: run.FinishedAt,
	}}, 1, nil
}

func (f *fakeStore) GetRun(_ context.Context, userID uint64, id string) (*Run, error) {
	if f.run == nil || f.run.UserID != userID || f.run.ID != id {
		return nil, ErrNotFound
	}
	copy := *f.run
	return &copy, nil
}

func (f *fakeStore) UpdateRunStatus(_ context.Context, id string, from, to RunStatus, message string) error {
	if f.run == nil || f.run.ID != id {
		return ErrNotFound
	}
	if f.run.Status != from {
		return ErrInvalidState
	}
	f.run.Status = to
	f.run.ErrorMessage = message
	if to != RunCancelPending {
		f.run.LeaseOwner = ""
		f.run.LeaseExpiresAt = nil
	}
	return nil
}

type fakeRuntime struct {
	dispatched  *Run
	canceled    *Run
	characters  CharacterApproval
	storyboard  StoryboardApproval
	concurrency RuntimeConcurrency
}

func (f *fakeRuntime) Dispatch(_ context.Context, run *Run) error {
	f.dispatched = run
	return nil
}

func (f *fakeRuntime) Estimate(context.Context, uint64, Graph) (*CostEstimate, error) {
	return &CostEstimate{Token: "token", ExpiresAt: time.Now().Add(time.Minute), TotalCredits: 1}, nil
}

func (f *fakeRuntime) Cancel(_ context.Context, run *Run) error {
	f.canceled = run
	return nil
}

func (f *fakeRuntime) ApproveCharacters(_ context.Context, _ *Run, approval CharacterApproval) error {
	f.characters = approval
	return nil
}

func (f *fakeRuntime) ApproveStoryboard(_ context.Context, _ *Run, approval StoryboardApproval) error {
	f.storyboard = approval
	return nil
}

func (f *fakeRuntime) Concurrency() RuntimeConcurrency {
	if f.concurrency == (RuntimeConcurrency{}) {
		return NormalizeRuntimeConcurrency(RuntimeConcurrency{})
	}
	return f.concurrency
}

func (f *fakeRuntime) UpdateConcurrency(next RuntimeConcurrency) RuntimeConcurrency {
	f.concurrency = NormalizeRuntimeConcurrency(next)
	return f.concurrency
}

type errorDispatcher struct{}

func (errorDispatcher) Dispatch(context.Context, *Run) error { return errors.New("dispatch failed") }

type strictFakeRuntime struct{ fakeRuntime }

func (f *strictFakeRuntime) EstimateFor(context.Context, uint64, uint64, Graph, RunMode, string) (*CostEstimate, error) {
	return &CostEstimate{Token: "valid", Hash: "hash", ExpiresAt: time.Now().Add(time.Minute), TotalCredits: 5}, nil
}

func (f *strictFakeRuntime) ValidateEstimate(_ context.Context, _ uint64, _ uint64, _ Graph, _ RunMode, _ string, token string) (*CostEstimate, error) {
	if token != "valid" {
		return nil, ErrInvalidEstimate
	}
	return &CostEstimate{Token: token, Hash: "hash", ExpiresAt: time.Now().Add(time.Minute), TotalCredits: 5}, nil
}

var _ RunRuntimeV2 = (*strictFakeRuntime)(nil)

type idempotentRuntime struct{ dispatches atomic.Int32 }

func (r *idempotentRuntime) Dispatch(context.Context, *Run) error {
	r.dispatches.Add(1)
	return nil
}
func (*idempotentRuntime) Estimate(context.Context, uint64, Graph) (*CostEstimate, error) {
	return &CostEstimate{Token: "valid", TotalCredits: 5}, nil
}
func (*idempotentRuntime) EstimateFor(context.Context, uint64, uint64, Graph, RunMode, string) (*CostEstimate, error) {
	return &CostEstimate{Token: "valid", TotalCredits: 5}, nil
}
func (*idempotentRuntime) ValidateEstimate(context.Context, uint64, uint64, Graph, RunMode, string, string) (*CostEstimate, error) {
	return &CostEstimate{Token: "valid", Hash: "hash", TotalCredits: 5}, nil
}
func (*idempotentRuntime) Cancel(context.Context, *Run) error { return nil }
func (*idempotentRuntime) ApproveCharacters(context.Context, *Run, CharacterApproval) error {
	return nil
}
func (*idempotentRuntime) ApproveStoryboard(context.Context, *Run, StoryboardApproval) error {
	return nil
}

var _ RunRuntimeV2 = (*idempotentRuntime)(nil)

type retryRuntime struct {
	dispatches     atomic.Int32
	validates      atomic.Int32
	rejectOriginal atomic.Bool
}

func (r *retryRuntime) Dispatch(context.Context, *Run) error {
	r.dispatches.Add(1)
	return nil
}
func (*retryRuntime) Estimate(context.Context, uint64, Graph) (*CostEstimate, error) {
	return &CostEstimate{Token: "original", Hash: "stable-hash", TotalCredits: 5}, nil
}
func (*retryRuntime) EstimateFor(context.Context, uint64, uint64, Graph, RunMode, string) (*CostEstimate, error) {
	return &CostEstimate{Token: "original", Hash: "stable-hash", TotalCredits: 5}, nil
}
func (r *retryRuntime) ValidateEstimate(_ context.Context, _ uint64, _ uint64, _ Graph, _ RunMode, _ string, token string) (*CostEstimate, error) {
	r.validates.Add(1)
	if token == "original" && r.rejectOriginal.Load() {
		return nil, ErrInvalidEstimate
	}
	switch token {
	case "original", "renewed":
		return &CostEstimate{Token: token, Hash: "stable-hash", TotalCredits: 5}, nil
	case "different-hash":
		return &CostEstimate{Token: token, Hash: "other-hash", TotalCredits: 5}, nil
	case "different-cost":
		return &CostEstimate{Token: token, Hash: "stable-hash", TotalCredits: 6}, nil
	default:
		return nil, ErrInvalidEstimate
	}
}
func (*retryRuntime) Cancel(context.Context, *Run) error { return nil }
func (*retryRuntime) ApproveCharacters(context.Context, *Run, CharacterApproval) error {
	return nil
}
func (*retryRuntime) ApproveStoryboard(context.Context, *Run, StoryboardApproval) error {
	return nil
}

var _ RunRuntimeV2 = (*retryRuntime)(nil)

var _ Store = (*fakeStore)(nil)
var _ RunRuntime = (*fakeRuntime)(nil)
