// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package orchestration

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/idempotency"
)

var promotionStart = time.Date(2026, time.August, 23, 6, 0, 0, 0, time.UTC)

// recordingEnqueuer stands in for the composition root's placement producer.
//
// It records rather than counts, because "exactly one item" is only half the
// property that matters: an item carrying the wrong attempt number would be
// placed once and execute the wrong attempt.
type recordingEnqueuer struct {
	items   []WorkItem
	failure error
}

func (enqueuer *recordingEnqueuer) EnqueueStage(_ context.Context, item WorkItem) error {
	if enqueuer.failure != nil {
		return enqueuer.failure
	}
	enqueuer.items = append(enqueuer.items, item)
	return nil
}

// blockedStage materializes one stage so a promotion has something to move.
func blockedStage(t *testing.T, repository *MemoryRepository) StageRecord {
	t.Helper()
	record, err := NewStage(testID(t, "run"), testID(t, "job"), testID(t, "stage"), promotionStart)
	if err != nil {
		t.Fatalf("new stage: %v", err)
	}
	stored, replayed, err := repository.PutStage(context.Background(), record)
	if err != nil || replayed {
		t.Fatalf("PutStage: err=%v replayed=%v", err, replayed)
	}
	return stored
}

// Nothing produced placement work before this seam existed, so a wired
// scheduler drained a permanently empty queue. A promotion to queued is the
// moment a stage becomes placeable, and it must produce exactly one item.
func TestPromotionToQueuedEnqueuesExactlyOneItem(t *testing.T) {
	enqueuer := &recordingEnqueuer{}
	repository := NewMemoryRepository(0, WithMemoryEnqueuer(enqueuer))
	stage := blockedStage(t, repository)

	promoted, replayed, err := repository.TransitionStage(context.Background(),
		stage.RunID, stage.StageID, StageQueued, stage.Version, promotionStart.Add(time.Second))
	if err != nil || replayed {
		t.Fatalf("TransitionStage: err=%v replayed=%v", err, replayed)
	}
	if promoted.State != StageQueued {
		t.Fatalf("state = %s, want queued", promoted.State)
	}
	if len(enqueuer.items) != 1 {
		t.Fatalf("enqueued %d items, want exactly 1", len(enqueuer.items))
	}
	item := enqueuer.items[0]
	if item.RunID != stage.RunID || item.JobID != stage.JobID || item.StageID != stage.StageID {
		t.Fatalf("item identity = %+v, want the promoted stage's", item)
	}
	// The stage has opened no attempt yet, so the item names the attempt the
	// claim will open. An item carrying attempt 0 would not even validate.
	if item.Attempt != 1 {
		t.Fatalf("attempt = %d, want 1", item.Attempt)
	}
}

// Idempotency here is the difference between a stage being placed once and
// being placed on every reconcile. A redelivered promotion replays the
// transition, so it must not produce a second item.
func TestReplayedPromotionEnqueuesNothing(t *testing.T) {
	enqueuer := &recordingEnqueuer{}
	repository := NewMemoryRepository(0, WithMemoryEnqueuer(enqueuer))
	stage := blockedStage(t, repository)
	ctx := context.Background()

	first, replayed, err := repository.TransitionStage(ctx,
		stage.RunID, stage.StageID, StageQueued, stage.Version, promotionStart.Add(time.Second))
	if err != nil || replayed {
		t.Fatalf("first promotion: err=%v replayed=%v", err, replayed)
	}
	_, replayed, err = repository.TransitionStage(ctx,
		stage.RunID, stage.StageID, StageQueued, first.Version, promotionStart.Add(2*time.Second))
	if err != nil {
		t.Fatalf("replayed promotion: %v", err)
	}
	if !replayed {
		t.Fatal("a second promotion to the state already held must replay")
	}
	if len(enqueuer.items) != 1 {
		t.Fatalf("enqueued %d items, want exactly 1 across both deliveries", len(enqueuer.items))
	}
}

// The enqueue and the transition are one durable mutation. A failure on the
// enqueue side must leave the stage where it was, or the run would hold a
// queued stage no queue knows about.
func TestAFailedEnqueueLeavesTheStageUnpromoted(t *testing.T) {
	failure := errors.New("placement queue is unreachable")
	enqueuer := &recordingEnqueuer{failure: failure}
	repository := NewMemoryRepository(0, WithMemoryEnqueuer(enqueuer))
	stage := blockedStage(t, repository)
	ctx := context.Background()

	_, _, err := repository.TransitionStage(ctx,
		stage.RunID, stage.StageID, StageQueued, stage.Version, promotionStart.Add(time.Second))
	if err == nil {
		t.Fatal("a promotion whose enqueue failed must surface as an error")
	}
	if !errors.Is(err, failure) {
		t.Fatalf("err = %v, want the enqueue failure preserved", err)
	}
	current, err := repository.GetStage(ctx, stage.RunID, stage.StageID)
	if err != nil {
		t.Fatalf("GetStage: %v", err)
	}
	if current.State != StageBlocked {
		t.Fatalf("state = %s, want blocked; the transition must not outlive its enqueue", current.State)
	}
	if current.Version.String() != stage.Version.String() {
		t.Fatal("a rolled-back transition must not advance the resource version")
	}
}

// Only promotion produces placement work. Enqueueing on every transition would
// place a stage again the moment it started running.
func TestNonPromotingTransitionsEnqueueNothing(t *testing.T) {
	enqueuer := &recordingEnqueuer{}
	repository := NewMemoryRepository(0, WithMemoryEnqueuer(enqueuer))
	stage := blockedStage(t, repository)
	ctx := context.Background()

	queued, _, err := repository.TransitionStage(ctx,
		stage.RunID, stage.StageID, StageQueued, stage.Version, promotionStart.Add(time.Second))
	if err != nil {
		t.Fatalf("promote: %v", err)
	}
	preparing, _, err := repository.TransitionStage(ctx,
		stage.RunID, stage.StageID, StagePreparing, queued.Version, promotionStart.Add(2*time.Second))
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if _, _, err := repository.TransitionStage(ctx,
		stage.RunID, stage.StageID, StageRunning, preparing.Version, promotionStart.Add(3*time.Second)); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(enqueuer.items) != 1 {
		t.Fatalf("enqueued %d items, want 1; only the promotion places work", len(enqueuer.items))
	}
}

// A stage outlives its attempts: a lease lost to a preempted node returns it to
// the queue. That is a genuinely new promotion, and the item it produces must
// name the next attempt rather than replaying the one that died.
func TestRequeueAfterALostLeasePlacesTheNextAttempt(t *testing.T) {
	enqueuer := &recordingEnqueuer{}
	repository := NewMemoryRepository(0, WithMemoryEnqueuer(enqueuer))
	stage := blockedStage(t, repository)
	ctx := context.Background()

	queued, _, err := repository.TransitionStage(ctx,
		stage.RunID, stage.StageID, StageQueued, stage.Version, promotionStart.Add(time.Second))
	if err != nil {
		t.Fatalf("promote: %v", err)
	}
	preparing, _, err := repository.TransitionStage(ctx,
		stage.RunID, stage.StageID, StagePreparing, queued.Version, promotionStart.Add(2*time.Second))
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if preparing.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1 after preparing", preparing.Attempts)
	}
	if _, _, err := repository.TransitionStage(ctx,
		stage.RunID, stage.StageID, StageQueued, preparing.Version, promotionStart.Add(3*time.Second)); err != nil {
		t.Fatalf("requeue: %v", err)
	}
	if len(enqueuer.items) != 2 {
		t.Fatalf("enqueued %d items, want 2", len(enqueuer.items))
	}
	if enqueuer.items[0].Attempt != 1 || enqueuer.items[1].Attempt != 2 {
		t.Fatalf("attempts = %d,%d; want 1,2", enqueuer.items[0].Attempt, enqueuer.items[1].Attempt)
	}
}

// A repository composed without a placement producer must still transition
// stages. Local single-process composition and every test above the queue seam
// depend on it, and refusing there would make placement a precondition for
// recording state.
func TestAnUnwiredRepositoryStillTransitions(t *testing.T) {
	repository := NewMemoryRepository(0)
	stage := blockedStage(t, repository)
	promoted, _, err := repository.TransitionStage(context.Background(),
		stage.RunID, stage.StageID, StageQueued, stage.Version, promotionStart.Add(time.Second))
	if err != nil {
		t.Fatalf("TransitionStage: %v", err)
	}
	if promoted.State != StageQueued {
		t.Fatalf("state = %s, want queued", promoted.State)
	}
}

// The end-to-end path the controller drives: admitting a ready stage is what
// puts it on the placement queue, and it does so once per stage.
func TestAdmitReadyPlacesEachPromotedStageOnce(t *testing.T) {
	enqueuer := &recordingEnqueuer{}
	repository := NewMemoryRepository(0, WithMemoryEnqueuer(enqueuer))
	service := Service{Repository: repository}
	ctx := context.Background()

	root := testID(t, "stage")
	child := testID(t, "stage")
	compiled, err := Compile(CompileRequest{Name: "pipeline", Stages: []StageSpec{
		testStage(t, root, "fetch"),
		testStage(t, child, "msa", root),
	}}, testWorkflowID(t))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if _, _, err := repository.PutWorkflow(ctx, compiled, promotionStart); err != nil {
		t.Fatalf("PutWorkflow: %v", err)
	}
	runID, jobID := testID(t, "run"), testID(t, "job")
	if _, err := service.StartRun(ctx, compiled.Workflow.ID, runID, jobID); err != nil {
		t.Fatalf("StartRun: %v", err)
	}

	promoted, err := service.AdmitReady(ctx, compiled.Workflow.ID, runID)
	if err != nil {
		t.Fatalf("AdmitReady: %v", err)
	}
	// Only the root is ready; the child waits on it.
	if len(promoted) != 1 || promoted[0].StageID != root {
		t.Fatalf("promoted = %+v, want only the root", promoted)
	}
	if len(enqueuer.items) != 1 {
		t.Fatalf("enqueued %d items, want exactly 1", len(enqueuer.items))
	}
	if enqueuer.items[0].StageID != root {
		t.Fatalf("placed %s, want the root %s", enqueuer.items[0].StageID, root)
	}

	// Re-driving the reconcile must not place the root a second time: it is no
	// longer blocked, so it is no longer ready.
	again, err := service.AdmitReady(ctx, compiled.Workflow.ID, runID)
	if err != nil {
		t.Fatalf("second AdmitReady: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("second reconcile promoted %d stages, want 0", len(again))
	}
	if len(enqueuer.items) != 1 {
		t.Fatalf("enqueued %d items after a re-reconcile, want 1", len(enqueuer.items))
	}
}

// scopedRepository records which stage read a Service call made.
//
// A read that is too wide costs latency rather than correctness, so nothing
// else in the suite would notice it: the answers are identical either way. The
// call is the only observable, so the call is what this records.
type scopedRepository struct {
	*MemoryRepository
	listed  int
	fetched [][]string
}

func (repository *scopedRepository) ListStages(ctx context.Context, runID string) ([]StageRecord, error) {
	repository.listed++
	return repository.MemoryRepository.ListStages(ctx, runID)
}

func (repository *scopedRepository) GetStages(ctx context.Context, runID string, stageIDs []string) ([]StageRecord, error) {
	repository.fetched = append(repository.fetched, append([]string(nil), stageIDs...))
	return repository.MemoryRepository.GetStages(ctx, runID, stageIDs)
}

// cancellationFor builds a valid intent. A run-scoped origin may not name a
// stage and an attempt-scoped one must, so the stage argument decides both.
func cancellationFor(t *testing.T, runID, stageID string, origin CancellationOrigin) CancellationIntent {
	t.Helper()
	return CancellationIntent{
		RunID:   runID,
		StageID: stageID,
		Origin:  origin,
		Reason:  "conformance cancellation",
		Idempotency: idempotency.Identity{
			Scope: idempotency.MustParseScope("control-plane/orchestration/cancel"),
			Key:   idempotency.MustParseKey("cancel-" + string(origin) + "-" + runID),
		},
		RequestedAt: promotionStart,
	}
}

// A preempted node cancels one attempt, not a run. Reading the whole run to do
// it made a preemption cost O(stages) against a graph bounded at 4096, on the
// path that runs once per lost lease -- and Propagate's attempt-scoped arm only
// ever reads the one stage the intent names, so the wide read bought nothing.
//
// The run-scoped half is asserted in the same test on purpose: the narrow read
// is only correct because the origin decides it, and a split that narrowed both
// arms would cancel one stage of a run an operator asked to stop.
func TestCancelReadsOnlyTheStagesItsOriginCanReach(t *testing.T) {
	repository := &scopedRepository{MemoryRepository: NewMemoryRepository(0)}
	service := Service{Repository: repository}
	ctx := context.Background()

	first, second, third := testID(t, "stage"), testID(t, "stage"), testID(t, "stage")
	compiled, err := Compile(CompileRequest{Name: "pipeline", Stages: []StageSpec{
		testStage(t, first, "fetch"),
		testStage(t, second, "msa"),
		testStage(t, third, "fold"),
	}}, testWorkflowID(t))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if _, _, err := repository.PutWorkflow(ctx, compiled, promotionStart); err != nil {
		t.Fatalf("PutWorkflow: %v", err)
	}
	runID, jobID := testID(t, "run"), testID(t, "job")
	if _, err := service.StartRun(ctx, compiled.Workflow.ID, runID, jobID); err != nil {
		t.Fatalf("StartRun: %v", err)
	}

	cancelling, replayed, err := service.Cancel(ctx, compiled.Workflow.ID,
		cancellationFor(t, runID, second, OriginPreemption))
	if err != nil || replayed {
		t.Fatalf("attempt-scoped Cancel: err=%v replayed=%v", err, replayed)
	}
	if repository.listed != 0 {
		t.Fatalf("an attempt-scoped cancellation listed the run %d times, want 0", repository.listed)
	}
	if len(repository.fetched) != 1 || len(repository.fetched[0]) != 1 || repository.fetched[0][0] != second {
		t.Fatalf("fetched %v, want exactly the named stage [%s]", repository.fetched, second)
	}
	// The narrow read must not have narrowed the outcome.
	if len(cancelling) != 1 || cancelling[0].StageID != second {
		t.Fatalf("cancelled %+v, want only the named stage", cancelling)
	}
	if cancelling[0].State != StageCancelling {
		t.Fatalf("state = %s, want cancelling", cancelling[0].State)
	}
	for _, untouched := range []string{first, third} {
		record, err := repository.GetStage(ctx, runID, untouched)
		if err != nil {
			t.Fatalf("GetStage %s: %v", untouched, err)
		}
		if record.State != StageBlocked {
			t.Fatalf("stage %s = %s after an attempt-scoped cancellation, want blocked", untouched, record.State)
		}
	}

	// An operator stop reaches the graph, so it still reads the graph.
	stopped, replayed, err := service.Cancel(ctx, compiled.Workflow.ID,
		cancellationFor(t, runID, "", OriginOperator))
	if err != nil || replayed {
		t.Fatalf("run-scoped Cancel: err=%v replayed=%v", err, replayed)
	}
	if repository.listed != 1 {
		t.Fatalf("a run-scoped cancellation listed the run %d times, want 1", repository.listed)
	}
	if len(repository.fetched) != 1 {
		t.Fatalf("a run-scoped cancellation made %d scoped fetches, want none of its own", len(repository.fetched)-1)
	}
	// The stage already cancelling is unchanged, so only the other two move.
	if len(stopped) != 2 {
		t.Fatalf("run-scoped cancellation moved %d stages, want 2", len(stopped))
	}
}

// PlacementItem is shared with the durable adapter so both derive the same
// item. A record whose identity does not validate must be refused rather than
// producing an item a worker would dead-letter.
func TestPlacementItemRefusesAnInvalidRecord(t *testing.T) {
	_, err := PlacementItem(StageRecord{})
	if err == nil {
		t.Fatal("an empty stage record must not yield a placement item")
	}
	if !faults.IsCode(err, faults.CodeInvalidArgument) {
		t.Fatalf("code = %s, want invalid_argument", faults.CodeOf(err))
	}
}

// StartRun must materialize stages that are usable, not merely constructed. A
// struct literal carries no sealed version and PutStage validates the seal, so
// an unsealed materialization refuses its own stages -- silently, until
// something drives the run far enough to promote one.
func TestStartRunMaterializesSealedStages(t *testing.T) {
	repository := NewMemoryRepository(0)
	service := Service{Repository: repository}
	ctx := context.Background()

	stage := testID(t, "stage")
	compiled, err := Compile(CompileRequest{Name: "pipeline", Stages: []StageSpec{
		testStage(t, stage, "fetch"),
	}}, testWorkflowID(t))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if _, _, err := repository.PutWorkflow(ctx, compiled, promotionStart); err != nil {
		t.Fatalf("PutWorkflow: %v", err)
	}
	records, err := service.StartRun(ctx, compiled.Workflow.ID, testID(t, "run"), testID(t, "job"))
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("materialized %d stages, want 1", len(records))
	}
	if err := records[0].Validate(); err != nil {
		t.Fatalf("a materialized stage must validate: %v", err)
	}
	if records[0].Version.IsZero() {
		t.Fatal("a materialized stage must carry a sealed version")
	}
}
