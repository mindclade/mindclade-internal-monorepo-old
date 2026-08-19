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

	"go.mindclade.dev/libs/go/audit"
	auditpostgres "go.mindclade.dev/libs/go/audit/postgres"
	mcclock "go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/coordination/outbox"
	outboxpostgres "go.mindclade.dev/libs/go/coordination/outbox/postgres"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/idempotency"
	idempotencypostgres "go.mindclade.dev/libs/go/idempotency/postgres"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/storage/sql/migrate"
	sqlpostgres "go.mindclade.dev/libs/go/storage/sql/postgres"
	"go.mindclade.dev/libs/go/storage/sql/transaction"
	"go.mindclade.dev/services/control_plane/internal/config"
)

// outboxTable is the table created by the outbox adapter's own schema. The
// composition root names it because the adapter supports several tables in one
// database; it must agree with the DDL applied by the migration runner.
const outboxTable = "mindclade_outbox"

// foundationMigrations is the forward-only order in which the shared adapter
// schemas are applied. Versions are owned here, not by libs/go, because one
// database holds every adapter's tables and the ordering must be global.
const (
	migrationAudit uint64 = iota + 1
	migrationIdempotency
	migrationOutbox
)

// database holds the PostgreSQL pool and every store that is backed by it.
// One *sql.DB is shared: the adapters join a caller's transaction through
// storage/sql/transaction, which is only correct on a single pool.
type database struct {
	db           *sql.DB
	pool         *sqlpostgres.Pool
	migrations   *migrate.Runner
	transactions transaction.Beginner
	audit        audit.Recorder
	idempotency  idempotency.Store
	outbox       outbox.Store
}

func newDatabase(settings config.Settings, value mcclock.Clock, ids *identifiers.Generator) (database, error) {
	db, err := openDatabase(settings)
	if err != nil {
		return database{}, err
	}
	result, err := buildDatabase(db, settings, value, ids)
	if err != nil {
		_ = db.Close()
		return database{}, err
	}
	return result, nil
}

func buildDatabase(db *sql.DB, settings config.Settings, value mcclock.Clock, ids *identifiers.Generator) (database, error) {
	pool, err := sqlpostgres.NewPool(db, sqlpostgres.PoolConfig{
		MaxOpenConnections:    settings.DatabaseMaxOpen,
		MaxIdleConnections:    settings.DatabaseMaxIdle,
		ConnectionMaxLifetime: 30 * time.Minute,
		ConnectionMaxIdleTime: 5 * time.Minute,
	})
	if err != nil {
		return database{}, err
	}
	recorder, err := auditpostgres.New(db)
	if err != nil {
		return database{}, err
	}
	records, err := idempotencypostgres.New(db,
		idempotencypostgres.WithClock(value),
		idempotencypostgres.WithGenerator(ids),
	)
	if err != nil {
		return database{}, err
	}
	messages, err := outboxpostgres.New(db, outboxTable)
	if err != nil {
		return database{}, err
	}
	result := database{
		db:           db,
		pool:         pool,
		transactions: db,
		audit:        recorder,
		idempotency:  records,
		outbox:       messages,
	}
	if settings.MigrationsEnabled {
		runner, err := newMigrationRunner()
		if err != nil {
			return database{}, err
		}
		result.migrations = runner
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

// newMigrationRunner applies the schemas the shared adapters declare. The
// service owns the version numbers; each adapter owns its own DDL.
func newMigrationRunner() (*migrate.Runner, error) {
	manifest, err := migrate.NewManifest(
		migrate.Migration{Version: migrationAudit, Name: "audit_events", Up: auditpostgres.Schema()},
		migrate.Migration{Version: migrationIdempotency, Name: "idempotency_records", Up: idempotencypostgres.Schema()},
		migrate.Migration{Version: migrationOutbox, Name: "outbox_messages", Up: outboxpostgres.Schema()},
	)
	if err != nil {
		return nil, err
	}
	return migrate.NewRunner(manifest, migrate.Options{})
}
