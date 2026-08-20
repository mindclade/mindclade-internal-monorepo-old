// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package leadership

import (
	"context"
	"testing"
	"time"

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
