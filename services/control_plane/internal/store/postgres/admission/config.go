// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package admissionpostgres

import (
	"database/sql"
	"reflect"

	"go.mindclade.dev/libs/go/audit"
	"go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/coordination/outbox"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/retry"
	sqlpostgres "go.mindclade.dev/libs/go/storage/sql/postgres"
)

const (
	DefaultEntitlementTable = "mindclade_gateway_entitlements"
	DefaultBudgetTable      = "mindclade_gateway_budgets"
	DefaultReservationTable = "mindclade_gateway_reservations"
)

type Option func(*Store) error

func WithClock(value clock.Clock) Option {
	return func(store *Store) error {
		if nilInterface(value) {
			return invalidConfig("clock is required", "admission_nil_clock")
		}
		store.clock = value
		return nil
	}
}

func WithGenerator(value *identifiers.Generator) Option {
	return func(store *Store) error {
		if value == nil {
			return invalidConfig("identifier generator is required", "admission_nil_generator")
		}
		store.generator = value
		return nil
	}
}

func WithRetry(value *retry.Executor) Option {
	return func(store *Store) error {
		if value == nil {
			return invalidConfig("retry executor is required", "admission_nil_retry")
		}
		store.retries = value
		return nil
	}
}

func WithTables(entitlements, budgets, reservations string) Option {
	return func(store *Store) error {
		for _, table := range []string{entitlements, budgets, reservations} {
			if !sqlpostgres.ValidQualifiedIdentifier(table) {
				return invalidConfig("admission table name is invalid", "admission_invalid_table")
			}
		}
		store.entitlements = entitlements
		store.budgets = budgets
		store.reservations = reservations
		return nil
	}
}

// Store is a serializable PostgreSQL implementation of the admission domain.
// Every mutation writes domain state, audit, and outbox records in one SQL
// transaction. It is safe for concurrent use.
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
	entitlements string
	budgets      string
	reservations string
}

func New(db *sql.DB, recorder audit.Recorder, messages outbox.Store, options ...Option) (*Store, error) {
	if db == nil || nilInterface(recorder) || nilInterface(messages) {
		return nil, invalidConfig("database, audit, and outbox stores are required", "admission_dependency_missing")
	}
	store := &Store{
		db: db, clock: clock.RealClock{}, recorder: recorder, messages: messages,
		entitlements: DefaultEntitlementTable, budgets: DefaultBudgetTable,
		reservations: DefaultReservationTable,
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
			return nil, internal(nil, err, "admission.postgres.New", "admission_generator_failed")
		}
		store.generator = generator
	}
	var err error
	if store.retries == nil {
		store.retries, err = retry.NewExecutor(retry.DefaultPolicy(), retry.WithClock(store.clock))
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
	store.actor, err = audit.NewSystemActor("admission-service")
	if err != nil {
		return nil, err
	}
	return store, nil
}

func invalidConfig(message, reason string) error {
	return faults.New(faults.CodeInvalidArgument, message,
		faults.WithReason(reason), faults.WithOperation("admission.postgres.New"),
		faults.WithRetryPolicy(faults.NoRetry()))
}

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
