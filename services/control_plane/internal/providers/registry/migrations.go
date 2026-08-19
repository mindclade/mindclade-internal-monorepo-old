// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package registry

import (
	"database/sql"

	"go.mindclade.dev/libs/go/audit"
	auditpostgres "go.mindclade.dev/libs/go/audit/postgres"
	mcclock "go.mindclade.dev/libs/go/clock"
	outboxpostgres "go.mindclade.dev/libs/go/coordination/outbox/postgres"
	"go.mindclade.dev/libs/go/idempotency"
	idempotencypostgres "go.mindclade.dev/libs/go/idempotency/postgres"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/storage/sql/migrate"
	"go.mindclade.dev/services/control_plane/internal/providers"
)

// Migration versions are owned by the role that owns the schema, because one
// database holds every adapter's tables and the ordering must be global. The
// dispatcher reads these tables but does not create them.
const (
	migrationAudit uint64 = iota + 1
	migrationIdempotency
	migrationOutbox
)

// newMigrationRunner applies the schemas the shared adapters declare for the
// tables this process configures. The service owns the version numbers; each
// adapter owns its own DDL.
func newMigrationRunner() (*migrate.Runner, error) {
	auditDDL, err := auditpostgres.DDL(providers.AuditTable)
	if err != nil {
		return nil, err
	}
	idempotencyDDL, err := idempotencypostgres.DDL(providers.IdempotencyTable)
	if err != nil {
		return nil, err
	}
	outboxDDL, err := outboxpostgres.DDL(providers.OutboxTable)
	if err != nil {
		return nil, err
	}
	manifest, err := migrate.NewManifest(
		migrate.Migration{Version: migrationAudit, Name: "audit_events", Up: auditDDL},
		migrate.Migration{Version: migrationIdempotency, Name: "idempotency_records", Up: idempotencyDDL},
		migrate.Migration{Version: migrationOutbox, Name: "outbox_messages", Up: outboxDDL},
	)
	if err != nil {
		return nil, err
	}
	return migrate.NewRunner(manifest, migrate.Options{})
}

func newAuditRecorder(db *sql.DB) (audit.Recorder, error) {
	return auditpostgres.New(db, auditpostgres.WithTable(providers.AuditTable))
}

func newIdempotencyStore(db *sql.DB, value mcclock.Clock, ids *identifiers.Generator) (idempotency.Store, error) {
	return idempotencypostgres.New(db,
		idempotencypostgres.WithClock(value),
		idempotencypostgres.WithGenerator(ids),
		idempotencypostgres.WithTable(providers.IdempotencyTable),
	)
}
