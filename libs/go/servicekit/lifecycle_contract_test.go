// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package servicekit

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

// allStates is the whole phase set, in order.
var allStates = []State{
	StateNew, StateStarting, StateRunning, StateDraining, StateStopping, StateStopped, StateFailed,
}

// The phase graph is a cross-language contract, so it is pinned edge by edge
// rather than spot-checked. libs/rust/servicekit/tests/lifecycle.rs pins the
// identical matrix against the Rust runtime; a change made on one side and not
// the other fails one of the two.
func TestPhaseGraphIsExhaustivelyPinned(t *testing.T) {
	t.Parallel()

	legal := map[State]map[State]bool{
		StateNew:      {StateStarting: true, StateFailed: true},
		StateStarting: {StateRunning: true, StateDraining: true, StateStopping: true, StateFailed: true},
		StateRunning:  {StateDraining: true, StateStopping: true, StateFailed: true},
		StateDraining: {StateStopping: true, StateFailed: true},
		StateStopping: {StateStopped: true, StateFailed: true},
		StateStopped:  {},
		StateFailed:   {},
	}

	for _, from := range allStates {
		for _, to := range allStates {
			want := legal[from][to]
			if got := from.CanTransitionTo(to); got != want {
				t.Errorf("%s.CanTransitionTo(%s) = %v, want %v", from, to, got, want)
			}
		}
		if from.Terminal() && len(legal[from]) != 0 {
			t.Errorf("%s is terminal but the pinned graph gives it successors", from)
		}
	}

	// An unknown phase — a corrupt report, or one from a newer peer — has no
	// successors, so it can never be treated as a phase this process models.
	for _, to := range allStates {
		if State(255).CanTransitionTo(to) {
			t.Errorf("unknown state must not transition to %s", to)
		}
	}
}

// Every transition Run actually performs has to be an edge of the graph above.
// This is the half that catches drift in the coordinator rather than in the
// table: a new shortcut through the phases fails here even if nobody updates
// the table.
func TestRunPerformsOnlyLegalTransitions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		build        func(t *testing.T, observer Observer) (*Service, context.Context)
		afterRunning func(t *testing.T, service *Service)
		want         []string
	}{
		{
			name: "graceful shutdown",
			build: func(t *testing.T, observer Observer) (*Service, context.Context) {
				t.Helper()
				service := newContractService(t, observer)
				addRunComponent(t, service, "server", nil)
				return service, context.Background()
			},
			afterRunning: func(t *testing.T, service *Service) {
				t.Helper()
				waitForState(t, service, StateRunning)
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := service.Shutdown(shutdownCtx); err != nil {
					t.Errorf("Shutdown returned %v", err)
				}
			},
			want: []string{
				"new->starting", "starting->running", "running->draining",
				"draining->stopping", "stopping->stopped",
			},
		},
		{
			name: "startup failure never reaches running",
			build: func(t *testing.T, observer Observer) (*Service, context.Context) {
				t.Helper()
				service := newContractService(t, observer)
				if err := service.Add(Component{
					Name:  "database",
					Start: func(context.Context) error { return errors.New("dial failed") },
				}); err != nil {
					t.Fatalf("Add returned %v", err)
				}
				return service, context.Background()
			},
			want: []string{"new->starting", "starting->stopping", "stopping->failed"},
		},
		{
			name: "run failure skips drain",
			build: func(t *testing.T, observer Observer) (*Service, context.Context) {
				t.Helper()
				service := newContractService(t, observer)
				addRunComponent(t, service, "server", errors.New("listener closed"))
				return service, context.Background()
			},
			want: []string{"new->starting", "starting->running", "running->stopping", "stopping->failed"},
		},
		{
			name: "termination before running drains without announcing ready",
			build: func(t *testing.T, observer Observer) (*Service, context.Context) {
				t.Helper()
				service := newContractService(t, observer)
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return service, ctx
			},
			want: []string{"new->starting", "starting->draining", "draining->stopping", "stopping->stopped"},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var mu sync.Mutex
			var observed []string
			var illegal []string
			observer := ObserverFunc(func(event Event) {
				if event.Kind != EventStateChanged {
					return
				}
				edge := event.From.String() + "->" + event.To.String()
				mu.Lock()
				observed = append(observed, edge)
				if !event.From.CanTransitionTo(event.To) {
					illegal = append(illegal, edge)
				}
				mu.Unlock()
			})

			service, ctx := test.build(t, observer)
			result := make(chan error, 1)
			go func() { result <- service.Run(ctx) }()
			if test.afterRunning != nil {
				test.afterRunning(t, service)
			}
			select {
			case <-result:
			case <-time.After(10 * time.Second):
				t.Fatal("Run did not return")
			}

			mu.Lock()
			gotObserved := append([]string(nil), observed...)
			gotIllegal := append([]string(nil), illegal...)
			mu.Unlock()
			if len(gotIllegal) != 0 {
				t.Errorf("Run performed transitions the phase graph forbids: %v", gotIllegal)
			}
			if !reflect.DeepEqual(gotObserved, test.want) {
				t.Fatalf("transitions = %v, want %v", gotObserved, test.want)
			}
		})
	}
}

// Probe answers are a function of the phase, and orchestration routes on them.
// Ready in exactly one phase is what keeps traffic away from a service that is
// draining; live for the whole of shutdown is what keeps the orchestrator from
// killing a service that is shutting down cleanly.
func TestProbeSemanticsPerPhase(t *testing.T) {
	t.Parallel()

	tests := []struct {
		state State
		live  bool
		ready bool
	}{
		{StateNew, false, false},
		{StateStarting, true, false},
		{StateRunning, true, true},
		{StateDraining, true, false},
		{StateStopping, true, false},
		{StateStopped, false, false},
		{StateFailed, false, false},
	}

	for _, test := range tests {
		service, err := New("api")
		if err != nil {
			t.Fatalf("New returned %v", err)
		}
		service.transition(test.state, nil)
		if got := service.Liveness(context.Background()).OK; got != test.live {
			t.Errorf("liveness in %s = %v, want %v", test.state, got, test.live)
		}
		if got := service.Readiness(context.Background()).OK; got != test.ready {
			t.Errorf("readiness in %s = %v, want %v", test.state, got, test.ready)
		}
	}
}

// A shutdown requested before Run must not be silently discarded.
//
// Shutdown used to return nil whenever Run had not started yet, which loses the
// request: a supervisor that decides to stop a process while its composition
// root is still wiring providers gets a successful Shutdown and a service that
// then runs forever. The request is recorded instead, and the Run that follows
// drains straight from starting — never announcing running, so an orchestrator
// is never told a service that is already stopping is ready.
func TestShutdownBeforeRunLatchesTheRequest(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var observed []string
	service, err := New(
		"api",
		WithShutdownTimeout(time.Second),
		WithObserver(ObserverFunc(func(event Event) {
			if event.Kind != EventStateChanged {
				return
			}
			mu.Lock()
			observed = append(observed, event.From.String()+"->"+event.To.String())
			mu.Unlock()
		})),
	)
	if err != nil {
		t.Fatalf("New returned %v", err)
	}
	if err := service.Add(Component{
		Name: "server",
		Start: func(context.Context) error {
			return nil
		},
		Run: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}); err != nil {
		t.Fatalf("Add returned %v", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := service.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown before Run returned %v", err)
	}

	result := make(chan error, 1)
	go func() { result <- service.Run(context.Background()) }()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("Run after latched Shutdown returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run ignored the shutdown request latched before it started")
	}
	if snapshot := service.Snapshot(); snapshot.State != StateStopped {
		t.Fatalf("final state = %s, want stopped", snapshot.State)
	}
	if service.Readiness(context.Background()).OK {
		t.Error("a service asked to stop before it ran must never report ready")
	}

	mu.Lock()
	got := append([]string(nil), observed...)
	mu.Unlock()
	want := []string{"new->starting", "starting->draining", "draining->stopping", "stopping->stopped"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("transitions = %v, want %v", got, want)
	}
}

func newContractService(t *testing.T, observer Observer) *Service {
	t.Helper()
	service, err := New(
		"contract",
		WithStartupTimeout(5*time.Second),
		WithShutdownTimeout(5*time.Second),
		WithComponentDrainTimeout(time.Second),
		WithComponentStopTimeout(time.Second),
		WithObserver(observer),
	)
	if err != nil {
		t.Fatalf("New returned %v", err)
	}
	return service
}

func addRunComponent(t *testing.T, service *Service, name string, runErr error) {
	t.Helper()
	if err := service.Add(Component{
		Name: name,
		Run: func(ctx context.Context) error {
			if runErr != nil {
				return runErr
			}
			<-ctx.Done()
			return ctx.Err()
		},
	}); err != nil {
		t.Fatalf("Add(%s) returned %v", name, err)
	}
}

func waitForState(t *testing.T, service *Service, want State) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if service.Snapshot().State == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("service did not reach %s, current state %s", want, service.Snapshot().State)
}
