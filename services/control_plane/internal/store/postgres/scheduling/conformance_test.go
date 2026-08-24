// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package schedulingpostgres

import (
	"context"
	"fmt"
	"testing"

	"go.mindclade.dev/control/scheduling"
	"go.mindclade.dev/control/scheduling/schedulingtest"
)

// The compile-time form: the compiler is the check, and this line fails the
// build the moment the store stops satisfying either the durable seam or the
// fleet-configuration surface the shared suite configures a fleet through.
var _ schedulingtest.Fleet = (*Store)(nil)

// TestConformance runs the shared suite against live PostgreSQL.
//
// store_test.go proves transaction shape against a fake driver and
// live_postgres_test.go proves this schema's own SQL. Neither can prove the
// property that makes swapping the adapter safe: that this store and
// scheduling.MemoryRepository answer the same questions with the same fault
// codes and the same reason strings. Only one suite run against both can, so
// this and TestMemoryRepositoryConformance in control/scheduling are the same
// assertions executed twice.
//
// It is opt-in through MINDCLADE_TEST_POSTGRES_DSN, and skips through the same
// helper every other live case here uses.
func TestConformance(t *testing.T) {
	// Taken on the parent, before any case runs, so a missing DSN skips the
	// whole suite cleanly rather than from inside a subtest.
	live := newLiveSchedulingStore(t)
	schedulingtest.Conformance(t, func(tb testing.TB) scheduling.Repository {
		tb.Helper()
		live.reset(tb)
		return live.store
	})
}

// reset returns the schema to the state DDL leaves it in.
//
// The suite requires a repository holding nothing per case -- it asserts on an
// epoch that only advances, on a store-wide fence floor that only rises, and on
// a snapshot naming exactly the domains someone recorded. A schema per case
// would say the same thing and pay a CREATE SCHEMA and a full DDL apply for it;
// truncating the four tables and re-seeding the singleton ledger row is the
// same starting state, and the assertion below is what makes that claim
// checkable rather than assumed.
func (live liveSchedulingStore) reset(tb testing.TB) {
	tb.Helper()
	ctx := context.Background()
	if _, err := live.db.ExecContext(ctx, fmt.Sprintf("TRUNCATE %s, %s, %s, %s, %s, %s",
		live.reservationTable, live.quotaTable, live.weightTable, live.ledgerTable,
		live.auditTable, live.outboxTable)); err != nil {
		tb.Fatalf("truncate the scheduling schema: %v", err)
	}
	// The ledger row is not data, it is the schema's initialization: a store
	// whose ledger row is missing cannot mint an epoch at all.
	if _, err := live.db.ExecContext(ctx, fmt.Sprintf(
		"INSERT INTO %s (singleton, fence, epoch, updated_at) VALUES (true, 0, 1, now())",
		live.ledgerTable)); err != nil {
		tb.Fatalf("re-seed the scheduling ledger row: %v", err)
	}
	var fence, epoch int64
	if err := live.db.QueryRowContext(ctx, fmt.Sprintf(
		"SELECT fence, epoch FROM %s WHERE singleton", live.ledgerTable)).Scan(&fence, &epoch); err != nil {
		tb.Fatalf("read the re-seeded scheduling ledger row: %v", err)
	}
	if fence != 0 || epoch != 1 {
		tb.Fatalf("the reset ledger row is fence=%d epoch=%d, want 0 and 1", fence, epoch)
	}
}
