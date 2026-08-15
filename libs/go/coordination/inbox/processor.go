// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package inbox

import (
	"context"
	"errors"
	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/idempotency"
	"mindclade.internal/libs/go/identifiers"
	"mindclade.internal/libs/go/requestmeta"
	"reflect"
	"time"
)

type Message struct {
	Identity      idempotency.Identity
	Fingerprint   identifiers.Digest
	RequestID     requestmeta.RequestID
	TTL           time.Duration
	LeaseDuration time.Duration
}

func (message Message) Validate() error {
	request := idempotency.AcquireRequest{Identity: message.Identity, Fingerprint: message.Fingerprint, RequestID: message.RequestID, TTL: message.TTL, LeaseDuration: message.LeaseDuration}
	return request.Validate()
}

type Handler func(context.Context) (idempotency.Result, error)
type Outcome struct {
	Processed bool
	Duplicate bool
	Record    idempotency.Record
	Result    idempotency.Result
}
type Processor struct {
	runner Runner
	store  idempotency.Store
}

func New(runner Runner, store idempotency.Store) (*Processor, error) {
	if nilValue(runner) || nilValue(store) {
		return nil, faults.Wrap(ErrInvalidRequest, faults.CodeInvalidArgument, "inbox runner and idempotency store are required", faults.WithReason("missing_inbox_dependencies"), faults.WithOperation("inbox.New"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return &Processor{runner: runner, store: store}, nil
}
func (processor *Processor) Process(ctx context.Context, message Message, handler Handler) (Outcome, error) {
	if ctx == nil || processor == nil || handler == nil {
		return Outcome{}, faults.Wrap(ErrInvalidRequest, faults.CodeInvalidArgument, "invalid inbox process request", faults.WithReason("invalid_inbox_request"), faults.WithOperation("inbox.Processor.Process"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	if err := message.Validate(); err != nil {
		return Outcome{}, err
	}
	var outcome Outcome
	err := processor.runner.Within(ctx, func(txctx context.Context) error {
		request := idempotency.AcquireRequest{Identity: message.Identity, Fingerprint: message.Fingerprint, RequestID: message.RequestID, TTL: message.TTL, LeaseDuration: message.LeaseDuration}.Normalized()
		acquisition, err := processor.store.Acquire(txctx, request)
		if err != nil {
			return err
		}
		switch acquisition.Disposition {
		case idempotency.DispositionReplay:
			outcome = Outcome{Duplicate: true, Record: acquisition.Record, Result: acquisition.Record.Result()}
			return nil
		case idempotency.DispositionConflict:
			return faults.Wrap(errors.Join(ErrConflict, idempotency.ErrKeyConflict), faults.CodeConflict, "inbox identity conflicts with a different payload", faults.WithReason("inbox_conflict"), faults.WithOperation("inbox.Processor.Process"), faults.WithRetryPolicy(faults.NoRetry()))
		case idempotency.DispositionInProgress:
			return faults.Wrap(errors.Join(ErrInProgress, idempotency.ErrInProgress), faults.CodeConflict, "inbox message is already being processed", faults.WithReason("inbox_in_progress"), faults.WithOperation("inbox.Processor.Process"), faults.WithRetryPolicy(faults.ImmediateRetry(3)))
		case idempotency.DispositionAcquired:
		default:
			return faults.Wrap(ErrInvalidRequest, faults.CodeInternal, "idempotency store returned an invalid inbox disposition", faults.WithReason("invalid_inbox_store_disposition"), faults.WithOperation("inbox.Processor.Process"))
		}
		result, err := handler(txctx)
		if err != nil {
			return err
		}
		if err = result.Validate(); err != nil {
			return err
		}
		completed, err := processor.store.Complete(txctx, idempotency.CompleteRequest{Lease: acquisition.Lease, Result: result})
		if err != nil {
			return err
		}
		outcome = Outcome{Processed: true, Record: completed, Result: result}
		return nil
	})
	if err != nil {
		return Outcome{}, faults.Wrap(errors.Join(ErrTransaction, err), faults.CodeOf(err), faults.PublicMessageOf(err), faults.WithReason(defaultReason(err)), faults.WithOperation("inbox.Processor.Process"), faults.WithFields(faults.FieldsOf(err)), faults.WithRetryPolicy(faults.RetryPolicyOf(err)), faults.WithContextMetadata(ctx))
	}
	return outcome, nil
}
func defaultReason(err error) string {
	if value := faults.ReasonOf(err); value != "" {
		return value
	}
	return "inbox_transaction_failed"
}
func nilValue(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	}
	return false
}
