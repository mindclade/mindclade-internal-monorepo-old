// Copyright 2026 Mindclade. All rights reserved.
package inbox

import (
	"context"
	"database/sql"
	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/storage/sql/transaction"
)

type Runner interface {
	Within(context.Context, func(context.Context) error) error
}
type RunnerFunc func(context.Context, func(context.Context) error) error

func (function RunnerFunc) Within(ctx context.Context, work func(context.Context) error) error {
	return function(ctx, work)
}

type SQLRunner struct {
	Beginner transaction.Beginner
	Options  transaction.Options
}

func (runner SQLRunner) Within(ctx context.Context, work func(context.Context) error) error {
	if runner.Beginner == nil || work == nil {
		return faults.Wrap(ErrInvalidRequest, faults.CodeInvalidArgument, "invalid inbox transaction runner", faults.WithReason("invalid_inbox_runner"), faults.WithOperation("inbox.SQLRunner.Within"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return transaction.RunVoid(ctx, runner.Beginner, runner.Options, func(txctx context.Context, _ *sql.Tx) error { return work(txctx) })
}
