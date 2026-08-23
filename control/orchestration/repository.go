// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package orchestration

import (
	"context"
	"sync"
	"time"

	"go.mindclade.dev/libs/go/resourceversion"
)

// StageRecord is the durable state of one stage within a run.
type StageRecord struct {
	RunID   string
	JobID   string
	StageID string
	State   StageState
	// Attempts is the number of attempts opened so far, including any that
	// ended without reporting. It is the counter the stage budget is measured
	// against, and it only ever increases.
	Attempts  uint32
	UpdatedAt time.Time
	Version   resourceversion.Version
}

// NewStage opens a stage in the blocked state with a sealed initial version.
//
// Sealing at construction rather than at first write is what makes
// TransitionStage's optimistic precondition meaningful from the very first
// transition: an unsealed record has no version to compare, so the first
// concurrent pair of reconcilers would both believe they held current state.
func NewStage(runID, jobID, stageID string, now time.Time) (StageRecord, error) {
	if err := validateID(runID, "run", "run_id"); err != nil {
		return StageRecord{}, err
	}
	if err := validateID(jobID, "job", "job_id"); err != nil {
		return StageRecord{}, err
	}
	if err := validateID(stageID, "stage", "stage_id"); err != nil {
		return StageRecord{}, err
	}
	if now.IsZero() {
		return StageRecord{}, invalid("stage_time_invalid", "stage creation time is required", nil)
	}
	record := StageRecord{
		RunID:     runID,
		JobID:     jobID,
		StageID:   stageID,
		State:     StageBlocked,
		UpdatedAt: now.Round(0).UTC(),
	}
	return SealStage(record, 1)
}

// SealStage stamps a stage with the version that seals its content.
//
// Exported because a durable adapter has to seal transitions identically: a row
// written by one repository and read by another must pass the same seal check,
// and two open-coded versions of this rule is exactly how that stops being true.
func SealStage(record StageRecord, generation uint64) (StageRecord, error) {
	version, err := resourceversion.New(generation, stageDigest(record))
	if err != nil {
		return StageRecord{}, unavailable("stage_version_unavailable", "stage resource version is unavailable", err)
	}
	record.Version = version
	if err := record.Validate(); err != nil {
		return StageRecord{}, err
	}
	return record, nil
}

func (record StageRecord) Validate() error {
	if err := validateID(record.RunID, "run", "run_id"); err != nil {
		return err
	}
	if err := validateID(record.JobID, "job", "job_id"); err != nil {
		return err
	}
	if err := validateID(record.StageID, "stage", "stage_id"); err != nil {
		return err
	}
	if !record.State.Valid() || record.UpdatedAt.IsZero() {
		return invalid("stage_record_invalid", "stage record is incomplete", nil)
	}
	if err := record.Version.Validate(); err != nil {
		return invalid("stage_version_invalid", "stage resource version is invalid", err)
	}
	if !record.Version.Digest().Equal(stageDigest(record)) {
		return invalid("stage_version_digest_mismatch", "stage version does not seal its content", nil)
	}
	return nil
}

// Repository is the durable boundary for workflow, stage, and attempt state.
//
// Implementations must make each mutation atomic with its audit record and its
// outbox append, in one transaction, with the resource-version precondition
// checked inside that transaction. Splitting them lets a crash publish an event
// for a mutation that never committed, or commit a mutation no consumer ever
// hears about.
//
// Every mutation returns (value, replayed, error): replayed reports that the
// call was a duplicate of one already applied, which is what makes at-least-once
// delivery safe to hand straight to a repository.
//
// TransitionStage carries one further obligation. A transition into StageQueued
// is the moment a stage becomes placeable, so an implementation given an
// Enqueuer must append the placement item inside the same transaction as the
// state change. That is why the append lives here and not in Service: only a
// repository owns the transaction, and a promotion whose placement is appended
// outside it is either work that was dropped by a crash or work scheduled for a
// transition that never committed. A replayed transition appends nothing, which
// is what stops a redelivered reconcile from placing the same stage twice.
//
// Parameters are types-only by convention across control/, and `now` is always
// supplied by the caller — a repository that read its own clock could not be
// tested deterministically and would disagree with the service about when a
// deadline elapsed.
type Repository interface {
	PutWorkflow(context.Context, CompiledWorkflow, time.Time) (Workflow, bool, error)
	GetWorkflow(context.Context, string) (CompiledWorkflow, error)

	PutStage(context.Context, StageRecord) (StageRecord, bool, error)
	GetStage(context.Context, string, string) (StageRecord, error)
	ListStages(context.Context, string) ([]StageRecord, error)
	// GetStages fetches a named subset. Completing one stage only needs that
	// stage's children and their parents -- a set bounded by graph shape rather
	// than by run size -- so a wide run does not pay for a whole-run scan on
	// every completion. Missing ids are omitted rather than erroring, because a
	// caller derives them from the graph and a gap is a stage not yet
	// materialized, not a fault.
	GetStages(context.Context, string, []string) ([]StageRecord, error)
	TransitionStage(context.Context, string, string, StageState, resourceversion.Version, time.Time) (StageRecord, bool, error)

	PutAttempt(context.Context, AttemptRecord) (AttemptRecord, bool, error)
	GetAttempt(context.Context, string, string, uint32) (AttemptRecord, error)
	LatestAttempt(context.Context, string, string) (AttemptRecord, error)

	RecordCancellation(context.Context, CancellationIntent) (CancellationIntent, bool, error)
}

// MaximumMemoryRecords bounds the reference repository. A memory adapter with no
// bound is an unbounded queue wearing a different name, and this one is reachable
// from local process composition rather than only from tests.
const MaximumMemoryRecords = 10_000

// MemoryRepository is a bounded, concurrency-safe reference adapter for tests
// and local composition. Durable deployments use a SQL implementation that gets
// the audit and outbox atomicity described on Repository.
type MemoryRepository struct {
	mu            sync.RWMutex
	workflows     map[string]CompiledWorkflow
	stages        map[string]StageRecord
	attempts      map[string]AttemptRecord
	cancellations map[string]CancellationIntent
	maximum       int
	enqueuer      Enqueuer
}

// MemoryOption configures the reference repository.
//
// Options rather than more constructor parameters, and variadic rather than a
// second constructor, so an existing caller keeps compiling and a composition
// that has no placement producer keeps saying so by omission.
type MemoryOption func(*MemoryRepository)

// WithMemoryEnqueuer binds the placement producer a promotion calls.
//
// Omitting it is a legitimate composition, not a misconfiguration: a repository
// with no enqueuer still records stage state, which is what local single-process
// composition and every test above the queue seam need. Making placement a
// precondition for recording state would make those unable to run at all. The
// composition root, not this adapter, is where "a scheduler role must have a
// producer wired" is enforced — it is the only layer that knows which role this
// process is playing.
func WithMemoryEnqueuer(enqueuer Enqueuer) MemoryOption {
	return func(repository *MemoryRepository) { repository.enqueuer = enqueuer }
}

func NewMemoryRepository(maximum int, options ...MemoryOption) *MemoryRepository {
	if maximum <= 0 {
		maximum = MaximumMemoryRecords
	}
	repository := &MemoryRepository{
		workflows:     make(map[string]CompiledWorkflow),
		stages:        make(map[string]StageRecord),
		attempts:      make(map[string]AttemptRecord),
		cancellations: make(map[string]CancellationIntent),
		maximum:       maximum,
	}
	for _, option := range options {
		if option != nil {
			option(repository)
		}
	}
	return repository
}

func stageKey(runID, stageID string) string { return canonicalJoin(runID, stageID) }

func attemptKey(runID, stageID string, attempt uint32) string {
	return canonicalJoin(runID, stageID, formatAttempt(attempt))
}

func formatAttempt(attempt uint32) string {
	const digits = "0123456789"
	if attempt == 0 {
		return "0"
	}
	buffer := make([]byte, 0, 10)
	for attempt > 0 {
		buffer = append([]byte{digits[attempt%10]}, buffer...)
		attempt /= 10
	}
	return string(buffer)
}

func (repository *MemoryRepository) guard(ctx context.Context) error {
	if ctx == nil {
		return invalid("context_nil", "context is required", nil)
	}
	return ctx.Err()
}

func (repository *MemoryRepository) PutWorkflow(ctx context.Context, compiled CompiledWorkflow, now time.Time) (Workflow, bool, error) {
	if err := repository.guard(ctx); err != nil {
		return Workflow{}, false, err
	}
	if err := compiled.Workflow.ValidateIdentity(); err != nil {
		return Workflow{}, false, err
	}
	if now.IsZero() {
		return Workflow{}, false, invalid("workflow_time_invalid", "workflow publication time is required", nil)
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if existing, ok := repository.workflows[compiled.Workflow.ID]; ok {
		// A workflow is immutable once published. Re-publishing the identical
		// plan is a replay; re-publishing a different one under the same id
		// would silently change what a running graph means.
		if !existing.Workflow.DefinitionDigest.Equal(compiled.Workflow.DefinitionDigest) {
			return Workflow{}, false, conflict("workflow_immutable", "a published workflow cannot be redefined")
		}
		return existing.Workflow, true, nil
	}
	if len(repository.workflows) >= repository.maximum {
		return Workflow{}, false, exhausted("workflow_store_bound", "workflow store bound was reached")
	}
	repository.workflows[compiled.Workflow.ID] = compiled
	return compiled.Workflow, false, nil
}

func (repository *MemoryRepository) GetWorkflow(ctx context.Context, id string) (CompiledWorkflow, error) {
	if err := repository.guard(ctx); err != nil {
		return CompiledWorkflow{}, err
	}
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	compiled, ok := repository.workflows[id]
	if !ok {
		return CompiledWorkflow{}, notFound("workflow_not_found", "workflow was not found")
	}
	return compiled, nil
}

func (repository *MemoryRepository) PutStage(ctx context.Context, record StageRecord) (StageRecord, bool, error) {
	if err := repository.guard(ctx); err != nil {
		return StageRecord{}, false, err
	}
	if err := record.Validate(); err != nil {
		return StageRecord{}, false, err
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	key := stageKey(record.RunID, record.StageID)
	if existing, ok := repository.stages[key]; ok {
		return existing, true, nil
	}
	if len(repository.stages) >= repository.maximum {
		return StageRecord{}, false, exhausted("stage_store_bound", "stage store bound was reached")
	}
	repository.stages[key] = record
	return record, false, nil
}

func (repository *MemoryRepository) GetStage(ctx context.Context, runID, stageID string) (StageRecord, error) {
	if err := repository.guard(ctx); err != nil {
		return StageRecord{}, err
	}
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	record, ok := repository.stages[stageKey(runID, stageID)]
	if !ok {
		return StageRecord{}, notFound("stage_not_found", "stage was not found")
	}
	return record, nil
}

func (repository *MemoryRepository) ListStages(ctx context.Context, runID string) ([]StageRecord, error) {
	if err := repository.guard(ctx); err != nil {
		return nil, err
	}
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	records := make([]StageRecord, 0, len(repository.stages))
	for _, record := range repository.stages {
		if record.RunID == runID {
			records = append(records, record)
		}
	}
	return records, nil
}

func (repository *MemoryRepository) GetStages(ctx context.Context, runID string, stageIDs []string) ([]StageRecord, error) {
	if err := repository.guard(ctx); err != nil {
		return nil, err
	}
	if len(stageIDs) > MaximumStageCount {
		return nil, exhausted("stage_lookup_bound", "stage lookup exceeds the maximum stage count")
	}
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	records := make([]StageRecord, 0, len(stageIDs))
	for _, stageID := range stageIDs {
		if record, ok := repository.stages[stageKey(runID, stageID)]; ok {
			records = append(records, record)
		}
	}
	return records, nil
}

// TransitionStage applies a state change under an optimistic precondition.
//
// A transition to the state already held is reported as replayed rather than
// rejected, so a redelivered completion is absorbed. A stale version is a
// conflict: something else moved the stage, and this caller's view of it is
// no longer the truth.
func (repository *MemoryRepository) TransitionStage(
	ctx context.Context, runID, stageID string, to StageState,
	expected resourceversion.Version, now time.Time,
) (StageRecord, bool, error) {
	if err := repository.guard(ctx); err != nil {
		return StageRecord{}, false, err
	}
	if !to.Valid() {
		return StageRecord{}, false, invalid("stage_state_invalid", "stage state is unrecognized", nil)
	}
	if now.IsZero() {
		return StageRecord{}, false, invalid("stage_time_invalid", "stage transition time is required", nil)
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	key := stageKey(runID, stageID)
	current, ok := repository.stages[key]
	if !ok {
		return StageRecord{}, false, notFound("stage_not_found", "stage was not found")
	}
	if current.State == to {
		return current, true, nil
	}
	if !expected.IsZero() && current.Version.String() != expected.String() {
		return StageRecord{}, false, conflict("stage_version_stale", "stage resource version is stale")
	}
	if !CanTransitionStage(current.State, to) {
		return StageRecord{}, false, conflict("stage_transition_invalid", "stage cannot make this transition")
	}
	updated := current
	updated.State = to
	updated.UpdatedAt = now.Round(0).UTC()
	if to == StagePreparing {
		// The attempt counter advances when work is about to start, so a stage
		// that crashed between counting and launching still charged the attempt.
		// Under-counting here is what would let a crash-looping stage retry
		// forever.
		updated.Attempts = current.Attempts + 1
	}
	sealed, err := SealStage(updated, current.Version.Generation()+1)
	if err != nil {
		return StageRecord{}, false, err
	}
	// The enqueue runs BEFORE the map write, not after. This adapter has no
	// rollback -- the mutex is its entire transaction -- so a map it has
	// already written cannot be un-written when the append then fails.
	// Ordering the fallible step first reproduces the durable adapter's
	// guarantee: the transition and its placement item land together, or
	// neither does. Enqueueing after the write here would be the in-memory
	// spelling of the crash-after-commit defect the transaction exists to stop.
	if err := repository.enqueuePlacement(ctx, sealed, to); err != nil {
		return StageRecord{}, false, err
	}
	repository.stages[key] = sealed
	return sealed, false, nil
}

// enqueuePlacement offers a promoted stage to the placement producer.
//
// Only a transition to StageQueued places work, and every entry into queued
// places it -- including the ones that come back from preparing or running
// after a lost lease. A stage outlives its attempts, so a returned stage needs
// a placement for the attempt that follows, while a stage merely starting to
// run needs none.
//
// The caller holds the write lock, so this runs inside the mutation's critical
// section, which is the contract Enqueuer states.
//
// The producer's error is returned unchanged rather than restated as a domain
// fault. Its code and retry policy are what a caller uses to tell contention
// that must be replayed from a payload that never can be, and the durable
// adapter passes the same error through for the same reason -- restating it in
// one adapter and not the other is how the two would stop agreeing.
func (repository *MemoryRepository) enqueuePlacement(ctx context.Context, record StageRecord, to StageState) error {
	if to != StageQueued || nilInterface(repository.enqueuer) {
		return nil
	}
	item, err := PlacementItem(record)
	if err != nil {
		return err
	}
	return repository.enqueuer.EnqueueStage(ctx, item)
}

func (repository *MemoryRepository) PutAttempt(ctx context.Context, record AttemptRecord) (AttemptRecord, bool, error) {
	if err := repository.guard(ctx); err != nil {
		return AttemptRecord{}, false, err
	}
	if err := record.Validate(); err != nil {
		return AttemptRecord{}, false, err
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	key := attemptKey(record.RunID, record.StageID, record.Attempt)
	if existing, ok := repository.attempts[key]; ok {
		if existing.Version.String() == record.Version.String() {
			return existing, true, nil
		}
		if existing.Version.Generation() >= record.Version.Generation() {
			return AttemptRecord{}, false, conflict("attempt_version_stale", "attempt resource version is stale")
		}
	} else if len(repository.attempts) >= repository.maximum {
		return AttemptRecord{}, false, exhausted("attempt_store_bound", "attempt store bound was reached")
	}
	repository.attempts[key] = record
	return record, false, nil
}

func (repository *MemoryRepository) GetAttempt(ctx context.Context, runID, stageID string, attempt uint32) (AttemptRecord, error) {
	if err := repository.guard(ctx); err != nil {
		return AttemptRecord{}, err
	}
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	record, ok := repository.attempts[attemptKey(runID, stageID, attempt)]
	if !ok {
		return AttemptRecord{}, notFound("attempt_not_found", "attempt was not found")
	}
	return record, nil
}

func (repository *MemoryRepository) LatestAttempt(ctx context.Context, runID, stageID string) (AttemptRecord, error) {
	if err := repository.guard(ctx); err != nil {
		return AttemptRecord{}, err
	}
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	var latest AttemptRecord
	var found bool
	for _, record := range repository.attempts {
		if record.RunID != runID || record.StageID != stageID {
			continue
		}
		if !found || record.Attempt > latest.Attempt {
			latest = record
			found = true
		}
	}
	if !found {
		return AttemptRecord{}, notFound("attempt_not_found", "no attempt was found for the stage")
	}
	return latest, nil
}

// RecordCancellation stores the intent, keyed by idempotency identity so a
// retried cancel request is absorbed rather than recorded twice.
func (repository *MemoryRepository) RecordCancellation(ctx context.Context, intent CancellationIntent) (CancellationIntent, bool, error) {
	if err := repository.guard(ctx); err != nil {
		return CancellationIntent{}, false, err
	}
	if err := intent.Validate(); err != nil {
		return CancellationIntent{}, false, err
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	key := canonicalJoin(intent.Idempotency.Scope.String(), intent.Idempotency.Key.String())
	if existing, ok := repository.cancellations[key]; ok {
		if existing.RunID != intent.RunID || existing.StageID != intent.StageID {
			return CancellationIntent{}, false, conflict("cancellation_idempotency_mismatch",
				"the idempotency key was reused for a different cancellation")
		}
		return existing, true, nil
	}
	if len(repository.cancellations) >= repository.maximum {
		return CancellationIntent{}, false, exhausted("cancellation_store_bound", "cancellation store bound was reached")
	}
	repository.cancellations[key] = intent
	return intent, false, nil
}
