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
	cursorpostgres "go.mindclade.dev/libs/go/coordination/cursor/postgres"
	outboxpostgres "go.mindclade.dev/libs/go/coordination/outbox/postgres"
	workqueuepostgres "go.mindclade.dev/libs/go/coordination/workqueue/postgres"
	"go.mindclade.dev/libs/go/idempotency"
	idempotencypostgres "go.mindclade.dev/libs/go/idempotency/postgres"
	"go.mindclade.dev/libs/go/identifiers"
	leasepostgres "go.mindclade.dev/libs/go/storage/lease/postgres"
	"go.mindclade.dev/libs/go/storage/sql/migrate"
	"go.mindclade.dev/services/control_plane/internal/providers"
	"go.mindclade.dev/services/control_plane/internal/providers/durable"
	registrystore "go.mindclade.dev/services/control_plane/internal/store/postgres"
)

// Migration versions are owned by the role that owns the schema, because one
// database holds every adapter's tables and the ordering must be global. The
// dispatcher and the scheduler read and write these tables but do not create
// them: a role that runs no migration runner cannot race one that does.
//
// Versions are append-only. A released number is never reused or reordered,
// because the runner records what it applied by version.
const (
	migrationAudit uint64 = iota + 1
	migrationIdempotency
	migrationOutbox
	migrationLease
	migrationWorkQueue
	migrationCursor
	migrationRegistryDescriptors
	migrationRegistryReleases
	migrationRegistryEvidenceGraphs
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
	leaseDDL, err := leasepostgres.DDL(providers.LeaseTable)
	if err != nil {
		return nil, err
	}
	workQueueDDL, err := workqueuepostgres.DDL(providers.WorkQueueTable)
	if err != nil {
		return nil, err
	}
	cursorDDL, err := cursorpostgres.DDL(providers.CursorTable)
	if err != nil {
		return nil, err
	}
	registryDDL, err := registrystore.DDL(
		registrystore.DefaultDescriptorTable,
		registrystore.DefaultReleaseTable,
		registrystore.DefaultEvidenceGraphTable,
	)
	if err != nil {
		return nil, err
	}
	manifest, err := migrate.NewManifest(
		migrate.Migration{Version: migrationAudit, Name: "audit_events", Up: auditDDL},
		migrate.Migration{Version: migrationIdempotency, Name: "idempotency_records", Up: idempotencyDDL},
		migrate.Migration{Version: migrationOutbox, Name: "outbox_messages", Up: outboxDDL},
		migrate.Migration{Version: migrationLease, Name: "leases", Up: leaseDDL},
		migrate.Migration{Version: migrationWorkQueue, Name: "work_items", Up: workQueueDDL},
		migrate.Migration{Version: migrationCursor, Name: "cursors", Up: cursorDDL},
		migrate.Migration{Version: migrationRegistryDescriptors, Name: "registry_model_descriptors", Up: registryDDL[0]},
		migrate.Migration{Version: migrationRegistryReleases, Name: "registry_releases", Up: registryDDL[1]},
		migrate.Migration{Version: migrationRegistryEvidenceGraphs, Name: "registry_evidence_graphs", Up: registryDDL[2]},
	)
	if err != nil {
		return nil, err
	}
	return migrate.NewRunner(manifest, migrate.Options{})
}

func newAuditRecorder(db *sql.DB) (audit.Recorder, error) {
	return durable.NewAuditRecorder(db)
}

func newIdempotencyStore(db *sql.DB, value mcclock.Clock, ids *identifiers.Generator) (idempotency.Store, error) {
	return durable.NewIdempotencyStore(db, value, ids)
}
