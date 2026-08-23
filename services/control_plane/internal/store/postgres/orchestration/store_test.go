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
	"go.mindclade.dev/control/runtime_authority"
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
func newHarness(t *testing.T) (*Store, *sqltest.State) {
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
		WithClock(clock.RealClock{}), WithRetry(retries))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	return store, state
}

func testID(t *testing.T, kind string) string {
	t.Helper()
	id, err := identifiers.NewID(identifiers.MustParseKind(kind))
	if err != nil {
		t.Fatalf("new %s id: %v", kind, err)
	}
	return id.String()
}

func testStage(t *testing.T, id string) orchestration.StageSpec {
	t.Helper()
	return orchestration.StageSpec{
		StageID:              id,
		Kind:                 orchestration.StagePreprocess,
		Operation:            "fetch",
		OutputNamespace:      "raw",
		ResolvedConfigDigest: identifiers.SHA256String("config"),
		Budget: runtime_authority.ExecutionBudget{
			CPUMillis: 1000, ResidentMemoryBytes: 1 << 20,
			OpenFileDescriptors: 16, CPUWorkerThreads: 1,
		},
		Timeout:         time.Minute,
		MaximumAttempts: 3,
	}
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

func TestStoreSatisfiesTheRepositoryContract(t *testing.T) {
	// The compile-time assertion in store.go is the real check; this names it so
	// a reader of the test file learns the contract exists.
	var repository orchestration.Repository = mustStore(t)
	if repository == nil {
		t.Fatal("the store must satisfy orchestration.Repository")
	}
}

func mustStore(t *testing.T) *Store {
	t.Helper()
	store, _ := newHarness(t)
	return store
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

// The version digest is shared with the memory adapter. If the two ever compute
// it differently, a record written by one and read by the other fails its own
// seal check, so the definitions are pinned against each other here.
func TestStageDigestMatchesTheMemoryAdapter(t *testing.T) {
	repository := orchestration.NewMemoryRepository(0)
	record := newStageRecord(t)
	if _, _, err := repository.PutStage(context.Background(), record); err != nil {
		t.Fatalf("memory PutStage: %v", err)
	}
	stored, err := repository.GetStage(context.Background(), record.RunID, record.StageID)
	if err != nil {
		t.Fatalf("memory GetStage: %v", err)
	}
	transitioned, _, err := repository.TransitionStage(context.Background(),
		record.RunID, record.StageID, orchestration.StageQueued, stored.Version, testStart.Add(time.Second))
	if err != nil {
		t.Fatalf("memory TransitionStage: %v", err)
	}
	if !transitioned.Version.Digest().Equal(stageDigest(transitioned)) {
		t.Fatal("this package computes a different stage digest than control/orchestration")
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
