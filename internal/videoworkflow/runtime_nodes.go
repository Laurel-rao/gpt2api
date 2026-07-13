package videoworkflow

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/432539/gpt2api/internal/billing"
)

const rawVideoDurationToleranceMS int64 = 1_000

type runtimeNodeOutput struct {
	VersionID string
	JSON      json.RawMessage
}

type nodeExecutionResult struct {
	VersionID  string
	Output     json.RawMessage
	CreditCost int64
	UpstreamID string
	AwaitType  string
	Media      *preparedNodeMedia
}

type preparedNodeMedia struct {
	Assets     []Asset
	Versions   []AssetVersion
	References []AssetReference
	Saved      []*SavedMedia
}

type timelineRuntimeClip struct {
	ID        string `json:"id"`
	VersionID string `json:"version_id"`
	TrimInMS  int    `json:"trim_in_ms"`
	TrimOutMS int    `json:"trim_out_ms"`
}

func (r *Runtime) executeRun(ctx context.Context, run *Run) error {
	if errs := ValidateGraph(run.GraphSnapshot, true); len(errs) > 0 {
		return &GraphValidationError{Errors: errs}
	}
	selected, err := selectedNodeIDs(run.GraphSnapshot, run.RunMode, run.StartNodeID)
	if err != nil {
		return err
	}
	nodes, err := topologicalNodes(run.GraphSnapshot, selected)
	if err != nil {
		return err
	}
	if err := r.ensureReservation(ctx, run); err != nil {
		return err
	}
	existing, err := r.config.Store.ListNodeRuns(ctx, run.ID)
	if err != nil {
		return fmt.Errorf("%w: list node runs: %v", errRecoverableRuntimePersistence, err)
	}
	states := make(map[string]*NodeRun, len(existing))
	outputs := make(map[string]runtimeNodeOutput, len(existing))
	for i := range existing {
		copy := existing[i]
		states[copy.NodeID] = &copy
		if copy.Status == NodeRunSucceeded {
			outputs[copy.NodeID] = runtimeNodeOutput{VersionID: copy.OutputVersionID, JSON: copy.Output}
		}
	}
	incoming := incomingEdges(run.GraphSnapshot, selected)
	waitingCharacter, waitingStoryboard := false, false
	completed := 0
	for _, node := range nodes {
		if err := ctx.Err(); err != nil {
			return err
		}
		state := states[node.ID]
		if state != nil {
			switch state.Status {
			case NodeRunSucceeded:
				completed++
				continue
			case NodeRunAwaitingApproval:
				if node.Type == NodeCharacter {
					waitingCharacter = true
				} else {
					waitingStoryboard = true
				}
				continue
			case NodeRunFailed, NodeRunCanceled, NodeRunCancelPending:
				return fmt.Errorf("node %s is %s: %s", node.ID, state.Status, state.ErrorMessage)
			}
		}
		dependenciesReady := true
		for _, edge := range incoming[node.ID] {
			dependency := states[edge.Source]
			if dependency == nil || dependency.Status != NodeRunSucceeded {
				dependenciesReady = false
				break
			}
		}
		if !dependenciesReady {
			continue
		}
		upstream := buildUpstreamVersionRefs(incoming[node.ID], outputs)
		if node.Type != NodeVideo {
			upstream = legacyNodeIDSortedRefs(upstream)
		}
		modelSnapshot, providerSnapshot, err := r.nodeModelSnapshot(run, node, state)
		if err != nil {
			return err
		}
		inputHash, err := InputHashWithProviderSnapshot(run.GraphSnapshot, node.ID, upstream, providerSnapshot)
		if err != nil {
			return err
		}
		legacyInputHash := false
		if node.Type == NodeVideo && state != nil && state.InputHash != "" && state.InputHash != inputHash {
			legacyUpstream := legacyNodeIDSortedRefs(upstream)
			legacyCandidates := make([]string, 0, 3)
			if len(providerSnapshot) > 0 {
				legacyProviderHash, legacyErr := InputHashWithProviderSnapshot(run.GraphSnapshot, node.ID, legacyUpstream, providerSnapshot)
				if legacyErr != nil {
					return legacyErr
				}
				legacyCandidates = append(legacyCandidates, legacyProviderHash)
			}
			legacyNoProviderHash, legacyErr := InputHash(run.GraphSnapshot, node.ID, legacyUpstream)
			if legacyErr != nil {
				return legacyErr
			}
			legacyCandidates = append(legacyCandidates, legacyNoProviderHash)
			if len(providerSnapshot) > 0 {
				currentOrderNoProviderHash, currentErr := InputHash(run.GraphSnapshot, node.ID, upstream)
				if currentErr != nil {
					return currentErr
				}
				legacyCandidates = append(legacyCandidates, currentOrderNoProviderHash)
			}
			for _, candidate := range legacyCandidates {
				if state.InputHash == candidate {
					// 历史运行必须沿用已持久化哈希和 provider 快照恢复，且不可复用缓存。
					inputHash = state.InputHash
					legacyInputHash = true
					break
				}
			}
		}
		boundAssetID, boundVersionID := nodeBoundAsset(node)
		providerIdentityAvailable := node.Type != NodeVideo || len(providerSnapshot) > 0
		cacheStateAllowed := state == nil || state.Status == NodeRunQueued
		if state != nil && state.Status == NodeRunRunning && isProviderNode(node.Type) && providerStatePhase(state.ProviderState) == "not_submitted" &&
			state.UpstreamTaskID == "" && state.CreditCost == 0 {
			cacheStateAllowed = true
		}
		cacheAllowed := cacheStateAllowed && !legacyInputHash && providerIdentityAvailable && isMediaNode(node.Type) && boundAssetID == "" && boundVersionID == "" && !(node.Type == NodeCharacter && run.GraphSnapshot.Settings.CharacterApprovalPolicy == ApprovalManual)
		if cacheAllowed {
			cached, cacheErr := r.config.Store.FindCachedAssetVersion(ctx, run.UserID, inputHash)
			if cacheErr == nil {
				state, err = r.completeCachedNode(ctx, run, node, state, inputHash, modelSnapshot, cached)
				if err != nil {
					return err
				}
				states[node.ID] = state
				outputs[node.ID] = runtimeNodeOutput{VersionID: cached.ID}
				completed++
				_ = r.config.Store.UpdateRunProgressFenced(ctx, run.ID, r.config.WorkerID, completed*100/len(nodes))
				continue
			}
			if !errors.Is(cacheErr, ErrNotFound) {
				return fmt.Errorf("%w: find cached output for %s: %v", errRecoverableRuntimePersistence, node.ID, cacheErr)
			}
		}
		if state == nil {
			state = &NodeRun{ID: NewNodeRunID(), RunID: run.ID, NodeID: node.ID, InputHash: inputHash, Status: NodeRunQueued, ModelSnapshot: modelSnapshot, Attempt: 1}
			if err := r.config.Store.CreateNodeRun(ctx, state, node.Type, 1); err != nil {
				found, findErr := r.config.Store.FindNodeRun(ctx, run.ID, node.ID)
				if findErr != nil || !createdNodeRunMatches(found, state, node.Type) {
					return fmt.Errorf("%w: create node %s: %v", errRecoverableRuntimePersistence, node.ID, err)
				}
				state = found
			}
			states[node.ID] = state
		}
		if state.InputHash != inputHash {
			return fmt.Errorf("node %s input hash changed inside immutable run", node.ID)
		}
		if state.Status == NodeRunQueued {
			var initialProviderState json.RawMessage
			if isProviderNode(node.Type) {
				initialProviderState, _ = json.Marshal(map[string]string{"phase": "not_submitted"})
			}
			transition := &FencedNodeRunTransition{
				RunID: run.ID, WorkerID: r.config.WorkerID, NodeRunID: state.ID, NodeID: node.ID, InputHash: state.InputHash,
				From: NodeRunQueued, To: NodeRunRunning, ProviderState: initialProviderState,
			}
			if err := r.config.Store.UpdateNodeRunStatusFenced(ctx, transition); err != nil {
				reconciled := r.reconcileStartedNode(run, node, state, isProviderNode(node.Type))
				if reconciled == nil {
					return fmt.Errorf("%w: start node %s: %v", errRecoverableRuntimePersistence, node.ID, err)
				}
				state = reconciled
				states[node.ID] = state
			} else {
				state.Status = NodeRunRunning
				state.ProviderState = initialProviderState
			}
		}
		if state.Status == NodeRunRunning && isProviderNode(node.Type) && providerStatePhase(state.ProviderState) != "not_submitted" && (node.Type != NodeVideo || state.UpstreamTaskID == "") {
			unknown := fmt.Errorf("%w: node %s has no durable task id; refusing automatic resubmit", ErrProviderSubmissionUnknown, node.ID)
			failCtx, failCancel := context.WithTimeout(context.Background(), RuntimeBillingTimeout)
			failErr := r.config.Store.UpdateNodeRunStatusFenced(failCtx, &FencedNodeRunTransition{
				RunID: run.ID, WorkerID: r.config.WorkerID, NodeRunID: state.ID, NodeID: node.ID, InputHash: state.InputHash,
				From: NodeRunRunning, To: NodeRunFailed, ErrorMessage: unknown.Error(),
			})
			failCancel()
			if failErr != nil {
				return fmt.Errorf("%w: fail recovered node %s: %v", errRecoverableRuntimePersistence, node.ID, failErr)
			}
			state.Status = NodeRunFailed
			return unknown
		}
		result, err := r.executeNode(ctx, run, node, state, incoming[node.ID], outputs)
		if err != nil {
			if errors.Is(err, errRecoverableRuntimePersistence) || errors.Is(err, errRecoverableNodeCommit) || errors.Is(err, context.Canceled) {
				return err
			}
			if errors.Is(err, ErrProviderStatePersistence) {
				if state.UpstreamTaskID != "" {
					providerState, _ := json.Marshal(map[string]any{"phase": "submitted", "task_id": state.UpstreamTaskID})
					persistCtx, persistCancel := context.WithTimeout(context.Background(), RuntimeBillingTimeout)
					_ = r.config.Store.UpdateNodeRunProgressFenced(persistCtx, run.ID, r.config.WorkerID, state.ID, state.InputHash, state.Progress, state.UpstreamTaskID, providerState)
					persistCancel()
				}
				return errRecoverableProviderSubmission
			}
			failCtx, failCancel := context.WithTimeout(context.Background(), RuntimeBillingTimeout)
			failErr := r.config.Store.UpdateNodeRunStatusFenced(failCtx, &FencedNodeRunTransition{
				RunID: run.ID, WorkerID: r.config.WorkerID, NodeRunID: state.ID, NodeID: node.ID, InputHash: state.InputHash,
				From: state.Status, To: NodeRunFailed, UpstreamTaskID: state.UpstreamTaskID, ErrorMessage: err.Error(),
			})
			failCancel()
			if failErr != nil {
				return fmt.Errorf("%w: fail node %s: %v", errRecoverableRuntimePersistence, node.ID, failErr)
			}
			state.Status = NodeRunFailed
			return fmt.Errorf("execute node %s: %w", node.ID, err)
		}
		if result.UpstreamID != "" {
			state.UpstreamTaskID = result.UpstreamID
		}
		targetStatus := NodeRunSucceeded
		var approval *Approval
		if result.AwaitType != "" {
			targetStatus = NodeRunAwaitingApproval
			approval = &Approval{ID: NewApprovalID(), RunID: run.ID, NodeRunID: state.ID, NodeID: node.ID, InputHash: inputHash,
				Type: result.AwaitType, Payload: result.Output, Status: "pending", ExpiresAt: time.Now().Add(RuntimeApprovalLifetime)}
		}
		commitUpstreamID := result.UpstreamID
		if commitUpstreamID == "" {
			commitUpstreamID = state.UpstreamTaskID
		}
		commit := &NodeResultCommit{
			RunID: run.ID, WorkerID: r.config.WorkerID, ExpectedRunStatus: RunRunning,
			NodeRunID: state.ID, NodeID: node.ID, InputHash: inputHash, From: NodeRunRunning, To: targetStatus,
			Output: result.Output, CreditCost: result.CreditCost, UpstreamTaskID: commitUpstreamID,
			OutputVersionID: result.VersionID, Approval: approval,
		}
		if result.Media != nil {
			commit.Assets = result.Media.Assets
			commit.Versions = result.Media.Versions
			commit.References = result.Media.References
		}
		commitCtx, commitCancel := boundedDetachedContext(ctx, RuntimeBillingTimeout)
		err = r.config.Store.CommitNodeResult(commitCtx, commit)
		commitCancel()
		if err != nil {
			reconciled, definitelyNotCommitted := r.reconcileNodeResultCommit(run.UserID, commit)
			if reconciled == nil {
				if definitelyNotCommitted {
					cleanupPreparedNodeMedia(result.Media)
					return err
				}
				return fmt.Errorf("%w: %v", errRecoverableNodeCommit, err)
			}
			state = reconciled
			states[node.ID] = state
		} else {
			state.OutputVersionID, state.Output, state.CreditCost = result.VersionID, result.Output, result.CreditCost
			state.Status = targetStatus
		}
		if state.Status == NodeRunAwaitingApproval {
			if result.AwaitType == "characters" {
				waitingCharacter = true
			} else {
				waitingStoryboard = true
			}
			continue
		}
		if state.Status != NodeRunSucceeded {
			return ErrInvalidState
		}
		outputs[node.ID] = runtimeNodeOutput{VersionID: state.OutputVersionID, JSON: state.Output}
		completed++
		_ = r.config.Store.UpdateRunProgressFenced(ctx, run.ID, r.config.WorkerID, completed*100/len(nodes))
	}
	if waitingCharacter || waitingStoryboard {
		status := RunAwaitingStoryboardApproval
		if waitingCharacter {
			status = RunAwaitingCharacterApproval
		}
		if err := r.config.Store.UpdateRunStatusFenced(ctx, run.ID, r.config.WorkerID, RunRunning, status, ""); err != nil {
			pauseErr := err
			reconcileCtx, reconcileCancel := context.WithTimeout(context.Background(), RuntimeBillingTimeout)
			persisted, loadErr := r.config.Store.GetRunByID(reconcileCtx, run.ID)
			reconcileCancel()
			if loadErr == nil && persisted.Status == status {
				run.Status = status
				return errAwaitingApproval
			}
			return fmt.Errorf("%w: %v", errRecoverableApprovalPause, pauseErr)
		}
		run.Status = status
		return errAwaitingApproval
	}
	for _, node := range nodes {
		if states[node.ID] == nil || states[node.ID].Status != NodeRunSucceeded {
			return fmt.Errorf("node %s is blocked by incomplete dependencies", node.ID)
		}
	}
	outputVersionID := ""
	for _, node := range nodes {
		if node.Type == NodeCompose {
			outputVersionID = states[node.ID].OutputVersionID
		}
	}
	if err := r.finishRun(ctx, run, RunSucceeded, "", "", outputVersionID); err != nil {
		finalizeErr := err
		reconcileCtx, reconcileCancel := context.WithTimeout(context.Background(), RuntimeBillingTimeout)
		persisted, loadErr := r.config.Store.GetRunByID(reconcileCtx, run.ID)
		reconcileCancel()
		if loadErr == nil && persisted.Status == RunSucceeded && persisted.OutputVersionID == outputVersionID {
			run.Status, run.OutputVersionID, run.ActualCost = persisted.Status, persisted.OutputVersionID, persisted.ActualCost
			return nil
		}
		return fmt.Errorf("%w: %v", errRecoverableRunFinalization, finalizeErr)
	}
	return nil
}

func (r *Runtime) nodeModelSnapshot(run *Run, node Node, state *NodeRun) (json.RawMessage, json.RawMessage, error) {
	if state != nil {
		modelSnapshot := append(json.RawMessage(nil), state.ModelSnapshot...)
		if node.Type != NodeVideo {
			return modelSnapshot, nil, nil
		}
		providerSnapshot, err := videoProviderConfig(modelSnapshot)
		return modelSnapshot, providerSnapshot, err
	}
	modelState := map[string]any{
		"text": run.GraphSnapshot.Settings.TextModel, "image": run.GraphSnapshot.Settings.ImageModel, "video": run.GraphSnapshot.Settings.VideoModel,
	}
	var providerSnapshot json.RawMessage
	if node.Type == NodeVideo {
		var rawSnapshot json.RawMessage
		if snapshotter, ok := r.config.VideoGenerator.(RuntimeVideoModelConfigSnapshotter); ok {
			rawSnapshot = snapshotter.VideoConfigSnapshotForModel(run.GraphSnapshot.Settings.VideoModel)
		} else if snapshotter, ok := r.config.VideoGenerator.(RuntimeVideoConfigSnapshotter); ok {
			rawSnapshot = snapshotter.VideoConfigSnapshot()
		}
		if len(rawSnapshot) > 0 {
			normalized, err := normalizeVideoProviderSnapshot(rawSnapshot)
			if err != nil {
				return nil, nil, err
			}
			if len(normalized) > 0 {
				providerSnapshot = normalized
				modelState["video_provider"] = normalized
			}
		}
	}
	modelSnapshot, err := json.Marshal(modelState)
	if err != nil {
		return nil, nil, err
	}
	return modelSnapshot, providerSnapshot, nil
}

func (r *Runtime) completeCachedNode(ctx context.Context, run *Run, node Node, state *NodeRun, inputHash string, modelSnapshot json.RawMessage, cached *AssetVersion) (*NodeRun, error) {
	if state == nil {
		state = &NodeRun{ID: NewNodeRunID(), RunID: run.ID, NodeID: node.ID, InputHash: inputHash, Status: NodeRunQueued, ModelSnapshot: modelSnapshot, CacheHit: true, Attempt: 1}
		if err := r.config.Store.CreateNodeRun(ctx, state, node.Type, 1); err != nil {
			found, findErr := r.config.Store.FindNodeRun(ctx, run.ID, node.ID)
			if findErr != nil || !createdNodeRunMatches(found, state, node.Type) {
				return nil, fmt.Errorf("%w: create cached node %s: %v", errRecoverableRuntimePersistence, node.ID, err)
			}
			state = found
		}
	}
	if state.InputHash != inputHash {
		return nil, fmt.Errorf("node %s input hash changed inside immutable run", node.ID)
	}
	if state.Status == NodeRunSucceeded {
		return state, nil
	}
	if state.Status == NodeRunQueued {
		if err := r.config.Store.UpdateNodeRunStatusFenced(ctx, &FencedNodeRunTransition{
			RunID: run.ID, WorkerID: r.config.WorkerID, NodeRunID: state.ID, NodeID: node.ID, InputHash: state.InputHash,
			From: NodeRunQueued, To: NodeRunRunning,
		}); err != nil {
			reconciled := r.reconcileStartedNode(run, node, state, false)
			if reconciled == nil {
				return nil, fmt.Errorf("%w: start cached node %s: %v", errRecoverableRuntimePersistence, node.ID, err)
			}
			state = reconciled
		} else {
			state.Status = NodeRunRunning
		}
	}
	output, _ := json.Marshal(map[string]string{"version_id": cached.ID})
	commit := &NodeResultCommit{
		RunID: run.ID, WorkerID: r.config.WorkerID, ExpectedRunStatus: RunRunning,
		NodeRunID: state.ID, NodeID: node.ID, InputHash: inputHash, From: state.Status, To: NodeRunSucceeded,
		Output: output, CreditCost: 0, CacheHit: true, OutputVersionID: cached.ID,
	}
	if err := r.config.Store.CommitNodeResult(ctx, commit); err != nil {
		reconciled, definitelyNotCommitted := r.reconcileNodeResultCommit(run.UserID, commit)
		if reconciled == nil {
			if definitelyNotCommitted {
				return nil, err
			}
			return nil, fmt.Errorf("%w: %v", errRecoverableNodeCommit, err)
		}
		return reconciled, nil
	}
	state.Status, state.OutputVersionID, state.Output, state.CacheHit = NodeRunSucceeded, cached.ID, output, true
	return state, nil
}

func createdNodeRunMatches(actual, expected *NodeRun, nodeType NodeType) bool {
	return actual != nil && expected != nil && actual.ID == expected.ID && actual.RunID == expected.RunID && actual.NodeID == expected.NodeID &&
		actual.NodeType == nodeType && actual.InputHash == expected.InputHash && actual.Status == NodeRunQueued && actual.Attempt == expected.Attempt &&
		bytes.Equal(normalizeJSON(actual.ModelSnapshot), normalizeJSON(expected.ModelSnapshot))
}

func providerStatePhase(raw json.RawMessage) string {
	var value struct {
		Phase string `json:"phase"`
	}
	_ = json.Unmarshal(raw, &value)
	return value.Phase
}

func (r *Runtime) reconcileStartedNode(run *Run, node Node, expected *NodeRun, provider bool) *NodeRun {
	ctx, cancel := context.WithTimeout(context.Background(), RuntimeBillingTimeout)
	defer cancel()
	persistedRun, err := r.config.Store.GetRunByID(ctx, run.ID)
	if err != nil || persistedRun.Status != RunRunning || persistedRun.LeaseOwner != r.config.WorkerID ||
		persistedRun.LeaseExpiresAt == nil || persistedRun.LeaseExpiresAt.Before(time.Now()) {
		return nil
	}
	persisted, err := r.config.Store.GetNodeRun(ctx, expected.ID)
	if err != nil || persisted.RunID != run.ID || persisted.NodeID != node.ID || persisted.InputHash != expected.InputHash || persisted.Status != NodeRunRunning {
		return nil
	}
	if provider && providerStatePhase(persisted.ProviderState) != "not_submitted" {
		return nil
	}
	return persisted
}

func (r *Runtime) markProviderSubmitting(ctx context.Context, run *Run, node Node, state *NodeRun) error {
	submitting, _ := json.Marshal(map[string]string{"phase": "submitting"})
	err := r.config.Store.UpdateNodeRunProgressFenced(ctx, run.ID, r.config.WorkerID, state.ID, state.InputHash, state.Progress, "", submitting)
	if err == nil {
		state.ProviderState = submitting
		return nil
	}
	reconcileCtx, reconcileCancel := context.WithTimeout(context.Background(), RuntimeBillingTimeout)
	defer reconcileCancel()
	persistedRun, runErr := r.config.Store.GetRunByID(reconcileCtx, run.ID)
	persisted, nodeErr := r.config.Store.GetNodeRun(reconcileCtx, state.ID)
	if runErr == nil && nodeErr == nil && persistedRun.Status == RunRunning && persistedRun.LeaseOwner == r.config.WorkerID &&
		persistedRun.LeaseExpiresAt != nil && persistedRun.LeaseExpiresAt.After(time.Now()) && persisted.RunID == run.ID &&
		persisted.NodeID == node.ID && persisted.InputHash == state.InputHash && persisted.Status == NodeRunRunning &&
		providerStatePhase(persisted.ProviderState) == "submitting" {
		state.ProviderState = append(json.RawMessage(nil), persisted.ProviderState...)
		return nil
	}
	return fmt.Errorf("%w: persist provider submitting phase for %s: %v", errRecoverableRuntimePersistence, node.ID, err)
}

func (r *Runtime) executeNode(ctx context.Context, run *Run, node Node, state *NodeRun, incoming []Edge, outputs map[string]runtimeNodeOutput) (*nodeExecutionResult, error) {
	switch node.Type {
	case NodeStoryBrief:
		if len(node.Config) == 0 {
			return &nodeExecutionResult{Output: json.RawMessage(`{}`)}, nil
		}
		return &nodeExecutionResult{Output: normalizeJSON(node.Config)}, nil
	case NodeCharacter, NodeBackground:
		return r.executeImageNode(ctx, run, node, state, incoming, outputs)
	case NodeScript:
		return r.executeScriptNode(ctx, run, node, state, incoming, outputs)
	case NodeScene:
		return executeSceneNode(node, incoming, outputs)
	case NodeVideo:
		return r.executeVideoNode(ctx, run, node, state, incoming, outputs)
	case NodeTimeline:
		return executeTimelineNode(node, incoming, outputs)
	case NodeCompose:
		return r.executeComposeNode(ctx, run, node, incoming, outputs, state)
	default:
		return nil, fmt.Errorf("unsupported node type %s", node.Type)
	}
}

func (r *Runtime) executeImageNode(ctx context.Context, run *Run, node Node, state *NodeRun, incoming []Edge, outputs map[string]runtimeNodeOutput) (*nodeExecutionResult, error) {
	assetID, versionID := nodeBoundAsset(node)
	if assetID != "" || versionID != "" {
		if assetID == "" || versionID == "" {
			return nil, errors.New("bound image requires both asset_id and asset_version_id")
		}
		version, err := r.config.Store.GetAssetVersion(ctx, run.UserID, versionID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return nil, ErrNotFound
			}
			return nil, fmt.Errorf("%w: load bound asset version for %s: %v", errRecoverableRuntimePersistence, node.ID, err)
		}
		if version.AssetID != assetID || version.Status != AssetReady || !strings.HasPrefix(version.MIMEType, "image/") {
			return nil, ErrNotFound
		}
		if err := r.config.Store.AddAssetReference(ctx, &AssetReference{VersionID: version.ID, UserID: run.UserID, RefType: "run", RefID: run.ID, NodeID: node.ID}, node.ID); err != nil {
			return nil, fmt.Errorf("%w: add bound asset reference for %s: %v", errRecoverableRuntimePersistence, node.ID, err)
		}
		output, _ := json.Marshal(map[string]any{"candidate_version_ids": []string{version.ID}, "selected_version_id": version.ID, "source": "bound_asset", "prompt": buildNodePrompt(node, incoming, outputs)})
		return &nodeExecutionResult{VersionID: version.ID, Output: output}, nil
	}
	if r.config.ImageGenerator == nil {
		return nil, errors.New("image generator is not configured")
	}
	slotCtx, release, err := r.acquireNodeExecutionSlot(ctx, run, state, "image", r.imageSlots, r.config.ImageConcurrency)
	if err != nil {
		return nil, err
	}
	defer release()
	ctx = slotCtx
	references, err := r.upstreamImageData(ctx, run.UserID, incoming, outputs)
	if err != nil {
		return nil, err
	}
	if err := r.markProviderSubmitting(ctx, run, node, state); err != nil {
		return nil, err
	}
	prompt := buildNodePrompt(node, incoming, outputs)
	generated, err := r.config.ImageGenerator.GenerateImage(ctx, ImageGenerationRequest{
		TaskID: state.ID, UserID: run.UserID, Prompt: prompt, Model: run.GraphSnapshot.Settings.ImageModel,
		Size: imageSizeForAspect(run.GraphSnapshot.Settings.AspectRatio), Count: imageCandidateCount(node), References: references,
	})
	if err != nil {
		return nil, err
	}
	if len(generated) == 0 {
		return nil, errors.New("image generator returned no images")
	}
	var cost int64
	upstreamID := ""
	for _, item := range generated {
		if item.CreditCost > 0 {
			cost += item.CreditCost
		} else {
			cost += r.config.ImageCredits
		}
		if upstreamID == "" {
			upstreamID = item.TaskID
		}
	}
	if err := r.recordProviderCost(ctx, state.ID, cost); err != nil {
		return nil, err
	}
	media, versionIDs, err := r.prepareGeneratedImages(ctx, run, node, state, generated)
	if err != nil {
		return nil, err
	}
	output, _ := json.Marshal(map[string]any{"candidate_version_ids": versionIDs, "selected_version_id": versionIDs[0], "prompt": prompt})
	await := ""
	if node.Type == NodeCharacter && run.GraphSnapshot.Settings.CharacterApprovalPolicy == ApprovalManual {
		await = "characters"
	}
	return &nodeExecutionResult{VersionID: versionIDs[0], Output: output, CreditCost: cost, UpstreamID: upstreamID, AwaitType: await, Media: media}, nil
}

func nodeBoundAsset(node Node) (string, string) {
	assetID, versionID := strings.TrimSpace(node.AssetID), strings.TrimSpace(node.AssetVersionID)
	if assetID != "" || versionID != "" {
		return assetID, versionID
	}
	var config struct {
		AssetID        string `json:"asset_id"`
		AssetVersionID string `json:"asset_version_id"`
	}
	if json.Unmarshal(node.Config, &config) == nil {
		return strings.TrimSpace(config.AssetID), strings.TrimSpace(config.AssetVersionID)
	}
	return "", ""
}

func (r *Runtime) executeScriptNode(ctx context.Context, run *Run, node Node, state *NodeRun, incoming []Edge, outputs map[string]runtimeNodeOutput) (*nodeExecutionResult, error) {
	if r.config.TextGenerator == nil {
		return nil, errors.New("text generator is not configured")
	}
	if err := r.markProviderSubmitting(ctx, run, node, state); err != nil {
		return nil, err
	}
	prompt := buildNodePrompt(node, incoming, outputs)
	output, cost, err := r.config.TextGenerator.GenerateText(ctx, TextGenerationRequest{Prompt: prompt, Model: run.GraphSnapshot.Settings.TextModel})
	if err != nil {
		return nil, err
	}
	if !json.Valid(output) {
		return nil, errors.New("text generator returned invalid JSON")
	}
	output = attachPromptToNodeOutput(output, prompt)
	if cost <= 0 {
		cost = r.config.TextCredits
	}
	if err := r.recordProviderCost(ctx, state.ID, cost); err != nil {
		return nil, err
	}
	await := ""
	if run.GraphSnapshot.Settings.StoryboardApprovalPolicy == ApprovalManual {
		await = "storyboard"
	}
	return &nodeExecutionResult{Output: output, CreditCost: cost, AwaitType: await}, nil
}

func executeSceneNode(node Node, incoming []Edge, outputs map[string]runtimeNodeOutput) (*nodeExecutionResult, error) {
	var config struct {
		Index int `json:"index"`
	}
	_ = json.Unmarshal(node.Config, &config)
	for _, edge := range incoming {
		raw := outputs[edge.Source].JSON
		var script struct {
			Scenes []json.RawMessage `json:"scenes"`
		}
		if json.Unmarshal(raw, &script) == nil && config.Index > 0 && config.Index <= len(script.Scenes) {
			return &nodeExecutionResult{Output: script.Scenes[config.Index-1]}, nil
		}
		if len(raw) > 0 {
			output, _ := json.Marshal(map[string]any{"index": maxInt(config.Index, 1), "content": json.RawMessage(raw)})
			return &nodeExecutionResult{Output: output}, nil
		}
	}
	return nil, errors.New("scene node has no script output")
}

func (r *Runtime) executeVideoNode(ctx context.Context, run *Run, node Node, state *NodeRun, incoming []Edge, outputs map[string]runtimeNodeOutput) (*nodeExecutionResult, error) {
	if r.config.VideoGenerator == nil {
		return nil, errors.New("video generator is not configured")
	}
	slotCtx, release, err := r.acquireNodeExecutionSlot(ctx, run, state, "video", r.videoSlots, r.config.VideoConcurrency)
	if err != nil {
		return nil, err
	}
	defer release()
	ctx = slotCtx
	referenceURLs, err := r.upstreamReferenceURLs(ctx, run.UserID, incoming, outputs)
	if err != nil {
		return nil, err
	}
	providerConfig, err := videoProviderConfig(state.ModelSnapshot)
	if err != nil {
		return nil, err
	}
	if state.UpstreamTaskID == "" {
		if err := r.markProviderSubmitting(ctx, run, node, state); err != nil {
			return nil, fmt.Errorf("persist video submitting state: %w", err)
		}
	}
	prompt := buildNodePrompt(node, incoming, outputs)
	generated, err := r.config.VideoGenerator.GenerateVideo(ctx, VideoGenerationRequest{
		Prompt: prompt, Model: run.GraphSnapshot.Settings.VideoModel,
		AspectRatio: string(run.GraphSnapshot.Settings.AspectRatio), Resolution: string(run.GraphSnapshot.Settings.EffectiveResolution()),
		DurationSec: SceneDuration, ReferenceURLs: referenceURLs, ProviderTaskID: state.UpstreamTaskID, ProviderConfig: providerConfig,
		OnSubmitted: func(taskID string, provider json.RawMessage) error {
			state.UpstreamTaskID = taskID
			state.ProviderState = provider
			persistCtx, persistCancel := context.WithTimeout(context.Background(), RuntimeBillingTimeout)
			defer persistCancel()
			if err := r.config.Store.UpdateNodeRunProgressFenced(persistCtx, run.ID, r.config.WorkerID, state.ID, state.InputHash, 0, taskID, provider); err != nil {
				return fmt.Errorf("%w: %v", ErrProviderStatePersistence, err)
			}
			return nil
		},
		OnProgress: func(taskID string, progress int, provider json.RawMessage) {
			persistCtx, persistCancel := context.WithTimeout(context.Background(), RuntimeBillingTimeout)
			_ = r.config.Store.UpdateNodeRunProgressFenced(persistCtx, run.ID, r.config.WorkerID, state.ID, state.InputHash, progress, taskID, provider)
			persistCancel()
			state.UpstreamTaskID = taskID
		},
	})
	if err != nil {
		return nil, err
	}
	cost := generated.CreditCost
	if cost <= 0 {
		cost = r.config.VideoCredits
	}
	if err := r.recordProviderCost(ctx, state.ID, cost); err != nil {
		return nil, err
	}
	version, media, err := r.prepareGeneratedMedia(ctx, run, node, state, MediaKindVideo, generated.Data, generated.URL, generated.MIMEType, state.InputHash, "generated")
	if err != nil {
		return nil, err
	}
	output, _ := json.Marshal(map[string]string{"version_id": version.ID, "prompt": prompt})
	return &nodeExecutionResult{VersionID: version.ID, Output: output, CreditCost: cost, UpstreamID: generated.TaskID, Media: media}, nil
}

func videoProviderConfig(modelSnapshot json.RawMessage) (json.RawMessage, error) {
	var value struct {
		VideoProvider json.RawMessage `json:"video_provider"`
	}
	if len(modelSnapshot) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(modelSnapshot, &value); err != nil {
		return nil, fmt.Errorf("decode model snapshot: %w", err)
	}
	return normalizeVideoProviderSnapshot(value.VideoProvider)
}

func executeTimelineNode(node Node, incoming []Edge, outputs map[string]runtimeNodeOutput) (*nodeExecutionResult, error) {
	var config TimelineConfig
	if err := json.Unmarshal(node.Config, &config); err != nil {
		return nil, err
	}
	clips := normalizedTimelineClips(config)
	versions := make(map[string]string)
	for _, edge := range incoming {
		versions[edge.Source] = outputs[edge.Source].VersionID
	}
	runtimeClips := make([]timelineRuntimeClip, 0, len(clips))
	for _, clip := range clips {
		versionID := versions[clip.SourceNodeID]
		if versionID == "" {
			continue
		}
		runtimeClips = append(runtimeClips, timelineRuntimeClip{ID: clip.ID, VersionID: versionID, TrimInMS: clip.TrimInMS, TrimOutMS: clip.TrimOutMS})
	}
	if len(runtimeClips) == 0 {
		return nil, errors.New("timeline has no enabled video clips")
	}
	output, _ := json.Marshal(map[string]any{"clips": runtimeClips})
	return &nodeExecutionResult{Output: output}, nil
}

func (r *Runtime) executeComposeNode(ctx context.Context, run *Run, node Node, incoming []Edge, outputs map[string]runtimeNodeOutput, state *NodeRun) (*nodeExecutionResult, error) {
	slotCtx, release, err := r.acquireNodeExecutionSlot(ctx, run, state, "compose", r.composeSlots, r.config.ComposeConcurrency)
	if err != nil {
		return nil, err
	}
	defer release()
	ctx = slotCtx
	var clips []timelineRuntimeClip
	for _, edge := range incoming {
		var value struct {
			Clips []timelineRuntimeClip `json:"clips"`
		}
		if json.Unmarshal(outputs[edge.Source].JSON, &value) == nil {
			clips = append(clips, value.Clips...)
		}
	}
	if len(clips) < 1 || len(clips) > 4 {
		return nil, errors.New("compose requires 1-4 timeline clips")
	}
	composeClips := make([]ComposeClip, 0, len(clips))
	for _, clip := range clips {
		version, err := r.config.Store.GetAssetVersion(ctx, run.UserID, clip.VersionID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return nil, ErrNotFound
			}
			return nil, fmt.Errorf("%w: load compose clip %s: %v", errRecoverableRuntimePersistence, clip.ID, err)
		}
		composeClips = append(composeClips, ComposeClip{Path: resolvedVersionPath(version, r.config.AssetRoot), TrimInMS: clip.TrimInMS, TrimOutMS: clip.TrimOutMS})
	}
	spec, err := ResolveOutputSpecFor(string(run.GraphSnapshot.Settings.AspectRatio), run.GraphSnapshot.Settings.EffectiveResolution())
	if err != nil {
		return nil, err
	}
	outputPath := filepath.Join(r.config.AssetRoot, fmt.Sprintf("%d", run.UserID), "composed", NewVersionID()+".mp4")
	result, err := r.config.Composer.ComposeClips(ctx, composeClips, outputPath, spec)
	if err != nil {
		return nil, err
	}
	defer os.Remove(outputPath)
	data, err := os.ReadFile(result.Path)
	if err != nil {
		return nil, err
	}
	version, media, err := r.prepareGeneratedMedia(ctx, run, node, state, MediaKindComposedVideo, data, "", "video/mp4", state.InputHash, "compose")
	if err != nil {
		return nil, err
	}
	output, _ := json.Marshal(map[string]any{"version_id": version.ID, "duration_ms": int64(result.Duration*1000 + 0.5), "width": result.Width, "height": result.Height})
	return &nodeExecutionResult{VersionID: version.ID, Output: output, Media: media}, nil
}

func (r *Runtime) prepareGeneratedImages(ctx context.Context, run *Run, node Node, state *NodeRun, images []GeneratedImage) (*preparedNodeMedia, []string, error) {
	asset := Asset{ID: NewAssetID(), OwnerUserID: run.UserID, Kind: MediaKindImage, Name: node.ID, Status: AssetPending}
	media := &preparedNodeMedia{Assets: []Asset{asset}}
	ids := make([]string, 0, len(images))
	for _, generated := range images {
		version, saved, err := r.prepareGeneratedMediaForAsset(ctx, run, node, state, asset.ID, MediaKindImage, generated.Data, generated.URL, generated.MIMEType, state.InputHash, "generated")
		if err != nil {
			cleanupPreparedNodeMedia(media)
			return nil, nil, err
		}
		media.Versions = append(media.Versions, *version)
		media.Saved = append(media.Saved, saved)
		media.References = append(media.References, AssetReference{VersionID: version.ID, UserID: run.UserID, RefType: "run", RefID: run.ID, NodeID: node.ID})
		ids = append(ids, version.ID)
	}
	return media, ids, nil
}

func (r *Runtime) prepareGeneratedMedia(ctx context.Context, run *Run, node Node, state *NodeRun, kind string, data []byte, rawURL, mimeType, inputHash, sourceType string) (*AssetVersion, *preparedNodeMedia, error) {
	asset := Asset{ID: NewAssetID(), OwnerUserID: run.UserID, Kind: kind, Name: node.ID, Status: AssetPending}
	version, saved, err := r.prepareGeneratedMediaForAsset(ctx, run, node, state, asset.ID, kind, data, rawURL, mimeType, inputHash, sourceType)
	if err != nil {
		return nil, nil, err
	}
	media := &preparedNodeMedia{
		Assets: []Asset{asset}, Versions: []AssetVersion{*version}, Saved: []*SavedMedia{saved},
		References: []AssetReference{{VersionID: version.ID, UserID: run.UserID, RefType: "run", RefID: run.ID, NodeID: node.ID}},
	}
	return version, media, nil
}

func (r *Runtime) prepareGeneratedMediaForAsset(ctx context.Context, run *Run, node Node, state *NodeRun, assetID, kind string, data []byte, rawURL, mimeType, inputHash, sourceType string) (*AssetVersion, *SavedMedia, error) {
	if len(data) == 0 {
		var err error
		data, mimeType, err = DownloadRemoteMedia(ctx, r.config.HTTPClient, rawURL, maxBytesForKind(kind))
		if err != nil {
			return nil, nil, err
		}
	}
	used, err := r.config.Store.UsedAssetBytes(ctx, run.UserID)
	if err != nil {
		return nil, nil, err
	}
	saved, err := SaveMedia(ctx, r.config.AssetRoot, run.UserID, kind, node.ID, bytes.NewReader(data), used)
	if err != nil {
		return nil, nil, err
	}
	version := &AssetVersion{ID: NewVersionID(), AssetID: assetID, OwnerUserID: run.UserID, Status: AssetReady,
		MIMEType: saved.MIME, StorageKey: storageKey(r.config.AssetRoot, saved.Path), FilePath: saved.Path,
		SizeBytes: saved.SizeBytes, SHA256: saved.SHA256, InputHash: inputHash, SourceType: sourceType,
		CreatedByRunID: run.ID, CreatedByNodeRunID: state.ID}
	if kind == MediaKindImage {
		service := &Service{composer: r.config.Composer}
		version.Width, version.Height, _, err = service.inspectUploadedMedia(ctx, kind, saved.Path)
	} else {
		service := &Service{composer: r.config.Composer}
		version.Width, version.Height, version.DurationMS, err = service.inspectUploadedMedia(ctx, MediaKindVideo, saved.Path)
	}
	if err != nil {
		removeNewMedia(saved)
		return nil, nil, err
	}
	if kind == MediaKindVideo && node.Type == NodeVideo {
		if err := validateRawVideoDuration(version.DurationMS); err != nil {
			removeNewMedia(saved)
			return nil, nil, err
		}
	}
	return version, saved, nil
}

func cleanupPreparedNodeMedia(media *preparedNodeMedia) {
	if media == nil {
		return
	}
	for _, saved := range media.Saved {
		removeNewMedia(saved)
	}
}

// reconcileNodeResultCommit 处理“事务可能已提交，但客户端只收到错误”的结果。
// 只有数据库明确仍处于提交前状态时才允许调用方删除物理文件；任何读取失败或
// 不一致都按未知状态处理，宁可留下可回收孤儿文件，也不能破坏已落库版本。
func (r *Runtime) reconcileNodeResultCommit(userID uint64, commit *NodeResultCommit) (*NodeRun, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), RuntimeBillingTimeout)
	defer cancel()
	node, err := r.config.Store.GetNodeRun(ctx, commit.NodeRunID)
	if err != nil {
		return nil, false
	}
	if node.RunID != commit.RunID || node.NodeID != commit.NodeID || node.InputHash != commit.InputHash {
		return nil, false
	}
	if node.Status == commit.From {
		return nil, true
	}
	if node.CreditCost != commit.CreditCost || node.CacheHit != commit.CacheHit || node.UpstreamTaskID != commit.UpstreamTaskID {
		return nil, false
	}
	if node.Status == commit.To && node.OutputVersionID == commit.OutputVersionID &&
		bytes.Equal(normalizeJSON(node.Output), normalizeJSON(commit.Output)) {
		if r.committedVersionsMatch(ctx, userID, commit) {
			return node, false
		}
		return nil, false
	}
	if commit.To != NodeRunAwaitingApproval || node.Status != NodeRunSucceeded || commit.Approval == nil {
		return nil, false
	}
	approval, err := r.config.Store.GetApprovalByNode(ctx, commit.RunID, commit.NodeRunID, commit.InputHash, commit.Approval.Type)
	if err != nil || approval.Status != "approved" || approval.NodeID != commit.NodeID {
		return nil, false
	}
	switch commit.Approval.Type {
	case "characters":
		var selection CharacterSelection
		if json.Unmarshal(approval.Payload, &selection) != nil || selection.NodeRunID != commit.NodeRunID || selection.InputHash != commit.InputHash {
			return nil, false
		}
		candidateIDs := outputVersionIDs(commit.Output)
		selected := false
		for _, versionID := range candidateIDs {
			if versionID == selection.SelectedVersionID {
				selected = true
				break
			}
		}
		if !selected || node.OutputVersionID != selection.SelectedVersionID {
			return nil, false
		}
		expectedOutput := characterApprovalOutput(node.Output, selection.SelectedVersionID)
		if !bytes.Equal(normalizeJSON(node.Output), normalizeJSON(expectedOutput)) {
			return nil, false
		}
	case "storyboard":
		if node.OutputVersionID != commit.OutputVersionID || !bytes.Equal(normalizeJSON(node.Output), normalizeJSON(approval.Payload)) {
			return nil, false
		}
	default:
		return nil, false
	}
	if !r.committedVersionsMatch(ctx, userID, commit) {
		return nil, false
	}
	return node, false
}

func (r *Runtime) committedVersionsMatch(ctx context.Context, userID uint64, commit *NodeResultCommit) bool {
	for i := range commit.Versions {
		expected := &commit.Versions[i]
		actual, err := r.config.Store.GetAssetVersion(ctx, userID, expected.ID)
		if err != nil || actual.ID != expected.ID || actual.AssetID != expected.AssetID || actual.OwnerUserID != expected.OwnerUserID ||
			actual.Status != expected.Status || actual.MIMEType != expected.MIMEType || actual.StorageKey != expected.StorageKey ||
			actual.FilePath != expected.FilePath || actual.SizeBytes != expected.SizeBytes || actual.SHA256 != expected.SHA256 ||
			actual.CreatedByRunID != expected.CreatedByRunID || actual.CreatedByNodeRunID != expected.CreatedByNodeRunID ||
			actual.Width != expected.Width || actual.Height != expected.Height || actual.DurationMS != expected.DurationMS || actual.InputHash != expected.InputHash {
			return false
		}
	}
	return true
}

func (r *Runtime) recordProviderCost(ctx context.Context, nodeRunID string, cost int64) error {
	if cost <= 0 {
		return nil
	}
	costCtx, cancel := boundedDetachedContext(ctx, RuntimeBillingTimeout)
	err := r.config.Store.RecordNodeRunCost(costCtx, nodeRunID, cost)
	cancel()
	if err == nil {
		return nil
	}
	for attempt := 0; attempt < 2; attempt++ {
		reconcileCtx, reconcileCancel := context.WithTimeout(context.Background(), RuntimeBillingTimeout)
		persisted, loadErr := r.config.Store.GetNodeRun(reconcileCtx, nodeRunID)
		reconcileCancel()
		if loadErr != nil {
			return fmt.Errorf("%w: reconcile provider cost for %s: %v", errRecoverableRuntimePersistence, nodeRunID, err)
		}
		if persisted.CreditCost >= cost {
			return nil
		}
		retryCtx, retryCancel := context.WithTimeout(context.Background(), RuntimeBillingTimeout)
		retryErr := r.config.Store.RecordNodeRunCost(retryCtx, nodeRunID, cost)
		retryCancel()
		if retryErr == nil {
			return nil
		}
		err = retryErr
	}
	finalCtx, finalCancel := context.WithTimeout(context.Background(), RuntimeBillingTimeout)
	finalNode, finalErr := r.config.Store.GetNodeRun(finalCtx, nodeRunID)
	finalCancel()
	if finalErr == nil && finalNode.CreditCost >= cost {
		return nil
	}
	return fmt.Errorf("%w: persist provider cost for %s: %v", errRecoverableRuntimePersistence, nodeRunID, err)
}

func validateRawVideoDuration(durationMS int64) error {
	if absInt64(durationMS-SceneDurationMS) > rawVideoDurationToleranceMS {
		return fmt.Errorf("generated video duration %dms, want %dms (+/-%dms)", durationMS, SceneDurationMS, rawVideoDurationToleranceMS)
	}
	return nil
}

func (r *Runtime) upstreamImageData(ctx context.Context, userID uint64, incoming []Edge, outputs map[string]runtimeNodeOutput) ([][]byte, error) {
	var references [][]byte
	for _, edge := range incoming {
		versionID := outputs[edge.Source].VersionID
		if versionID == "" {
			continue
		}
		version, err := r.config.Store.GetAssetVersion(ctx, userID, versionID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return nil, ErrNotFound
			}
			return nil, fmt.Errorf("%w: load upstream image %s: %v", errRecoverableRuntimePersistence, versionID, err)
		}
		if !strings.HasPrefix(version.MIMEType, "image/") {
			return nil, fmt.Errorf("upstream version %s is not an image", versionID)
		}
		data, err := os.ReadFile(resolvedVersionPath(version, r.config.AssetRoot))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, ErrNotFound
			}
			return nil, fmt.Errorf("%w: read upstream image %s: %v", errRecoverableRuntimePersistence, versionID, err)
		}
		references = append(references, data)
	}
	return references, nil
}

func (r *Runtime) upstreamReferenceURLs(ctx context.Context, userID uint64, incoming []Edge, outputs map[string]runtimeNodeOutput) ([]string, error) {
	var urls []string
	hasImageInput := false
	for _, edge := range incoming {
		versionID := outputs[edge.Source].VersionID
		if versionID == "" {
			continue
		}
		version, err := r.config.Store.GetAssetVersion(ctx, userID, versionID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return nil, ErrNotFound
			}
			return nil, fmt.Errorf("%w: load video reference %s: %v", errRecoverableRuntimePersistence, versionID, err)
		}
		if !strings.HasPrefix(version.MIMEType, "image/") {
			return nil, fmt.Errorf("video reference %s is not an image", versionID)
		}
		info, err := os.Stat(resolvedVersionPath(version, r.config.AssetRoot))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, ErrNotFound
			}
			return nil, fmt.Errorf("%w: stat video reference %s: %v", errRecoverableRuntimePersistence, versionID, err)
		}
		if !info.Mode().IsRegular() {
			return nil, ErrNotFound
		}
		hasImageInput = true
		if r.config.MediaSigner == nil || r.config.PublicBaseURL == "" {
			return nil, errors.New("media signer and public base URL are required for image-to-video")
		}
		expires := time.Now().Add(SeedanceSignTTL)
		signature, err := r.config.MediaSigner.Sign(version.ID, MediaPurposeSeedance, expires)
		if err != nil {
			return nil, err
		}
		urls = append(urls, strings.TrimRight(r.config.PublicBaseURL, "/")+SignedMediaPath(version.ID, MediaPurposeSeedance, expires, signature))
	}
	if hasImageInput && len(urls) == 0 {
		return nil, errors.New("video node image references could not be signed")
	}
	return urls, nil
}

func incomingEdges(graph Graph, selected map[string]bool) map[string][]Edge {
	out := make(map[string][]Edge)
	inputOrder := make(map[string]map[string]int, len(graph.Nodes))
	for _, node := range graph.Nodes {
		ports := make(map[string]int, len(node.Inputs))
		for index, port := range node.Inputs {
			ports[port.ID] = index
		}
		inputOrder[node.ID] = ports
	}
	for _, edge := range graph.Edges {
		if selected[edge.Source] && selected[edge.Target] {
			out[edge.Target] = append(out[edge.Target], edge)
		}
	}
	for target := range out {
		edges := out[target]
		ports := inputOrder[target]
		sort.Slice(edges, func(i, j int) bool {
			left, right := edges[i], edges[j]
			leftOrder, leftDeclared := ports[left.TargetPort]
			rightOrder, rightDeclared := ports[right.TargetPort]
			if leftDeclared != rightDeclared {
				return leftDeclared
			}
			if leftDeclared && leftOrder != rightOrder {
				return leftOrder < rightOrder
			}
			if left.TargetPort != right.TargetPort {
				return left.TargetPort < right.TargetPort
			}
			if left.Source != right.Source {
				return left.Source < right.Source
			}
			if left.SourcePort != right.SourcePort {
				return left.SourcePort < right.SourcePort
			}
			return left.ID < right.ID
		})
		out[target] = edges
	}
	return out
}

func buildUpstreamVersionRefs(incoming []Edge, outputs map[string]runtimeNodeOutput) []AssetVersionRef {
	upstream := make([]AssetVersionRef, 0, len(incoming))
	for _, edge := range incoming {
		output := outputs[edge.Source]
		versionID := output.VersionID
		if versionID == "" && len(output.JSON) > 0 {
			sum := sha256.Sum256(output.JSON)
			versionID = "json:" + hex.EncodeToString(sum[:])
		}
		upstream = append(upstream, AssetVersionRef{NodeID: edge.Source, VersionID: versionID})
	}
	return upstream
}

func legacyNodeIDSortedRefs(upstream []AssetVersionRef) []AssetVersionRef {
	legacy := append([]AssetVersionRef(nil), upstream...)
	sort.Slice(legacy, func(i, j int) bool { return legacy[i].NodeID < legacy[j].NodeID })
	return legacy
}

func buildNodePrompt(node Node, incoming []Edge, outputs map[string]runtimeNodeOutput) string {
	var config map[string]any
	_ = json.Unmarshal(node.Config, &config)
	prompt, _ := config["prompt"].(string)
	if prompt == "" {
		prompt = string(node.Config)
	}
	var contextParts []string
	for _, edge := range incoming {
		if raw := outputs[edge.Source].JSON; len(raw) > 0 {
			text := string(raw)
			if len(text) > 8000 {
				text = text[:8000]
			}
			contextParts = append(contextParts, edge.Source+": "+text)
		}
	}
	if len(contextParts) > 0 {
		prompt += "\n\n上游输入：\n" + strings.Join(contextParts, "\n")
	}
	return strings.TrimSpace(prompt)
}

func attachPromptToNodeOutput(raw json.RawMessage, prompt string) json.RawMessage {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return raw
	}
	var object map[string]any
	if json.Unmarshal(raw, &object) == nil && object != nil {
		if _, exists := object["prompt"]; !exists {
			object["prompt"] = prompt
		}
		out, err := json.Marshal(object)
		if err == nil {
			return out
		}
	}
	out, err := json.Marshal(map[string]any{"prompt": prompt, "result": raw})
	if err != nil {
		return raw
	}
	return out
}

func characterApprovalOutput(previous json.RawMessage, selectedVersionID string) json.RawMessage {
	output := map[string]any{
		"selected_version_id":   selectedVersionID,
		"candidate_version_ids": outputVersionIDs(previous),
	}
	var previousObject map[string]any
	if json.Unmarshal(previous, &previousObject) == nil && previousObject != nil {
		if prompt, _ := previousObject["prompt"].(string); strings.TrimSpace(prompt) != "" {
			output["prompt"] = strings.TrimSpace(prompt)
		}
	}
	encoded, _ := json.Marshal(output)
	return encoded
}

func imageSizeForAspect(ratio AspectRatio) string {
	switch ratio {
	case AspectRatioLandscape:
		return "1792x1024"
	case AspectRatioPortrait:
		return "1024x1792"
	default:
		return "1024x1024"
	}
}

func isMediaNode(nodeType NodeType) bool {
	return nodeType == NodeCharacter || nodeType == NodeBackground || nodeType == NodeVideo || nodeType == NodeCompose
}

func isProviderNode(nodeType NodeType) bool {
	return nodeType == NodeCharacter || nodeType == NodeBackground || nodeType == NodeScript || nodeType == NodeVideo
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func absInt64(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}

func (r *Runtime) ensureReservation(ctx context.Context, run *Run) error {
	if r.config.Billing == nil || run.EstimatedCost <= 0 {
		return nil
	}
	key := "video-workflow:" + run.ID
	charge, err := r.config.Store.GetChargeByIdempotencyKey(ctx, key)
	if errors.Is(err, ErrNotFound) {
		charge = &ChargeReservation{ID: NewChargeID(), UserID: run.UserID, RunID: run.ID, IdempotencyKey: key, Amount: run.EstimatedCost, Status: ChargeReserved, BillingRef: "pending"}
		if err := r.config.Store.CreateCharge(ctx, charge); err != nil {
			charge, err = r.config.Store.GetChargeByIdempotencyKey(ctx, key)
			if err != nil {
				return fmt.Errorf("%w: create reservation: %v", ErrReservationPending, err)
			}
		}
	} else if err != nil {
		return fmt.Errorf("%w: load reservation: %v", ErrReservationPending, err)
	}
	if charge.Status == ChargeSettled {
		// 结算成功后、运行终态落库前崩溃时，允许恢复并完成终态更新。
		return nil
	}
	if charge.Status != ChargeReserved {
		return fmt.Errorf("reservation %s is %s", charge.ID, charge.Status)
	}
	if charge.BillingRef == "reserved" {
		return nil
	}
	if strings.HasPrefix(charge.BillingRef, "settling:") {
		settled, ledgerErr := r.config.Store.HasBillingTransaction(ctx, run.UserID, key, "unfreeze", "consume")
		if ledgerErr != nil {
			return fmt.Errorf("%w: inspect settlement ledger: %v", ErrReservationPending, ledgerErr)
		}
		lockFresh := !charge.UpdatedAt.IsZero() && time.Since(charge.UpdatedAt) < RuntimeLeaseDuration
		if !settled && lockFresh {
			return ErrReservationPending
		}
		// 已落账或锁已超过一轮运行租约，恢复为可结算状态；后续 finishRun
		// 会先查账本，因此不会重复调用计费引擎。
		if err := r.config.Store.CompareAndSwapChargeBillingRef(ctx, charge.ID, charge.BillingRef, "reserved"); err != nil {
			return fmt.Errorf("%w: recover settlement lock: %v", ErrReservationPending, err)
		}
		charge.BillingRef = "reserved"
		return nil
	}

	lockRef := "reserving:" + r.config.WorkerID + ":" + NewRunID()
	if strings.HasPrefix(charge.BillingRef, "reserving:") {
		frozen, ledgerErr := r.config.Store.HasBillingTransaction(ctx, run.UserID, key, "freeze")
		if ledgerErr != nil {
			return fmt.Errorf("%w: inspect freeze ledger: %v", ErrReservationPending, ledgerErr)
		}
		if frozen {
			if err := r.config.Store.CompareAndSwapChargeBillingRef(ctx, charge.ID, charge.BillingRef, "reserved"); err != nil {
				return fmt.Errorf("%w: confirm recovered reservation: %v", ErrReservationPending, err)
			}
			return nil
		}
		lockFresh := !charge.UpdatedAt.IsZero() && time.Since(charge.UpdatedAt) < RuntimeBillingLockLease
		if lockFresh {
			return ErrReservationPending
		}
		if err := r.config.Store.CompareAndSwapChargeBillingRef(ctx, charge.ID, charge.BillingRef, lockRef); err != nil {
			return fmt.Errorf("%w: recover reservation lock: %v", ErrReservationPending, err)
		}
	} else {
		from := charge.BillingRef
		if err := r.config.Store.CompareAndSwapChargeBillingRef(ctx, charge.ID, from, lockRef); err != nil {
			return fmt.Errorf("%w: claim reservation: %v", ErrReservationPending, err)
		}
	}

	reserveCtx, cancel := boundedDetachedContext(ctx, RuntimeBillingTimeout)
	defer cancel()
	err = r.config.Store.ReserveRunCredits(reserveCtx, &RunCreditReservation{
		RunID: run.ID, WorkerID: r.config.WorkerID, ExpectedStatus: RunRunning,
		ChargeID: charge.ID, LockRef: lockRef, UserID: run.UserID, Amount: run.EstimatedCost, IdempotencyKey: key,
	})
	if err == nil || errors.Is(err, billing.ErrInsufficient) {
		return err
	}
	current, loadErr := r.config.Store.GetChargeByIdempotencyKey(reserveCtx, key)
	if loadErr == nil && current.Status == ChargeReserved && current.BillingRef == "reserved" {
		return nil
	}
	if loadErr == nil && current.Status == ChargeReserved && current.BillingRef == lockRef {
		_ = r.config.Store.CompareAndSwapChargeBillingRef(reserveCtx, current.ID, lockRef, "pending")
	}
	return fmt.Errorf("%w: reserve credits: %v", ErrReservationPending, err)
}

func (r *Runtime) finishRun(ctx context.Context, run *Run, target RunStatus, errorCode, errorMessage, outputVersionID string) error {
	from := run.Status
	if target == RunCanceled {
		from = RunCancelPending
	}
	finalizeCtx, cancel := boundedDetachedContext(ctx, RuntimeFinalizationLease)
	defer cancel()
	// 任何账本读取或结算前先以未过期的数据库租约做 fencing。失租旧 worker
	// 无法续租，也就无法进入同事务结算。
	if err := r.config.Store.RenewRunFinalizationLease(finalizeCtx, run.ID, r.config.WorkerID, from, RuntimeFinalizationLease); err != nil {
		return err
	}
	actual, err := r.config.Store.FinalizeRun(finalizeCtx, &RunFinalization{
		RunID: run.ID, WorkerID: r.config.WorkerID, ExpectedStatus: from, TargetStatus: target,
		UserID: run.UserID, EstimatedCost: run.EstimatedCost, BillingEnabled: r.config.Billing != nil,
		OutputVersionID: outputVersionID, ErrorCode: errorCode, ErrorMessage: errorMessage,
	})
	if err != nil {
		return err
	}
	run.Status, run.OutputVersionID, run.ActualCost = target, outputVersionID, actual
	return nil
}

func boundedDetachedContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(parent), timeout)
}
