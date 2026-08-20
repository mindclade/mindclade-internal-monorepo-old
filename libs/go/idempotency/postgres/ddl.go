// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package postgres

import (
	"fmt"
	"strings"

	"go.mindclade.dev/libs/go/faults"
)

// maximumPostgresIdentifierBytes is PostgreSQL's NAMEDATALEN-1 limit. Index
// names are derived from the table, so a long table name must truncate rather
// than silently collide after the server truncates it.
const maximumPostgresIdentifierBytes = 63

// DDL returns the forward-only schema for table, matching the statements this
// adapter's queries assume.
//
// The composition root owns migration versioning and ordering, so this returns
// a statement rather than a migration. It takes the table name because the
// store does: applying a fixed schema next to a WithTable store is how a
// migration and its reader silently stop describing the same table.
func DDL(table string) (string, error) {
	table = strings.TrimSpace(table)
	if !validQualifiedIdentifier(table) {
		return "", faults.Wrap(ErrInvalidConfig, faults.CodeInvalidArgument,
			"invalid idempotency table name",
			faults.WithReason("invalid_idempotency_table"),
			faults.WithOperation("idempotency.postgres.DDL"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
    identity_digest  text PRIMARY KEY,
    scope            text NOT NULL,
    idempotency_key  text NOT NULL,
    record_id        text NOT NULL UNIQUE,
    fingerprint      text NOT NULL,
    state            text NOT NULL CHECK (state IN ('in_progress', 'completed')),
    result           jsonb,
    request_id       text,
    created_at       timestamptz NOT NULL,
    updated_at       timestamptz NOT NULL,
    expires_at       timestamptz NOT NULL,
    lease_token      text,
    lease_expires_at timestamptz,
    version          bigint NOT NULL CHECK (version > 0),
    CHECK (
        (state = 'in_progress' AND lease_token IS NOT NULL AND lease_expires_at IS NOT NULL AND result IS NULL)
        OR
        (state = 'completed' AND lease_token IS NULL AND lease_expires_at IS NULL AND result IS NOT NULL)
    )
);

CREATE INDEX IF NOT EXISTS %s
    ON %s (expires_at);
`, table, indexName(table, "expiry_idx"), table), nil
}

func indexName(table, suffix string) string {
	base := strings.ReplaceAll(table, ".", "_")
	maximumBase := maximumPostgresIdentifierBytes - len(suffix) - 1
	if len(base) > maximumBase {
		base = base[:maximumBase]
	}
	return base + "_" + suffix
}
