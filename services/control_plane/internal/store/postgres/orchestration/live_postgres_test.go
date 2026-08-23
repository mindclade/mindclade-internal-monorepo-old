// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Live PostgreSQL qualification for the orchestration repository.
//
// store_test.go uses a fake driver, which can prove transaction *shape* -- one
// transaction, rollback on failure, nesting refused -- and nothing about SQL.
// Every property below needs a real server: the CHECK constraints that tie each
// projected column to the stored document, FOR UPDATE serialization between
// concurrent reconcilers, and the serializable isolation the store asks for.
//
// Opt-in through MINDCLADE_TEST_POSTGRES_DSN, and each test isolates into its
// own schema so a shared server cannot make two runs interfere.
package orchestrationpostgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/lib/pq"

	"go.mindclade.dev/control/orchestration"
	"go.mindclade.dev/control/runtime_authority"
	auditpostgres "go.mindclade.dev/libs/go/audit/postgres"
	"go.mindclade.dev/libs/go/clock"
	outboxpostgres "go.mindclade.dev/libs/go/coordination/outbox/postgres"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/idempotency"
	"go.mindclade.dev/libs/go/identifiers"
)

const livePostgresEnvironment = "MINDCLADE_TEST_POSTGRES_DSN"

var liveOrchestrationSchemaSequence atomic.Uint64

type liveOrchestrationStore struct {
	store             *Store
	db                *sql.DB
	auditTable        string
	outboxTable       string
	workflowTable     string
	stageTable        string
	attemptTable      string
	cancellationTable string
}

func newLiveOrchestrationStore(t *testing.T) liveOrchestrationStore {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv(livePostgresEnvironment))
	if dsn == "" {
		t.Skipf("%s is not set; live PostgreSQL qualification is opt-in", livePostgresEnvironment)
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(32)
	db.SetMaxIdleConns(16)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		t.Fatalf("connect to live PostgreSQL: %v", err)
	}

	schema := fmt.Sprintf("mc_orch_qual_%d_%d", os.Getpid(), liveOrchestrationSchemaSequence.Add(1))
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, openErr := sql.Open("postgres", dsn)
		if openErr == nil {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
			_, _ = cleanup.ExecContext(cleanupCtx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
			cleanupCancel()
			_ = cleanup.Close()
		}
		_ = db.Close()
	})

	live := liveOrchestrationStore{
		db:                db,
		auditTable:        schema + ".audit_events",
		outboxTable:       schema + ".outbox_messages",
		workflowTable:     schema + ".orchestration_workflows",
		stageTable:        schema + ".orchestration_stages",
		attemptTable:      schema + ".orchestration_attempts",
		cancellationTable: schema + ".orchestration_cancellations",
	}
	auditDDL, err := auditpostgres.DDL(live.auditTable)
	if err != nil {
		t.Fatal(err)
	}
	outboxDDL, err := outboxpostgres.DDL(live.outboxTable)
	if err != nil {
		t.Fatal(err)
	}
	orchestrationDDL, err := DDL(live.workflowTable, live.stageTable, live.attemptTable, live.cancellationTable)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range append([]string{auditDDL, outboxDDL}, orchestrationDDL...) {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("apply orchestration DDL: %v", err)
		}
	}
	recorder, err := auditpostgres.New(db, auditpostgres.WithTable(live.auditTable))
	if err != nil {
		t.Fatal(err)
	}
	messages, err := outboxpostgres.New(db, live.outboxTable)
	if err != nil {
		t.Fatal(err)
	}
	store, err := New(db, recorder, messages,
		WithClock(clock.RealClock{}),
		WithTables(live.workflowTable, live.stageTable, live.attemptTable, live.cancellationTable))
	if err != nil {
		t.Fatal(err)
	}
	live.store = store
	return live
}

func liveID(t *testing.T, kind string) string {
	t.Helper()
	id, err := identifiers.NewID(identifiers.MustParseKind(kind))
	if err != nil {
		t.Fatalf("new %s id: %v", kind, err)
	}
	return id.String()
}

func (live liveOrchestrationStore) count(t *testing.T, table string) int {
	t.Helper()
	var total int
	if err := live.db.QueryRowContext(context.Background(),
		"SELECT count(*) FROM "+table).Scan(&total); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return total
}

func liveStage(t *testing.T) orchestration.StageRecord {
	t.Helper()
	record, err := orchestration.NewStage(
		liveID(t, "run"), liveID(t, "job"), liveID(t, "stage"), time.Now())
	if err != nil {
		t.Fatalf("new stage: %v", err)
	}
	return record
}

// The whole point of the single-transaction rule: the domain row, the audit
// record, and the outbox message either all land or none do.
func TestLivePostgresStageWriteIsAtomicWithAuditAndOutbox(t *testing.T) {
	live := newLiveOrchestrationStore(t)
	record := liveStage(t)
	if _, replayed, err := live.store.PutStage(context.Background(), record); err != nil || replayed {
		t.Fatalf("PutStage: err=%v replayed=%v", err, replayed)
	}
	if got := live.count(t, live.stageTable); got != 1 {
		t.Fatalf("stages = %d, want 1", got)
	}
	if got := live.count(t, live.auditTable); got != 1 {
		t.Fatalf("audit events = %d, want 1", got)
	}
	if got := live.count(t, live.outboxTable); got != 1 {
		t.Fatalf("outbox messages = %d, want 1", got)
	}
}

// A CHECK constraint that never fires is indistinguishable from one that is not
// there. This proves the constraints tying each projected column to the stored
// document are live, by writing a row whose projection disagrees with its
// document and requiring the server to refuse it.
func TestLivePostgresRejectsProjectionDriftFromTheDocument(t *testing.T) {
	live := newLiveOrchestrationStore(t)
	record := liveStage(t)
	if _, _, err := live.store.PutStage(context.Background(), record); err != nil {
		t.Fatalf("PutStage: %v", err)
	}
	// The document still says "blocked"; the column would say "running".
	_, err := live.db.ExecContext(context.Background(),
		"UPDATE "+live.stageTable+" SET state='running' WHERE run_id=$1 AND stage_id=$2",
		record.RunID, record.StageID)
	if err == nil {
		t.Fatal("a state column that disagrees with its document must be refused")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "constraint") {
		t.Fatalf("expected a constraint violation, got: %v", err)
	}
}

// A published workflow is immutable. Republishing the same definition replays;
// a different definition under the same id would change what an already-running
// graph means, and is refused.
func TestLivePostgresWorkflowIsImmutableOncePublished(t *testing.T) {
	live := newLiveOrchestrationStore(t)
	ctx := context.Background()
	id, err := identifiers.NewID(identifiers.MustParseKind("workflow"))
	if err != nil {
		t.Fatalf("workflow id: %v", err)
	}
	first, err := orchestration.Compile(orchestration.CompileRequest{
		Name: "pipeline", Stages: []orchestration.StageSpec{liveStageSpec(t)},
	}, id)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if _, replayed, err := live.store.PutWorkflow(ctx, first, time.Now()); err != nil || replayed {
		t.Fatalf("PutWorkflow: err=%v replayed=%v", err, replayed)
	}
	if _, replayed, err := live.store.PutWorkflow(ctx, first, time.Now()); err != nil || !replayed {
		t.Fatalf("republishing an identical plan must replay: err=%v replayed=%v", err, replayed)
	}
	second, err := orchestration.Compile(orchestration.CompileRequest{
		Name: "pipeline", Stages: []orchestration.StageSpec{liveStageSpec(t)},
	}, id)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	_, _, err = live.store.PutWorkflow(ctx, second, time.Now())
	if err == nil {
		t.Fatal("redefining a published workflow must be refused")
	}
	if !faults.IsCode(err, faults.CodeConflict) {
		t.Fatalf("code = %s, want conflict", faults.CodeOf(err))
	}
}

func liveStageSpec(t *testing.T) orchestration.StageSpec {
	t.Helper()
	return orchestration.StageSpec{
		StageID:              liveID(t, "stage"),
		Kind:                 orchestration.StagePreprocess,
		Operation:            "fetch",
		OutputNamespace:      "raw",
		ResolvedConfigDigest: identifiers.SHA256String("config"),
		Budget:               liveBudget(),
		Timeout:              time.Minute,
		MaximumAttempts:      3,
	}
}

// Two reconcilers racing one completion must produce one transition, not two.
// FOR UPDATE is what serializes them, and only a real server has it.
func TestLivePostgresConcurrentTransitionsProduceOneWinner(t *testing.T) {
	live := newLiveOrchestrationStore(t)
	ctx := context.Background()
	record := liveStage(t)
	stored, _, err := live.store.PutStage(ctx, record)
	if err != nil {
		t.Fatalf("PutStage: %v", err)
	}

	const racers = 8
	var applied atomic.Int64
	var conflicts atomic.Int64
	var group sync.WaitGroup
	group.Add(racers)
	for range racers {
		go func() {
			defer group.Done()
			_, replayed, err := live.store.TransitionStage(ctx,
				stored.RunID, stored.StageID, orchestration.StageQueued, stored.Version, time.Now())
			switch {
			case err == nil && !replayed:
				applied.Add(1)
			case err == nil && replayed:
				// Another racer already made this transition; absorbing it is
				// correct, and is what makes redelivery safe.
			default:
				conflicts.Add(1)
			}
		}()
	}
	group.Wait()

	if got := applied.Load(); got != 1 {
		t.Fatalf("applied = %d, want exactly 1 winner among %d racers", got, racers)
	}
	final, err := live.store.GetStage(ctx, stored.RunID, stored.StageID)
	if err != nil {
		t.Fatalf("GetStage: %v", err)
	}
	if final.State != orchestration.StageQueued {
		t.Fatalf("final state = %s, want queued", final.State)
	}
	// Exactly one durable transition means exactly one audit record and one
	// outbox message beyond the create, however many racers there were.
	if got := live.count(t, live.outboxTable); got != 2 {
		t.Fatalf("outbox messages = %d, want 2 (create + one transition)", got)
	}
}

// A stale version must lose. This is the optimistic precondition the domain
// relies on, evaluated inside the transaction rather than before it.
func TestLivePostgresStaleVersionIsRefused(t *testing.T) {
	live := newLiveOrchestrationStore(t)
	ctx := context.Background()
	record := liveStage(t)
	stored, _, err := live.store.PutStage(ctx, record)
	if err != nil {
		t.Fatalf("PutStage: %v", err)
	}
	moved, _, err := live.store.TransitionStage(ctx,
		stored.RunID, stored.StageID, orchestration.StageQueued, stored.Version, time.Now())
	if err != nil {
		t.Fatalf("first transition: %v", err)
	}
	if moved.Version.String() == stored.Version.String() {
		t.Fatal("a transition must advance the resource version")
	}
	_, _, err = live.store.TransitionStage(ctx,
		stored.RunID, stored.StageID, orchestration.StagePreparing, stored.Version, time.Now())
	if err == nil {
		t.Fatal("a transition carrying a stale version must be refused")
	}
	if !faults.IsCode(err, faults.CodeConflict) {
		t.Fatalf("code = %s, want conflict", faults.CodeOf(err))
	}
}

// A worker that lost its lease keeps running until it notices. Its next write
// carries an older generation and must not overwrite the replacement's state.
func TestLivePostgresStaleAttemptGenerationCannotOverwrite(t *testing.T) {
	live := newLiveOrchestrationStore(t)
	ctx := context.Background()
	spec := liveStageSpec(t)
	runID, jobID := liveID(t, "run"), liveID(t, "job")
	attempt, err := orchestration.NewAttempt(runID, jobID, spec, 1, 7, liveID(t, "ticket"), "cpu-general", time.Now())
	if err != nil {
		t.Fatalf("new attempt: %v", err)
	}
	if _, _, err := live.store.PutAttempt(ctx, attempt); err != nil {
		t.Fatalf("PutAttempt: %v", err)
	}
	advanced, err := attempt.Transition(orchestration.AttemptStarting, time.Now())
	if err != nil {
		t.Fatalf("transition: %v", err)
	}
	if _, _, err := live.store.PutAttempt(ctx, advanced); err != nil {
		t.Fatalf("PutAttempt advanced: %v", err)
	}
	// The original record is now a generation behind.
	_, _, err = live.store.PutAttempt(ctx, attempt)
	if err == nil {
		t.Fatal("a stale attempt generation must not overwrite a newer one")
	}
	if !faults.IsCode(err, faults.CodeConflict) {
		t.Fatalf("code = %s, want conflict", faults.CodeOf(err))
	}
	stored, err := live.store.GetAttempt(ctx, runID, spec.StageID, 1)
	if err != nil {
		t.Fatalf("GetAttempt: %v", err)
	}
	if stored.State != orchestration.AttemptStarting {
		t.Fatalf("state = %s, want starting; the stale write won", stored.State)
	}
}

// The identity is the primary key, so a retried cancel replays rather than
// recording a second intent.
func TestLivePostgresCancellationReplaysOnItsIdempotencyIdentity(t *testing.T) {
	live := newLiveOrchestrationStore(t)
	ctx := context.Background()
	intent := liveCancellation(t)
	if _, replayed, err := live.store.RecordCancellation(ctx, intent); err != nil || replayed {
		t.Fatalf("RecordCancellation: err=%v replayed=%v", err, replayed)
	}
	if _, replayed, err := live.store.RecordCancellation(ctx, intent); err != nil || !replayed {
		t.Fatalf("a retried cancellation must replay: err=%v replayed=%v", err, replayed)
	}
	if got := live.count(t, live.cancellationTable); got != 1 {
		t.Fatalf("cancellations = %d, want 1", got)
	}
	// Reusing one key for a different run is a caller bug, and returning the
	// first intent would report success for a cancellation that never happened.
	other := intent
	other.RunID = liveID(t, "run")
	_, _, err := live.store.RecordCancellation(ctx, other)
	if err == nil {
		t.Fatal("reusing an idempotency key for a different run must be refused")
	}
	if !faults.IsCode(err, faults.CodeConflict) {
		t.Fatalf("code = %s, want conflict", faults.CodeOf(err))
	}
}

// GetStages omits ids the run has not materialized rather than erroring, and
// binds every id as a parameter.
func TestLivePostgresGetStagesReturnsOnlyMaterializedStages(t *testing.T) {
	live := newLiveOrchestrationStore(t)
	ctx := context.Background()
	present := liveStage(t)
	if _, _, err := live.store.PutStage(ctx, present); err != nil {
		t.Fatalf("PutStage: %v", err)
	}
	absent := liveID(t, "stage")
	records, err := live.store.GetStages(ctx, present.RunID, []string{present.StageID, absent})
	if err != nil {
		t.Fatalf("GetStages: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1; a gap is a stage not yet materialized, not a fault", len(records))
	}
	if records[0].StageID != present.StageID {
		t.Fatalf("returned %s, want %s", records[0].StageID, present.StageID)
	}
}

func liveBudget() runtime_authority.ExecutionBudget {
	return runtime_authority.ExecutionBudget{
		CPUMillis: 1000, ResidentMemoryBytes: 1 << 20,
		OpenFileDescriptors: 16, CPUWorkerThreads: 1,
	}
}

func liveCancellation(t *testing.T) orchestration.CancellationIntent {
	t.Helper()
	return orchestration.CancellationIntent{
		RunID:  liveID(t, "run"),
		Origin: orchestration.OriginClient,
		Reason: "operator requested a stop",
		Idempotency: idempotency.Identity{
			Scope: idempotency.MustParseScope("control-plane/orchestration/cancel"),
			Key:   idempotency.MustParseKey("cancel-" + liveID(t, "run")),
		},
		RequestedAt: time.Now(),
	}
}
