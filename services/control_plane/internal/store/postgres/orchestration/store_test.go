// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package orchestrationpostgres

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"go.mindclade.dev/control/orchestration"
	"go.mindclade.dev/libs/go/audit"
	"go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/coordination/outbox"
	outboxmemory "go.mindclade.dev/libs/go/coordination/outbox/memory"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/retry"
	"go.mindclade.dev/libs/go/storage/sql/sqltest"
	"go.mindclade.dev/libs/go/storage/sql/transaction"
)

var testStart = time.Date(2026, time.August, 23, 6, 0, 0, 0, time.UTC)

// The fake driver is what lets these tests assert transaction shape without a
// database. It cannot check SQL semantics, so the live suite covers those; what
// it proves here is the part a live database would happily let through --
// whether the store opens one transaction, commits once, and refuses to nest.
func newHarness(t *testing.T, options ...Option) (*Store, *sqltest.State) {
	t.Helper()
	state := &sqltest.State{}
	state.Exec = func(context.Context, string, []driver.NamedValue) (driver.Result, error) {
		return driver.RowsAffected(1), nil
	}
	state.Query = func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
		return sqltest.NewRows([]string{"document"}), nil
	}
	database, err := sqltest.Open(state)
	if err != nil {
		t.Fatalf("open fake database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	messages, err := outboxmemory.New()
	if err != nil {
		t.Fatalf("outbox: %v", err)
	}
	// A real clock, deliberately. The store's retry executor sleeps between
	// serialization retries, and a fake clock that nothing advances makes that
	// sleep block forever -- the test would hang rather than fail, which is the
	// worst way for an assertion to be wrong. Backoff is collapsed to nothing so
	// the retry path stays fast without becoming untested.
	policy, err := retry.NewPolicy(
		retry.WithMaxAttempts(2),
		retry.WithBackoff(retry.ImmediateBackoff()),
		retry.WithoutJitter(),
	)
	if err != nil {
		t.Fatalf("retry policy: %v", err)
	}
	retries, err := retry.NewExecutor(policy)
	if err != nil {
		t.Fatalf("retry executor: %v", err)
	}
	store, err := New(database, audit.NopRecorder{}, messages,
		append([]Option{WithClock(clock.RealClock{}), WithRetry(retries)}, options...)...)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	return store, state
}

// serveStage makes the locking read return this record, so a transition has a
// current row to move. Without it the fake driver answers every read with no
// rows and TransitionStage stops at stage_not_found, short of the code under
// test.
func serveStage(t *testing.T, state *sqltest.State, record orchestration.StageRecord) {
	t.Helper()
	document, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal stage: %v", err)
	}
	state.Query = func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
		return sqltest.NewRows([]string{"document"}, []driver.Value{document}), nil
	}
}

func testID(t *testing.T, kind string) string {
	t.Helper()
	id, err := identifiers.NewID(identifiers.MustParseKind(kind))
	if err != nil {
		t.Fatalf("new %s id: %v", kind, err)
	}
	return id.String()
}

// newStageRecord builds a sealed stage. StageRecord.Validate requires the
// version to seal the content, so a hand-built literal is not a valid record.
func newStageRecord(t *testing.T) orchestration.StageRecord {
	t.Helper()
	record, err := orchestration.NewStage(testID(t, "run"), testID(t, "job"), testID(t, "stage"), testStart)
	if err != nil {
		t.Fatalf("new stage: %v", err)
	}
	return record
}

// A mutation must own exactly one transaction. Two would mean a partial commit
// is reachable: the domain write could land while the audit record and the
// outbox message did not, which is the failure the single-transaction rule
// exists to prevent.
func TestMutationOpensExactlyOneTransaction(t *testing.T) {
	store, state := newHarness(t)
	record := newStageRecord(t)
	if _, _, err := store.PutStage(context.Background(), record); err != nil {
		t.Fatalf("PutStage: %v", err)
	}
	if begins := state.Begins.Load(); begins != 1 {
		t.Fatalf("begins = %d, want exactly 1", begins)
	}
	if commits := state.Commits.Load(); commits != 1 {
		t.Fatalf("commits = %d, want exactly 1", commits)
	}
	if rollbacks := state.Rollbacks.Load(); rollbacks != 0 {
		t.Fatalf("rollbacks = %d, want 0", rollbacks)
	}
}

// A failed write must leave nothing behind. Committing a domain row whose audit
// record or outbox message failed is precisely the split this store is built to
// make impossible.
func TestAFailedWriteRollsBack(t *testing.T) {
	store, state := newHarness(t)
	state.Exec = func(context.Context, string, []driver.NamedValue) (driver.Result, error) {
		return nil, errors.New("write failed")
	}
	record := newStageRecord(t)
	if _, _, err := store.PutStage(context.Background(), record); err == nil {
		t.Fatal("a failing write must surface as an error")
	}
	if state.Commits.Load() != 0 {
		t.Fatal("a failed write must not commit")
	}
	if state.Rollbacks.Load() == 0 {
		t.Fatal("a failed write must roll back")
	}
}

// The store owns its serializable transaction, so joining a caller's is refused
// rather than silently downgraded to whatever isolation the outer one chose.
func TestNestedTransactionIsRefused(t *testing.T) {
	store, _ := newHarness(t)
	state := &sqltest.State{}
	outer, err := sqltest.Open(state)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = outer.Close() }()
	tx, err := outer.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	ctx, err := transaction.ContextWithTx(context.Background(), tx)
	if err != nil {
		t.Fatalf("context: %v", err)
	}
	record := newStageRecord(t)
	_, _, err = store.PutStage(ctx, record)
	if err == nil {
		t.Fatal("a nested transaction must be refused")
	}
	if !faults.IsCode(err, faults.CodeFailedPrecondition) {
		t.Fatalf("code = %s, want failed_precondition", faults.CodeOf(err))
	}
}

func TestGetStagesRejectsAnOversizedLookup(t *testing.T) {
	store, _ := newHarness(t)
	ids := make([]string, MaximumStageFetch+1)
	for index := range ids {
		ids[index] = "stage_00000000000000000000000000000000"
	}
	_, err := store.GetStages(context.Background(), testID(t, "run"), ids)
	if err == nil {
		t.Fatal("an oversized lookup must be refused")
	}
	if !faults.IsCode(err, faults.CodeResourceExhausted) {
		t.Fatalf("code = %s, want resource_exhausted", faults.CodeOf(err))
	}
}

// An empty list must not become `IN ()`, which is a syntax error rather than an
// empty result.
func TestGetStagesShortCircuitsAnEmptyList(t *testing.T) {
	store, state := newHarness(t)
	records, err := store.GetStages(context.Background(), testID(t, "run"), nil)
	if err != nil {
		t.Fatalf("GetStages: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("records = %d, want 0", len(records))
	}
	if state.Queries.Load() != 0 {
		t.Fatal("an empty lookup must not reach the database")
	}
}

// Stage ids reach the database as bound parameters. Interpolating them would
// make a stage id readable as SQL.
func TestGetStagesBindsEveryIdentifier(t *testing.T) {
	store, state := newHarness(t)
	var seen string
	var arguments int
	state.Query = func(_ context.Context, query string, values []driver.NamedValue) (driver.Rows, error) {
		seen, arguments = query, len(values)
		return sqltest.NewRows([]string{"document"}), nil
	}
	ids := []string{testID(t, "stage"), testID(t, "stage")}
	if _, err := store.GetStages(context.Background(), testID(t, "run"), ids); err != nil {
		t.Fatalf("GetStages: %v", err)
	}
	if arguments != len(ids)+1 {
		t.Fatalf("bound %d arguments, want %d", arguments, len(ids)+1)
	}
	for _, id := range ids {
		if strings.Contains(seen, id) {
			t.Fatalf("stage id was interpolated into the query: %s", seen)
		}
	}
}

// The sealed version is shared with the memory adapter. A record written by one
// adapter and transitioned by the other has to pass its own seal check, so the
// two must mint the same version for the same transition.
//
// It has to be the SAME transition through BOTH adapters. This test used to
// re-derive the digest of the memory adapter's own output with a copy of the
// rule that lived in this package, which pinned that copy against itself: the
// store was never driven at all, so the assertion held no matter what
// TransitionStage did. The copy is gone (stages.go calls
// orchestration.SealStage), and this drives both adapters step for step.
//
// Two steps, not one. Attempts is part of the sealed preimage and only a
// promotion into preparing advances it, so a one-step test would agree on a
// field neither adapter had moved.
func TestStageDigestMatchesTheMemoryAdapter(t *testing.T) {
	ctx := context.Background()
	memory := orchestration.NewMemoryRepository(0)
	store, state := newHarness(t)
	record := newStageRecord(t)
	if _, replayed, err := memory.PutStage(ctx, record); err != nil || replayed {
		t.Fatalf("memory PutStage: err=%v replayed=%v", err, replayed)
	}

	current := record
	for _, step := range []struct {
		to orchestration.StageState
		at time.Time
	}{
		{to: orchestration.StageQueued, at: testStart.Add(time.Second)},
		{to: orchestration.StagePreparing, at: testStart.Add(2 * time.Second)},
	} {
		reference, replayed, err := memory.TransitionStage(ctx,
			current.RunID, current.StageID, step.to, current.Version, step.at)
		if err != nil || replayed {
			t.Fatalf("memory TransitionStage to %s: err=%v replayed=%v", step.to, err, replayed)
		}
		// The fake driver answers the locking read with whatever it was last
		// given, so the store starts each step from the same record the memory
		// adapter did rather than from a row it wrote itself.
		serveStage(t, state, current)
		durable, replayed, err := store.TransitionStage(ctx,
			current.RunID, current.StageID, step.to, current.Version, step.at)
		if err != nil || replayed {
			t.Fatalf("store TransitionStage to %s: err=%v replayed=%v", step.to, err, replayed)
		}
		if durable.Version.String() != reference.Version.String() {
			t.Fatalf("transition to %s sealed %q durably and %q in the reference adapter; "+
				"the two have forked the stage version",
				step.to, durable.Version, reference.Version)
		}
		if durable.Attempts != reference.Attempts || durable.State != reference.State ||
			!durable.UpdatedAt.Equal(reference.UpdatedAt) {
			t.Fatalf("transition to %s produced %+v durably and %+v in the reference adapter",
				step.to, durable, reference)
		}
		current = reference
	}
	// A promotion into preparing charges an attempt. If it did not, the loop
	// above would have compared two records that differ in no sealed field the
	// first step had not already covered.
	if current.Attempts != 1 {
		t.Fatalf("attempts = %d after preparing, want 1", current.Attempts)
	}
}

// A document that no longer validates must not be handed to a reconciler as if
// it were sound.
func TestDecodeRejectsATamperedDocument(t *testing.T) {
	record := newStageRecord(t)
	document, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	tampered := strings.Replace(string(document), `"State":"blocked"`, `"State":"nonsense"`, 1)
	if tampered == string(document) {
		t.Fatal("the test fixture did not change the stored state")
	}
	if _, err := decodeDocument[stageDocument](context.Background(), []byte(tampered), "test"); err == nil {
		t.Fatal("a document with an unrecognized state must be rejected")
	}
}

func TestStoreRequiresItsProviders(t *testing.T) {
	state := &sqltest.State{}
	database, err := sqltest.Open(state)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = database.Close() }()
	messages, err := outboxmemory.New()
	if err != nil {
		t.Fatalf("outbox: %v", err)
	}
	cases := map[string]func() (*Store, error){
		"no database": func() (*Store, error) {
			return New(nil, audit.NopRecorder{}, messages)
		},
		"no recorder": func() (*Store, error) {
			return New(database, nil, messages)
		},
		"no outbox": func() (*Store, error) {
			return New(database, audit.NopRecorder{}, nil)
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := build(); err == nil {
				t.Fatal("a store missing a provider must fail construction")
			}
		})
	}
}

var _ outbox.Store = (*outboxmemory.Store)(nil)

// nilEnqueuer exists to be a typed nil. A (*nilEnqueuer)(nil) is not == nil, so
// it is exactly the wiring mistake WithEnqueuer has to catch.
type nilEnqueuer struct{}

func (*nilEnqueuer) EnqueueStage(context.Context, orchestration.WorkItem) error { return nil }

// The placement append must run on the mutation's transaction context. On any
// other context it would commit -- or fail -- independently of the stage
// transition it is supposed to be atomic with, which is the entire reason the
// seam takes a context at all.
func TestPromotionEnqueuesInsideTheMutationTransaction(t *testing.T) {
	record := newStageRecord(t)
	var calls int
	var joined bool
	var placed orchestration.WorkItem
	store, state := newHarness(t, WithEnqueuer(orchestration.EnqueuerFunc(
		func(ctx context.Context, item orchestration.WorkItem) error {
			calls++
			_, joined = transaction.FromContext(ctx)
			placed = item
			return nil
		})))
	serveStage(t, state, record)

	_, replayed, err := store.TransitionStage(context.Background(), record.RunID, record.StageID,
		orchestration.StageQueued, record.Version, testStart.Add(time.Second))
	if err != nil || replayed {
		t.Fatalf("TransitionStage: err=%v replayed=%v", err, replayed)
	}
	if calls != 1 {
		t.Fatalf("enqueued %d times, want exactly 1", calls)
	}
	if !joined {
		t.Fatal("the placement append must receive the mutation's transaction, not an ambient context")
	}
	if placed.RunID != record.RunID || placed.StageID != record.StageID || placed.Attempt != 1 {
		t.Fatalf("placed %+v, want the promoted stage at attempt 1", placed)
	}
	if state.Commits.Load() != 1 {
		t.Fatalf("commits = %d, want exactly 1", state.Commits.Load())
	}
}

// A placement that could not be appended must take the transition down with it.
// Committing the stage into queued while its work item was lost is the failure
// this whole seam exists to make unreachable.
func TestAFailedPlacementRollsBackTheTransition(t *testing.T) {
	record := newStageRecord(t)
	failure := errors.New("placement queue rejected the item")
	store, state := newHarness(t, WithEnqueuer(orchestration.EnqueuerFunc(
		func(context.Context, orchestration.WorkItem) error { return failure })))
	serveStage(t, state, record)

	_, _, err := store.TransitionStage(context.Background(), record.RunID, record.StageID,
		orchestration.StageQueued, record.Version, testStart.Add(time.Second))
	if err == nil {
		t.Fatal("a promotion whose placement failed must surface as an error")
	}
	if !errors.Is(err, failure) {
		t.Fatalf("err = %v, want the producer's failure preserved", err)
	}
	if state.Commits.Load() != 0 {
		t.Fatal("a promotion whose placement failed must not commit")
	}
	if state.Rollbacks.Load() == 0 {
		t.Fatal("a promotion whose placement failed must roll back")
	}
}

// Only promotion places work. A stage starting to run is already placed, and
// enqueueing again would put a second worker on the same attempt.
func TestOnlyPromotionPlacesWork(t *testing.T) {
	record := newStageRecord(t)
	queued := record
	queued.State = orchestration.StageQueued
	sealed, err := orchestration.SealStage(queued, record.Version.Generation()+1)
	if err != nil {
		t.Fatalf("seal queued stage: %v", err)
	}
	var calls int
	store, state := newHarness(t, WithEnqueuer(orchestration.EnqueuerFunc(
		func(context.Context, orchestration.WorkItem) error {
			calls++
			return nil
		})))
	serveStage(t, state, sealed)

	if _, _, err := store.TransitionStage(context.Background(), sealed.RunID, sealed.StageID,
		orchestration.StagePreparing, sealed.Version, testStart.Add(time.Second)); err != nil {
		t.Fatalf("TransitionStage: %v", err)
	}
	if calls != 0 {
		t.Fatalf("enqueued %d times, want 0; only a promotion places work", calls)
	}
}

// A store composed without a producer still records stage state. Local
// composition and every test above the queue seam depend on that.
func TestAStoreWithNoProducerStillTransitions(t *testing.T) {
	record := newStageRecord(t)
	store, state := newHarness(t)
	serveStage(t, state, record)
	moved, _, err := store.TransitionStage(context.Background(), record.RunID, record.StageID,
		orchestration.StageQueued, record.Version, testStart.Add(time.Second))
	if err != nil {
		t.Fatalf("TransitionStage: %v", err)
	}
	if moved.State != orchestration.StageQueued {
		t.Fatalf("state = %s, want queued", moved.State)
	}
}

// Wiring a nil producer is a mistake, not a composition. Accepting it would
// drop every placement in silence, which is indistinguishable from the bug this
// task fixed.
func TestWithEnqueuerRefusesANilProducer(t *testing.T) {
	state := &sqltest.State{}
	database, err := sqltest.Open(state)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = database.Close() }()
	messages, err := outboxmemory.New()
	if err != nil {
		t.Fatalf("outbox: %v", err)
	}
	cases := map[string]orchestration.Enqueuer{
		"untyped nil": nil,
		"typed nil":   (*nilEnqueuer)(nil),
	}
	for name, producer := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := New(database, audit.NopRecorder{}, messages, WithEnqueuer(producer)); err == nil {
				t.Fatal("a nil placement producer must fail construction")
			}
		})
	}
}
