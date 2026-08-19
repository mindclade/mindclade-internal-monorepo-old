// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package providers

import (
	"database/sql"
	"slices"
	"strings"
	"time"

	mcclock "go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/coordination/outbox"
	outboxpostgres "go.mindclade.dev/libs/go/coordination/outbox/postgres"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
	sqlpostgres "go.mindclade.dev/libs/go/storage/sql/postgres"
	"go.mindclade.dev/libs/go/storage/sql/transaction"
	"go.mindclade.dev/services/control_plane/internal/config"
)

// Table names are named once and passed to both the store and its DDL. Each
// adapter accepts a table because one database may hold several; deriving the
// schema from the same constant is what keeps a migration and the queries that
// read it describing the same table.
const (
	AuditTable       = "mindclade_audit_events"
	IdempotencyTable = "mindclade_idempotency_records"
	OutboxTable      = "mindclade_outbox"
	LeaseTable       = "mindclade_leases"
	WorkQueueTable   = "mindclade_work_items"
	CursorTable      = "mindclade_cursors"
)

// Database holds the PostgreSQL pool and every store that is backed by it.
// One *sql.DB is shared: the adapters join a caller's transaction through
// storage/sql/transaction, which is only correct on a single pool.
type Database struct {
	DB           *sql.DB
	Pool         *sqlpostgres.Pool
	Transactions transaction.Beginner
	Outbox       outbox.Store
}

func NewDatabase(settings config.Settings, value mcclock.Clock, ids *identifiers.Generator) (Database, error) {
	db, err := openDatabase(settings)
	if err != nil {
		return Database{}, err
	}
	result, err := buildDatabase(db, settings, value, ids)
	if err != nil {
		_ = db.Close()
		return Database{}, err
	}
	return result, nil
}

func buildDatabase(db *sql.DB, settings config.Settings, value mcclock.Clock, ids *identifiers.Generator) (Database, error) {
	pool, err := sqlpostgres.NewPool(db, sqlpostgres.PoolConfig{
		MaxOpenConnections:    settings.DatabaseMaxOpen,
		MaxIdleConnections:    settings.DatabaseMaxIdle,
		ConnectionMaxLifetime: 30 * time.Minute,
		ConnectionMaxIdleTime: 5 * time.Minute,
	})
	if err != nil {
		return Database{}, err
	}
	messages, err := outboxpostgres.New(db, OutboxTable)
	if err != nil {
		return Database{}, err
	}
	result := Database{
		DB:           db,
		Pool:         pool,
		Transactions: db,
		Outbox:       messages,
	}
	return result, nil
}

// openDatabase resolves the configured driver against the drivers actually
// linked into this binary. A DSN alone is not enough: an unregistered driver
// name fails at first use rather than at startup, which is the wrong time.
func openDatabase(settings config.Settings) (*sql.DB, error) {
	driver := strings.TrimSpace(settings.DatabaseDriver)
	dsn := strings.TrimSpace(settings.DatabaseDSN)
	if dsn == "" {
		return nil, faults.New(
			faults.CodeFailedPrecondition,
			"control-plane database is not configured",
			faults.WithReason("database_dsn_not_configured"),
			faults.WithOperation("controlplane.providers.openDatabase"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	linked := sql.Drivers()
	if !slices.Contains(linked, driver) {
		return nil, faults.New(
			faults.CodeFailedPrecondition,
			"configured database driver is not linked into this binary",
			faults.WithReason("database_driver_not_linked"),
			faults.WithOperation("controlplane.providers.openDatabase"),
			faults.WithField("driver", driver),
			faults.WithField("linked_drivers", linked),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, faults.Wrap(err, faults.CodeUnavailable,
			"unable to open the control-plane database",
			faults.WithReason("database_open_failed"),
			faults.WithOperation("controlplane.providers.openDatabase"),
			faults.WithField("driver", driver),
		)
	}
	return db, nil
}
