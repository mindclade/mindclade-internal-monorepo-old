// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package orchestration

import (
	"strconv"
	"time"

	"go.mindclade.dev/libs/go/coordination"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/resourceversion"
)

// AttemptRecord is the durable state of one execution attempt: which fence owns
// it, which ticket authorizes it, where it is in the worker state machine, and
// how it ended.
//
// It is immutable. Every transition returns a new value whose resource version
// re-seals the content, so a caller holding a stale copy cannot write it back
// over a newer one — the version comparison in the repository will reject it.
type AttemptRecord struct {
	RunID             string
	JobID             string
	StageID           string
	Attempt           uint32
	FencingToken      uint64
	ExecutionTicketID string
	ExecutionClass    string
	State             AttemptState
	Failure           coordination.Failure
	// Sequence is the highest worker status sequence applied to this record.
	// The worker protocol requires strictly increasing, non-zero sequences
	// (worker_protocol/src/sequence.rs), so this is what lets the control plane
	// discard a status that arrived late or twice.
	Sequence  uint64
	CreatedAt time.Time
	UpdatedAt time.Time
	Version   resourceversion.Version
}

// NewAttempt opens an attempt in the Created state.
//
// It does NOT check the attempt number against the stage budget. That check
// belongs to the transition that would start the work, not to construction:
// recording an over-budget attempt has to remain possible so the stage can be
// failed terminally with an accurate record of what happened.
func NewAttempt(runID, jobID string, spec StageSpec, number uint32, fence uint64, ticketID, executionClass string, now time.Time) (AttemptRecord, error) {
	if err := spec.Validate(); err != nil {
		return AttemptRecord{}, err
	}
	if err := validateID(runID, "run", "run_id"); err != nil {
		return AttemptRecord{}, err
	}
	if err := validateID(jobID, "job", "job_id"); err != nil {
		return AttemptRecord{}, err
	}
	if err := validateID(ticketID, "ticket", "execution_ticket_id"); err != nil {
		return AttemptRecord{}, err
	}
	if err := validateBoundedName(executionClass, "execution_class", MaximumResourceClassLength); err != nil {
		return AttemptRecord{}, err
	}
	if number == 0 {
		return AttemptRecord{}, invalid("attempt_number_invalid", "attempt number must be positive", nil)
	}
	if fence == 0 {
		return AttemptRecord{}, invalid("fencing_token_required", "fencing token is required", nil)
	}
	if now.IsZero() {
		return AttemptRecord{}, invalid("attempt_time_invalid", "attempt creation time is required", nil)
	}
	record := AttemptRecord{
		RunID:             runID,
		JobID:             jobID,
		StageID:           spec.StageID,
		Attempt:           number,
		FencingToken:      fence,
		ExecutionTicketID: ticketID,
		ExecutionClass:    executionClass,
		State:             AttemptCreated,
		CreatedAt:         now.Round(0).UTC(),
		UpdatedAt:         now.Round(0).UTC(),
	}
	return sealAttempt(record, 1)
}

func (record AttemptRecord) Validate() error {
	if err := validateID(record.RunID, "run", "run_id"); err != nil {
		return err
	}
	if err := validateID(record.JobID, "job", "job_id"); err != nil {
		return err
	}
	if err := validateID(record.StageID, "stage", "stage_id"); err != nil {
		return err
	}
	if err := validateID(record.ExecutionTicketID, "ticket", "execution_ticket_id"); err != nil {
		return err
	}
	if record.Attempt == 0 || record.FencingToken == 0 || !record.State.Valid() ||
		record.CreatedAt.IsZero() || record.UpdatedAt.Before(record.CreatedAt) {
		return invalid("attempt_record_invalid", "attempt record is incomplete or inconsistent", nil)
	}
	if !record.Failure.IsZero() {
		if err := record.Failure.Validate(); err != nil {
			return invalid("attempt_failure_invalid", "attempt failure record is invalid", err)
		}
		if !record.State.Terminal() {
			return invalid("attempt_failure_state_mismatch", "only a terminal attempt carries a failure", nil)
		}
	}
	if err := record.Version.Validate(); err != nil {
		return invalid("attempt_version_invalid", "attempt resource version is invalid", err)
	}
	if !record.Version.Digest().Equal(attemptDigest(record)) {
		return invalid("attempt_version_digest_mismatch", "attempt version does not seal its content", nil)
	}
	return nil
}

// Transition moves the attempt along the worker state machine.
//
// Idempotent on the current state so a duplicated status delivery is a no-op
// rather than a conflict, which is what makes at-least-once status delivery
// safe to apply directly.
func (record AttemptRecord) Transition(to AttemptState, now time.Time) (AttemptRecord, error) {
	if err := record.Validate(); err != nil {
		return AttemptRecord{}, err
	}
	if record.State == to {
		return record, nil
	}
	if record.State.Terminal() {
		return AttemptRecord{}, failedPrecondition("attempt_terminal", "a terminal attempt cannot transition")
	}
	if !CanTransition(record.State, to) {
		return AttemptRecord{}, conflict("attempt_transition_invalid",
			"the worker protocol does not permit this attempt transition")
	}
	updated := record
	updated.State = to
	updated.UpdatedAt = now.Round(0).UTC()
	return sealAttempt(updated, record.Version.Generation()+1)
}

// Fail moves the attempt to a terminal failure and stores a bounded, sanitized
// record of the cause. coordination.FailureFromError keeps the code, reason,
// public message, and retry policy, and drops the private cause — durable state
// must not become a place internal error text accumulates.
func (record AttemptRecord) Fail(cause error, now time.Time) (AttemptRecord, error) {
	if cause == nil {
		return AttemptRecord{}, invalid("attempt_failure_required", "failing an attempt requires a cause", nil)
	}
	updated, err := record.Transition(AttemptFailed, now)
	if err != nil {
		return AttemptRecord{}, err
	}
	updated.Failure = coordination.FailureFromError(cause, now.Round(0).UTC())
	return sealAttempt(updated, record.Version.Generation()+1)
}

// ApplyStatus admits a worker status only if it is fenced correctly and its
// sequence advances.
//
// The fence check is the whole point of fencing: a worker that lost its lease
// keeps running until it notices, and its next status would otherwise overwrite
// the state of the replacement that took over.
func (record AttemptRecord) ApplyStatus(fence uint64, sequence uint64, to AttemptState, now time.Time) (AttemptRecord, error) {
	if err := record.Validate(); err != nil {
		return AttemptRecord{}, err
	}
	if fence == 0 || sequence == 0 {
		return AttemptRecord{}, invalid("attempt_status_invalid", "worker status requires a non-zero fence and sequence", nil)
	}
	if fence != record.FencingToken {
		return AttemptRecord{}, conflict("stale_fencing_token", "worker status carries a stale fencing token")
	}
	if sequence <= record.Sequence {
		// Not an error: a replayed status is a duplicate, and the protocol
		// treats duplicates as already-applied rather than as corruption.
		return record, nil
	}
	updated, err := record.Transition(to, now)
	if err != nil {
		return AttemptRecord{}, err
	}
	updated.Sequence = sequence
	return sealAttempt(updated, record.Version.Generation()+1)
}

// Retryable reports whether the stage may open another attempt after this one.
//
// This is where the attempt budget is enforced, and deliberately not in
// Validate. libs/go/coordination/workqueue learned this the hard way: rejecting
// an over-budget record during validation made the item permanently unusable,
// because a lease that expired without a terminal transition legitimately
// consumes an attempt the worker never reported, and the record could then
// neither transition nor dead-letter. The budget governs whether to start more
// work; it does not invalidate work that already happened.
func (record AttemptRecord) Retryable(spec StageSpec) bool {
	if !record.State.Terminal() || record.State == AttemptCompleted {
		return false
	}
	if record.State == AttemptCancelled {
		return false
	}
	return record.Attempt < spec.MaximumAttempts
}

func sealAttempt(record AttemptRecord, generation uint64) (AttemptRecord, error) {
	version, err := resourceversion.New(generation, attemptDigest(record))
	if err != nil {
		return AttemptRecord{}, unavailable("attempt_version_unavailable", "attempt resource version is unavailable", err)
	}
	record.Version = version
	if err := record.Validate(); err != nil {
		return AttemptRecord{}, err
	}
	return record, nil
}

func attemptDigest(record AttemptRecord) identifiers.Digest {
	return identifiers.SHA256String(canonicalJoin(
		record.RunID,
		record.JobID,
		record.StageID,
		strconv.FormatUint(uint64(record.Attempt), 10),
		strconv.FormatUint(record.FencingToken, 10),
		record.ExecutionTicketID,
		record.ExecutionClass,
		string(record.State),
		string(record.Failure.Code),
		record.Failure.Reason,
		strconv.FormatUint(record.Sequence, 10),
		strconv.FormatInt(record.CreatedAt.UnixNano(), 10),
		strconv.FormatInt(record.UpdatedAt.UnixNano(), 10),
	))
}

// stageDigest seals a stage record so a stored row that drifts from its version
// can be detected rather than trusted.
func stageDigest(record StageRecord) identifiers.Digest {
	return identifiers.SHA256String(canonicalJoin(
		record.RunID,
		record.JobID,
		record.StageID,
		string(record.State),
		strconv.FormatUint(uint64(record.Attempts), 10),
		strconv.FormatInt(record.UpdatedAt.UnixNano(), 10),
	))
}
