// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package transaction

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"reflect"

	"go.mindclade.dev/libs/go/faults"
)

type Beginner interface {
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
}
type Func[T any] func(context.Context, *sql.Tx) (T, error)

type Options struct {
	Isolation sql.IsolationLevel
	ReadOnly  bool
}

func (options Options) txOptions() *sql.TxOptions {
	return &sql.TxOptions{Isolation: options.Isolation, ReadOnly: options.ReadOnly}
}

func Run[T any](ctx context.Context, beginner Beginner, options Options, function Func[T]) (result T, err error) {
	const operation = "storage.sql.transaction.Run"
	if ctx == nil {
		return result, invalid(ctx, ErrInvalidRequest, operation)
	}
	if nilInterface(beginner) || function == nil {
		return result, invalid(ctx, ErrInvalidRequest, operation)
	}
	if _, ok := FromContext(ctx); ok {
		return result, invalid(ctx, ErrNested, operation)
	}
	if cause := ctx.Err(); cause != nil {
		return result, provider(ctx, cause, operation)
	}
	tx, beginErr := beginner.BeginTx(ctx, options.txOptions())
	if beginErr != nil {
		return result, provider(ctx, errors.Join(ErrBegin, beginErr), operation)
	}
	txContext, contextErr := ContextWithTx(ctx, tx)
	if contextErr != nil {
		_ = tx.Rollback()
		return result, contextErr
	}
	committed := false
	defer func() {
		if recovered := recover(); recovered != nil {
			_ = tx.Rollback()
			panic(recovered)
		}
		if !committed && err != nil {
			if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
				err = errors.Join(err, faults.Wrap(errors.Join(ErrRollback, rollbackErr), faults.CodeInternal, "database transaction rollback failed", faults.WithReason("sql_transaction_rollback_failed"), faults.WithOperation(operation), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry())))
			}
		}
	}()
	result, err = function(txContext, tx)
	if err != nil {
		return result, err
	}
	if commitErr := tx.Commit(); commitErr != nil {
		return result, provider(ctx, errors.Join(ErrCommit, commitErr), operation)
	}
	committed = true
	return result, nil
}

func RunVoid(ctx context.Context, beginner Beginner, options Options, function func(context.Context, *sql.Tx) error) error {
	if function == nil {
		return invalid(ctx, ErrInvalidRequest, "storage.sql.transaction.RunVoid")
	}
	_, err := Run(ctx, beginner, options, func(ctx context.Context, tx *sql.Tx) (struct{}, error) { return struct{}{}, function(ctx, tx) })
	return err
}

func invalid(ctx context.Context, cause error, operation string) error {
	return faults.Wrap(cause, faults.CodeInvalidArgument, "invalid database transaction request", faults.WithReason("sql_transaction_invalid_request"), faults.WithOperation(operation), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
}
func provider(ctx context.Context, cause error, operation string) error {
	code := faults.CodeUnavailable
	retryPolicy := faults.BackoffRetry(3)
	if errors.Is(cause, context.Canceled) {
		code = faults.CodeCanceled
		retryPolicy = faults.NoRetry()
	} else if errors.Is(cause, context.DeadlineExceeded) {
		code = faults.CodeDeadlineExceeded
		retryPolicy = faults.NoRetry()
	} else if !errors.Is(cause, driver.ErrBadConn) {
		code = faults.CodeAborted
		retryPolicy = faults.NoRetry()
	}
	return faults.Wrap(cause, code, "database transaction failed", faults.WithReason("sql_transaction_failed"), faults.WithOperation(operation), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(retryPolicy))
}
func nilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
