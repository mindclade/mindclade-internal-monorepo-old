// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package leadership

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"go.mindclade.dev/libs/go/servicekit"
	"go.mindclade.dev/libs/go/storage/lease"
	leasememory "go.mindclade.dev/libs/go/storage/lease/memory"
)

func TestElectorAcquiresAndStopsHandler(t *testing.T) {
	store, err := leasememory.New()
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan Session, 1)
	stopped := make(chan struct{})
	handler := func(ctx context.Context, session Session) error {
		started <- session
		<-ctx.Done()
		close(stopped)
		return nil
	}
	elector, err := New(store, Config{Key: lease.MustParseKey("scheduler/global"), Owner: "scheduler-1", TTL: 300 * time.Millisecond, RenewInterval: 50 * time.Millisecond, AcquireInterval: 10 * time.Millisecond}, handler)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- elector.Run(ctx) }()
	select {
	case session := <-started:
		if session.Fence() == 0 {
			t.Fatal("zero fence")
		}
	case <-time.After(time.Second):
		t.Fatal("handler did not start")
	}
	if !elector.IsLeader() {
		t.Fatal("elector not leader")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("elector did not stop")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("handler did not stop")
	}
}

// TestShutdownReleasesLeaseOnlyAfterHandlerStops pins two things graceful
// shutdown has to get right, both of which were wrong.
//
// Ordering: releasing the lease publishes "this singleton is free" to every
// standby, so it must not happen while the outgoing leader's handler is still
// running. hold used to return as soon as the parent context was canceled,
// without waiting for the handler, and Run released the lease immediately -- a
// standby could acquire it and start the same singleton work alongside a leader
// that had not finished writing.
//
// Effectiveness: the release has to name the epoch the process actually holds.
// Run released the acquire-time lease, but every renewal bumps Lease.Version
// and both adapters delete on an exact version match, so once a process had led
// for longer than one RenewInterval the release failed with ErrStale -- an
// error release swallows -- and standbys waited out the whole TTL instead. The
// test therefore leads across at least one renewal before shutting down.
func TestShutdownReleasesLeaseOnlyAfterHandlerStops(t *testing.T) {
	inner, err := leasememory.New()
	if err != nil {
		t.Fatal(err)
	}
	var handlerRunning, releasedWhileRunning atomic.Bool
	store := &releaseWatchingStore{Store: inner, running: &handlerRunning, violated: &releasedWhileRunning}

	key := lease.MustParseKey("projector/global")
	started := make(chan struct{})
	handler := func(ctx context.Context, _ Session) error {
		handlerRunning.Store(true)
		close(started)
		<-ctx.Done()
		// A real leader-owned component does not stop instantly: it drains
		// in-flight work, flushes, and closes connections after cancellation.
		time.Sleep(150 * time.Millisecond)
		handlerRunning.Store(false)
		return nil
	}
	renewed := make(chan struct{}, 1)
	elector, err := New(store, Config{
		Key:             key,
		Owner:           "projector-1",
		TTL:             2 * time.Second,
		RenewInterval:   20 * time.Millisecond,
		AcquireInterval: 10 * time.Millisecond,
	}, handler, WithObserver(ObserverFunc(func(event Event) {
		if event.Kind == EventRenewed {
			select {
			case renewed <- struct{}{}:
			default:
			}
		}
	})))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- elector.Run(ctx) }()
	defer cancel()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not start")
	}
	// Lead past a renewal so the held epoch differs from the acquired one.
	select {
	case <-renewed:
	case <-time.After(2 * time.Second):
		t.Fatal("leadership was never renewed")
	}

	cancel()
	select {
	case runErr := <-done:
		if runErr != nil {
			t.Fatalf("Run() error = %v, want nil", runErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("elector did not stop")
	}
	if handlerRunning.Load() {
		t.Fatal("Run() returned while the leader handler was still running")
	}
	if releasedWhileRunning.Load() {
		t.Fatal("the leadership lease was released while the leader handler was still running")
	}
	if _, err := inner.Lookup(context.Background(), key); !errors.Is(err, lease.ErrNotFound) {
		t.Fatalf("Lookup() after shutdown = %v, want ErrNotFound: the lease was never actually released, so standbys wait out the full TTL", err)
	}
}

type releaseWatchingStore struct {
	lease.Store
	running  *atomic.Bool
	violated *atomic.Bool
}

func (store *releaseWatchingStore) Release(ctx context.Context, value lease.Lease) error {
	if store.running.Load() {
		store.violated.Store(true)
	}
	return store.Store.Release(ctx, value)
}

func TestReadinessCanRequireLeadership(t *testing.T) {
	store, err := leasememory.New()
	if err != nil {
		t.Fatal(err)
	}
	elector, err := New(store, Config{Key: lease.MustParseKey("registry/maintenance"), Owner: "registry-1", RequireLeaderReadiness: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	component := elector.Component("leadership")
	if err := component.Readiness(context.Background()); err == nil {
		t.Fatal("readiness passed without leadership")
	}
}

func TestExitOnLeadershipLossIsFailStop(t *testing.T) {
	store, err := leasememory.New()
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan Session, 1)
	handler := func(ctx context.Context, session Session) error {
		started <- session
		<-ctx.Done()
		return ctx.Err()
	}
	elector, err := New(store, Config{
		Key:                    lease.MustParseKey("controller/global"),
		Owner:                  "controller-1",
		TTL:                    300 * time.Millisecond,
		RenewInterval:          20 * time.Millisecond,
		AcquireInterval:        10 * time.Millisecond,
		ExitOnLeadershipLoss:   true,
		RequireLeaderReadiness: true,
	}, handler)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- elector.Run(context.Background()) }()

	var session Session
	select {
	case session = <-started:
	case <-time.After(time.Second):
		t.Fatal("handler did not start")
	}
	if err := store.Release(context.Background(), session.Lease); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, ErrLeadershipLost) {
			t.Fatalf("Run() error = %v, want leadership loss", err)
		}
	case <-time.After(time.Second):
		t.Fatal("elector attempted to reacquire after leadership loss")
	}
}

func TestGateComponentMovesRunUnderLeadership(t *testing.T) {
	started := make(chan struct{})
	component := servicekit.Component{
		Name: "singleton-worker",
		Run: func(ctx context.Context) error {
			close(started)
			<-ctx.Done()
			return nil
		},
		Readiness: func(context.Context) error { return nil },
	}
	handler, gated, err := GateComponent(component)
	if err != nil {
		t.Fatal(err)
	}
	if gated.Run != nil {
		t.Fatal("gated component retained an independent run loop")
	}
	if gated.Readiness == nil {
		t.Fatal("gated component lost its readiness probe")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- handler(ctx, Session{}) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("leadership handler did not start the component")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("leadership handler did not stop the component")
	}
}
