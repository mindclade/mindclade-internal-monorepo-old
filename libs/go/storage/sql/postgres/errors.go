// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"

	"go.mindclade.dev/libs/go/faults"
)

type sqlStateError interface{ SQLState() string }

// Qualify converts database and PostgreSQL failures into stable Mindclade
// faults while preserving the original error for errors.Is/errors.As callers.
func Qualify(ctx context.Context, err error, operation string) error {
	if err == nil {
		return nil
	}
	if faults.CodeOf(err) != faults.CodeUnknown {
		return err
	}
	if strings.TrimSpace(operation) == "" {
		operation = "storage.sql.postgres"
	}

	switch {
	case errors.Is(err, context.Canceled):
		return faults.Wrap(err, faults.CodeCanceled, "database operation canceled",
			faults.WithReason("database_operation_canceled"),
			faults.WithOperation(operation),
			faults.WithContextMetadata(ctx),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	case errors.Is(err, context.DeadlineExceeded):
		return faults.Wrap(err, faults.CodeDeadlineExceeded, "database operation deadline exceeded",
			faults.WithReason("database_deadline_exceeded"),
			faults.WithOperation(operation),
			faults.WithContextMetadata(ctx),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	case errors.Is(err, sql.ErrNoRows):
		return faults.Wrap(err, faults.CodeNotFound, "database record not found",
			faults.WithReason("database_record_not_found"),
			faults.WithOperation(operation),
			faults.WithContextMetadata(ctx),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	case errors.Is(err, driver.ErrBadConn):
		return faults.Wrap(err, faults.CodeUnavailable, "database connection is unavailable",
			faults.WithReason("database_bad_connection"),
			faults.WithOperation(operation),
			faults.WithContextMetadata(ctx),
			faults.WithRetryPolicy(faults.BackoffRetry(5)),
		)
	}

	var state sqlStateError
	if !errors.As(err, &state) {
		return faults.Wrap(err, faults.CodeUnavailable, "database operation failed",
			faults.WithReason("database_unavailable"),
			faults.WithOperation(operation),
			faults.WithContextMetadata(ctx),
			faults.WithRetryPolicy(faults.BackoffRetry(5)),
		)
	}

	sqlState := strings.ToUpper(strings.TrimSpace(state.SQLState()))
	code, message, reason, policy := classifySQLState(ctx, sqlState)
	return faults.Wrap(err, code, message,
		faults.WithReason(reason),
		faults.WithOperation(operation),
		faults.WithField("postgres_sqlstate", sqlState),
		faults.WithContextMetadata(ctx),
		faults.WithRetryPolicy(policy),
	)
}

func classifySQLState(ctx context.Context, state string) (faults.Code, string, string, faults.RetryPolicy) {
	switch state {
	case "23505":
		return faults.CodeAlreadyExists, "database record already exists", "postgres_unique_violation", faults.NoRetry()
	case "23503":
		return faults.CodeFailedPrecondition, "database relationship constraint failed", "postgres_foreign_key_violation", faults.NoRetry()
	case "23502", "22001", "22003", "22P02":
		return faults.CodeInvalidArgument, "database value is invalid", "postgres_invalid_value", faults.NoRetry()
	case "42501":
		return faults.CodePermissionDenied, "database operation is not permitted", "postgres_insufficient_privilege", faults.NoRetry()
	case "28P01", "28000":
		return faults.CodeUnauthenticated, "database authentication failed", "postgres_authentication_failed", faults.NoRetry()
	case "40001", "40P01":
		return faults.CodeAborted, "database transaction was aborted", "postgres_transaction_retryable", faults.BackoffRetry(5)
	case "55P03":
		return faults.CodeUnavailable, "database lock is unavailable", "postgres_lock_unavailable", faults.BackoffRetry(5)
	case "53300", "53400":
		return faults.CodeResourceExhausted, "database resources are exhausted", "postgres_resource_exhausted", faults.BackoffRetry(5)
	case "57014":
		if ctx != nil {
			switch {
			case errors.Is(ctx.Err(), context.Canceled):
				return faults.CodeCanceled, "database operation canceled", "database_operation_canceled", faults.NoRetry()
			case errors.Is(ctx.Err(), context.DeadlineExceeded):
				return faults.CodeDeadlineExceeded, "database operation deadline exceeded", "database_deadline_exceeded", faults.NoRetry()
			}
		}
		return faults.CodeAborted, "database statement was canceled", "postgres_query_canceled", faults.NoRetry()
	case "57P01", "57P02", "57P03":
		return faults.CodeUnavailable, "database is unavailable", "postgres_unavailable", faults.BackoffRetry(5)
	}

	switch {
	case strings.HasPrefix(state, "08"):
		return faults.CodeUnavailable, "database connection is unavailable", "postgres_connection_exception", faults.BackoffRetry(5)
	case strings.HasPrefix(state, "40"):
		return faults.CodeAborted, "database transaction was aborted", "postgres_transaction_retryable", faults.BackoffRetry(5)
	case strings.HasPrefix(state, "53"):
		return faults.CodeResourceExhausted, "database resources are exhausted", "postgres_resource_exhausted", faults.BackoffRetry(5)
	case strings.HasPrefix(state, "22"):
		return faults.CodeInvalidArgument, "database value is invalid", "postgres_invalid_value", faults.NoRetry()
	case strings.HasPrefix(state, "23"):
		return faults.CodeFailedPrecondition, "database integrity constraint failed", "postgres_integrity_constraint", faults.NoRetry()
	default:
		return faults.CodeInternal, "database operation failed", "postgres_error", faults.NoRetry()
	}
}
