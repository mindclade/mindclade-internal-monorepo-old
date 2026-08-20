// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package postgres

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"
	"time"

	"go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/idempotency"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/storage/sql/sqltest"
)

func TestAcquireInsertsNewRecord(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	state := &sqltest.State{Exec: func(_ context.Context, query string, arguments []driver.NamedValue) (driver.Result, error) {
		if !strings.Contains(query, "ON CONFLICT (identity_digest) DO NOTHING") || len(arguments) != 11 {
			t.Fatalf("query=%q args=%d", query, len(arguments))
		}
		return driver.RowsAffected(1), nil
	}}
	db, err := sqltest.Open(state)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	fake := clock.NewFake(now)
	store, err := New(db, WithClock(fake))
	if err != nil {
		t.Fatal(err)
	}
	scope := idempotency.MustParseScope("org.test/runs.create")
	key := idempotency.MustParseKey("request-00000001")
	identity, err := idempotency.NewIdentity(scope, key)
	if err != nil {
		t.Fatal(err)
	}
	acquisition, err := store.Acquire(context.Background(), idempotency.AcquireRequest{
		Identity: identity, Fingerprint: identifiers.SHA256([]byte("request")), TTL: time.Hour, LeaseDuration: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if acquisition.Disposition != idempotency.DispositionAcquired || acquisition.Record.Version() != 1 || acquisition.Lease.IsZero() {
		t.Fatalf("acquisition=%+v", acquisition)
	}
	if state.Begins.Load() != 1 || state.Commits.Load() != 1 {
		t.Fatalf("begins=%d commits=%d", state.Begins.Load(), state.Commits.Load())
	}
}
