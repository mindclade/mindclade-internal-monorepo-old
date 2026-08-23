// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package orchestrationpostgres

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
	DefaultWorkflowTable     = "mindclade_orchestration_workflows"
	DefaultStageTable        = "mindclade_orchestration_stages"
	DefaultAttemptTable      = "mindclade_orchestration_attempts"
	DefaultCancellationTable = "mindclade_orchestration_cancellations"

	// orchestrationMutationMaxAttempts bounds the serialization retry budget.
	// A stage transition serializes on one stage row plus the audit and outbox
	// appends, so contention is a bounded burst of sibling stages completing at
	// once rather than a hot global row; the package-wide three-attempt default
	// is too small for that burst and an unbounded budget would turn a
	// persistent conflict into an unbounded retry loop.
	orchestrationMutationMaxAttempts = 8
)

type Option func(*Store) error

func WithClock(value clock.Clock) Option {
	return func(store *Store) error {
		if nilInterface(value) {
			return invalidConfig("clock is required", "orchestration_nil_clock")
		}
		store.clock = value
		return nil
	}
}

func WithGenerator(value *identifiers.Generator) Option {
	return func(store *Store) error {
		if value == nil {
			return invalidConfig("identifier generator is required", "orchestration_nil_generator")
		}
		store.generator = value
		return nil
	}
}

func WithRetry(value *retry.Executor) Option {
	return func(store *Store) error {
		if value == nil {
			return invalidConfig("retry executor is required", "orchestration_nil_retry")
		}
		store.retries = value
		return nil
	}
}

func WithTables(workflows, stages, attempts, cancellations string) Option {
	return func(store *Store) error {
		for _, table := range []string{workflows, stages, attempts, cancellations} {
			if !sqlpostgres.ValidQualifiedIdentifier(table) {
				return invalidConfig("orchestration table name is invalid", "orchestration_invalid_table")
			}
		}
		store.workflows = workflows
		store.stages = stages
		store.attempts = attempts
		store.cancellations = cancellations
		return nil
	}
}

// Store is a serializable PostgreSQL implementation of the orchestration
// domain's durable seam. In the production composition its audit and outbox
// dependencies are the PostgreSQL adapters, so they join the same SQL
// transaction as the domain write. It is safe for concurrent use.
type Store struct {
	db            *sql.DB
	clock         clock.Clock
	generator     *identifiers.Generator
	recorder      audit.Recorder
	messages      outbox.Store
	audits        *audit.Factory
	events        *outbox.Factory
	retries       *retry.Executor
	actor         audit.Actor
	workflows     string
	stages        string
	attempts      string
	cancellations string
}

// Component exposes schema readiness without taking lifecycle ownership of the
// shared database pool: the pool outlives any one adapter that reads it.
func (store *Store) Component(name string) servicekit.Component {
	return servicekit.Component{Name: name, Readiness: store.Readiness}
}

func New(db *sql.DB, recorder audit.Recorder, messages outbox.Store, options ...Option) (*Store, error) {
	if db == nil || nilInterface(recorder) || nilInterface(messages) {
		return nil, invalidConfig("database, audit, and outbox stores are required", "orchestration_dependency_missing")
	}
	store := &Store{
		db: db, clock: clock.RealClock{}, recorder: recorder, messages: messages,
		workflows: DefaultWorkflowTable, stages: DefaultStageTable,
		attempts: DefaultAttemptTable, cancellations: DefaultCancellationTable,
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
			return nil, internal(context.Background(), err, "orchestration.postgres.New", "orchestration_generator_failed")
		}
		store.generator = generator
	}
	var err error
	if store.retries == nil {
		policy, policyErr := retry.NewPolicy(retry.WithMaxAttempts(orchestrationMutationMaxAttempts))
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
	store.actor, err = audit.NewSystemActor("orchestration-service")
	if err != nil {
		return nil, err
	}
	return store, nil
}

func invalidConfig(message, reason string) error {
	return faults.New(faults.CodeInvalidArgument, message,
		faults.WithReason(reason), faults.WithOperation("orchestration.postgres.New"),
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
