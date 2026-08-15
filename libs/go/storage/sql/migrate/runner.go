// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package migrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"hash/fnv"
	"time"

	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/servicekit"
	"go.mindclade.dev/libs/go/storage/sql/postgres"
)

const DefaultTable = "mindclade_schema_migrations"

type Options struct {
	Table          string
	AdvisoryLockID int64
	UnlockTimeout  time.Duration
}

func (options Options) normalized() (Options, error) {
	if options.Table == "" {
		options.Table = DefaultTable
	}
	table, err := postgres.QualifiedIdentifier(options.Table)
	if err != nil {
		return Options{}, err
	}
	options.Table = table
	if options.AdvisoryLockID == 0 {
		hasher := fnv.New64a()
		_, _ = hasher.Write([]byte(table))
		options.AdvisoryLockID = int64(hasher.Sum64() & 0x7fffffffffffffff)
	}
	if options.UnlockTimeout == 0 {
		options.UnlockTimeout = 5 * time.Second
	}
	if options.UnlockTimeout < 0 {
		return Options{}, invalid(ErrInvalidMigration, "invalid_unlock_timeout", nil)
	}
	return options, nil
}

type Runner struct {
	manifest Manifest
	options  Options
}

func NewRunner(manifest Manifest, options Options) (*Runner, error) {
	normalized, err := options.normalized()
	if err != nil {
		return nil, err
	}
	return &Runner{manifest: manifest, options: normalized}, nil
}

func (runner *Runner) Apply(ctx context.Context, db *sql.DB) (Plan, error) {
	const operation = "storage.sql.migrate.Runner.Apply"
	if ctx == nil || db == nil || runner == nil {
		return Plan{}, invalid(ErrInvalidMigration, "invalid_apply_request", nil)
	}
	connection, err := db.Conn(ctx)
	if err != nil {
		return Plan{}, failed(errors.Join(ErrApplyFailed, err), "migration_connection_failed", operation, nil)
	}
	defer connection.Close()
	if _, err := connection.ExecContext(ctx, "SELECT pg_advisory_lock($1)", runner.options.AdvisoryLockID); err != nil {
		return Plan{}, failed(errors.Join(ErrLockFailed, err), "migration_lock_failed", operation, nil)
	}
	defer runner.unlock(connection)
	if err := runner.ensureTable(ctx, connection); err != nil {
		return Plan{}, err
	}
	plan, err := runner.plan(ctx, connection)
	if err != nil {
		return Plan{}, err
	}
	applied := append([]Applied(nil), plan.Applied...)
	for _, migration := range plan.Pending {
		receipt, applyErr := runner.applyOne(ctx, connection, migration)
		if applyErr != nil {
			return Plan{Applied: applied, Pending: append([]Migration(nil), plan.Pending...)}, applyErr
		}
		applied = append(applied, receipt)
	}
	return Plan{Applied: applied}, nil
}

func (runner *Runner) Plan(ctx context.Context, db *sql.DB) (Plan, error) {
	if ctx == nil || db == nil || runner == nil {
		return Plan{}, invalid(ErrInvalidMigration, "invalid_plan_request", nil)
	}
	connection, err := db.Conn(ctx)
	if err != nil {
		return Plan{}, failed(errors.Join(ErrApplyFailed, err), "migration_connection_failed", "storage.sql.migrate.Runner.Plan", nil)
	}
	defer connection.Close()
	if err := runner.ensureTable(ctx, connection); err != nil {
		return Plan{}, err
	}
	return runner.plan(ctx, connection)
}

func (runner *Runner) ensureTable(ctx context.Context, connection *sql.Conn) error {
	query := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
        version BIGINT PRIMARY KEY CHECK (version > 0),
        name TEXT NOT NULL,
        checksum CHAR(64) NOT NULL,
        applied_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
    )`, runner.options.Table)
	if _, err := connection.ExecContext(ctx, query); err != nil {
		return failed(errors.Join(ErrApplyFailed, err), "migration_table_failed", "storage.sql.migrate.Runner.EnsureTable", nil)
	}
	return nil
}

func (runner *Runner) plan(ctx context.Context, connection *sql.Conn) (Plan, error) {
	query := fmt.Sprintf("SELECT version, name, checksum, applied_at FROM %s ORDER BY version", runner.options.Table)
	rows, err := connection.QueryContext(ctx, query)
	if err != nil {
		return Plan{}, failed(errors.Join(ErrApplyFailed, err), "migration_query_failed", "storage.sql.migrate.Runner.Plan", nil)
	}
	defer rows.Close()
	var plan Plan
	appliedVersions := make(map[uint64]struct{})
	for rows.Next() {
		var receipt Applied
		if err := rows.Scan(&receipt.Version, &receipt.Name, &receipt.Checksum, &receipt.AppliedAt); err != nil {
			return Plan{}, failed(errors.Join(ErrApplyFailed, err), "migration_scan_failed", "storage.sql.migrate.Runner.Plan", nil)
		}
		expected, exists := runner.manifest.lookup(receipt.Version)
		if !exists {
			return Plan{}, failed(ErrUnknownApplied, "unknown_applied_migration", "storage.sql.migrate.Runner.Plan", faults.Fields{"version": receipt.Version})
		}
		if expected.Name != receipt.Name || expected.Checksum() != receipt.Checksum {
			return Plan{}, failed(ErrChecksumMismatch, "migration_checksum_mismatch", "storage.sql.migrate.Runner.Plan", faults.Fields{"version": receipt.Version, "name": receipt.Name})
		}
		appliedVersions[receipt.Version] = struct{}{}
		plan.Applied = append(plan.Applied, receipt)
	}
	if err := rows.Err(); err != nil {
		return Plan{}, failed(errors.Join(ErrApplyFailed, err), "migration_rows_failed", "storage.sql.migrate.Runner.Plan", nil)
	}
	for _, migration := range runner.manifest.migrations {
		if _, exists := appliedVersions[migration.Version]; !exists {
			plan.Pending = append(plan.Pending, migration)
		}
	}
	return plan, nil
}

func (runner *Runner) applyOne(ctx context.Context, connection *sql.Conn, migration Migration) (Applied, error) {
	tx, err := connection.BeginTx(ctx, nil)
	if err != nil {
		return Applied{}, failed(errors.Join(ErrApplyFailed, err), "migration_begin_failed", "storage.sql.migrate.Runner.ApplyOne", faults.Fields{"version": migration.Version})
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if _, err := tx.ExecContext(ctx, migration.Up); err != nil {
		return Applied{}, failed(errors.Join(ErrApplyFailed, err), "migration_sql_failed", "storage.sql.migrate.Runner.ApplyOne", faults.Fields{"version": migration.Version, "name": migration.Name})
	}
	appliedAt := time.Now().UTC()
	insert := fmt.Sprintf("INSERT INTO %s (version, name, checksum, applied_at) VALUES ($1, $2, $3, $4)", runner.options.Table)
	if _, err := tx.ExecContext(ctx, insert, migration.Version, migration.Name, migration.Checksum(), appliedAt); err != nil {
		return Applied{}, failed(errors.Join(ErrApplyFailed, err), "migration_record_failed", "storage.sql.migrate.Runner.ApplyOne", faults.Fields{"version": migration.Version})
	}
	if err := tx.Commit(); err != nil {
		return Applied{}, failed(errors.Join(ErrApplyFailed, err), "migration_commit_failed", "storage.sql.migrate.Runner.ApplyOne", faults.Fields{"version": migration.Version})
	}
	committed = true
	return Applied{Version: migration.Version, Name: migration.Name, Checksum: migration.Checksum(), AppliedAt: appliedAt}, nil
}

func (runner *Runner) unlock(connection *sql.Conn) {
	if connection == nil {
		return
	}
	ctx := context.Background()
	cancel := func() {}
	if runner.options.UnlockTimeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, runner.options.UnlockTimeout)
	}
	defer cancel()
	_, _ = connection.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", runner.options.AdvisoryLockID)
}

// Component applies all pending migrations during service startup. It is a
// passive infrastructure component and therefore stops with no side effects.
func (runner *Runner) Component(name string, db *sql.DB) servicekit.Component {
	return servicekit.Component{Name: name, Start: func(ctx context.Context) error { _, err := runner.Apply(ctx, db); return err }}
}
