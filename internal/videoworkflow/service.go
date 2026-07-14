package videoworkflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

type RunDispatcher interface {
	Dispatch(context.Context, *Run) error
}

type CostEstimate struct {
	Token            string    `json:"token"`
	Hash             string    `json:"hash"`
	ExpiresAt        time.Time `json:"expires_at"`
	CharacterCredits int64     `json:"character_credits"`
	SceneCredits     int64     `json:"scene_credits"`
	TotalCredits     int64     `json:"total_credits"`
}

type CharacterSelection struct {
	NodeRunID         string `json:"node_run_id"`
	InputHash         string `json:"input_hash"`
	SelectedVersionID string `json:"selected_version_id"`
}

type CharacterApproval struct {
	Selections []CharacterSelection `json:"selections"`
}

type StoryboardApproval struct {
	NodeRunID string          `json:"node_run_id"`
	InputHash string          `json:"input_hash"`
	Script    json.RawMessage `json:"script"`
}

type RunRuntime interface {
	RunDispatcher
	Estimate(context.Context, uint64, Graph) (*CostEstimate, error)
	Cancel(context.Context, *Run) error
	ApproveCharacters(context.Context, *Run, CharacterApproval) error
	ApproveStoryboard(context.Context, *Run, StoryboardApproval) error
}

type RunRuntimeV2 interface {
	RunRuntime
	EstimateFor(context.Context, uint64, uint64, Graph, RunMode, string) (*CostEstimate, error)
	ValidateEstimate(context.Context, uint64, uint64, Graph, RunMode, string, string) (*CostEstimate, error)
}

type GraphValidationError struct {
	Errors ValidationErrors
}

func (e *GraphValidationError) Error() string {
	return e.Errors.Error()
}

func (e *GraphValidationError) Unwrap() error { return ErrInvalidGraph }

type Service struct {
	store         Store
	dispatcher    RunDispatcher
	runtime       RunRuntime
	assetRoot     string
	mediaSigner   *MediaSigner
	composer      *Composer
	acceptNewRuns atomic.Bool
}

func NewService(store Store) *Service {
	service := &Service{store: store}
	service.acceptNewRuns.Store(true)
	return service
}

func (s *Service) SetAcceptNewRuns(accept bool) { s.acceptNewRuns.Store(accept) }

func (s *Service) AcceptNewRuns() bool { return s.acceptNewRuns.Load() }

func (s *Service) SetDispatcher(dispatcher RunDispatcher) { s.dispatcher = dispatcher }

func (s *Service) SetRuntime(runtime RunRuntime) {
	s.runtime = runtime
	s.dispatcher = runtime
}

func (s *Service) EnsureBuiltinTemplate(ctx context.Context) error {
	templates, err := BuiltinTemplates()
	if err != nil {
		return err
	}
	for i := range templates {
		NormalizeBackgroundEnvironmentPorts(&templates[i].Graph)
		if errs := ValidateGraph(templates[i].Graph, true); len(errs) != 0 {
			return &GraphValidationError{Errors: errs}
		}
		if err := s.store.EnsureTemplate(ctx, &templates[i]); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) ListTemplates(ctx context.Context) ([]Template, error) {
	return s.store.ListTemplates(ctx)
}

type CreateWorkflowInput struct {
	UserID     uint64
	TemplateID uint64
	Name       string
}

func (s *Service) CreateWorkflow(ctx context.Context, input CreateWorkflowInput) (*Workflow, error) {
	template, err := s.store.GetTemplate(ctx, input.TemplateID)
	if err != nil {
		return nil, err
	}
	graph, err := CloneGraph(template.Graph)
	if err != nil {
		return nil, fmt.Errorf("clone video workflow template: %w", err)
	}
	NormalizeBackgroundEnvironmentPorts(&graph)
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = template.Name
	}
	workflow := &Workflow{
		ID: NewWorkflowID(), UserID: input.UserID, TemplateID: &template.ID,
		TemplateVersion: template.Version, Name: name, Revision: 1, Graph: graph,
	}
	if err := s.store.CreateWorkflow(ctx, workflow); err != nil {
		return nil, err
	}
	return workflow, nil
}

func (s *Service) ListWorkflows(ctx context.Context, userID uint64, limit, offset int) ([]Workflow, int64, error) {
	return s.store.ListWorkflows(ctx, userID, limit, offset)
}

func (s *Service) GetWorkflow(ctx context.Context, userID uint64, workflowID string) (*Workflow, error) {
	return s.store.GetWorkflow(ctx, userID, workflowID)
}

type UpdateWorkflowInput struct {
	UserID           uint64
	WorkflowID       string
	ExpectedRevision uint64
	Name             string
	Graph            Graph
}

func (s *Service) UpdateWorkflow(ctx context.Context, input UpdateWorkflowInput) (*Workflow, error) {
	NormalizeBackgroundEnvironmentPorts(&input.Graph)
	if errs := ValidateGraph(input.Graph, false); len(errs) != 0 {
		return nil, &GraphValidationError{Errors: errs}
	}
	workflow := &Workflow{
		ID: input.WorkflowID, UserID: input.UserID, Name: strings.TrimSpace(input.Name),
		Revision: input.ExpectedRevision, Graph: input.Graph,
	}
	if workflow.Name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrInvalidInput)
	}
	if err := s.store.UpdateWorkflow(ctx, workflow, input.ExpectedRevision); err != nil {
		return nil, err
	}
	return workflow, nil
}

func (s *Service) DeleteWorkflow(ctx context.Context, userID uint64, workflowID string) error {
	return s.store.DeleteWorkflow(ctx, userID, workflowID)
}

func (s *Service) ListWorkflowRevisions(ctx context.Context, userID uint64, workflowID string, limit, offset int) ([]WorkflowRevisionListItem, int64, error) {
	workflow, err := s.store.GetWorkflow(ctx, userID, workflowID)
	if err != nil {
		return nil, 0, err
	}
	items, total, err := s.store.ListWorkflowRevisions(ctx, userID, workflowID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	for i := range items {
		items[i].IsCurrent = items[i].Revision == workflow.Revision
	}
	return items, total, nil
}

func (s *Service) GetWorkflowRevision(ctx context.Context, userID uint64, workflowID string, revision uint64) (*WorkflowRevision, error) {
	workflow, err := s.store.GetWorkflow(ctx, userID, workflowID)
	if err != nil {
		return nil, err
	}
	item, err := s.store.GetWorkflowRevision(ctx, userID, workflowID, revision)
	if err != nil {
		return nil, err
	}
	item.IsCurrent = item.Revision == workflow.Revision
	return item, nil
}

func (s *Service) ValidateWorkflow(ctx context.Context, userID uint64, workflowID string) (ValidationErrors, error) {
	return s.validateWorkflow(ctx, userID, workflowID, nil)
}

// ValidateWorkflowGraph 在确认工作流归属后校验请求中的 Graph，不读取库中已保存版本。
func (s *Service) ValidateWorkflowGraph(ctx context.Context, userID uint64, workflowID string, graph Graph) (ValidationErrors, error) {
	return s.validateWorkflow(ctx, userID, workflowID, &graph)
}

func (s *Service) validateWorkflow(ctx context.Context, userID uint64, workflowID string, graph *Graph) (ValidationErrors, error) {
	workflow, err := s.store.GetWorkflow(ctx, userID, workflowID)
	if err != nil {
		return nil, err
	}
	target := workflow.Graph
	if graph != nil {
		target = *graph
	}
	NormalizeBackgroundEnvironmentPorts(&target)
	return ValidateGraph(target, true), nil
}

type StartRunInput struct {
	Revision      uint64  `json:"revision"`
	RunMode       RunMode `json:"run_mode"`
	StartNodeID   string  `json:"start_node_id,omitempty"`
	EstimateToken string  `json:"estimate_token"`
	RequestID     string  `json:"request_id"`
}

func (s *Service) StartRun(ctx context.Context, userID uint64, workflowID string) (*Run, error) {
	return s.StartRunWithInput(ctx, userID, workflowID, StartRunInput{RunMode: RunModeFull, RequestID: "legacy-" + NewRunID()})
}

func (s *Service) StartRunWithInput(ctx context.Context, userID uint64, workflowID string, input StartRunInput) (*Run, error) {
	var err error
	input, err = normalizeStartRunInput(input)
	if err != nil {
		return nil, err
	}
	if lookup, ok := s.store.(interface {
		GetRunByRequestID(context.Context, uint64, string) (*Run, error)
	}); ok {
		existing, lookupErr := lookup.GetRunByRequestID(ctx, userID, input.RequestID)
		if lookupErr == nil {
			if err := s.validateExistingRunRetry(ctx, userID, workflowID, input, existing); err != nil {
				return nil, err
			}
			return existing, nil
		}
		if !errors.Is(lookupErr, ErrNotFound) {
			return nil, lookupErr
		}
	}
	if !s.acceptNewRuns.Load() {
		return nil, ErrRunsDraining
	}
	workflow, err := s.store.GetWorkflow(ctx, userID, workflowID)
	if err != nil {
		return nil, err
	}
	NormalizeBackgroundEnvironmentPorts(&workflow.Graph)
	if errs := ValidateGraph(workflow.Graph, true); len(errs) != 0 {
		return nil, &GraphValidationError{Errors: errs}
	}
	if input.Revision != 0 && input.Revision != workflow.Revision {
		return nil, ErrRevisionConflict
	}
	if err := validateRunSelection(workflow.Graph, input.RunMode, input.StartNodeID); err != nil {
		return nil, err
	}
	var estimate *CostEstimate
	if runtime, ok := s.runtime.(RunRuntimeV2); ok {
		if strings.TrimSpace(input.EstimateToken) == "" {
			return nil, ErrInvalidEstimate
		}
		estimate, err = runtime.ValidateEstimate(ctx, userID, workflow.Revision, workflow.Graph, input.RunMode, input.StartNodeID, input.EstimateToken)
		if err != nil {
			return nil, err
		}
	}
	snapshot, err := CloneGraph(workflow.Graph)
	if err != nil {
		return nil, fmt.Errorf("clone video workflow run snapshot: %w", err)
	}
	run := &Run{
		ID: NewRunID(), WorkflowID: workflow.ID, UserID: userID, Revision: workflow.Revision,
		GraphSnapshot: snapshot, Status: RunQueued, RequestID: input.RequestID, RunMode: input.RunMode,
		StartNodeID: input.StartNodeID, EstimateToken: input.EstimateToken,
	}
	if estimate != nil {
		run.EstimatedCost = estimate.TotalCredits
		run.EstimateHash = estimate.Hash
	}
	if err := s.store.CreateRun(ctx, run); err != nil {
		if lookup, ok := s.store.(interface {
			GetRunByRequestID(context.Context, uint64, string) (*Run, error)
		}); ok {
			existing, lookupErr := lookup.GetRunByRequestID(ctx, userID, input.RequestID)
			if lookupErr == nil {
				if conflictErr := s.validateExistingRunRetry(ctx, userID, workflowID, input, existing); conflictErr != nil {
					return nil, conflictErr
				}
				return existing, nil
			}
		}
		return nil, err
	}
	if s.dispatcher != nil {
		if err := s.dispatcher.Dispatch(ctx, run); err != nil {
			_ = s.store.UpdateRunStatus(ctx, run.ID, RunQueued, RunFailed, err.Error())
			run.Status = RunFailed
			run.ErrorMessage = err.Error()
			return run, fmt.Errorf("dispatch video workflow run: %w", err)
		}
	}
	return run, nil
}

func normalizeStartRunInput(input StartRunInput) (StartRunInput, error) {
	input.RequestID = strings.TrimSpace(input.RequestID)
	input.StartNodeID = strings.TrimSpace(input.StartNodeID)
	if input.RequestID == "" || len(input.RequestID) > 128 {
		return input, fmt.Errorf("%w: request_id is required and must not exceed 128 characters", ErrInvalidInput)
	}
	if input.RunMode == "" {
		input.RunMode = RunModeFull
	}
	if input.RunMode != RunModeFull && input.RunMode != RunModeNodeOnly && input.RunMode != RunModeDownstream {
		return input, fmt.Errorf("%w: run_mode must be full, node_only or downstream", ErrInvalidInput)
	}
	if input.RunMode == RunModeFull && input.StartNodeID != "" {
		return input, fmt.Errorf("%w: full run must not set start_node_id", ErrInvalidInput)
	}
	if input.RunMode != RunModeFull && input.StartNodeID == "" {
		return input, fmt.Errorf("%w: start_node_id is required", ErrInvalidInput)
	}
	return input, nil
}

func (s *Service) validateExistingRunRetry(ctx context.Context, userID uint64, workflowID string, input StartRunInput, existing *Run) error {
	if existing == nil {
		return fmt.Errorf("%w: existing run is missing", ErrRequestConflict)
	}
	revision := input.Revision
	if revision == 0 {
		revision = existing.Revision
	}
	if existing.WorkflowID != workflowID || existing.Revision != revision || existing.RunMode != input.RunMode || existing.StartNodeID != input.StartNodeID {
		return fmt.Errorf("%w: workflow, revision, run_mode or start_node_id differs", ErrRequestConflict)
	}
	runtime, isV2 := s.runtime.(RunRuntimeV2)
	if !isV2 {
		if input.EstimateToken != existing.EstimateToken {
			return fmt.Errorf("%w: estimate token differs", ErrRequestConflict)
		}
		return nil
	}
	if existing.EstimateHash == "" {
		return fmt.Errorf("%w: existing run has no estimate hash", ErrRequestConflict)
	}
	if input.EstimateToken != "" && input.EstimateToken == existing.EstimateToken {
		// 相同令牌代表同一原始请求，即使令牌现已过期也必须返回原运行。
		return nil
	}
	if input.EstimateToken == "" {
		return fmt.Errorf("%w: estimate token is missing", ErrRequestConflict)
	}
	estimate, err := runtime.ValidateEstimate(ctx, userID, existing.Revision, existing.GraphSnapshot, existing.RunMode, existing.StartNodeID, input.EstimateToken)
	if err != nil || estimate == nil || estimate.Hash != existing.EstimateHash || estimate.TotalCredits != existing.EstimatedCost {
		return fmt.Errorf("%w: estimate does not match the existing run", ErrRequestConflict)
	}
	return nil
}

func (s *Service) GetRun(ctx context.Context, userID uint64, runID string) (*Run, error) {
	return s.store.GetRun(ctx, userID, runID)
}

func (s *Service) ListRuns(ctx context.Context, userID uint64, workflowID string, limit, offset int) ([]RunListItem, int64, error) {
	if _, err := s.store.GetWorkflow(ctx, userID, workflowID); err != nil {
		return nil, 0, err
	}
	return s.store.ListRuns(ctx, userID, workflowID, limit, offset)
}

type RunDetail struct {
	*Run
	NodeRuns  []NodeRun     `json:"node_runs"`
	Output    *AssetVersion `json:"output,omitempty"`
	OutputURL string        `json:"output_url,omitempty"`
}

func (s *Service) GetRunDetail(ctx context.Context, userID uint64, runID string) (*RunDetail, error) {
	run, err := s.store.GetRun(ctx, userID, runID)
	if err != nil {
		return nil, err
	}
	detail := &RunDetail{Run: run, NodeRuns: []NodeRun{}}
	if store, ok := s.store.(interface {
		ListNodeRuns(context.Context, string) ([]NodeRun, error)
	}); ok {
		detail.NodeRuns, err = store.ListNodeRuns(ctx, run.ID)
		if err != nil {
			return nil, err
		}
	}
	if run.OutputVersionID != "" {
		if store, ok := s.store.(interface {
			GetAssetVersion(context.Context, uint64, string) (*AssetVersion, error)
		}); ok {
			detail.Output, _ = store.GetAssetVersion(ctx, userID, run.OutputVersionID)
		}
	}
	s.decorateRunDetailMedia(ctx, userID, detail, time.Now())
	return detail, nil
}

func (s *Service) decorateRunDetailMedia(ctx context.Context, userID uint64, detail *RunDetail, now time.Time) {
	if detail == nil || s.mediaSigner == nil {
		return
	}
	store, ok := s.store.(interface {
		GetAssetVersion(context.Context, uint64, string) (*AssetVersion, error)
	})
	if !ok {
		return
	}
	expires := now.Add(PreviewSignTTL)
	signedURL := func(versionID string) string {
		if versionID == "" {
			return ""
		}
		if _, err := store.GetAssetVersion(ctx, userID, versionID); err != nil {
			return ""
		}
		signature, err := s.mediaSigner.Sign(versionID, MediaPurposePreview, expires)
		if err != nil {
			return ""
		}
		return SignedMediaPath(versionID, MediaPurposePreview, expires, signature)
	}
	for i := range detail.NodeRuns {
		ids := outputVersionIDs(detail.NodeRuns[i].Output)
		if len(ids) == 0 && detail.NodeRuns[i].OutputVersionID != "" {
			ids = []string{detail.NodeRuns[i].OutputVersionID}
		}
		if len(ids) == 0 {
			continue
		}
		var output map[string]any
		if json.Unmarshal(detail.NodeRuns[i].Output, &output) != nil || output == nil {
			output = make(map[string]any)
		}
		candidates := make([]map[string]string, 0, len(ids))
		for _, id := range ids {
			if previewURL := signedURL(id); previewURL != "" {
				candidates = append(candidates, map[string]string{"id": id, "version_id": id, "preview_url": previewURL})
			}
		}
		if len(candidates) > 0 {
			if detail.NodeRuns[i].NodeType == NodeCharacter {
				output["candidates"] = candidates
			}
		}
		if outputURL := signedURL(detail.NodeRuns[i].OutputVersionID); outputURL != "" {
			output["output_url"] = outputURL
		}
		detail.NodeRuns[i].Output, _ = json.Marshal(output)
	}
	detail.OutputURL = signedURL(detail.Run.OutputVersionID)
	if detail.Output != nil {
		detail.Output.PreviewURL = detail.OutputURL
	}
}

func (s *Service) EstimateRun(ctx context.Context, userID uint64, workflowID string) (*CostEstimate, error) {
	return s.EstimateRunWithInput(ctx, userID, workflowID, RunModeFull, "")
}

func (s *Service) EstimateRunWithInput(ctx context.Context, userID uint64, workflowID string, mode RunMode, startNodeID string) (*CostEstimate, error) {
	workflow, err := s.store.GetWorkflow(ctx, userID, workflowID)
	if err != nil {
		return nil, err
	}
	NormalizeBackgroundEnvironmentPorts(&workflow.Graph)
	if errs := ValidateGraph(workflow.Graph, true); len(errs) != 0 {
		return nil, &GraphValidationError{Errors: errs}
	}
	if s.runtime == nil {
		return nil, errors.New("videoworkflow: runtime is not configured")
	}
	if mode == "" {
		mode = RunModeFull
	}
	if err := validateRunSelection(workflow.Graph, mode, startNodeID); err != nil {
		return nil, err
	}
	if runtime, ok := s.runtime.(RunRuntimeV2); ok {
		return runtime.EstimateFor(ctx, userID, workflow.Revision, workflow.Graph, mode, startNodeID)
	}
	return s.runtime.Estimate(ctx, userID, workflow.Graph)
}

func validateRunSelection(graph Graph, mode RunMode, startNodeID string) error {
	if mode != RunModeFull && mode != RunModeNodeOnly && mode != RunModeDownstream {
		return fmt.Errorf("%w: run_mode must be full, node_only or downstream", ErrInvalidInput)
	}
	if mode == RunModeFull {
		if strings.TrimSpace(startNodeID) != "" {
			return fmt.Errorf("%w: full run must not set start_node_id", ErrInvalidInput)
		}
		return nil
	}
	for _, node := range graph.Nodes {
		if node.ID == startNodeID {
			return nil
		}
	}
	return fmt.Errorf("%w: start_node_id does not exist", ErrInvalidInput)
}

func (s *Service) CancelRun(ctx context.Context, userID uint64, runID string) error {
	run, err := s.store.GetRun(ctx, userID, runID)
	if err != nil {
		return err
	}
	if run.Status == RunCancelPending || run.Status == RunCanceled {
		// cancel_pending 会由租约执行器恢复；重复取消保持幂等，避免再次清空
		// 正在执行取消收口的 worker 租约。
		return nil
	}
	if run.Status == RunQueued {
		if err := s.store.UpdateRunStatus(ctx, run.ID, RunQueued, RunCancelPending, ""); err != nil {
			return err
		}
		return s.store.UpdateRunStatus(ctx, run.ID, RunCancelPending, RunCanceled, "")
	}
	if !CanTransitionRun(run.Status, RunCancelPending) {
		return fmt.Errorf("%w: run %s cannot be canceled", ErrInvalidState, run.Status)
	}
	if err := s.store.UpdateRunStatus(ctx, run.ID, run.Status, RunCancelPending, ""); err != nil {
		return err
	}
	run.Status = RunCancelPending
	if s.runtime != nil {
		return s.runtime.Cancel(ctx, run)
	}
	return nil
}

func (s *Service) ApproveCharacters(ctx context.Context, userID uint64, runID string, approval CharacterApproval) error {
	run, err := s.store.GetRun(ctx, userID, runID)
	if err != nil {
		return err
	}
	if !isApprovalDecisionSuccessor(RunAwaitingCharacterApproval, run.Status) {
		return fmt.Errorf("%w: run is %s", ErrInvalidState, run.Status)
	}
	if len(approval.Selections) == 0 {
		return fmt.Errorf("%w: character selections are required", ErrInvalidInput)
	}
	if s.runtime == nil {
		if run.Status == RunSucceeded || run.Status == RunFailed || run.Status == RunCanceled {
			return nil
		}
		return errors.New("videoworkflow: runtime is not configured")
	}
	return s.runtime.ApproveCharacters(ctx, run, approval)
}

func (s *Service) ApproveStoryboard(ctx context.Context, userID uint64, runID string, approval StoryboardApproval) error {
	run, err := s.store.GetRun(ctx, userID, runID)
	if err != nil {
		return err
	}
	if !isApprovalDecisionSuccessor(RunAwaitingStoryboardApproval, run.Status) {
		return fmt.Errorf("%w: run is %s", ErrInvalidState, run.Status)
	}
	if approval.NodeRunID == "" || approval.InputHash == "" || !json.Valid(approval.Script) {
		return fmt.Errorf("%w: valid storyboard approval is required", ErrInvalidInput)
	}
	if s.runtime == nil {
		if run.Status == RunSucceeded || run.Status == RunFailed || run.Status == RunCanceled {
			return nil
		}
		return errors.New("videoworkflow: runtime is not configured")
	}
	return s.runtime.ApproveStoryboard(ctx, run, approval)
}
