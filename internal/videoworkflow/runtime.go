package videoworkflow

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	RuntimeLeaseDuration     = 60 * time.Second
	RuntimeHeartbeatEvery    = 10 * time.Second
	RuntimeRecoveryEvery     = 30 * time.Second
	RuntimeApprovalLifetime  = 72 * time.Hour
	RuntimeFinalizationLease = 90 * time.Second
	RuntimeBillingLockLease  = 60 * time.Second
	RuntimeBillingTimeout    = 30 * time.Second
	RuntimeSlotLease         = 60 * time.Second
	RuntimeSlotHeartbeat     = 10 * time.Second
	EstimateTokenLifetime    = 5 * time.Minute
)

var (
	ErrInvalidEstimate               = errors.New("videoworkflow: invalid or expired estimate token")
	ErrProviderStatePersistence      = errors.New("videoworkflow: provider submission state persistence failed")
	ErrProviderSubmissionUnknown     = errors.New("videoworkflow: provider submission state is unknown")
	ErrReservationPending            = errors.New("videoworkflow: charge reservation result is pending reconciliation")
	errAwaitingApproval              = errors.New("videoworkflow: awaiting approval")
	errRecoverableProviderSubmission = errors.New("videoworkflow: provider submission saved for lease recovery")
	errRecoverableApprovalPause      = errors.New("videoworkflow: approval pause pending lease recovery")
	errRecoverableNodeCommit         = errors.New("videoworkflow: node result commit pending reconciliation")
	errRecoverableRuntimePersistence = errors.New("videoworkflow: runtime state write pending lease recovery")
	errRecoverableRunFinalization    = errors.New("videoworkflow: successful run finalization pending lease recovery")
)

type RuntimeStore interface {
	GetRunByID(context.Context, string) (*Run, error)
	ClaimNextRun(context.Context, string, time.Duration) (*Run, error)
	HeartbeatRun(context.Context, string, string, RunStatus, time.Duration) error
	RenewRunFinalizationLease(context.Context, string, string, RunStatus, time.Duration) error
	UpdateRunStatus(context.Context, string, RunStatus, RunStatus, string) error
	UpdateRunStatusFenced(context.Context, string, string, RunStatus, RunStatus, string) error
	UpdateRunProgress(context.Context, string, int) error
	UpdateRunProgressFenced(context.Context, string, string, int) error
	CompleteRun(context.Context, string, string, RunStatus, RunStatus, int64, string, string, string) error
	CreateNodeRun(context.Context, *NodeRun, NodeType, int) error
	ListNodeRuns(context.Context, string) ([]NodeRun, error)
	GetNodeRun(context.Context, string) (*NodeRun, error)
	FindNodeRun(context.Context, string, string) (*NodeRun, error)
	UpdateNodeRunStatus(context.Context, string, NodeRunStatus, NodeRunStatus, string, string, string) error
	UpdateNodeRunStatusFenced(context.Context, *FencedNodeRunTransition) error
	UpdateNodeRunOutput(context.Context, string, json.RawMessage, int64, bool) error
	RecordNodeRunCost(context.Context, string, int64) error
	CommitNodeResult(context.Context, *NodeResultCommit) error
	UpdateNodeRunProgress(context.Context, string, int, string, json.RawMessage) error
	UpdateNodeRunProgressFenced(context.Context, string, string, string, string, int, string, json.RawMessage) error
	CreateAsset(context.Context, *Asset) error
	CreateAssetVersion(context.Context, *AssetVersion) error
	GetAssetVersion(context.Context, uint64, string) (*AssetVersion, error)
	FindCachedAssetVersion(context.Context, uint64, string) (*AssetVersion, error)
	UsedAssetBytes(context.Context, uint64) (int64, error)
	AddAssetReference(context.Context, *AssetReference, string) error
	CreateCharge(context.Context, *ChargeReservation) error
	GetChargeByIdempotencyKey(context.Context, string) (*ChargeReservation, error)
	UpdateChargeStatus(context.Context, string, ChargeStatus, ChargeStatus, int64) error
	SetChargeBillingRef(context.Context, string, string) error
	CompareAndSwapChargeBillingRef(context.Context, string, string, string) error
	HasBillingTransaction(context.Context, uint64, string, ...string) (bool, error)
	ReserveRunCredits(context.Context, *RunCreditReservation) error
	FinalizeRun(context.Context, *RunFinalization) (int64, error)
	CreateApproval(context.Context, *Approval) error
	DecideApproval(context.Context, string, string, string, string, json.RawMessage) error
	CommitApprovalDecision(context.Context, *ApprovalDecisionCommit) error
	GetApprovalByNode(context.Context, string, string, string, string) (*Approval, error)
	GetPendingApproval(context.Context, string, string) (*Approval, error)
	CancelExpiredApprovals(context.Context) ([]string, error)
	AcquireRuntimeSlot(context.Context, string, int, string, string, string, string, time.Duration) (int, error)
	RenewRuntimeSlot(context.Context, string, int, string, string, string, string, time.Duration) error
	ReleaseRuntimeSlot(context.Context, string, int, string) error
}

type FencedNodeRunTransition struct {
	RunID           string
	WorkerID        string
	NodeRunID       string
	NodeID          string
	InputHash       string
	From            NodeRunStatus
	To              NodeRunStatus
	UpstreamTaskID  string
	OutputVersionID string
	ProviderState   json.RawMessage
	ErrorMessage    string
}

type RunCreditReservation struct {
	RunID          string
	WorkerID       string
	ExpectedStatus RunStatus
	ChargeID       string
	LockRef        string
	UserID         uint64
	Amount         int64
	IdempotencyKey string
}

type RunFinalization struct {
	RunID           string
	WorkerID        string
	ExpectedStatus  RunStatus
	TargetStatus    RunStatus
	UserID          uint64
	EstimatedCost   int64
	BillingEnabled  bool
	OutputVersionID string
	ErrorCode       string
	ErrorMessage    string
}

// NodeResultCommit 把一次节点执行的数据库可见结果作为单个事务提交。物理媒体
// 文件在事务前写入，事务失败时由运行时删除本次请求独占的文件路径。
type NodeResultCommit struct {
	RunID             string
	WorkerID          string
	ExpectedRunStatus RunStatus
	NodeRunID         string
	NodeID            string
	InputHash         string
	From              NodeRunStatus
	To                NodeRunStatus
	Output            json.RawMessage
	CreditCost        int64
	CacheHit          bool
	UpstreamTaskID    string
	OutputVersionID   string
	Assets            []Asset
	Versions          []AssetVersion
	References        []AssetReference
	Approval          *Approval
}

type ApprovalNodeDecision struct {
	NodeRunID       string
	NodeID          string
	InputHash       string
	ApprovalType    string
	DecisionPayload json.RawMessage
	Output          json.RawMessage
	OutputVersionID string
}

type ApprovalDecisionCommit struct {
	RunID          string
	ExpectedStatus RunStatus
	Decisions      []ApprovalNodeDecision
}

type ImageGenerationRequest struct {
	TaskID     string
	UserID     uint64
	Prompt     string
	Model      string
	Size       string
	Count      int
	References [][]byte
}

type GeneratedImage struct {
	Data       []byte
	URL        string
	MIMEType   string
	TaskID     string
	CreditCost int64
}

type RuntimeImageGenerator interface {
	GenerateImage(context.Context, ImageGenerationRequest) ([]GeneratedImage, error)
}

type TextGenerationRequest struct {
	Prompt string
	Model  string
}

type RuntimeTextGenerator interface {
	GenerateText(context.Context, TextGenerationRequest) (json.RawMessage, int64, error)
}

type VideoGenerationRequest struct {
	Prompt         string
	Model          string
	AspectRatio    string
	Resolution     string
	DurationSec    int
	ReferenceURLs  []string
	ProviderTaskID string
	ProviderConfig json.RawMessage
	OnSubmitted    func(taskID string, state json.RawMessage) error
	OnProgress     func(taskID string, progress int, state json.RawMessage)
}

type GeneratedVideo struct {
	Data       []byte
	URL        string
	MIMEType   string
	TaskID     string
	CreditCost int64
}

type RuntimeVideoGenerator interface {
	GenerateVideo(context.Context, VideoGenerationRequest) (*GeneratedVideo, error)
}

type RuntimeVideoConfigSnapshotter interface {
	VideoConfigSnapshot() json.RawMessage
}

type RuntimeVideoModelConfigSnapshotter interface {
	VideoConfigSnapshotForModel(string) json.RawMessage
}

type RuntimeBilling interface {
	PreDeduct(context.Context, uint64, uint64, int64, string, string) error
	Settle(context.Context, uint64, uint64, int64, int64, string, string) error
	Refund(context.Context, uint64, uint64, int64, string, string) error
}

type RuntimeUsageEvent struct {
	UserID     uint64
	RequestID  string
	Type       string
	CreditCost int64
	DurationMS int64
	Status     string
	ErrorCode  string
}

type RuntimeUsageLogger interface {
	LogVideoWorkflowUsage(RuntimeUsageEvent)
}

type RuntimeConfig struct {
	Store              RuntimeStore
	WorkerID           string
	EstimateSecret     string
	AssetRoot          string
	PublicBaseURL      string
	MediaSigner        *MediaSigner
	HTTPClient         *http.Client
	AllowedRemoteHosts []string
	Composer           *Composer
	ImageGenerator     RuntimeImageGenerator
	TextGenerator      RuntimeTextGenerator
	VideoGenerator     RuntimeVideoGenerator
	Billing            RuntimeBilling
	Usage              RuntimeUsageLogger
	ImageCredits       int64
	TextCredits        int64
	VideoCredits       int64
	ImageConcurrency   int
	VideoConcurrency   int
	ComposeConcurrency int
	WorkerConcurrency  int
}

type Runtime struct {
	config       RuntimeConfig
	wake         chan struct{}
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	imageSlots   chan struct{}
	videoSlots   chan struct{}
	composeSlots chan struct{}
	runSlots     chan struct{}
	mu           sync.Mutex
	runCancels   map[string]context.CancelFunc
	started      bool
	closed       bool
}

func NewRuntime(config RuntimeConfig) (*Runtime, error) {
	if config.Store == nil {
		return nil, errors.New("videoworkflow: runtime store is required")
	}
	if strings.TrimSpace(config.WorkerID) == "" {
		config.WorkerID = "vw-worker-" + NewRunID()
	}
	if strings.TrimSpace(config.EstimateSecret) == "" {
		return nil, errors.New("videoworkflow: estimate secret is required")
	}
	if config.AssetRoot == "" {
		config.AssetRoot = AssetRoot()
	}
	if err := validateRuntimePublicBaseURL(config.PublicBaseURL); err != nil {
		return nil, err
	}
	if config.HTTPClient == nil {
		config.HTTPClient = NewRestrictedHTTPClientWithAllowedHosts(5*time.Minute, config.AllowedRemoteHosts)
	}
	if config.Composer == nil {
		config.Composer = NewComposer()
	}
	if config.ImageCredits <= 0 {
		config.ImageCredits = 1
	}
	if config.TextCredits <= 0 {
		config.TextCredits = 1
	}
	if config.VideoCredits <= 0 {
		config.VideoCredits = 10
	}
	config.ImageConcurrency = defaultPositive(config.ImageConcurrency, 2)
	config.VideoConcurrency = defaultPositive(config.VideoConcurrency, 2)
	config.ComposeConcurrency = defaultPositive(config.ComposeConcurrency, 1)
	config.WorkerConcurrency = defaultPositive(config.WorkerConcurrency, 4)
	ctx, cancel := context.WithCancel(context.Background())
	return &Runtime{
		config: config, wake: make(chan struct{}, 1), ctx: ctx, cancel: cancel,
		imageSlots: make(chan struct{}, config.ImageConcurrency), videoSlots: make(chan struct{}, config.VideoConcurrency),
		composeSlots: make(chan struct{}, config.ComposeConcurrency), runSlots: make(chan struct{}, config.WorkerConcurrency),
		runCancels: make(map[string]context.CancelFunc),
	}, nil
}

func validateRuntimePublicBaseURL(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.EscapedPath() != "" && parsed.EscapedPath() != "/") {
		return errors.New("videoworkflow: public base URL must be an http(s) origin")
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return errors.New("videoworkflow: public base URL must not use localhost")
	}
	return nil
}

func (r *Runtime) Start() {
	r.mu.Lock()
	if r.started || r.closed {
		r.mu.Unlock()
		return
	}
	r.started = true
	r.mu.Unlock()
	r.wg.Add(1)
	go r.loop()
	r.notify()
}

func (r *Runtime) Close() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	r.mu.Unlock()
	r.cancel()
	r.wg.Wait()
}

func (r *Runtime) Dispatch(context.Context, *Run) error {
	if err := r.Ready(); err != nil {
		return err
	}
	r.notify()
	return nil
}

func (r *Runtime) Ready() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.started {
		return errors.New("videoworkflow: runtime has not started")
	}
	if r.closed || r.ctx.Err() != nil {
		return errors.New("videoworkflow: runtime is closed")
	}
	return nil
}

func (r *Runtime) notify() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

func (r *Runtime) loop() {
	defer r.wg.Done()
	ticker := time.NewTicker(RuntimeRecoveryEvery)
	defer ticker.Stop()
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-r.wake:
			r.claimAvailable()
		case <-ticker.C:
			r.expireApprovals()
			r.claimAvailable()
		}
	}
}

func (r *Runtime) claimAvailable() {
	for {
		select {
		case r.runSlots <- struct{}{}:
		case <-r.ctx.Done():
			return
		default:
			return
		}
		run, err := r.config.Store.ClaimNextRun(r.ctx, r.config.WorkerID, RuntimeLeaseDuration)
		if err != nil {
			<-r.runSlots
			if errors.Is(err, ErrNotFound) {
				return
			}
			return
		}
		r.wg.Add(1)
		go func(run *Run) {
			defer r.wg.Done()
			defer func() { <-r.runSlots; r.notify() }()
			r.executeClaimed(run)
		}(run)
	}
}

func (r *Runtime) executeClaimed(run *Run) {
	ctx, cancel := context.WithCancel(r.ctx)
	r.mu.Lock()
	r.runCancels[run.ID] = cancel
	r.mu.Unlock()
	defer func() {
		cancel()
		r.mu.Lock()
		delete(r.runCancels, run.ID)
		r.mu.Unlock()
	}()
	heartbeatDone := make(chan struct{})
	go r.heartbeat(ctx, cancel, run.ID, run.Status, heartbeatDone)
	start := time.Now()
	var err error
	if run.Status == RunCancelPending {
		err = r.finalizeCanceledRun(ctx, run)
	} else {
		err = r.executeRun(ctx, run)
	}
	cancel()
	<-heartbeatDone
	if errors.Is(err, errAwaitingApproval) || errors.Is(err, errRecoverableProviderSubmission) || errors.Is(err, errRecoverableApprovalPause) ||
		errors.Is(err, errRecoverableNodeCommit) || errors.Is(err, errRecoverableRuntimePersistence) || errors.Is(err, errRecoverableRunFinalization) ||
		errors.Is(err, ErrReservationPending) || errors.Is(err, context.Canceled) {
		return
	}
	if run.Status == RunCancelPending {
		// cancel_pending 是可恢复状态；任一结算/存储瞬态错误都留给租约扫描重试，
		// 不得把用户取消请求改写为 failed 终态。
		return
	}
	if err != nil {
		_ = r.finishRun(context.Background(), run, RunFailed, "runtime_failed", err.Error(), "")
		r.logUsage(run, start, "failed", "runtime_failed")
		return
	}
	r.logUsage(run, start, "success", "")
}

func (r *Runtime) heartbeat(ctx context.Context, cancel context.CancelFunc, runID string, expectedStatus RunStatus, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(RuntimeHeartbeatEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := r.config.Store.HeartbeatRun(ctx, runID, r.config.WorkerID, expectedStatus, RuntimeLeaseDuration); err != nil {
				cancel()
				return
			}
		}
	}
}

func (r *Runtime) Estimate(ctx context.Context, userID uint64, graph Graph) (*CostEstimate, error) {
	return r.EstimateFor(ctx, userID, 0, graph, RunModeFull, "")
}

func (r *Runtime) EstimateFor(_ context.Context, userID, revision uint64, graph Graph, mode RunMode, startNodeID string) (*CostEstimate, error) {
	selected, err := selectedNodeIDs(graph, mode, startNodeID)
	if err != nil {
		return nil, err
	}
	var characterCredits, sceneCredits int64
	for _, node := range graph.Nodes {
		if !selected[node.ID] {
			continue
		}
		switch node.Type {
		case NodeCharacter, NodeBackground:
			count := int64(imageCandidateCount(node))
			characterCredits += count * r.config.ImageCredits
		case NodeScript:
			characterCredits += r.config.TextCredits
		case NodeVideo:
			sceneCredits += r.config.VideoCredits
		}
	}
	hash, err := SemanticGraphHash(graph)
	if err != nil {
		return nil, err
	}
	estimate := &CostEstimate{Hash: hash, ExpiresAt: time.Now().Add(EstimateTokenLifetime), CharacterCredits: characterCredits, SceneCredits: sceneCredits}
	estimate.TotalCredits = characterCredits + sceneCredits
	estimate.Token, err = r.signEstimate(userID, revision, hash, mode, startNodeID, estimate.TotalCredits, estimate.ExpiresAt)
	return estimate, err
}

func (r *Runtime) ValidateEstimate(_ context.Context, userID, revision uint64, graph Graph, mode RunMode, startNodeID, token string) (*CostEstimate, error) {
	payload, err := r.decodeEstimate(token)
	if err != nil || payload.UserID != userID || payload.Revision != revision || payload.Mode != mode || payload.StartNodeID != startNodeID || payload.ExpiresAt <= time.Now().Unix() {
		return nil, ErrInvalidEstimate
	}
	hash, err := SemanticGraphHash(graph)
	if err != nil || payload.Hash != hash {
		return nil, ErrInvalidEstimate
	}
	return &CostEstimate{Token: token, Hash: hash, ExpiresAt: time.Unix(payload.ExpiresAt, 0), TotalCredits: payload.Credits}, nil
}

type estimatePayload struct {
	UserID      uint64  `json:"uid"`
	Revision    uint64  `json:"rev"`
	Hash        string  `json:"hash"`
	Mode        RunMode `json:"mode"`
	StartNodeID string  `json:"start"`
	Credits     int64   `json:"credits"`
	ExpiresAt   int64   `json:"exp"`
}

func (r *Runtime) signEstimate(userID, revision uint64, hash string, mode RunMode, startNodeID string, credits int64, expires time.Time) (string, error) {
	payload, err := json.Marshal(estimatePayload{UserID: userID, Revision: revision, Hash: hash, Mode: mode, StartNodeID: startNodeID, Credits: credits, ExpiresAt: expires.Unix()})
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(r.config.EstimateSecret))
	_, _ = io.WriteString(mac, encoded)
	return encoded + "." + hex.EncodeToString(mac.Sum(nil)), nil
}

func (r *Runtime) decodeEstimate(token string) (estimatePayload, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return estimatePayload{}, ErrInvalidEstimate
	}
	mac := hmac.New(sha256.New, []byte(r.config.EstimateSecret))
	_, _ = io.WriteString(mac, parts[0])
	got, err := hex.DecodeString(parts[1])
	if err != nil || !hmac.Equal(got, mac.Sum(nil)) {
		return estimatePayload{}, ErrInvalidEstimate
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return estimatePayload{}, ErrInvalidEstimate
	}
	var payload estimatePayload
	if json.Unmarshal(raw, &payload) != nil {
		return estimatePayload{}, ErrInvalidEstimate
	}
	return payload, nil
}

func (r *Runtime) Cancel(ctx context.Context, run *Run) error {
	r.mu.Lock()
	cancel := r.runCancels[run.ID]
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	// 取消终态由数据库租约重新领取 cancel_pending 后统一完成，避免请求线程
	// 与原执行线程并发结算同一笔预授权。即使本次请求中断，恢复扫描也会继续。
	r.notify()
	return nil
}

func (r *Runtime) finalizeCanceledRun(ctx context.Context, run *Run) error {
	nodes, err := r.config.Store.ListNodeRuns(ctx, run.ID)
	if err != nil {
		return err
	}
	for i := range nodes {
		status := nodes[i].Status
		if status == NodeRunSucceeded || status == NodeRunFailed || status == NodeRunCanceled {
			continue
		}
		if status != NodeRunCancelPending {
			if !CanTransitionNodeRun(status, NodeRunCancelPending) {
				continue
			}
			if err := r.config.Store.UpdateNodeRunStatus(ctx, nodes[i].ID, status, NodeRunCancelPending, "", "", ""); err != nil {
				if errors.Is(err, ErrInvalidState) {
					continue
				}
				return err
			}
		}
		if err := r.config.Store.UpdateNodeRunStatus(ctx, nodes[i].ID, NodeRunCancelPending, NodeRunCanceled, "", "", ""); err != nil && !errors.Is(err, ErrInvalidState) {
			return err
		}
	}
	return r.finishRun(ctx, run, RunCanceled, "", "", "")
}

func (r *Runtime) ApproveCharacters(ctx context.Context, run *Run, approval CharacterApproval) error {
	decisions := make([]ApprovalNodeDecision, 0, len(approval.Selections))
	for _, selection := range approval.Selections {
		nodeRun, err := r.config.Store.GetNodeRun(ctx, selection.NodeRunID)
		if err != nil || nodeRun.RunID != run.ID || nodeRun.InputHash != selection.InputHash ||
			(nodeRun.Status != NodeRunAwaitingApproval && nodeRun.Status != NodeRunSucceeded) {
			return ErrInvalidState
		}
		version, err := r.config.Store.GetAssetVersion(ctx, run.UserID, selection.SelectedVersionID)
		if err != nil {
			return ErrNotFound
		}
		if !outputContainsVersion(nodeRun.Output, version.ID) {
			return errors.New("videoworkflow: selected character version is not a candidate")
		}
		payload, _ := json.Marshal(selection)
		output := characterApprovalOutput(nodeRun.Output, version.ID)
		decisions = append(decisions, ApprovalNodeDecision{NodeRunID: nodeRun.ID, NodeID: nodeRun.NodeID, InputHash: nodeRun.InputHash,
			ApprovalType: "characters", DecisionPayload: payload, Output: output, OutputVersionID: version.ID})
	}
	if err := r.config.Store.CommitApprovalDecision(ctx, &ApprovalDecisionCommit{
		RunID: run.ID, ExpectedStatus: RunAwaitingCharacterApproval, Decisions: decisions,
	}); err != nil {
		return err
	}
	if run.Status == RunAwaitingCharacterApproval {
		run.Status = RunRunning
	}
	r.notify()
	return nil
}

func (r *Runtime) ApproveStoryboard(ctx context.Context, run *Run, approval StoryboardApproval) error {
	nodeRun, err := r.config.Store.GetNodeRun(ctx, approval.NodeRunID)
	if err != nil || nodeRun.RunID != run.ID || nodeRun.InputHash != approval.InputHash ||
		(nodeRun.Status != NodeRunAwaitingApproval && nodeRun.Status != NodeRunSucceeded) {
		return ErrInvalidState
	}
	if !json.Valid(approval.Script) {
		return errors.New("videoworkflow: invalid storyboard JSON")
	}
	if err := r.config.Store.CommitApprovalDecision(ctx, &ApprovalDecisionCommit{
		RunID: run.ID, ExpectedStatus: RunAwaitingStoryboardApproval,
		Decisions: []ApprovalNodeDecision{{NodeRunID: nodeRun.ID, NodeID: nodeRun.NodeID, InputHash: nodeRun.InputHash,
			ApprovalType: "storyboard", DecisionPayload: approval.Script, Output: approval.Script}},
	}); err != nil {
		return err
	}
	if run.Status == RunAwaitingStoryboardApproval {
		run.Status = RunRunning
	}
	r.notify()
	return nil
}

func (r *Runtime) expireApprovals() {
	runIDs, err := r.config.Store.CancelExpiredApprovals(r.ctx)
	if err != nil {
		return
	}
	for _, runID := range runIDs {
		run, err := r.config.Store.GetRunByID(r.ctx, runID)
		if err != nil {
			continue
		}
		if run.Status == RunCancelPending {
			r.notify()
			continue
		}
		if run.Status != RunAwaitingCharacterApproval && run.Status != RunAwaitingStoryboardApproval {
			continue
		}
		if r.config.Store.UpdateRunStatus(r.ctx, run.ID, run.Status, RunCancelPending, "approval expired") == nil {
			run.Status = RunCancelPending
			_ = r.Cancel(r.ctx, run)
		}
	}
}

func selectedNodeIDs(graph Graph, mode RunMode, startNodeID string) (map[string]bool, error) {
	if err := validateRunSelection(graph, mode, startNodeID); err != nil {
		return nil, err
	}
	active := activeNodeIDs(graph)
	selected := make(map[string]bool, len(graph.Nodes))
	if mode == RunModeFull {
		for _, node := range graph.Nodes {
			if active[node.ID] {
				selected[node.ID] = true
			}
		}
		if countSelectedType(graph, selected, NodeVideo) == 0 {
			return nil, errors.New("videoworkflow: no enabled video scene remains")
		}
		return selected, nil
	}
	if !active[startNodeID] {
		return nil, errors.New("videoworkflow: start node is disabled or depends on a disabled node")
	}
	reverse, forward := make(map[string][]string), make(map[string][]string)
	for _, edge := range graph.Edges {
		reverse[edge.Target] = append(reverse[edge.Target], edge.Source)
		forward[edge.Source] = append(forward[edge.Source], edge.Target)
	}
	visitClosureAllowed(startNodeID, reverse, active, selected)
	if mode == RunModeDownstream {
		visitClosureAllowed(startNodeID, forward, active, selected)
		for id := range selected {
			visitClosureAllowed(id, reverse, active, selected)
		}
	}
	return selected, nil
}

func activeNodeIDs(graph Graph) map[string]bool {
	active := make(map[string]bool, len(graph.Nodes))
	types := make(map[string]NodeType, len(graph.Nodes))
	disabled := make(map[string]bool)
	for _, node := range graph.Nodes {
		active[node.ID] = true
		types[node.ID] = node.Type
		if node.Enabled != nil && !*node.Enabled && node.Type != NodeTimeline && node.Type != NodeCompose {
			disabled[node.ID] = true
		}
	}
	for _, group := range graph.Groups {
		if group.Type == "scene" && !group.Enabled {
			for _, nodeID := range group.NodeIDs {
				if types[nodeID] != NodeTimeline && types[nodeID] != NodeCompose {
					disabled[nodeID] = true
				}
			}
		}
	}
	changed := true
	for changed {
		changed = false
		for _, edge := range graph.Edges {
			if disabled[edge.Source] && !disabled[edge.Target] && types[edge.Target] != NodeTimeline && types[edge.Target] != NodeCompose {
				disabled[edge.Target] = true
				changed = true
			}
		}
	}
	for id := range disabled {
		active[id] = false
	}
	return active
}

func visitClosureAllowed(id string, edges map[string][]string, allowed, seen map[string]bool) {
	if seen[id] || !allowed[id] {
		return
	}
	seen[id] = true
	for _, next := range edges[id] {
		visitClosureAllowed(next, edges, allowed, seen)
	}
}

func countSelectedType(graph Graph, selected map[string]bool, nodeType NodeType) int {
	count := 0
	for _, node := range graph.Nodes {
		if selected[node.ID] && node.Type == nodeType {
			count++
		}
	}
	return count
}

func visitClosure(id string, edges map[string][]string, seen map[string]bool) {
	if seen[id] {
		return
	}
	seen[id] = true
	for _, next := range edges[id] {
		visitClosure(next, edges, seen)
	}
}

func topologicalNodes(graph Graph, selected map[string]bool) ([]Node, error) {
	index := make(map[string]int, len(graph.Nodes))
	nodes := make(map[string]Node, len(graph.Nodes))
	degree := make(map[string]int)
	forward := make(map[string][]string)
	for i, node := range graph.Nodes {
		index[node.ID], nodes[node.ID] = i, node
		if selected[node.ID] {
			degree[node.ID] = 0
		}
	}
	for _, edge := range graph.Edges {
		if selected[edge.Source] && selected[edge.Target] {
			degree[edge.Target]++
			forward[edge.Source] = append(forward[edge.Source], edge.Target)
		}
	}
	queue := make([]string, 0)
	for id, count := range degree {
		if count == 0 {
			queue = append(queue, id)
		}
	}
	sort.Slice(queue, func(i, j int) bool { return index[queue[i]] < index[queue[j]] })
	out := make([]Node, 0, len(selected))
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		out = append(out, nodes[id])
		for _, target := range forward[id] {
			degree[target]--
			if degree[target] == 0 {
				queue = append(queue, target)
				sort.Slice(queue, func(i, j int) bool { return index[queue[i]] < index[queue[j]] })
			}
		}
	}
	if len(out) != len(selected) {
		return nil, errors.New("videoworkflow: selected graph contains a cycle")
	}
	return out, nil
}

func acquireSlot(ctx context.Context, slot chan struct{}) error {
	select {
	case slot <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func releaseSlot(slot chan struct{}) { <-slot }

func (r *Runtime) acquireNodeExecutionSlot(ctx context.Context, run *Run, state *NodeRun, kind string, local chan struct{}, capacity int) (context.Context, func(), error) {
	if err := acquireSlot(ctx, local); err != nil {
		return nil, nil, err
	}
	releaseLocal := true
	defer func() {
		if releaseLocal {
			releaseSlot(local)
		}
	}()
	token := strings.Join([]string{r.config.WorkerID, run.ID, state.ID, NewRunID()}, ":")
	var index int
	for {
		claimed, err := r.config.Store.AcquireRuntimeSlot(ctx, kind, capacity, run.ID, state.ID, r.config.WorkerID, token, RuntimeSlotLease)
		if err == nil {
			index = claimed
			break
		}
		if !errors.Is(err, ErrNotFound) {
			if ctx.Err() != nil {
				return nil, nil, ctx.Err()
			}
			return nil, nil, fmt.Errorf("%w: acquire %s execution slot: %v", errRecoverableRuntimePersistence, kind, err)
		}
		timer := time.NewTimer(200 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, nil, ctx.Err()
		case <-timer.C:
		}
	}
	slotCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(RuntimeSlotHeartbeat)
		defer ticker.Stop()
		for {
			select {
			case <-slotCtx.Done():
				return
			case <-ticker.C:
				if err := r.config.Store.RenewRuntimeSlot(slotCtx, kind, index, run.ID, state.ID, r.config.WorkerID, token, RuntimeSlotLease); err != nil {
					cancel()
					return
				}
			}
		}
	}()
	releaseLocal = false
	var once sync.Once
	release := func() {
		once.Do(func() {
			cancel()
			<-done
			releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer releaseCancel()
			_ = r.config.Store.ReleaseRuntimeSlot(releaseCtx, kind, index, token)
			releaseSlot(local)
		})
	}
	return slotCtx, release, nil
}

func defaultPositive(value, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}

func (r *Runtime) logUsage(run *Run, started time.Time, status, code string) {
	if r.config.Usage == nil {
		return
	}
	r.config.Usage.LogVideoWorkflowUsage(RuntimeUsageEvent{UserID: run.UserID, RequestID: run.RequestID, Type: "video_workflow", CreditCost: run.ActualCost, DurationMS: time.Since(started).Milliseconds(), Status: status, ErrorCode: code})
}

func outputVersionIDs(raw json.RawMessage) []string {
	var value struct {
		CandidateVersionIDs []string `json:"candidate_version_ids"`
		SelectedVersionID   string   `json:"selected_version_id"`
	}
	_ = json.Unmarshal(raw, &value)
	if len(value.CandidateVersionIDs) == 0 && value.SelectedVersionID != "" {
		value.CandidateVersionIDs = []string{value.SelectedVersionID}
	}
	return value.CandidateVersionIDs
}

func outputContainsVersion(raw json.RawMessage, versionID string) bool {
	for _, candidate := range outputVersionIDs(raw) {
		if candidate == versionID {
			return true
		}
	}
	return false
}

func imageCandidateCount(node Node) int {
	var config struct {
		CandidateCount int `json:"candidate_count"`
	}
	_ = json.Unmarshal(node.Config, &config)
	if config.CandidateCount <= 0 {
		return 1
	}
	if config.CandidateCount > 4 {
		return 4
	}
	return config.CandidateCount
}

func resolvedVersionPath(version *AssetVersion, root string) string {
	if version.FilePath != "" {
		return version.FilePath
	}
	return root + string(os.PathSeparator) + strings.TrimPrefix(version.StorageKey, "/")
}

var _ RunRuntimeV2 = (*Runtime)(nil)
