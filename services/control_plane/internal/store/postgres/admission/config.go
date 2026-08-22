// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package admissionpostgres

import (
	"context"
	"database/sql"
	"reflect"

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
	DefaultEntitlementTable      = "mindclade_gateway_entitlements"
	DefaultBudgetTable           = "mindclade_gateway_budgets"
	DefaultReservationTable      = "mindclade_gateway_reservations"
	DefaultBundleTable           = "mindclade_gateway_policy_bundles"
	DefaultProposalTable         = "mindclade_gateway_policy_proposals"
	DefaultReceiptTable          = "mindclade_gateway_policy_receipts"
	admissionMutationMaxAttempts = 8
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

func WithGovernanceTables(bundles, proposals, receipts string) Option {
	return func(store *Store) error {
		for _, table := range []string{bundles, proposals, receipts} {
			if !sqlpostgres.ValidQualifiedIdentifier(table) {
				return invalidConfig("gateway governance table name is invalid", "admission_invalid_governance_table")
			}
		}
		store.bundles = bundles
		store.proposals = proposals
		store.receipts = receipts
		return nil
	}
}

// Store is a serializable PostgreSQL implementation of the admission domain.
// In the production composition, its PostgreSQL audit and outbox dependencies
// join the same SQL transaction as domain state. It is safe for concurrent use.
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
	bundles      string
	proposals    string
	receipts     string
}

// Component exposes schema readiness without taking lifecycle ownership of the
// shared database pool.
func (store *Store) Component(name string) servicekit.Component {
	return servicekit.Component{Name: name, Readiness: store.Readiness}
}

func New(db *sql.DB, recorder audit.Recorder, messages outbox.Store, options ...Option) (*Store, error) {
	if db == nil || nilInterface(recorder) || nilInterface(messages) {
		return nil, invalidConfig("database, audit, and outbox stores are required", "admission_dependency_missing")
	}
	store := &Store{
		db: db, clock: clock.RealClock{}, recorder: recorder, messages: messages,
		entitlements: DefaultEntitlementTable, budgets: DefaultBudgetTable,
		reservations: DefaultReservationTable, bundles: DefaultBundleTable,
		proposals: DefaultProposalTable, receipts: DefaultReceiptTable,
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
			return nil, internal(context.Background(), err, "admission.postgres.New", "admission_generator_failed")
		}
		store.generator = generator
	}
	var err error
	if store.retries == nil {
		// A reservation serializes on policy and quota rows. The package-wide three-attempt
		// default is intentionally conservative, but it is too small for a bounded burst of
		// otherwise valid contenders: PostgreSQL correctly returns SQLSTATE 40001 until the
		// winning transactions become visible. Keep the adapter-specific budget finite while
		// allowing jittered backoff to drain that contention.
		policy, policyErr := retry.NewPolicy(retry.WithMaxAttempts(admissionMutationMaxAttempts))
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
