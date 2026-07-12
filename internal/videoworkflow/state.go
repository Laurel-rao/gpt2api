package videoworkflow

import "fmt"

var runTransitions = map[RunStatus]map[RunStatus]bool{
	RunQueued:                     {RunRunning: true, RunFailed: true, RunCancelPending: true},
	RunRunning:                    {RunAwaitingCharacterApproval: true, RunAwaitingStoryboardApproval: true, RunSucceeded: true, RunFailed: true, RunCancelPending: true},
	RunAwaitingCharacterApproval:  {RunRunning: true, RunFailed: true, RunCancelPending: true},
	RunAwaitingStoryboardApproval: {RunRunning: true, RunFailed: true, RunCancelPending: true},
	RunCancelPending:              {RunCanceled: true, RunFailed: true},
}

var nodeRunTransitions = map[NodeRunStatus]map[NodeRunStatus]bool{
	NodeRunQueued:           {NodeRunRunning: true, NodeRunFailed: true, NodeRunCancelPending: true},
	NodeRunRunning:          {NodeRunAwaitingApproval: true, NodeRunSucceeded: true, NodeRunFailed: true, NodeRunCancelPending: true},
	NodeRunAwaitingApproval: {NodeRunRunning: true, NodeRunFailed: true, NodeRunCancelPending: true},
	NodeRunCancelPending:    {NodeRunCanceled: true, NodeRunFailed: true},
}

var assetTransitions = map[AssetStatus]map[AssetStatus]bool{
	AssetPending: {AssetReady: true, AssetFailed: true, AssetDeleted: true},
	AssetReady:   {AssetDeleted: true},
	AssetFailed:  {AssetPending: true, AssetDeleted: true},
}

var chargeTransitions = map[ChargeStatus]map[ChargeStatus]bool{
	ChargeReserved: {ChargeSettled: true, ChargeRefunded: true},
}

func CanTransitionRun(from, to RunStatus) bool {
	return from == to || runTransitions[from][to]
}

func CanTransitionNodeRun(from, to NodeRunStatus) bool {
	return from == to || nodeRunTransitions[from][to]
}

func CanTransitionAsset(from, to AssetStatus) bool {
	return from == to || assetTransitions[from][to]
}

func CanTransitionCharge(from, to ChargeStatus) bool {
	return from == to || chargeTransitions[from][to]
}

func isApprovalDecisionSuccessor(expected, current RunStatus) bool {
	if current == expected {
		return true
	}
	switch expected {
	case RunAwaitingCharacterApproval:
		switch current {
		case RunRunning, RunAwaitingStoryboardApproval, RunCancelPending, RunCanceled, RunSucceeded, RunFailed:
			return true
		}
	case RunAwaitingStoryboardApproval:
		switch current {
		case RunRunning, RunCancelPending, RunCanceled, RunSucceeded, RunFailed:
			return true
		}
	}
	return false
}

func (r *Run) Transition(to RunStatus) error {
	if !CanTransitionRun(r.Status, to) {
		return fmt.Errorf("%w: run %s -> %s", ErrInvalidState, r.Status, to)
	}
	r.Status = to
	return nil
}

func (n *NodeRun) Transition(to NodeRunStatus) error {
	if !CanTransitionNodeRun(n.Status, to) {
		return fmt.Errorf("%w: node run %s -> %s", ErrInvalidState, n.Status, to)
	}
	n.Status = to
	return nil
}

func (a *Asset) Transition(to AssetStatus) error {
	if !CanTransitionAsset(a.Status, to) {
		return fmt.Errorf("%w: asset %s -> %s", ErrInvalidState, a.Status, to)
	}
	a.Status = to
	return nil
}

func (c *ChargeReservation) Transition(to ChargeStatus) error {
	if !CanTransitionCharge(c.Status, to) {
		return fmt.Errorf("%w: charge %s -> %s", ErrInvalidState, c.Status, to)
	}
	c.Status = to
	return nil
}
