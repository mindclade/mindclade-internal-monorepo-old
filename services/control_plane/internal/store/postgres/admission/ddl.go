// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package admissionpostgres

import (
	"fmt"
	"strings"

	"go.mindclade.dev/libs/go/faults"
	sqlpostgres "go.mindclade.dev/libs/go/storage/sql/postgres"
)

const maximumPostgresIdentifierBytes = 63

// DDL returns the admission schemas in foreign-key dependency order.
func DDL(entitlementTable, budgetTable, reservationTable string) ([]string, error) {
	for _, table := range []string{entitlementTable, budgetTable, reservationTable} {
		if !sqlpostgres.ValidQualifiedIdentifier(table) {
			return nil, faults.New(faults.CodeInvalidArgument, "admission table name is invalid",
				faults.WithReason("admission_invalid_table"), faults.WithOperation("admission.postgres.DDL"),
				faults.WithRetryPolicy(faults.NoRetry()))
		}
	}
	entitlements := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
    subject TEXT NOT NULL,
    workspace TEXT NOT NULL,
    entitlement_id TEXT NOT NULL UNIQUE,
    policy_epoch BIGINT NOT NULL CHECK (policy_epoch > 0),
    resource_version TEXT NOT NULL,
    resource_generation BIGINT NOT NULL CHECK (resource_generation > 0),
    not_before TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    document JSONB NOT NULL,
    written_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (subject, workspace),
    CHECK (expires_at > not_before)
);
CREATE INDEX IF NOT EXISTS %s ON %s (workspace, expires_at);`, entitlementTable,
		indexName(entitlementTable, "workspace_idx"), entitlementTable)
	budgets := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
    workspace TEXT PRIMARY KEY,
    budget_id TEXT NOT NULL UNIQUE,
    resource_version TEXT NOT NULL,
    resource_generation BIGINT NOT NULL CHECK (resource_generation > 0),
    starts_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    document JSONB NOT NULL,
    written_at TIMESTAMPTZ NOT NULL,
    CHECK (expires_at > starts_at)
);
CREATE INDEX IF NOT EXISTS %s ON %s (expires_at);`, budgetTable,
		indexName(budgetTable, "expires_idx"), budgetTable)
	reservations := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
    reservation_id TEXT PRIMARY KEY,
    idempotency_scope TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    request_digest TEXT NOT NULL,
    subject TEXT NOT NULL,
    workspace TEXT NOT NULL,
    budget_id TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('reserved','committed','released','expired')),
    reserved_requests BIGINT NOT NULL CHECK (reserved_requests >= 0),
    reserved_input_tokens BIGINT NOT NULL CHECK (reserved_input_tokens >= 0),
    reserved_output_tokens BIGINT NOT NULL CHECK (reserved_output_tokens >= 0),
    reserved_cost_micros BIGINT NOT NULL CHECK (reserved_cost_micros >= 0),
    actual_requests BIGINT NOT NULL CHECK (actual_requests >= 0),
    actual_input_tokens BIGINT NOT NULL CHECK (actual_input_tokens >= 0),
    actual_output_tokens BIGINT NOT NULL CHECK (actual_output_tokens >= 0),
    actual_cost_micros BIGINT NOT NULL CHECK (actual_cost_micros >= 0),
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    finalized_at TIMESTAMPTZ NULL,
    resource_version TEXT NOT NULL,
    resource_generation BIGINT NOT NULL CHECK (resource_generation > 0),
    document JSONB NOT NULL,
    written_at TIMESTAMPTZ NOT NULL,
    UNIQUE (idempotency_scope, idempotency_key),
    CHECK (expires_at > created_at),
    CHECK ((state = 'reserved') = (finalized_at IS NULL))
);
CREATE INDEX IF NOT EXISTS %s ON %s (budget_id, state, expires_at);
CREATE INDEX IF NOT EXISTS %s ON %s (workspace, created_at DESC);`, reservationTable,
		indexName(reservationTable, "budget_state_idx"), reservationTable,
		indexName(reservationTable, "workspace_idx"), reservationTable)
	return []string{entitlements, budgets, reservations}, nil
}

func indexName(table, suffix string) string {
	base := strings.ReplaceAll(table, ".", "_")
	maximumBase := maximumPostgresIdentifierBytes - len(suffix) - 1
	if len(base) > maximumBase {
		base = base[:maximumBase]
	}
	return base + "_" + suffix
}
