// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package postgres

import (
	"database/sql"

	canonical "mindclade.internal/libs/go/coordination/outbox/postgres"
	"mindclade.internal/libs/go/storage/lease"
)

// Repository is the PostgreSQL implementation of storage/outbox.Repository.
type Repository = canonical.Store

// Store is retained for compatibility with the canonical adapter vocabulary.
type Store = canonical.Store

type Option = canonical.Option

func New(db *sql.DB, table string, options ...Option) (*Repository, error) {
	return canonical.New(db, table, options...)
}

func DDL(table string) (string, error) {
	return canonical.DDL(table)
}

func WithTokenGenerator(value func() (lease.Token, error)) Option {
	return canonical.WithTokenGenerator(value)
}
