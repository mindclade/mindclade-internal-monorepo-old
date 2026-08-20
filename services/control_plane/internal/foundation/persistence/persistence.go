// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package persistence carries the relational substrate every control-plane
// role needs: the pool, its migrations, and the transaction boundary domain
// repositories join.
package persistence

import (
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/servicekit"
	"go.mindclade.dev/libs/go/servicekit/production"
	"go.mindclade.dev/libs/go/storage/sql/migrate"
	sqlpostgres "go.mindclade.dev/libs/go/storage/sql/postgres"
	"go.mindclade.dev/libs/go/storage/sql/transaction"
	"go.mindclade.dev/services/control_plane/internal/foundation"
)

type SQL struct {
	Postgres     *sqlpostgres.Pool
	Migrations   *migrate.Runner
	Transactions transaction.Beginner
}

func (sql SQL) declarations() []foundation.Declaration {
	var pool, migrations *servicekit.Component
	if sql.Postgres != nil {
		component := sql.Postgres.Component("postgres")
		pool = &component
		if sql.Migrations != nil && sql.Postgres.DB() != nil {
			component := sql.Migrations.Component("postgres-migrations", sql.Postgres.DB())
			migrations = &component
		}
	}
	return []foundation.Declaration{
		{Capability: production.CapabilityDatabase, Present: sql.Postgres != nil, Component: pool},
		{Capability: production.CapabilityMigrations, Present: migrations != nil, Component: migrations},
		{
			Capability: production.CapabilityTransactions,
			Present:    !foundation.IsNil(sql.Transactions) || sql.Postgres != nil,
		},
	}
}

func (sql SQL) Capabilities() []production.Capability { return foundation.Present(sql.declarations()) }

func (sql SQL) ServiceOptions() []servicekit.Option { return nil }

func (sql SQL) Register(builder *production.Builder) error {
	// A migration runner without a pool would start a process that reports the
	// migration capability and can never apply one.
	if sql.Migrations != nil && (sql.Postgres == nil || sql.Postgres.DB() == nil) {
		return faults.New(
			faults.CodeFailedPrecondition,
			"migration runner requires a configured PostgreSQL pool",
			faults.WithReason("migrations_without_database"),
			faults.WithOperation("controlplane.persistence.SQL.Register"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	return foundation.Register(builder, sql.declarations())
}
