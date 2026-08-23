// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package orchestration

import (
	"context"
	"encoding/json"
	"time"

	"go.mindclade.dev/libs/go/coordination/workqueue"
	"go.mindclade.dev/libs/go/faults"
)

// LaunchOutcome is what a launcher reports about a workload it was asked to
// start.
type LaunchOutcome struct {
	// ExternalID is the launcher's handle for the workload — a JobSet name, a
	// Slurm job id, a local process key. It must be derivable from the envelope
	// so a retried launch addresses the same object rather than creating a
	// second one.
	ExternalID string
	// Existed reports that the workload was already present. A launcher must
	// return this rather than an error: duplicate delivery is normal for an
	// at-least-once queue, and treating it as a failure would burn the stage's
	// attempt budget on work that is already running.
	Existed bool
	// State is the attempt state observed immediately after launch.
	State AttemptState
}

func (outcome LaunchOutcome) Validate() error {
	if err := validateBoundedName(outcome.ExternalID, "external_id", MaximumOutputNamespaceLength); err != nil {
		return err
	}
	if !outcome.State.Valid() {
		return invalid("launch_state_invalid", "launch outcome carries an unrecognized attempt state", nil)
	}
	return nil
}

// Observation is the launcher's view of a workload it previously started.
type Observation struct {
	ExternalID string
	State      AttemptState
	// Sequence orders observations from one launcher. It must strictly increase
	// per attempt, matching the worker protocol's status sequence rule, so a
	// late observation can be discarded rather than applied out of order.
	Sequence uint64
	// Failure carries the cause when State is terminal and unsuccessful.
	Failure error
	// ObservedAt is when the launcher read this state, not when it reported it.
	ObservedAt time.Time
}

func (observation Observation) Validate() error {
	if err := validateBoundedName(observation.ExternalID, "external_id", MaximumOutputNamespaceLength); err != nil {
		return err
	}
	if !observation.State.Valid() {
		return invalid("observation_state_invalid", "observation carries an unrecognized attempt state", nil)
	}
	if observation.Sequence == 0 {
		return invalid("observation_sequence_invalid", "observation sequence must be positive", nil)
	}
	if observation.ObservedAt.IsZero() {
		return invalid("observation_time_invalid", "observation time is required", nil)
	}
	return nil
}

// Launcher is the seam between orchestration policy and a concrete execution
// backend. Kubernetes, Slurm, and the local development runner all implement it
// identically, which is what lets the same workflow run against any of them.
//
// Every method must be idempotent under duplicate delivery and must refuse to
// act on a stale fence. A launcher owns no policy: it does not decide whether to
// retry, where to place work, or when to give up.
type Launcher interface {
	// Launch starts the workload, or reports that it is already running.
	Launch(context.Context, WorkloadEnvelope) (LaunchOutcome, error)
	// Observe reads the current state of a previously launched workload.
	Observe(context.Context, WorkloadEnvelope) (Observation, error)
	// Cancel stops the workload. Cancelling something already gone is not an
	// error, because a cancellation that raced a completion must not fail.
	Cancel(context.Context, WorkloadEnvelope, string) error
}

// WorkItem is the payload orchestration enqueues for one stage attempt.
//
// It carries identity only. The envelope, its ticket, and the stage spec are
// re-read from durable state when the item is claimed, because a queue payload
// is not authority: an item that sat in the queue while the run was cancelled
// must not be executed from its own stale copy of the plan.
type WorkItem struct {
	RunID   string `json:"run_id"`
	JobID   string `json:"job_id"`
	StageID string `json:"stage_id"`
	Attempt uint32 `json:"attempt"`
}

func (item WorkItem) Validate() error {
	if err := validateID(item.RunID, "run", "run_id"); err != nil {
		return err
	}
	if err := validateID(item.JobID, "job", "job_id"); err != nil {
		return err
	}
	if err := validateID(item.StageID, "stage", "stage_id"); err != nil {
		return err
	}
	if item.Attempt == 0 {
		return invalid("work_item_attempt_invalid", "work item attempt must be positive", nil)
	}
	return nil
}

// EncodeWorkItem renders a work item as a queue payload.
func EncodeWorkItem(item WorkItem) (json.RawMessage, error) {
	if err := item.Validate(); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(item)
	if err != nil {
		return nil, invalid("work_item_encode_failed", "work item could not be encoded", err)
	}
	return payload, nil
}

// DecodeWorkItem parses and validates a queue payload. A malformed payload is
// invalid rather than retryable: replaying it would produce the same parse
// failure forever, so it belongs in the dead-letter path immediately.
func DecodeWorkItem(payload json.RawMessage) (WorkItem, error) {
	var item WorkItem
	if err := json.Unmarshal(payload, &item); err != nil {
		return WorkItem{}, invalid("work_item_decode_failed", "work item payload could not be decoded", err)
	}
	if err := item.Validate(); err != nil {
		return WorkItem{}, err
	}
	return item, nil
}

// StageHandler executes the domain meaning of one claimed work item. The
// composition root adapts it into a workqueue.Handler; this package never
// constructs a worker, a queue, or a goroutine.
type StageHandler interface {
	HandleStage(context.Context, WorkItem) error
}

// StageHandlerFunc adapts a function to StageHandler.
type StageHandlerFunc func(context.Context, WorkItem) error

func (function StageHandlerFunc) HandleStage(ctx context.Context, item WorkItem) error {
	return function(ctx, item)
}

// Handler adapts a StageHandler to the shared work queue.
//
// The returned fault's retry policy is what the worker reads to choose between
// retry and dead-letter, so Classify's decision is expressed as a policy rather
// than as a second control flow the queue would have to be taught about.
func Handler(handler StageHandler) (workqueue.HandlerFunc, error) {
	if handler == nil || nilInterface(handler) {
		return nil, unavailable("stage_handler_unavailable", "stage handler is not configured", nil)
	}
	return func(ctx context.Context, item workqueue.Item) (workqueue.Result, error) {
		decoded, err := DecodeWorkItem(item.Payload)
		if err != nil {
			return workqueue.Result{}, err
		}
		if err := handler.HandleStage(ctx, decoded); err != nil {
			return workqueue.Result{}, retryPolicyFor(err)
		}
		return workqueue.Result{}, nil
	}, nil
}

// retryPolicyFor restates a handler error with the retry policy its
// classification implies, leaving the original as the cause.
func retryPolicyFor(err error) error {
	reason := faults.ReasonOf(err)
	if reason == "" {
		reason = "stage_handler_failed"
	}
	switch Classify(err) {
	case DispositionRetry:
		return faults.Wrap(err, faults.CodeOf(err), faults.PublicMessageOf(err),
			faults.WithReason(reason), faults.WithOperation(operation),
			faults.WithRetryPolicy(faults.BackoffRetry(3)))
	case DispositionReschedule:
		// Rescheduled work waits for capacity rather than backing off against a
		// fault it did not cause.
		return faults.Wrap(err, faults.CodeOf(err), faults.PublicMessageOf(err),
			faults.WithReason(reason), faults.WithOperation(operation),
			faults.WithRetryPolicy(faults.DelayedRetry(rescheduleDelay, 0)))
	default:
		return faults.Wrap(err, faults.CodeOf(err), faults.PublicMessageOf(err),
			faults.WithReason(reason), faults.WithOperation(operation),
			faults.WithRetryPolicy(faults.NoRetry()))
	}
}

// rescheduleDelay is the pause before re-offering work that was refused for
// capacity. It is deliberately longer than a transient-fault backoff: the
// condition it waits on is another workload finishing, not a network blip.
const rescheduleDelay = 30 * time.Second
