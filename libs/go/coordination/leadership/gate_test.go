// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package leadership

import (
	"context"
	"testing"

	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/servicekit"
)

// GateComponents is what makes one elector own several run loops. Each
// component must come back unable to start on its own, and the handler must
// start every one of them.
func TestGateComponentsRunsEveryComponentAndStripsItsRunLoop(t *testing.T) {
	started := make(chan string, 2)
	component := func(name string) servicekit.Component {
		return servicekit.Component{Name: name, Run: func(context.Context) error {
			started <- name
			return nil
		}}
	}
	handler, gated, err := GateComponents("test-leader", component("first"), component("second"))
	if err != nil {
		t.Fatalf("GateComponents: %v", err)
	}
	if len(gated) != 2 {
		t.Fatalf("gated %d components, want two", len(gated))
	}
	for _, value := range gated {
		if value.Run != nil {
			t.Fatalf("component %q kept an independent run loop", value.Name)
		}
	}
	// Both components return nil while leadership is still held, which the
	// handler reports as leader work that stopped: a leader loop that finishes
	// on its own has stopped doing the thing the lease serializes, and calling
	// that graceful completion would leave the process holding a lease and
	// reconciling nothing.
	if err := handler(context.Background(), Session{}); !faults.IsReason(err, "leader_work_stopped") {
		t.Fatalf("leader handler = %q, want leader_work_stopped", faults.ReasonOf(err))
	}
	close(started)
	names := map[string]bool{}
	for name := range started {
		names[name] = true
	}
	if !names["first"] || !names["second"] {
		t.Fatalf("leader handler started %v, want both components", names)
	}
}

// The group fails as a unit. A manager that stopped while a worker kept
// reconciling would be reconciling against a cache nothing refreshes, and the
// honest answer is to surrender the lease rather than half-run the role.
func TestGateComponentsFailAsAUnit(t *testing.T) {
	failure := faults.New(faults.CodeUnavailable, "manager stopped", faults.WithReason("manager_stopped"))
	cancelled := make(chan struct{})
	handler, _, err := GateComponents("test-leader",
		servicekit.Component{Name: "failing", Run: func(context.Context) error { return failure }},
		servicekit.Component{Name: "waiting", Run: func(ctx context.Context) error {
			<-ctx.Done()
			close(cancelled)
			return ctx.Err()
		}},
	)
	if err != nil {
		t.Fatalf("GateComponents: %v", err)
	}
	if err := handler(context.Background(), Session{}); !faults.IsReason(err, "manager_stopped") {
		t.Fatalf("leader handler = %q, want the failing component's reason", faults.ReasonOf(err))
	}
	select {
	case <-cancelled:
	default:
		t.Fatal("the surviving component was not cancelled with the group")
	}
}

// Per-component validation is GateComponent's, so the plural gate refuses the
// same components with the same reason. This pins that: the two gates cannot
// disagree about what is gateable, which is the drift that having a second,
// service-local implementation of this loop invited.
func TestGateComponentsRefuseAComponentTheyCannotGate(t *testing.T) {
	runnable := servicekit.Component{Name: "runnable", Run: func(context.Context) error { return nil }}
	if _, _, err := GateComponents("", runnable); !faults.IsReason(err, "invalid_leader_work_group") {
		t.Fatalf("unnamed group = %q, want invalid_leader_work_group", faults.ReasonOf(err))
	}
	if _, _, err := GateComponents(" untrimmed ", runnable); !faults.IsReason(err, "invalid_leader_work_group") {
		t.Fatalf("untrimmed group name = %q, want invalid_leader_work_group", faults.ReasonOf(err))
	}
	if _, _, err := GateComponents("test-leader"); !faults.IsReason(err, "invalid_leader_work_group") {
		t.Fatalf("empty group = %q, want invalid_leader_work_group", faults.ReasonOf(err))
	}
	unrunnable := servicekit.Component{Name: "no-run"}
	if _, _, err := GateComponents("test-leader", unrunnable); !faults.IsReason(err, "invalid_leader_managed_component") {
		t.Fatalf("component with no run = %q, want invalid_leader_managed_component", faults.ReasonOf(err))
	}
	if _, _, err := GateComponent(unrunnable); !faults.IsReason(err, "invalid_leader_managed_component") {
		t.Fatalf("singular gate = %q, want the same reason as the plural one", faults.ReasonOf(err))
	}
}

// The gated components must reach their aggregates under their own names.
// Every gated component has the same type, so a positional result let a caller
// swap two arguments and register one under the other's key -- which compiles,
// and which a standby-run-loop test cannot catch because both are stripped of
// Run either way.
func TestGateComponentsKeyComponentsByName(t *testing.T) {
	component := func(name string) servicekit.Component {
		return servicekit.Component{Name: name, Run: func(context.Context) error { return nil }}
	}
	_, gated, err := GateComponents("test-leader", component("first"), component("second"))
	if err != nil {
		t.Fatalf("GateComponents: %v", err)
	}
	for _, name := range []string{"first", "second"} {
		value, found := gated[name]
		if !found {
			t.Fatalf("component %q is missing from the gated set", name)
		}
		if value.Name != name {
			t.Fatalf("key %q holds component %q", name, value.Name)
		}
	}
}

// Two components under one name would have collided at the first leadership
// acquisition, inside servicekit.TaskGroup.Add, where the failure is a lost
// lease rather than a refused composition.
func TestGateComponentsRefuseADuplicateComponentName(t *testing.T) {
	component := servicekit.Component{Name: "twice", Run: func(context.Context) error { return nil }}
	if _, _, err := GateComponents("test-leader", component, component); !faults.IsReason(err, "duplicate_leader_managed_component") {
		t.Fatalf("duplicate name = %q, want duplicate_leader_managed_component", faults.ReasonOf(err))
	}
}

// A gated group under a cancelled lease reports graceful completion, not a
// fault: the components stopped because leadership ended, which is the one way
// leader work is allowed to finish.
func TestGateComponentsReportNoFaultWhenLeadershipEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	handler, _, err := GateComponents("test-leader",
		servicekit.Component{Name: "waiting", Run: func(runCtx context.Context) error {
			<-runCtx.Done()
			return runCtx.Err()
		}},
	)
	if err != nil {
		t.Fatalf("GateComponents: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- handler(ctx, Session{}) }()
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("leader handler = %v, want nil when leadership ended", err)
	}
}
