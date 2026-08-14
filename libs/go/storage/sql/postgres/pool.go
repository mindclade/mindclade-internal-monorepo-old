// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"sync/atomic"
	"time"

	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/servicekit"
)

const DefaultHealthProbeTimeout = 2 * time.Second

type PoolOption func(*Pool) error

// WithCloseOnStop controls whether the servicekit component closes the pool.
// The default is true because the composition root normally owns *sql.DB.
func WithCloseOnStop(value bool) PoolOption {
	return func(pool *Pool) error { pool.closeOnStop = value; return nil }
}

func WithHealthProbeTimeout(value time.Duration) PoolOption {
	return func(pool *Pool) error {
		if value < 0 {
			return faults.New(faults.CodeInvalidArgument, "invalid PostgreSQL health-probe timeout", faults.WithReason("invalid_postgres_health_timeout"), faults.WithOperation("storage.sql.postgres.WithHealthProbeTimeout"), faults.WithRetryPolicy(faults.NoRetry()))
		}
		pool.healthTimeout = value
		return nil
	}
}

// Pool is the canonical service-owned database/sql lifecycle adapter. It keeps
// pool configuration, startup reachability, health probes, and shutdown in one
// servicekit component while exposing the underlying DB to repositories.
type Pool struct {
	db            *sql.DB
	config        PoolConfig
	healthTimeout time.Duration
	closeOnStop   bool
	started       atomic.Bool
	closed        atomic.Bool
}

func NewPool(db *sql.DB, config PoolConfig, options ...PoolOption) (*Pool, error) {
	if db == nil {
		return nil, faults.New(faults.CodeInvalidArgument, "database must not be nil", faults.WithReason("nil_database"), faults.WithOperation("storage.sql.postgres.NewPool"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	probeTimeout := config.PingTimeout
	if probeTimeout == 0 {
		probeTimeout = DefaultHealthProbeTimeout
	}
	pool := &Pool{db: db, config: config, healthTimeout: probeTimeout, closeOnStop: true}
	for _, option := range options {
		if option != nil {
			if err := option(pool); err != nil {
				return nil, err
			}
		}
	}
	return pool, nil
}

func (pool *Pool) DB() *sql.DB {
	if pool == nil {
		return nil
	}
	return pool.db
}

func (pool *Pool) Start(ctx context.Context) error {
	if ctx == nil || pool == nil || pool.db == nil || pool.closed.Load() {
		return faults.New(faults.CodeFailedPrecondition, "PostgreSQL pool is not configured", faults.WithReason("postgres_pool_not_configured"), faults.WithOperation("storage.sql.postgres.Pool.Start"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	if pool.started.Load() {
		return nil
	}
	if err := ConfigureAndPing(ctx, pool.db, pool.config); err != nil {
		return err
	}
	pool.started.Store(true)
	return nil
}

func (pool *Pool) Probe(ctx context.Context) error {
	if ctx == nil || pool == nil || pool.db == nil || !pool.started.Load() || pool.closed.Load() {
		return faults.New(faults.CodeUnavailable, "PostgreSQL pool is not ready", faults.WithReason("postgres_pool_not_ready"), faults.WithOperation("storage.sql.postgres.Pool.Probe"), faults.WithRetryPolicy(faults.ImmediateRetry(0)))
	}
	return Ping(ctx, pool.db, pool.healthTimeout)
}

func (pool *Pool) Stop(context.Context) error {
	if pool == nil || pool.db == nil || !pool.closeOnStop || pool.closed.Swap(true) {
		return nil
	}
	pool.started.Store(false)
	if err := pool.db.Close(); err != nil && !errors.Is(err, sql.ErrConnDone) {
		return Qualify(context.Background(), err, "storage.sql.postgres.Pool.Stop")
	}
	return nil
}

func (pool *Pool) Component(name string) servicekit.Component {
	return servicekit.Component{
		Name:      name,
		Start:     pool.Start,
		Stop:      pool.Stop,
		Liveness:  pool.Probe,
		Readiness: pool.Probe,
	}
}
