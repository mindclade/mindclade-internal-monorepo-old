// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package schedulingpostgres

import (
	"context"
	"database/sql"
	"reflect"
	"time"

	"go.mindclade.dev/libs/go/audit"
	"go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/coordination/outbox"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/retry"
	"go.mindclade.dev/libs/go/servicekit"
	sqlpostgres "go.mindclade.dev/libs/go/storage/sql/postgres"
)

const (
	DefaultReservationTable = "mindclade_scheduling_reservations"
	DefaultQuotaTable       = "mindclade_scheduling_quotas"
	DefaultWeightTable      = "mindclade_scheduling_weights"
	DefaultLedgerTable      = "mindclade_scheduling_ledger"
)

// The serialization retry budget, re-argued from this store's own contention
// shape rather than inherited from the orchestration adapter's eight.
//
// Orchestration sized its budget for a burst of sibling stages each locking a
// different row: contention there is incidental, and most concurrent
// transitions never touch the same row at all. This store has the opposite
// shape. Every mutation -- Reserve, each lifecycle transition, Preempt, and the
// two reads-that-write, Snapshot and Held -- takes SELECT ... FOR UPDATE on one
// singleton ledger row before it does anything else. Under SERIALIZABLE a
// blocked locking read cannot fall forward to a newer row version the way READ
// COMMITTED can, so when the holder commits, the waiter is aborted with 40001.
// Contention here is therefore not a tail event: it is the ordinary outcome of
// two concurrent mutations, one per losing writer, every time.
//
// That makes the budget a concurrency question, not a reliability question. The
// k-th writer of a simultaneous burst needs up to k attempts, so the budget has
// to cover the burst the composition root can actually produce. Today that is
// the placement worker's four concurrent items (placementConcurrency in
// services/control_plane/internal/providers/scheduler), and each Place takes
// the ledger row twice -- once for its Snapshot, once for its Reserve -- for
// eight acquisitions, plus concurrent lifecycle transitions from the reconcile
// path. Twelve covers that burst with room for four more writers; it is not
// unbounded, because a conflict that survives twelve attempts is a wedged
// holder or a lock cycle, and reporting that is more useful than spinning on it.
const schedulingMutationMaxAttempts = 12

// The default delay strategy is deliberately much tighter than the retry
// package's 100ms-to-5s default. That default is sized for a remote dependency
// that is down; a 40001 here means another writer committed microseconds ago
// and this transaction has to be replayed against the row it left behind. The
// useful delay is long enough to break a synchronized herd and no longer, so
// twelve attempts cost under two seconds of sleep in the worst case rather than
// half a minute. The elapsed cap is the backstop: it bounds the wall-clock cost
// of the budget even if the backoff is later retuned.
const (
	schedulingRetryInitialDelay = 5 * time.Millisecond
	schedulingRetryMaximumDelay = 250 * time.Millisecond
	schedulingRetryMultiplier   = 2.0
	schedulingRetryMaxElapsed   = 5 * time.Second
)

type Option func(*Store) error

func WithClock(value clock.Clock) Option {
	return func(store *Store) error {
		if nilInterface(value) {
			return invalidConfig("clock is required", "scheduling_nil_clock")
		}
		store.clock = value
		return nil
	}
}

func WithGenerator(value *identifiers.Generator) Option {
	return func(store *Store) error {
		if value == nil {
			return invalidConfig("identifier generator is required", "scheduling_nil_generator")
		}
		store.generator = value
		return nil
	}
}

func WithRetry(value *retry.Executor) Option {
	return func(store *Store) error {
		if value == nil {
			return invalidConfig("retry executor is required", "scheduling_nil_retry")
		}
		store.retries = value
		return nil
	}
}

func WithTables(reservations, quotas, weights, ledger string) Option {
	return func(store *Store) error {
		for _, table := range []string{reservations, quotas, weights, ledger} {
			if !sqlpostgres.ValidQualifiedIdentifier(table) {
				return invalidConfig("scheduling table name is invalid", "scheduling_invalid_table")
			}
		}
		store.reservations = reservations
		store.quotas = quotas
		store.weights = weights
		store.ledger = ledger
		return nil
	}
}

// Store is a serializable PostgreSQL implementation of the scheduling domain's
// durable seam. In the production composition its audit and outbox dependencies
// are the PostgreSQL adapters, so they join the same SQL transaction as the
// domain write. It is safe for concurrent use.
type Store struct {
	db           *sql.DB
	clock        clock.Clock
	generator    *identifiers.Generator
	recorder     audit.Recorder
	messages     outbox.Store
	audits       *audit.Factory
	events       *outbox.Factory
	retries      *retry.Executor
	actor        audit.Actor
	reservations string
	quotas       string
	weights      string
	ledger       string
}

// Component exposes schema readiness without taking lifecycle ownership of the
// shared database pool: the pool outlives any one adapter that reads it.
func (store *Store) Component(name string) servicekit.Component {
	return servicekit.Component{Name: name, Readiness: store.Readiness}
}

func New(db *sql.DB, recorder audit.Recorder, messages outbox.Store, options ...Option) (*Store, error) {
	if db == nil || nilInterface(recorder) || nilInterface(messages) {
		return nil, invalidConfig("database, audit, and outbox stores are required", "scheduling_dependency_missing")
	}
	store := &Store{
		db: db, clock: clock.RealClock{}, recorder: recorder, messages: messages,
		reservations: DefaultReservationTable, quotas: DefaultQuotaTable,
		weights: DefaultWeightTable, ledger: DefaultLedgerTable,
	}
	for _, option := range options {
		if option != nil {
			if err := option(store); err != nil {
				return nil, err
			}
		}
	}
	if store.generator == nil {
		generator, err := identifiers.NewGenerator(identifiers.WithTimeSource(store.clock.Now))
		if err != nil {
			return nil, internal(context.Background(), err, "scheduling.postgres.New", "scheduling_generator_failed")
		}
		store.generator = generator
	}
	var err error
	if store.retries == nil {
		backoff, backoffErr := retry.ExponentialBackoff(
			schedulingRetryInitialDelay, schedulingRetryMaximumDelay, schedulingRetryMultiplier)
		if backoffErr != nil {
			return nil, backoffErr
		}
		policy, policyErr := retry.NewPolicy(
			retry.WithMaxAttempts(schedulingMutationMaxAttempts),
			retry.WithBackoff(backoff),
			retry.WithMaxElapsed(schedulingRetryMaxElapsed),
		)
		if policyErr != nil {
			return nil, policyErr
		}
		store.retries, err = retry.NewExecutor(policy, retry.WithClock(store.clock))
		if err != nil {
			return nil, err
		}
	}
	store.audits, err = audit.NewFactory(audit.WithClock(store.clock), audit.WithGenerator(store.generator))
	if err != nil {
		return nil, err
	}
	store.events, err = outbox.NewFactory(outbox.WithFactoryClock(store.clock), outbox.WithFactoryGenerator(store.generator))
	if err != nil {
		return nil, err
	}
	store.actor, err = audit.NewSystemActor("scheduling-service")
	if err != nil {
		return nil, err
	}
	return store, nil
}

func invalidConfig(message, reason string) error {
	return faults.New(faults.CodeInvalidArgument, message,
		faults.WithReason(reason), faults.WithOperation("scheduling.postgres.New"),
		faults.WithRetryPolicy(faults.NoRetry()))
}

// nilInterface reports whether an interface holds a nil pointer. A typed nil is
// not == nil, so a store handed a (*memory.Store)(nil) would pass a plain guard
// and panic on its first append.
func nilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
