// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package postgres

import (
	"database/sql"
	"reflect"
	"strings"

	"go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/faults"
)

// Default table names. They carry the mindclade prefix every other adapter in
// this database uses, because one database holds every adapter's tables and
// the registry role owns their migration ordering.
const (
	DefaultDescriptorTable    = "mindclade_registry_model_descriptors"
	DefaultReleaseTable       = "mindclade_registry_releases"
	DefaultEvidenceGraphTable = "mindclade_registry_evidence_graphs"
)

// Option configures a Store.
type Option func(*Store) error

// WithClock replaces the clock used to stamp write times. The stamps are
// operational metadata for support queries, never domain state: a Descriptor
// carries its own Created and Expires, and nothing reads a row's write time
// back into a domain value.
func WithClock(value clock.Clock) Option {
	return func(store *Store) error {
		if nilInterface(value) {
			return invalidConfig("registry clock must not be nil", "nil_clock")
		}
		store.clock = value
		return nil
	}
}

// WithDescriptorTable overrides the model descriptor table name.
func WithDescriptorTable(value string) Option {
	return func(store *Store) error {
		value = strings.TrimSpace(value)
		if !validQualifiedIdentifier(value) {
			return invalidConfig("invalid model descriptor table name", "invalid_descriptor_table")
		}
		store.descriptors = value
		return nil
	}
}

// WithReleaseTable overrides the release table name.
func WithReleaseTable(value string) Option {
	return func(store *Store) error {
		value = strings.TrimSpace(value)
		if !validQualifiedIdentifier(value) {
			return invalidConfig("invalid release table name", "invalid_release_table")
		}
		store.releases = value
		return nil
	}
}

// WithEvidenceGraphTable overrides the evidence graph table name.
func WithEvidenceGraphTable(value string) Option {
	return func(store *Store) error {
		value = strings.TrimSpace(value)
		if !validQualifiedIdentifier(value) {
			return invalidConfig("invalid evidence graph table name", "invalid_graph_table")
		}
		store.graphs = value
		return nil
	}
}

// Store implements the control/registry repository contracts against one
// PostgreSQL database. It is safe for concurrent use by every handler in a
// process, and holds no per-request state.
//
// It is a single type rather than one per record because all three tables live
// in one database and a mutation that spans them -- promoting a release writes
// the graph and the release together -- must reach one commit. Separate stores
// over the same *sql.DB would still work, but would make it easy to construct
// them against different handles and lose that guarantee silently.
type Store struct {
	db          *sql.DB
	clock       clock.Clock
	descriptors string
	releases    string
	graphs      string
}

// New constructs a Store over db.
func New(db *sql.DB, options ...Option) (*Store, error) {
	if db == nil {
		return nil, invalidConfig("database must not be nil", "nil_database")
	}
	store := &Store{
		db:          db,
		clock:       clock.RealClock{},
		descriptors: DefaultDescriptorTable,
		releases:    DefaultReleaseTable,
		graphs:      DefaultEvidenceGraphTable,
	}
	for _, option := range options {
		if option != nil {
			if err := option(store); err != nil {
				return nil, err
			}
		}
	}
	if nilInterface(store.clock) {
		return nil, invalidConfig("invalid registry PostgreSQL configuration", "invalid_configuration")
	}
	for _, table := range []string{store.descriptors, store.releases, store.graphs} {
		if !validQualifiedIdentifier(table) {
			return nil, invalidConfig("invalid registry PostgreSQL configuration", "invalid_configuration")
		}
	}
	return store, nil
}

// DescriptorTable, ReleaseTable, and EvidenceGraphTable report the configured
// names so the composition root can emit DDL for the same tables the store
// reads. Applying a fixed schema next to a renamed store is how a migration
// and its reader stop describing the same table.
func (store *Store) DescriptorTable() string    { return store.descriptors }
func (store *Store) ReleaseTable() string       { return store.releases }
func (store *Store) EvidenceGraphTable() string { return store.graphs }

// validQualifiedIdentifier accepts a lowercase, optionally schema-qualified
// PostgreSQL identifier. Table names reach string-formatted SQL, so this is
// the boundary that keeps a configured name from becoming an injection point.
func validQualifiedIdentifier(value string) bool {
	if value == "" || len(value) > 127 || strings.HasPrefix(value, ".") || strings.HasSuffix(value, ".") || strings.Contains(value, "..") {
		return false
	}
	for _, part := range strings.Split(value, ".") {
		if part == "" || len(part) > 63 || part[0] < 'a' || part[0] > 'z' {
			return false
		}
		for _, character := range part {
			lower := character >= 'a' && character <= 'z'
			digit := character >= '0' && character <= '9'
			if !lower && !digit && character != '_' {
				return false
			}
		}
	}
	return true
}

func invalidConfig(message, reason string) error {
	return faults.Wrap(ErrInvalidConfig, faults.CodeInvalidArgument, message,
		faults.WithReason(reason),
		faults.WithOperation("registry.postgres.New"),
		faults.WithRetryPolicy(faults.NoRetry()),
	)
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
