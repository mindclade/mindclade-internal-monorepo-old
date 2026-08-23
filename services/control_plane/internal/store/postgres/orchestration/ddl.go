// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package orchestrationpostgres

import (
	"fmt"
	"strings"

	"go.mindclade.dev/control/orchestration"
	"go.mindclade.dev/libs/go/faults"
	sqlpostgres "go.mindclade.dev/libs/go/storage/sql/postgres"
)

// maximumPostgresIdentifierBytes is PostgreSQL's NAMEDATALEN-1 limit. Index
// names are derived from their table, so an index for a long table name must be
// truncated here rather than silently truncated by the server, where two
// distinct names could collide into one.
const maximumPostgresIdentifierBytes = 63

// DDL returns the orchestration schemas in apply order.
//
// Each row keeps the whole domain record in a jsonb `document` column and
// projects out only what is queried, constrained, or compared. The projection
// is derived on write and never read back into a domain value: every read below
// reconstructs the record from `document` and revalidates it, so a projected
// column that drifted can corrupt a query plan's answer but never a returned
// record. The CHECK constraints are what stop that drift from happening at all.
//
// The document keys are Go field names because control/orchestration declares
// no JSON tags on these types. That is a coupling worth naming: renaming a
// field in the domain changes the on-disk key, and the constraints below are
// what turn that into a loud write failure instead of a silent one.
func DDL(workflowTable, stageTable, attemptTable, cancellationTable string) ([]string, error) {
	const operation = "orchestration.postgres.DDL"
	for _, table := range []string{workflowTable, stageTable, attemptTable, cancellationTable} {
		if err := checkTable(table, operation); err != nil {
			return nil, err
		}
	}
	workflows := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
    workflow_id       text PRIMARY KEY,
    name              text NOT NULL,
    definition_digest text NOT NULL,
    schema_version    bigint NOT NULL CHECK (schema_version > 0),
    stage_count       bigint NOT NULL CHECK (stage_count > 0 AND stage_count <= %d),
    document          jsonb NOT NULL,
    written_at        timestamptz NOT NULL,
    CHECK (jsonb_typeof(document) = 'object'),
    CHECK (document->>'ID' IS NOT DISTINCT FROM workflow_id),
    CHECK (document->>'Name' IS NOT DISTINCT FROM name),
    CHECK (document->>'DefinitionDigest' IS NOT DISTINCT FROM definition_digest),
    CHECK ((document->>'SchemaVersion')::numeric IS NOT DISTINCT FROM schema_version),
    CHECK (jsonb_array_length(document->'Stages') IS NOT DISTINCT FROM stage_count)
);

CREATE INDEX IF NOT EXISTS %s
    ON %s (definition_digest);
`, workflowTable, orchestration.MaximumStageCount,
		indexName(workflowTable, "definition_idx"), workflowTable)

	// A stage is keyed by (run, stage) rather than by a surrogate: the domain
	// addresses it that way, and a surrogate key would let one run hold two
	// rows for the same stage with nothing in the schema to forbid it.
	//
	// resource_version is nullable here and NOT NULL on attempts, because the
	// two records differ: AttemptRecord.Validate rejects a record whose version
	// does not seal its content, while StageRecord carries no such check and
	// orchestration.Service.StartRun materializes every stage with a zero
	// version. Declaring the column NOT NULL would make this adapter refuse a
	// record the domain considers valid, which is a disagreement the storage
	// layer is not entitled to have.
	stages := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
    run_id              text NOT NULL,
    stage_id            text NOT NULL,
    job_id              text NOT NULL,
    state               text NOT NULL CHECK (state IN ('blocked', 'queued', 'preparing', 'running', 'succeeded', 'failed', 'cancelling', 'cancelled')),
    attempts            bigint NOT NULL CHECK (attempts >= 0),
    updated_at          timestamptz NOT NULL,
    resource_version    text NULL,
    resource_generation bigint NULL CHECK (resource_generation IS NULL OR resource_generation > 0),
    document            jsonb NOT NULL,
    written_at          timestamptz NOT NULL,
    PRIMARY KEY (run_id, stage_id),
    CHECK (jsonb_typeof(document) = 'object'),
    CHECK (document->>'RunID' IS NOT DISTINCT FROM run_id),
    CHECK (document->>'StageID' IS NOT DISTINCT FROM stage_id),
    CHECK (document->>'JobID' IS NOT DISTINCT FROM job_id),
    CHECK (document->>'State' IS NOT DISTINCT FROM state),
    CHECK ((document->>'Attempts')::numeric IS NOT DISTINCT FROM attempts),
    CHECK ((document->>'UpdatedAt')::timestamptz IS NOT DISTINCT FROM updated_at),
    CHECK (document->>'Version' IS NOT DISTINCT FROM resource_version),
    CHECK (split_part(resource_version, ':', 2)::numeric IS NOT DISTINCT FROM resource_generation)
);

CREATE INDEX IF NOT EXISTS %s
    ON %s (run_id, state);
`, stageTable,
		indexName(stageTable, "run_state_idx"), stageTable)

	// attempt is bigint rather than int because AttemptRecord.Attempt is a
	// uint32: the whole unsigned range has to be representable, and int4 stops
	// half of it.
	attempts := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
    run_id              text NOT NULL,
    stage_id            text NOT NULL,
    attempt             bigint NOT NULL CHECK (attempt > 0),
    job_id              text NOT NULL,
    state               text NOT NULL CHECK (state IN ('created', 'starting', 'ready', 'leased', 'running', 'draining', 'committing', 'completed', 'recovering', 'cancelling', 'cancelled', 'failed')),
    fencing_token       bigint NOT NULL CHECK (fencing_token > 0),
    execution_ticket_id text NOT NULL,
    sequence            bigint NOT NULL CHECK (sequence >= 0),
    created_at          timestamptz NOT NULL,
    updated_at          timestamptz NOT NULL,
    resource_version    text NOT NULL,
    resource_generation bigint NOT NULL CHECK (resource_generation > 0),
    document            jsonb NOT NULL,
    written_at          timestamptz NOT NULL,
    PRIMARY KEY (run_id, stage_id, attempt),
    CHECK (updated_at >= created_at),
    CHECK (jsonb_typeof(document) = 'object'),
    CHECK (document->>'RunID' IS NOT DISTINCT FROM run_id),
    CHECK (document->>'StageID' IS NOT DISTINCT FROM stage_id),
    CHECK ((document->>'Attempt')::numeric IS NOT DISTINCT FROM attempt),
    CHECK (document->>'JobID' IS NOT DISTINCT FROM job_id),
    CHECK (document->>'State' IS NOT DISTINCT FROM state),
    CHECK ((document->>'FencingToken')::numeric IS NOT DISTINCT FROM fencing_token),
    CHECK (document->>'ExecutionTicketID' IS NOT DISTINCT FROM execution_ticket_id),
    CHECK ((document->>'Sequence')::numeric IS NOT DISTINCT FROM sequence),
    CHECK ((document->>'CreatedAt')::timestamptz IS NOT DISTINCT FROM created_at),
    CHECK ((document->>'UpdatedAt')::timestamptz IS NOT DISTINCT FROM updated_at),
    CHECK (document->>'Version' IS NOT DISTINCT FROM resource_version),
    CHECK (split_part(resource_version, ':', 2)::numeric IS NOT DISTINCT FROM resource_generation)
);

CREATE INDEX IF NOT EXISTS %s
    ON %s (run_id, stage_id, attempt DESC);
`, attemptTable,
		indexName(attemptTable, "latest_idx"), attemptTable)

	// The primary key is the idempotency identity, not the run: that identity
	// is what the domain deduplicates on, and keying the table any other way
	// would make a retried cancel request a second durable intent.
	cancellations := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
    idempotency_scope text NOT NULL,
    idempotency_key   text NOT NULL,
    run_id            text NOT NULL,
    stage_id          text NULL,
    origin            text NOT NULL CHECK (origin IN ('client', 'operator', 'claim_loss', 'deadline', 'preemption')),
    requested_at      timestamptz NOT NULL,
    document          jsonb NOT NULL,
    written_at        timestamptz NOT NULL,
    PRIMARY KEY (idempotency_scope, idempotency_key),
    CHECK ((origin IN ('client', 'operator', 'deadline')) = (stage_id IS NULL)),
    CHECK (jsonb_typeof(document) = 'object'),
    CHECK (document#>>'{Idempotency,scope}' IS NOT DISTINCT FROM idempotency_scope),
    CHECK (document#>>'{Idempotency,key}' IS NOT DISTINCT FROM idempotency_key),
    CHECK (document->>'RunID' IS NOT DISTINCT FROM run_id),
    CHECK (NULLIF(document->>'StageID', '') IS NOT DISTINCT FROM stage_id),
    CHECK (document->>'Origin' IS NOT DISTINCT FROM origin),
    CHECK ((document->>'RequestedAt')::timestamptz IS NOT DISTINCT FROM requested_at)
);

CREATE INDEX IF NOT EXISTS %s
    ON %s (run_id, requested_at DESC);
`, cancellationTable,
		indexName(cancellationTable, "run_idx"), cancellationTable)

	return []string{workflows, stages, attempts, cancellations}, nil
}

func checkTable(table, operation string) error {
	if !sqlpostgres.ValidQualifiedIdentifier(strings.TrimSpace(table)) {
		return faults.New(faults.CodeInvalidArgument, "orchestration table name is invalid",
			faults.WithReason("orchestration_invalid_table"), faults.WithOperation(operation),
			faults.WithRetryPolicy(faults.NoRetry()))
	}
	return nil
}

func indexName(table, suffix string) string {
	base := strings.ReplaceAll(table, ".", "_")
	maximumBase := maximumPostgresIdentifierBytes - len(suffix) - 1
	if len(base) > maximumBase {
		base = base[:maximumBase]
	}
	return base + "_" + suffix
}
