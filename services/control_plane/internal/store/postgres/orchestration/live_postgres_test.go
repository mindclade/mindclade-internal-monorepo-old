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
	"errors"
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
	"go.mindclade.dev/libs/go/audit"
	auditpostgres "go.mindclade.dev/libs/go/audit/postgres"
	"go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/coordination/outbox"
	outboxpostgres "go.mindclade.dev/libs/go/coordination/outbox/postgres"
	"go.mindclade.dev/libs/go/coordination/workqueue"
	workqueuepostgres "go.mindclade.dev/libs/go/coordination/workqueue/postgres"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/idempotency"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/requestmeta"
)

const livePostgresEnvironment = "MINDCLADE_TEST_POSTGRES_DSN"

var liveOrchestrationSchemaSequence atomic.Uint64

type liveOrchestrationStore struct {
	store             *Store
	db                *sql.DB
	recorder          audit.Recorder
	messages          outbox.Store
	schema            string
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
		schema:            schema,
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
	live.recorder = recorder
	live.messages = messages
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
	t.Run("commit", func(t *testing.T) {
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
	})

	t.Run("outbox failure rolls back domain and audit writes", func(t *testing.T) {
		live := newLiveOrchestrationStore(t)
		if _, err := live.db.ExecContext(context.Background(),
			"ALTER TABLE "+live.outboxTable+" ADD CONSTRAINT reject_all_messages CHECK (false)"); err != nil {
			t.Fatalf("install outbox failure: %v", err)
		}
		if _, _, err := live.store.PutStage(context.Background(), liveStage(t)); err == nil {
			t.Fatal("PutStage must fail when its outbox append is rejected")
		}
		if got := live.count(t, live.stageTable); got != 0 {
			t.Fatalf("stages = %d, want 0 after rollback", got)
		}
		if got := live.count(t, live.auditTable); got != 0 {
			t.Fatalf("audit events = %d, want 0 after rollback", got)
		}
		if got := live.count(t, live.outboxTable); got != 0 {
			t.Fatalf("outbox messages = %d, want 0 after rollback", got)
		}
	})
}

// A CHECK constraint that never fires is indistinguishable from one that is not
// there. This proves the constraints tying each projected column to the stored
// document are live, by writing a row whose projection disagrees with its
// document and requiring the server to refuse it.
func TestLivePostgresRejectsStageStateProjectionDriftFromTheDocument(t *testing.T) {
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
	type transitionResult struct {
		replayed bool
		err      error
	}
	results := make(chan transitionResult, racers)
	start := make(chan struct{})
	var ready sync.WaitGroup
	var group sync.WaitGroup
	ready.Add(racers)
	group.Add(racers)
	for range racers {
		go func() {
			defer group.Done()
			ready.Done()
			<-start
			_, replayed, err := live.store.TransitionStage(ctx,
				stored.RunID, stored.StageID, orchestration.StageQueued, stored.Version, time.Now())
			results <- transitionResult{replayed: replayed, err: err}
		}()
	}
	ready.Wait()
	close(start)
	group.Wait()
	close(results)

	applied, replayed := 0, 0
	for result := range results {
		if result.err != nil {
			t.Errorf("racing transition returned an unexpected error: %v", result.err)
			continue
		}
		if result.replayed {
			replayed++
		} else {
			applied++
		}
	}
	if applied != 1 {
		t.Errorf("applied = %d, want exactly 1 winner among %d racers", applied, racers)
	}
	if replayed != racers-1 {
		t.Errorf("replayed = %d, want %d successful losing replays", replayed, racers-1)
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
	absent := "stage_00000000000000000000000000000000') OR true --"
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

// livePlacementQueue is a test-local queue name on purpose.
//
// The production name is scheduling.PlacementQueue, and this package cannot
// reach it: control/scheduling imports control/orchestration, so the reverse
// edge is a cycle. That the name is supplied here, by the code standing in for
// the composition root, is precisely the property the seam is built to have.
const livePlacementQueue = "test/placement"

// withPlacement rebuilds the store over the same schema with a real durable
// work queue bound as its placement producer, and returns that queue's table.
//
// A real queue rather than a recording double: the property under test is that
// the work item lands in the SAME PostgreSQL transaction as the stage row, and
// only a producer that actually writes to the database can be atomic with it.
func (live *liveOrchestrationStore) withPlacement(t *testing.T) string {
	t.Helper()
	table, producer := live.placementProducer(t)
	live.bindPlacement(t, producer)
	return table
}

// placementProducer creates the queue table and builds the durable producer
// over it without binding anything.
//
// It is separate from withPlacement so a test can hold the real producer and
// decide what happens after it succeeds. Proving the append joined the
// transaction needs exactly that: an append that writes its row and a mutation
// that then fails.
func (live *liveOrchestrationStore) placementProducer(t *testing.T) (string, orchestration.Enqueuer) {
	t.Helper()
	table := live.schema + ".placement_queue"
	ddl, err := workqueuepostgres.DDL(table)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := live.db.ExecContext(context.Background(), ddl); err != nil {
		t.Fatalf("apply work queue DDL: %v", err)
	}
	queue, err := workqueuepostgres.New(live.db, table)
	if err != nil {
		t.Fatal(err)
	}
	return table, orchestration.EnqueuerFunc(func(ctx context.Context, work orchestration.WorkItem) error {
		payload, encodeErr := orchestration.EncodeWorkItem(work)
		if encodeErr != nil {
			return encodeErr
		}
		item, itemErr := workqueue.NewItem(
			livePlacementQueue, payload, 0, time.Time{}, 10, requestmeta.Metadata{})
		if itemErr != nil {
			return itemErr
		}
		return queue.Enqueue(ctx, item)
	})
}

// bindPlacement rebuilds the store over the same schema with this producer.
func (live *liveOrchestrationStore) bindPlacement(t *testing.T, producer orchestration.Enqueuer) {
	t.Helper()
	store, err := New(live.db, live.recorder, live.messages,
		WithClock(clock.RealClock{}),
		WithTables(live.workflowTable, live.stageTable, live.attemptTable, live.cancellationTable),
		WithEnqueuer(producer))
	if err != nil {
		t.Fatal(err)
	}
	live.store = store
}

// The claim this task makes: promoting a stage and placing its work are one
// durable act. Nothing produced placement work before, so a wired scheduler
// drained a permanently empty queue; now a promotion fills it, and it does so
// inside the transaction that carries the transition.
func TestLivePostgresPromotionPlacesWorkInTheSameTransaction(t *testing.T) {
	t.Run("commit", func(t *testing.T) {
		live := newLiveOrchestrationStore(t)
		queueTable := live.withPlacement(t)
		ctx := context.Background()
		stored, _, err := live.store.PutStage(ctx, liveStage(t))
		if err != nil {
			t.Fatalf("PutStage: %v", err)
		}
		// Materializing a stage is not placing it. Only promotion is.
		if got := live.count(t, queueTable); got != 0 {
			t.Fatalf("work items = %d after materialization, want 0", got)
		}
		if _, replayed, err := live.store.TransitionStage(ctx,
			stored.RunID, stored.StageID, orchestration.StageQueued, stored.Version, time.Now()); err != nil || replayed {
			t.Fatalf("promote: err=%v replayed=%v", err, replayed)
		}
		if got := live.count(t, queueTable); got != 1 {
			t.Fatalf("work items = %d, want exactly 1", got)
		}

		var queue string
		var payload []byte
		if err := live.db.QueryRowContext(ctx,
			"SELECT queue,payload FROM "+queueTable).Scan(&queue, &payload); err != nil {
			t.Fatalf("read work item: %v", err)
		}
		if queue != livePlacementQueue {
			t.Fatalf("queue = %q, want %q; the root supplies the name", queue, livePlacementQueue)
		}
		item, err := orchestration.DecodeWorkItem(payload)
		if err != nil {
			t.Fatalf("decode work item: %v", err)
		}
		if item.RunID != stored.RunID || item.JobID != stored.JobID || item.StageID != stored.StageID {
			t.Fatalf("item identity = %+v, want the promoted stage's", item)
		}
		if item.Attempt != 1 {
			t.Fatalf("attempt = %d, want 1", item.Attempt)
		}

		// A redelivered promotion replays the transition, so it must not place
		// the stage a second time. This is the difference between placing a
		// stage once and placing it on every reconcile.
		current, err := live.store.GetStage(ctx, stored.RunID, stored.StageID)
		if err != nil {
			t.Fatalf("GetStage: %v", err)
		}
		if _, replayed, err := live.store.TransitionStage(ctx,
			stored.RunID, stored.StageID, orchestration.StageQueued, current.Version, time.Now()); err != nil || !replayed {
			t.Fatalf("replayed promotion: err=%v replayed=%v", err, replayed)
		}
		if got := live.count(t, queueTable); got != 1 {
			t.Fatalf("work items = %d after a replay, want 1", got)
		}
	})

	// The append succeeds and the mutation fails after it. That ordering is the
	// whole test: an item that was really written and is gone again can only
	// have been removed by the rollback of the transaction it was written on.
	//
	// This subtest used to install CHECK (false) on the queue table instead,
	// which makes the insert fail on ANY connection -- so it passed identically
	// whether or not the append had joined the transaction, and it did pass:
	// with enqueuePlacement mutated onto the ambient context it stayed green
	// while the fake-driver test failed. It proved that a rejected placement
	// fails the promotion, which is worth having, but not the atomicity its
	// name claimed.
	t.Run("an appended placement does not survive a failed transaction", func(t *testing.T) {
		live := newLiveOrchestrationStore(t)
		queueTable, producer := live.placementProducer(t)
		aborted := errors.New("promotion aborted after its placement was appended")
		appends := 0
		live.bindPlacement(t, orchestration.EnqueuerFunc(
			func(ctx context.Context, work orchestration.WorkItem) error {
				if err := producer.EnqueueStage(ctx, work); err != nil {
					return err
				}
				appends++
				return aborted
			}))
		ctx := context.Background()
		stored, _, err := live.store.PutStage(ctx, liveStage(t))
		if err != nil {
			t.Fatalf("PutStage: %v", err)
		}
		outboxBefore := live.count(t, live.outboxTable)

		_, _, err = live.store.TransitionStage(ctx,
			stored.RunID, stored.StageID, orchestration.StageQueued, stored.Version, time.Now())
		if err == nil {
			t.Fatal("a promotion whose mutation failed must surface as an error")
		}
		if !errors.Is(err, aborted) {
			t.Fatalf("err = %v, want the producer's failure preserved", err)
		}
		// A plain error carries no retryable fault policy, so the store's retry
		// executor stops after one attempt. Exactly one append ran, and it
		// inserted a row -- without that, the count below would be zero for the
		// uninteresting reason.
		if appends != 1 {
			t.Fatalf("appended %d times, want exactly 1", appends)
		}
		if got := live.count(t, queueTable); got != 0 {
			t.Fatalf("work items = %d, want 0; an appended item that outlived the rollback was never in the transaction", got)
		}
		current, err := live.store.GetStage(ctx, stored.RunID, stored.StageID)
		if err != nil {
			t.Fatalf("GetStage: %v", err)
		}
		if current.State != orchestration.StageBlocked {
			t.Fatalf("state = %s, want blocked; a stage must not commit as queued without its work item", current.State)
		}
		if current.Version.String() != stored.Version.String() {
			t.Fatal("a rolled-back promotion must not advance the resource version")
		}
		if got := live.count(t, live.outboxTable); got != outboxBefore {
			t.Fatalf("outbox messages = %d, want %d; the stage event must roll back too", got, outboxBefore)
		}
	})
}
