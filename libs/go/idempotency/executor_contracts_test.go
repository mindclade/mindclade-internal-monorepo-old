// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package idempotency

import (
	"context"
	"errors"
	"testing"
	"time"

	mcclock "go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
)

type stubStore struct {
	acquire  func(context.Context, AcquireRequest) (Acquisition, error)
	complete func(context.Context, CompleteRequest) (Record, error)
	release  func(context.Context, ReleaseRequest) error
	renew    func(context.Context, RenewRequest) (Lease, error)
	lookup   func(context.Context, Identity) (Record, error)
}

func (store *stubStore) Acquire(ctx context.Context, request AcquireRequest) (Acquisition, error) {
	if store.acquire == nil {
		return Acquisition{}, errors.New("unexpected Acquire")
	}
	return store.acquire(ctx, request)
}
func (store *stubStore) Complete(ctx context.Context, request CompleteRequest) (Record, error) {
	if store.complete == nil {
		return Record{}, errors.New("unexpected Complete")
	}
	return store.complete(ctx, request)
}
func (store *stubStore) Release(ctx context.Context, request ReleaseRequest) error {
	if store.release == nil {
		return errors.New("unexpected Release")
	}
	return store.release(ctx, request)
}
func (store *stubStore) Renew(ctx context.Context, request RenewRequest) (Lease, error) {
	if store.renew == nil {
		return Lease{}, errors.New("unexpected Renew")
	}
	return store.renew(ctx, request)
}
func (store *stubStore) Lookup(ctx context.Context, identity Identity) (Record, error) {
	if store.lookup == nil {
		return Record{}, errors.New("unexpected Lookup")
	}
	return store.lookup(ctx, identity)
}

func acquiredFixture(t *testing.T) (AcquireRequest, Acquisition) {
	t.Helper()
	record, err := NewRecord(testRecordData(t, StateInProgress))
	if err != nil {
		t.Fatal(err)
	}
	lease := Lease{
		RecordID:    record.ID(),
		Identity:    record.Identity(),
		Fingerprint: record.Fingerprint(),
		Token:       identifiers.MustParseUUID("018f3f4a-5b6c-4d8e-8f90-0123456789ab"),
		ExpiresAt:   record.LeaseExpiresAt(),
		Version:     record.Version(),
	}
	request := AcquireRequest{
		Identity:      record.Identity(),
		Fingerprint:   record.Fingerprint(),
		TTL:           time.Hour,
		LeaseDuration: time.Minute,
	}
	return request, Acquisition{Disposition: DispositionAcquired, Record: record, Lease: lease}
}

func completedFrom(t *testing.T, acquisition Acquisition, result Result) Record {
	t.Helper()
	data := acquisition.Record.Data()
	data.State = StateCompleted
	data.Result = result
	data.UpdatedAt = data.UpdatedAt.Add(time.Second)
	data.LeaseExpiresAt = time.Time{}
	data.Version++
	record, err := NewRecord(data)
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func TestExecutorInProgressAndAcquireErrors(t *testing.T) {
	t.Parallel()

	request, acquisition := acquiredFixture(t)
	clock := mcclock.NewFake(acquisition.Record.CreatedAt())
	inProgressStore := &stubStore{acquire: func(context.Context, AcquireRequest) (Acquisition, error) {
		return Acquisition{Disposition: DispositionInProgress, Record: acquisition.Record}, nil
	}}
	executor, err := NewExecutor(inProgressStore, WithClock(clock))
	if err != nil {
		t.Fatal(err)
	}
	called := false
	_, err = executor.Execute(context.Background(), request, func(context.Context) (Result, error) {
		called = true
		return EmptyResult()
	})
	if called || !errors.Is(err, ErrInProgress) || !faults.IsReason(err, ReasonInProgress) || !faults.IsRetryable(err) {
		t.Fatalf("in-progress result called=%v err=%v", called, err)
	}

	providerFailure := faults.New(
		faults.CodeUnavailable,
		"store offline",
		faults.WithReason("postgres_unavailable"),
		faults.WithField("region", "us-central1"),
		faults.WithRetryPolicy(faults.BackoffRetry(3)),
	)
	failureStore := &stubStore{acquire: func(context.Context, AcquireRequest) (Acquisition, error) {
		return Acquisition{}, providerFailure
	}}
	executor, _ = NewExecutor(failureStore)
	_, err = executor.Execute(context.Background(), request, func(context.Context) (Result, error) { return EmptyResult() })
	if !faults.IsCode(err, faults.CodeUnavailable) || !faults.IsReason(err, "postgres_unavailable") || faults.FieldsOf(err)["region"] != "us-central1" || !faults.IsRetryable(err) {
		t.Fatalf("qualified store error = %v", err)
	}
}

func TestExecutorRejectsInvalidAcquisition(t *testing.T) {
	t.Parallel()

	request, _ := acquiredFixture(t)
	store := &stubStore{acquire: func(context.Context, AcquireRequest) (Acquisition, error) {
		return Acquisition{}, nil
	}}
	executor, _ := NewExecutor(store)
	_, err := executor.Execute(context.Background(), request, func(context.Context) (Result, error) { return EmptyResult() })
	if !faults.IsCode(err, faults.CodeInternal) || !faults.IsReason(err, ReasonStoreFailed) {
		t.Fatalf("invalid acquisition error = %v", err)
	}
}

func TestExecutorInvalidResultReleasesAndClassifiesInternal(t *testing.T) {
	t.Parallel()

	request, acquisition := acquiredFixture(t)
	released := false
	store := &stubStore{
		acquire: func(context.Context, AcquireRequest) (Acquisition, error) { return acquisition, nil },
		release: func(ctx context.Context, release ReleaseRequest) error {
			released = true
			if ctx.Err() != nil || release.Lease != acquisition.Lease {
				t.Fatalf("release context/lease = %v, %#v", ctx.Err(), release.Lease)
			}
			return nil
		},
	}
	executor, _ := NewExecutor(store)
	execution, err := executor.Execute(context.Background(), request, func(context.Context) (Result, error) {
		return Result{}, nil
	})
	if !released || !execution.Executed() || execution.Committed() || !faults.IsCode(err, faults.CodeInternal) || !faults.IsReason(err, ReasonInvalidOperationResult) || !errors.Is(err, ErrInvalidResult) {
		t.Fatalf("invalid-result execution=%#v released=%v err=%v", execution, released, err)
	}
}

func TestExecutorCommitFailureIsNotRetryable(t *testing.T) {
	t.Parallel()

	request, acquisition := acquiredFixture(t)
	result, _ := NewResult([]byte("created"), "application/json", nil)
	store := &stubStore{
		acquire: func(context.Context, AcquireRequest) (Acquisition, error) { return acquisition, nil },
		complete: func(ctx context.Context, complete CompleteRequest) (Record, error) {
			if ctx.Err() != nil || complete.Lease != acquisition.Lease {
				t.Fatalf("complete context/lease = %v, %#v", ctx.Err(), complete.Lease)
			}
			return Record{}, errors.New("commit unavailable")
		},
	}
	executor, _ := NewExecutor(store)
	execution, err := executor.Execute(context.Background(), request, func(context.Context) (Result, error) { return result, nil })
	if !execution.Executed() || execution.Committed() || !errors.Is(err, ErrCommitFailed) || !faults.IsReason(err, ReasonCommitFailed) || faults.IsRetryable(err) {
		t.Fatalf("commit-failure execution=%#v err=%v", execution, err)
	}
}

func TestExecutorFinalizesAfterInboundCancellation(t *testing.T) {
	t.Parallel()

	request, acquisition := acquiredFixture(t)
	result, _ := NewResult([]byte("created"), "", nil)
	ctx, cancel := context.WithCancel(context.Background())
	store := &stubStore{
		acquire: func(context.Context, AcquireRequest) (Acquisition, error) { return acquisition, nil },
		complete: func(finalizeCtx context.Context, complete CompleteRequest) (Record, error) {
			if finalizeCtx.Err() != nil {
				t.Fatalf("finalization inherited cancellation: %v", finalizeCtx.Err())
			}
			return completedFrom(t, acquisition, complete.Result), nil
		},
	}
	executor, _ := NewExecutor(store, WithFinalizationTimeout(time.Second))
	execution, err := executor.Execute(ctx, request, func(context.Context) (Result, error) {
		cancel()
		return result, nil
	})
	if err != nil || !execution.Committed() || execution.Record().State() != StateCompleted {
		t.Fatalf("execution=%#v err=%v", execution, err)
	}
}

func TestExecutorOperationAndReleaseFailure(t *testing.T) {
	t.Parallel()

	request, acquisition := acquiredFixture(t)
	operationFailure := faults.New(faults.CodeUnavailable, "backend unavailable", faults.WithReason("backend_unavailable"), faults.WithRetryPolicy(faults.BackoffRetry(2)))
	store := &stubStore{
		acquire: func(context.Context, AcquireRequest) (Acquisition, error) { return acquisition, nil },
		release: func(context.Context, ReleaseRequest) error { return errors.New("release failed") },
	}
	executor, _ := NewExecutor(store)
	_, err := executor.Execute(context.Background(), request, func(context.Context) (Result, error) { return Result{}, operationFailure })
	if !errors.Is(err, operationFailure) || faults.FieldsOf(err)["idempotency_release_failed"] != true || !faults.IsRetryable(err) {
		t.Fatalf("operation/release error = %v", err)
	}
}

func TestExecutorConstructionValidation(t *testing.T) {
	t.Parallel()

	var nilStore *stubStore
	if _, err := NewExecutor(nilStore); !errors.Is(err, ErrNilStore) {
		t.Fatalf("typed nil store error = %v", err)
	}
	store := &stubStore{}
	if _, err := NewExecutor(store, WithFinalizationTimeout(0)); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("zero finalization timeout error = %v", err)
	}
	if _, err := NewExecutor(store, WithFinalizationTimeout(MaximumFinalizationTimeout+time.Second)); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("large finalization timeout error = %v", err)
	}
	var nilClock *mcclock.FakeClock
	if _, err := NewExecutor(store, WithClock(nilClock)); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("typed nil clock error = %v", err)
	}
}

func TestExecutorRejectsAcquisitionForDifferentRequest(t *testing.T) {
	t.Parallel()

	request, acquisition := acquiredFixture(t)
	otherIdentity, err := NewIdentity(MustParseScope("control-plane/models.create"), MustParseKey("request-654321"))
	if err != nil {
		t.Fatal(err)
	}
	request.Identity = otherIdentity
	store := &stubStore{acquire: func(context.Context, AcquireRequest) (Acquisition, error) {
		return acquisition, nil
	}}
	executor, err := NewExecutor(store)
	if err != nil {
		t.Fatal(err)
	}
	_, err = executor.Execute(context.Background(), request, func(context.Context) (Result, error) {
		return EmptyResult()
	})
	if !faults.IsCode(err, faults.CodeInternal) || !faults.IsReason(err, ReasonStoreFailed) {
		t.Fatalf("wrong-request acquisition error = %v", err)
	}
}

func TestExecutorRejectsMismatchedCompletedRecord(t *testing.T) {
	t.Parallel()

	request, acquisition := acquiredFixture(t)
	result, err := NewResult([]byte("created"), "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	wrongResult, err := NewResult([]byte("different"), "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	store := &stubStore{
		acquire: func(context.Context, AcquireRequest) (Acquisition, error) { return acquisition, nil },
		complete: func(context.Context, CompleteRequest) (Record, error) {
			return completedFrom(t, acquisition, wrongResult), nil
		},
	}
	executor, err := NewExecutor(store)
	if err != nil {
		t.Fatal(err)
	}
	execution, err := executor.Execute(context.Background(), request, func(context.Context) (Result, error) {
		return result, nil
	})
	if !execution.Executed() || execution.Committed() || !faults.IsCode(err, faults.CodeInternal) ||
		!faults.IsReason(err, ReasonStoreFailed) || faults.FieldsOf(err)["operation_completed"] != true {
		t.Fatalf("mismatched completion execution=%#v error=%v", execution, err)
	}
}
