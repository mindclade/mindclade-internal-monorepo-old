// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package durable binds the PostgreSQL-backed foundation contracts that more
// than one role holds: audit, idempotency, leases, and the work queue.
//
// It is a sibling of the composition root rather than part of it so that a
// role links only the adapters it actually uses. The dispatcher, for instance,
// publishes and does not lease, and importing the root package should not put
// a lease adapter into its binary.
package durable

import (
	"database/sql"

	"go.mindclade.dev/libs/go/audit"
	auditpostgres "go.mindclade.dev/libs/go/audit/postgres"
	mcclock "go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/coordination/cursor"
	cursorpostgres "go.mindclade.dev/libs/go/coordination/cursor/postgres"
	"go.mindclade.dev/libs/go/coordination/workqueue"
	workqueuepostgres "go.mindclade.dev/libs/go/coordination/workqueue/postgres"
	"go.mindclade.dev/libs/go/idempotency"
	idempotencypostgres "go.mindclade.dev/libs/go/idempotency/postgres"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/storage/lease"
	leasepostgres "go.mindclade.dev/libs/go/storage/lease/postgres"
	"go.mindclade.dev/services/control_plane/internal/providers"
)

// Each constructor is a thin binding of a libs/go contract to the one
// supported production adapter. The choice of PostgreSQL is made here and in
// the root package's table names, and nowhere else in the service.

// NewAuditRecorder binds the audit contract to its PostgreSQL adapter.
func NewAuditRecorder(db *sql.DB) (audit.Recorder, error) {
	return auditpostgres.New(db, auditpostgres.WithTable(providers.AuditTable))
}

// NewIdempotencyStore binds the idempotency contract to its PostgreSQL adapter.
func NewIdempotencyStore(db *sql.DB, value mcclock.Clock, ids *identifiers.Generator) (idempotency.Store, error) {
	return idempotencypostgres.New(db,
		idempotencypostgres.WithClock(value),
		idempotencypostgres.WithGenerator(ids),
		idempotencypostgres.WithTable(providers.IdempotencyTable),
	)
}

// NewLeaseStore binds the lease contract to its PostgreSQL adapter. Every role
// that must not run two active copies fences through this one table, so the
// leases of different roles are distinguished by key rather than by store.
func NewLeaseStore(db *sql.DB) (lease.Store, error) {
	return leasepostgres.New(db, providers.LeaseTable)
}

// NewWorkQueueStore binds the durable work queue to its PostgreSQL adapter.
func NewWorkQueueStore(db *sql.DB) (workqueue.Store, error) {
	return workqueuepostgres.New(db, providers.WorkQueueTable)
}

// NewCursorStore binds the monotonic cursor contract to its PostgreSQL
// adapter. Cursors are compare-and-advance, so the store must be the same pool
// the projector's inbox transaction runs on: advancing in a different
// connection would let a projection commit without its cursor.
func NewCursorStore(db *sql.DB) (cursor.Store, error) {
	return cursorpostgres.New(db, providers.CursorTable)
}
