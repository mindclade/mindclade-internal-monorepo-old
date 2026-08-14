package ingestion

import "testing"

func TestStateMachine(t *testing.T) {
	if !CanTransition(StatePending, StateRunning) || !CanTransition(StateRunning, StateCompleted) || CanTransition(StateCompleted, StateRunning) {
		t.Fatal("invalid ingestion state transitions")
	}
}
