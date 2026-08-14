// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package migrate

import (
	"errors"

	"mindclade.internal/libs/go/faults"
)

var (
	ErrInvalidMigration = errors.New("storage/sql/migrate: invalid migration")
	ErrChecksumMismatch = errors.New("storage/sql/migrate: checksum mismatch")
	ErrUnknownApplied   = errors.New("storage/sql/migrate: unknown applied migration")
	ErrApplyFailed      = errors.New("storage/sql/migrate: apply failed")
	ErrLockFailed       = errors.New("storage/sql/migrate: lock failed")
)

func invalid(cause error, reason string, fields faults.Fields) error {
	if cause == nil {
		cause = ErrInvalidMigration
	}
	return faults.Wrap(cause, faults.CodeInvalidArgument, "invalid schema migration",
		faults.WithReason(reason), faults.WithOperation("storage.sql.migrate.Validate"), faults.WithFields(fields), faults.WithRetryPolicy(faults.NoRetry()))
}

func failed(cause error, reason, operation string, fields faults.Fields) error {
	return faults.Wrap(cause, faults.CodeAborted, "database schema migration failed",
		faults.WithReason(reason), faults.WithOperation(operation), faults.WithFields(fields), faults.WithRetryPolicy(faults.NoRetry()))
}
