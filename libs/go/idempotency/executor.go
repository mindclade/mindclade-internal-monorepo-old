// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package idempotency

import (
	"context"
	"errors"
	"time"

	mcclock "mindclade.internal/libs/go/clock"
	"mindclade.internal/libs/go/faults"
)

const (
	DefaultFinalizationTimeout = 10 * time.Second
	MaximumFinalizationTimeout = time.Minute
)

// Operation produces an opaque successful result. Returning an error releases
// the acquired lease so a later request may retry. The operation must complete
// within the acquired lease or use Store directly to renew long-running work.
type Operation func(context.Context) (Result, error)

type Execution struct {
	result    Result
	record    Record
	replayed  bool
	executed  bool
	committed bool
}

func (execution Execution) Result() Result  { return execution.result }
func (execution Execution) Record() Record  { return execution.record }
func (execution Execution) Replayed() bool  { return execution.replayed }
func (execution Execution) Executed() bool  { return execution.executed }
func (execution Execution) Committed() bool { return execution.committed }

type Executor struct {
	store               Store
	clock               mcclock.Clock
	finalizationTimeout time.Duration
}
type ExecutorOption func(*Executor) error

func WithClock(clock mcclock.Clock) ExecutorOption {
	return func(executor *Executor) error {
		if nilInterface(clock) {
			return invalid(ErrInvalidRequest, ReasonInvalidRequest, "idempotency clock must not be nil", "idempotency.NewExecutor", nil)
		}
		executor.clock = clock
		return nil
	}
}

func WithFinalizationTimeout(timeout time.Duration) ExecutorOption {
	return func(executor *Executor) error {
		if timeout <= 0 || timeout > MaximumFinalizationTimeout {
			return invalid(ErrInvalidRequest, ReasonInvalidRequest, "invalid idempotency finalization timeout", "idempotency.NewExecutor", faults.Fields{"timeout": timeout.String()})
		}
		executor.finalizationTimeout = timeout
		return nil
	}
}
func NewExecutor(store Store, options ...ExecutorOption) (*Executor, error) {
	if nilInterface(store) {
		return nil, invalid(ErrNilStore, ReasonInvalidRequest, "idempotency store must not be nil", "idempotency.NewExecutor", nil)
	}
	executor := &Executor{
		store:               store,
		clock:               mcclock.RealClock{},
		finalizationTimeout: DefaultFinalizationTimeout,
	}
	for _, option := range options {
		if option != nil {
			if err := option(executor); err != nil {
				return nil, err
			}
		}
	}
	return executor, nil
}

func (executor *Executor) Execute(ctx context.Context, request AcquireRequest, operation Operation) (Execution, error) {
	if ctx == nil {
		return Execution{}, invalid(ErrNilContext, ReasonInvalidRequest, "context must not be nil", "idempotency.Executor.Execute", nil)
	}
	if executor == nil || nilInterface(executor.store) || nilInterface(executor.clock) || executor.finalizationTimeout <= 0 {
		return Execution{}, faults.Wrap(ErrNilStore, faults.CodeFailedPrecondition, "idempotency executor is not configured", faults.WithReason(ReasonStoreFailed), faults.WithOperation("idempotency.Executor.Execute"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	if operation == nil {
		return Execution{}, invalid(ErrNilOperation, ReasonInvalidRequest, "idempotent operation must not be nil", "idempotency.Executor.Execute", nil)
	}
	request = request.Normalized()
	if err := request.Validate(); err != nil {
		return Execution{}, err
	}
	acquisition, err := executor.store.Acquire(ctx, request)
	if err != nil {
		return Execution{}, qualifyStoreError(ctx, err, "acquire")
	}
	if err := acquisition.Validate(); err != nil {
		return Execution{}, invalidStoreContract(ctx, err, "idempotency store returned an invalid acquisition", false)
	}
	if err := validateAcquisitionForRequest(acquisition, request); err != nil {
		return Execution{}, invalidStoreContract(ctx, err, "idempotency store returned an acquisition for the wrong request", false)
	}
	switch acquisition.Disposition {
	case DispositionReplay:
		return Execution{result: acquisition.Record.Result(), record: acquisition.Record, replayed: true, committed: true}, nil
	case DispositionConflict:
		return Execution{}, faults.Wrap(ErrKeyConflict, faults.CodeConflict, "idempotency key was already used for a different request", faults.WithReason(ReasonKeyConflict), faults.WithOperation("idempotency.Executor.Execute"), faults.WithField("idempotency_scope", request.Identity.Scope.String()), faults.WithField("idempotency_key_digest", request.Identity.Digest().String()), faults.WithRetryPolicy(faults.NoRetry()), faults.WithContextMetadata(ctx))
	case DispositionInProgress:
		delay := acquisition.Record.LeaseExpiresAt().Sub(executor.clock.Now())
		if delay <= 0 {
			delay = time.Millisecond
		}
		return Execution{}, faults.Wrap(ErrInProgress, faults.CodeConflict, "an equivalent request is already in progress", faults.WithReason(ReasonInProgress), faults.WithOperation("idempotency.Executor.Execute"), faults.WithField("idempotency_record_id", acquisition.Record.ID().String()), faults.WithRetryPolicy(faults.DelayedRetry(delay, 0)), faults.WithContextMetadata(ctx))
	case DispositionAcquired:
	default:
		return Execution{}, faults.Wrap(ErrInvalidRequest, faults.CodeInternal, "idempotency store returned an unknown acquisition", faults.WithReason(ReasonStoreFailed), faults.WithOperation("idempotency.Executor.Execute"))
	}
	result, operationErr := operation(ctx)
	execution := Execution{result: result, record: acquisition.Record, executed: true}
	if operationErr != nil {
		finalizeCtx, cancel := executor.finalizationContext(ctx)
		releaseErr := executor.store.Release(finalizeCtx, ReleaseRequest{Lease: acquisition.Lease})
		cancel()
		if releaseErr == nil {
			return execution, operationErr
		}
		code := faults.CodeOf(operationErr)
		if code == faults.CodeUnknown {
			code = faults.CodeInternal
		}
		reason := faults.ReasonOf(operationErr)
		if reason == "" {
			reason = ReasonReleaseFailed
		}
		return execution, faults.Wrap(errors.Join(operationErr, releaseErr), code, faults.PublicMessageOf(operationErr), faults.WithReason(reason), faults.WithOperation("idempotency.Executor.Execute"), faults.WithFields(faults.FieldsOf(operationErr)), faults.WithField("idempotency_release_failed", true), faults.WithRetryPolicy(faults.RetryPolicyOf(operationErr)), faults.WithContextMetadata(ctx))
	}
	if err := result.Validate(); err != nil {
		finalizeCtx, cancel := executor.finalizationContext(ctx)
		releaseErr := executor.store.Release(finalizeCtx, ReleaseRequest{Lease: acquisition.Lease})
		cancel()
		cause := errors.Join(ErrInvalidResult, err)
		fields := faults.Fields{"idempotency_record_id": acquisition.Record.ID().String()}
		if releaseErr != nil {
			cause = errors.Join(cause, releaseErr)
			fields["idempotency_release_failed"] = true
		}
		return execution, faults.Wrap(
			cause,
			faults.CodeInternal,
			"idempotent operation returned an invalid result",
			faults.WithReason(ReasonInvalidOperationResult),
			faults.WithOperation("idempotency.Executor.Execute"),
			faults.WithFields(fields),
			faults.WithRetryPolicy(faults.NoRetry()),
			faults.WithContextMetadata(ctx),
		)
	}
	finalizeCtx, cancel := executor.finalizationContext(ctx)
	completed, err := executor.store.Complete(finalizeCtx, CompleteRequest{Lease: acquisition.Lease, Result: result})
	cancel()
	if err != nil {
		return execution, faults.Wrap(errors.Join(ErrCommitFailed, err), faults.CodeUnavailable, "operation completed but its idempotency result could not be committed", faults.WithReason(ReasonCommitFailed), faults.WithOperation("idempotency.Executor.Execute"), faults.WithField("operation_completed", true), faults.WithField("idempotency_record_id", acquisition.Record.ID().String()), faults.WithRetryPolicy(faults.NoRetry()), faults.WithContextMetadata(ctx))
	}
	if err := validateCompletedRecord(completed, acquisition.Record, result); err != nil {
		return execution, invalidStoreContract(ctx, err, "idempotency store returned an invalid completed record", true)
	}
	execution.record = completed
	execution.committed = true
	return execution, nil
}

func validateAcquisitionForRequest(acquisition Acquisition, request AcquireRequest) error {
	record := acquisition.Record
	if record.Identity() != request.Identity {
		return errors.Join(ErrInvalidRequest, errors.New("acquisition identity does not match request"))
	}
	fingerprintMatches := record.Fingerprint().Equal(request.Fingerprint)
	if acquisition.Disposition == DispositionConflict {
		if fingerprintMatches {
			return errors.Join(ErrInvalidRequest, errors.New("conflict acquisition has a matching fingerprint"))
		}
		return nil
	}
	if !fingerprintMatches {
		return errors.Join(ErrInvalidRequest, errors.New("acquisition fingerprint does not match request"))
	}
	return nil
}

func validateCompletedRecord(completed, acquired Record, result Result) error {
	if err := completed.Validate(); err != nil {
		return errors.Join(ErrInvalidRecord, err)
	}
	if completed.State() != StateCompleted ||
		completed.ID() != acquired.ID() ||
		completed.Identity() != acquired.Identity() ||
		!completed.Fingerprint().Equal(acquired.Fingerprint()) ||
		completed.RequestID() != acquired.RequestID() ||
		!completed.CreatedAt().Equal(acquired.CreatedAt()) ||
		!completed.ExpiresAt().Equal(acquired.ExpiresAt()) ||
		completed.UpdatedAt().Before(acquired.UpdatedAt()) ||
		completed.Version() <= acquired.Version() ||
		!completed.Result().Equal(result) {
		return errors.Join(ErrInvalidRecord, errors.New("completed record does not match the acquired operation"))
	}
	return nil
}

func invalidStoreContract(ctx context.Context, cause error, message string, operationCompleted bool) error {
	options := []faults.Option{
		faults.WithReason(ReasonStoreFailed),
		faults.WithOperation("idempotency.Executor.Execute"),
		faults.WithRetryPolicy(faults.NoRetry()),
		faults.WithContextMetadata(ctx),
	}
	if operationCompleted {
		options = append(options, faults.WithField("operation_completed", true))
	}
	return faults.Wrap(cause, faults.CodeInternal, message, options...)
}

func (executor *Executor) finalizationContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), executor.finalizationTimeout)
}

func qualifyStoreError(ctx context.Context, err error, action string) error {
	if err == nil {
		return nil
	}
	code := faults.CodeOf(err)
	if code == faults.CodeUnknown {
		code = faults.CodeUnavailable
	}
	reason := faults.ReasonOf(err)
	if reason == "" {
		reason = ReasonStoreFailed
	}
	retry := faults.RetryPolicyOf(err)
	if !retry.Specified() && code == faults.CodeUnavailable {
		retry = faults.BackoffRetry(0)
	}
	return faults.Wrap(
		err,
		code,
		"idempotency store operation failed",
		faults.WithReason(reason),
		faults.WithOperation("idempotency.store."+action),
		faults.WithFields(faults.FieldsOf(err)),
		faults.WithRetryPolicy(retry),
		faults.WithContextMetadata(ctx),
	)
}
