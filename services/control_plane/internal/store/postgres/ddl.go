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
// names are derived from their table, so a long table name must truncate here
// rather than silently collide after the server truncates it.
const maximumPostgresIdentifierBytes = 63

// DescriptorDDL returns the forward-only schema for the model descriptor table.
//
// The row stores the whole descriptor as JSONB in `document` and projects only
// the fields something queries or constrains. The projection is derived on
// write and never read back into a domain value: Resolve reconstructs the
// descriptor from `document` and re-verifies its seal, so a projected column
// that drifted could not corrupt a returned descriptor, only a query result.
//
// descriptor_digest is the primary key because it is the identity the domain
// already enforces -- SealDigest hashes every other field, so two rows with the
// same digest and different content is a seal failure, not a duplicate.
//
// content_digest is stored alongside it as the digest of the exact stored
// document. It exists to make a digest collision detectable on write: without
// it, a second Put under an existing digest could not be distinguished from an
// idempotent republish without decoding and re-canonicalizing the stored row.
func DescriptorDDL(table string) (string, error) {
	if err := checkTable(table, "invalid_descriptor_table", "registry.postgres.DescriptorDDL"); err != nil {
		return "", err
	}
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
    descriptor_digest    text PRIMARY KEY,
    content_digest       text NOT NULL,
    model_id             text NOT NULL,
    family               text NOT NULL,
    version              text NOT NULL,
    lifecycle            text NOT NULL CHECK (lifecycle IN ('draft', 'qualified', 'serving', 'deprecated', 'revoked')),
    model_bundle_digest  text NOT NULL,
    schema_version       bigint NOT NULL CHECK (schema_version > 0),
    policy_epoch         bigint NOT NULL,
    created              timestamptz NOT NULL,
    expires              timestamptz NOT NULL,
    document             jsonb NOT NULL,
    written_at           timestamptz NOT NULL
);

CREATE INDEX IF NOT EXISTS %s
    ON %s (model_id, version);

CREATE INDEX IF NOT EXISTS %s
    ON %s (lifecycle, expires);
`,
		table,
		indexName(table, "model_idx"), table,
		indexName(table, "lifecycle_idx"), table,
	), nil
}

// ReleaseDDL returns the forward-only schema for the release table.
//
// resource_version carries the domain's own optimistic-concurrency counter, so
// the CHECK keeps a zero or rolled-back version out of the table rather than
// letting a compare-and-swap succeed against a value the domain never issued.
func ReleaseDDL(table string) (string, error) {
	if err := checkTable(table, "invalid_release_table", "registry.postgres.ReleaseDDL"); err != nil {
		return "", err
	}
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
    release_id            text PRIMARY KEY,
    model_bundle_digest   text NOT NULL,
    evidence_graph_digest text NOT NULL,
    channel               text NOT NULL,
    status                text NOT NULL,
    resource_version      bigint NOT NULL CHECK (resource_version > 0),
    written_at            timestamptz NOT NULL
);

CREATE INDEX IF NOT EXISTS %s
    ON %s (channel, status);

CREATE INDEX IF NOT EXISTS %s
    ON %s (model_bundle_digest);
`,
		table,
		indexName(table, "channel_idx"), table,
		indexName(table, "subject_idx"), table,
	), nil
}

// EvidenceGraphDDL returns the forward-only schema for the evidence graph table.
//
// A graph is immutable once written, so graph_digest is UNIQUE as well as
// derived: the release quotes it, and two release identifiers resolving to one
// graph would make the quote ambiguous.
func EvidenceGraphDDL(table string) (string, error) {
	if err := checkTable(table, "invalid_graph_table", "registry.postgres.EvidenceGraphDDL"); err != nil {
		return "", err
	}
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
    release_id     text PRIMARY KEY,
    graph_digest   text NOT NULL UNIQUE,
    subject_digest text NOT NULL,
    policy_digest  text NOT NULL,
    policy_epoch   bigint NOT NULL,
    document       jsonb NOT NULL,
    written_at     timestamptz NOT NULL
);

CREATE INDEX IF NOT EXISTS %s
    ON %s (subject_digest);
`,
		table,
		indexName(table, "subject_idx"), table,
	), nil
}

// DDL returns the three statements in the order a migration must apply them.
// The composition root owns migration versioning, so this returns statements
// rather than a migration.
func DDL(descriptorTable, releaseTable, graphTable string) ([]string, error) {
	descriptors, err := DescriptorDDL(descriptorTable)
	if err != nil {
		return nil, err
	}
	releases, err := ReleaseDDL(releaseTable)
	if err != nil {
		return nil, err
	}
	graphs, err := EvidenceGraphDDL(graphTable)
	if err != nil {
		return nil, err
	}
	return []string{descriptors, releases, graphs}, nil
}

func checkTable(table, reason, operation string) error {
	if !validQualifiedIdentifier(strings.TrimSpace(table)) {
		return faults.Wrap(ErrInvalidConfig, faults.CodeInvalidArgument, "invalid registry table name",
			faults.WithReason(reason),
			faults.WithOperation(operation),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	return nil
}

func indexName(table, suffix string) string {
	base := strings.ReplaceAll(strings.TrimSpace(table), ".", "_")
	maximumBase := maximumPostgresIdentifierBytes - len(suffix) - 1
	if len(base) > maximumBase {
		base = base[:maximumBase]
	}
	return base + "_" + suffix
}
