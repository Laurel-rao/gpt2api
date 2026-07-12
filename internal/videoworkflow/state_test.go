package videoworkflow

import (
	"errors"
	"testing"
)

func TestRunState_HappyPath(t *testing.T) {
	run := Run{Status: RunQueued}
	path := []RunStatus{RunRunning, RunAwaitingCharacterApproval, RunRunning, RunAwaitingStoryboardApproval, RunRunning, RunSucceeded}
	for _, next := range path {
		if err := run.Transition(next); err != nil {
			t.Fatalf("transition to %s: %v", next, err)
		}
	}
	if err := run.Transition(RunRunning); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("terminal transition error = %v", err)
	}
}

func TestRunState_CancelPending(t *testing.T) {
	for _, start := range []RunStatus{RunQueued, RunRunning, RunAwaitingCharacterApproval, RunAwaitingStoryboardApproval} {
		run := Run{Status: start}
		if err := run.Transition(RunCancelPending); err != nil {
			t.Fatalf("%s -> cancel_pending: %v", start, err)
		}
		if err := run.Transition(RunCanceled); err != nil {
			t.Fatalf("cancel_pending -> canceled: %v", err)
		}
	}
}

func TestNodeAssetAndChargeState(t *testing.T) {
	node := NodeRun{Status: NodeRunQueued}
	for _, next := range []NodeRunStatus{NodeRunRunning, NodeRunAwaitingApproval, NodeRunRunning, NodeRunSucceeded} {
		if err := node.Transition(next); err != nil {
			t.Fatal(err)
		}
	}
	asset := Asset{Status: AssetPending}
	if err := asset.Transition(AssetReady); err != nil {
		t.Fatal(err)
	}
	if err := asset.Transition(AssetPending); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("asset invalid transition error = %v", err)
	}
	charge := ChargeReservation{Status: ChargeReserved}
	if err := charge.Transition(ChargeSettled); err != nil {
		t.Fatal(err)
	}
	if err := charge.Transition(ChargeRefunded); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("charge invalid transition error = %v", err)
	}
}
