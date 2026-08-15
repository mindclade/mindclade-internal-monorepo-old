// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package postgres

import (
	"context"
	"database/sql"
	"time"

	"go.mindclade.dev/libs/go/faults"
)

// PoolConfig contains database/sql pool limits and an optional startup health
// probe budget. Zero values retain database/sql's documented defaults.
type PoolConfig struct {
	MaxOpenConnections    int
	MaxIdleConnections    int
	ConnectionMaxLifetime time.Duration
	ConnectionMaxIdleTime time.Duration
	PingTimeout           time.Duration
}

func (config PoolConfig) Validate() error {
	if config.MaxOpenConnections < 0 ||
		config.MaxIdleConnections < 0 ||
		config.MaxOpenConnections > 0 && config.MaxIdleConnections > config.MaxOpenConnections ||
		config.ConnectionMaxLifetime < 0 ||
		config.ConnectionMaxIdleTime < 0 ||
		config.PingTimeout < 0 {
		return faults.New(
			faults.CodeInvalidArgument,
			"invalid PostgreSQL pool configuration",
			faults.WithReason("invalid_postgres_pool_config"),
			faults.WithOperation("storage.sql.postgres.PoolConfig.Validate"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	return nil
}

// Configure applies pool settings. It does not establish a connection; use
// ConfigureAndPing when startup must prove database reachability.
func Configure(db *sql.DB, config PoolConfig) error {
	if db == nil {
		return faults.New(
			faults.CodeInvalidArgument,
			"database must not be nil",
			faults.WithReason("nil_database"),
			faults.WithOperation("storage.sql.postgres.Configure"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	if err := config.Validate(); err != nil {
		return err
	}
	db.SetMaxOpenConns(config.MaxOpenConnections)
	db.SetMaxIdleConns(config.MaxIdleConnections)
	db.SetConnMaxLifetime(config.ConnectionMaxLifetime)
	db.SetConnMaxIdleTime(config.ConnectionMaxIdleTime)
	return nil
}

// ConfigureAndPing applies pool settings and verifies connectivity within the
// configured ping budget.
func ConfigureAndPing(ctx context.Context, db *sql.DB, config PoolConfig) error {
	if ctx == nil {
		return faults.New(
			faults.CodeInvalidArgument,
			"context must not be nil",
			faults.WithReason("nil_context"),
			faults.WithOperation("storage.sql.postgres.ConfigureAndPing"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	if err := Configure(db, config); err != nil {
		return err
	}
	return Ping(ctx, db, config.PingTimeout)
}

// Ping verifies database reachability. A zero timeout uses the caller's
// context without adding another deadline.
func Ping(ctx context.Context, db *sql.DB, timeout time.Duration) error {
	if ctx == nil || db == nil || timeout < 0 {
		return faults.New(
			faults.CodeInvalidArgument,
			"invalid PostgreSQL ping request",
			faults.WithReason("invalid_postgres_ping_request"),
			faults.WithOperation("storage.sql.postgres.Ping"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	if err := db.PingContext(ctx); err != nil {
		return Qualify(ctx, err, "storage.sql.postgres.Ping")
	}
	return nil
}
