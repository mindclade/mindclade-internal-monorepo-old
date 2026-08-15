// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package postgres

import (
	"context"
	"testing"
	"time"

	"mindclade.internal/libs/go/storage/sql/sqltest"
)

func TestPoolComponentLifecycle(t *testing.T) {
	state := &sqltest.State{}
	db, err := sqltest.Open(state)
	if err != nil {
		t.Fatal(err)
	}
	pool, err := NewPool(db, PoolConfig{MaxOpenConnections: 4, MaxIdleConnections: 2, PingTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	component := pool.Component("postgres")
	if err := component.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := component.Readiness(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := db.Stats().MaxOpenConnections; got != 4 {
		t.Fatalf("max open=%d", got)
	}
	if state.Pings.Load() < 2 {
		t.Fatalf("pings=%d", state.Pings.Load())
	}
	if err := component.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := component.Readiness(context.Background()); err == nil {
		t.Fatal("closed pool remained ready")
	}
}

func TestPoolCanLeaveExternallyOwnedDatabaseOpen(t *testing.T) {
	state := &sqltest.State{}
	db, err := sqltest.Open(state)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	pool, err := NewPool(db, PoolConfig{}, WithCloseOnStop(false))
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := pool.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := db.PingContext(context.Background()); err != nil {
		t.Fatalf("externally owned db was closed: %v", err)
	}
}
