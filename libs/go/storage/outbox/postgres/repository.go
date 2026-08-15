// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

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
