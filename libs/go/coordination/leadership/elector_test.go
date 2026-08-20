// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package leadership

import (
	"context"
	"errors"
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
