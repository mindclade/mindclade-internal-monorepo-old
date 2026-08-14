// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package postgres

import (
	"database/sql"
	"reflect"
	"strings"

	"mindclade.internal/libs/go/clock"
	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/identifiers"
)

const DefaultTable = "mindclade_idempotency_records"

type Option func(*Store) error

func WithClock(value clock.Clock) Option {
	return func(store *Store) error {
		if nilInterface(value) {
			return invalidConfig("idempotency clock must not be nil", "nil_clock")
		}
		store.clock = value
		return nil
	}
}

func WithGenerator(value *identifiers.Generator) Option {
	return func(store *Store) error {
		if value == nil {
			return invalidConfig("idempotency identifier generator must not be nil", "nil_generator")
		}
		store.generator = value
		return nil
	}
}

func WithTable(value string) Option {
	return func(store *Store) error {
		value = strings.TrimSpace(value)
		if !validQualifiedIdentifier(value) {
			return invalidConfig("invalid idempotency table name", "invalid_table")
		}
		store.table = value
		return nil
	}
}

func New(db *sql.DB, options ...Option) (*Store, error) {
	if db == nil {
		return nil, invalidConfig("database must not be nil", "nil_database")
	}
	store := &Store{db: db, clock: clock.RealClock{}, table: DefaultTable}
	for _, option := range options {
		if option != nil {
			if err := option(store); err != nil {
				return nil, err
			}
		}
	}
	if nilInterface(store.clock) || !validQualifiedIdentifier(store.table) {
		return nil, invalidConfig("invalid idempotency PostgreSQL configuration", "invalid_configuration")
	}
	if store.generator == nil {
		generator, err := identifiers.NewGenerator(identifiers.WithTimeSource(store.clock.Now))
		if err != nil {
			return nil, faults.Wrap(err, faults.CodeInternal, "unable to configure idempotency identifiers", faults.WithReason("idempotency_generator_configuration_failed"), faults.WithOperation("idempotency.postgres.New"), faults.WithRetryPolicy(faults.NoRetry()))
		}
		store.generator = generator
	}
	return store, nil
}

func validQualifiedIdentifier(value string) bool {
	if value == "" || len(value) > 127 || strings.HasPrefix(value, ".") || strings.HasSuffix(value, ".") || strings.Contains(value, "..") {
		return false
	}
	for _, part := range strings.Split(value, ".") {
		if part == "" || len(part) > 63 || part[0] < 'a' || part[0] > 'z' {
			return false
		}
		for _, character := range part {
			if !((character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character == '_') {
				return false
			}
		}
	}
	return true
}

func invalidConfig(message, reason string) error {
	return faults.Wrap(ErrInvalidConfig, faults.CodeInvalidArgument, message, faults.WithReason(reason), faults.WithOperation("idempotency.postgres.New"), faults.WithRetryPolicy(faults.NoRetry()))
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
