// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package admissionpostgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"go.mindclade.dev/control/admission"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/retry"
	"go.mindclade.dev/libs/go/storage/sql/transaction"
)

var _ admission.Repository = (*Store)(nil)

type executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// Readiness verifies that every table and security/accounting column used by
// the adapter is queryable. It intentionally does not treat an empty policy
// catalog as healthy authorization; missing policy still fails each decision
// closed with the domain's entitlement/budget errors.
func (store *Store) Readiness(ctx context.Context) error {
	const operation = "admission.postgres.Readiness"
	if err := store.validate(ctx, operation); err != nil {
		return err
	}
	queries := []string{
		fmt.Sprintf(`SELECT subject,workspace,entitlement_id,policy_epoch,resource_version,resource_generation,not_before,expires_at,document,written_at FROM %s LIMIT 0`, store.entitlements),
		fmt.Sprintf(`SELECT workspace,budget_id,resource_version,resource_generation,starts_at,expires_at,document,written_at FROM %s LIMIT 0`, store.budgets),
		fmt.Sprintf(`SELECT reservation_id,idempotency_scope,idempotency_key,request_digest,subject,workspace,budget_id,state,
reserved_requests,reserved_input_tokens,reserved_output_tokens,reserved_cost_micros,
actual_requests,actual_input_tokens,actual_output_tokens,actual_cost_micros,
created_at,expires_at,finalized_at,resource_version,resource_generation,document,written_at FROM %s LIMIT 0`, store.reservations),
	}
	for _, query := range queries {
		rows, err := store.db.QueryContext(ctx, query)
		if err != nil {
			return provider(ctx, err, operation)
		}
		if err := rows.Close(); err != nil {
			return provider(ctx, err, operation)
		}
	}
	return nil
}

func (store *Store) executor(ctx context.Context) executor {
	if tx, ok := transaction.FromContext(ctx); ok {
		return tx
	}
	return store.db
}

func (store *Store) validate(ctx context.Context, operation string) error {
	if ctx == nil {
		return domainError(ctx, faults.CodeInvalidArgument, "admission_nil_context", "context is required", operation)
	}
	if store == nil || store.db == nil || nilInterface(store.clock) || store.generator == nil ||
		nilInterface(store.recorder) || nilInterface(store.messages) || store.audits == nil || store.events == nil || store.retries == nil {
		return domainError(ctx, faults.CodeFailedPrecondition, "admission_store_unconfigured", "admission store is not configured", operation)
	}
	if err := ctx.Err(); err != nil {
		return faults.Wrap(err, faults.CodeOf(err), faults.PublicMessageOf(err),
			faults.WithReason("admission_context_done"), faults.WithOperation(operation),
			faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return nil
}

func runMutation[T any](ctx context.Context, store *Store, operation string, function func(context.Context) (T, error)) (T, error) {
	return runMutationAtIsolation(ctx, store, operation, sql.LevelSerializable, function)
}

func runMutationAtIsolation[T any](ctx context.Context, store *Store, operation string, isolation sql.IsolationLevel, function func(context.Context) (T, error)) (T, error) {
	var zero T
	if err := store.validate(ctx, operation); err != nil {
		return zero, err
	}
	if _, nested := transaction.FromContext(ctx); nested {
		return zero, domainError(ctx, faults.CodeFailedPrecondition,
			"admission_nested_transaction_unsupported",
			"admission mutations require their own transaction", operation)
	}
	var result T
	_, err := store.retries.Do(ctx, operation, func(attemptContext context.Context, _ retry.Attempt) error {
		var attemptErr error
		result, attemptErr = transaction.Run(attemptContext, store.db,
			transaction.Options{Isolation: isolation},
			func(txContext context.Context, _ *sql.Tx) (T, error) { return function(txContext) })
		if faults.IsCode(attemptErr, faults.CodeAborted) && faults.IsReason(attemptErr, "sql_transaction_failed") {
			return faults.Wrap(attemptErr, faults.CodeAborted, "admission transaction must be retried",
				faults.WithReason("admission_serialization_retry"), faults.WithOperation(operation),
				faults.WithRetryPolicy(faults.BackoffRetry(admissionMutationMaxAttempts)), faults.WithContextMetadata(attemptContext))
		}
		return attemptErr
	})
	return result, err
}

func marshalDocument(ctx context.Context, value any, operation string) ([]byte, error) {
	document, err := json.Marshal(value)
	if err != nil {
		return nil, internal(ctx, err, operation, "admission_document_encoding_failed")
	}
	return document, nil
}

func decodeDocument[T interface{ Validate() error }](ctx context.Context, document []byte, operation string) (T, error) {
	var value T
	if err := json.Unmarshal(document, &value); err != nil {
		return value, internal(ctx, err, operation, "admission_document_decoding_failed")
	}
	if err := value.Validate(); err != nil {
		return value, internal(ctx, err, operation, "admission_document_invalid")
	}
	return value, nil
}

func sqlUint(value uint64, label string) (int64, error) {
	if value > math.MaxInt64 {
		return 0, domainError(context.Background(), faults.CodeInvalidArgument,
			"admission_"+label+"_out_of_range", label+" is outside PostgreSQL bounds", "admission.postgres")
	}
	return int64(value), nil
}

func (store *Store) Snapshot(ctx context.Context, subject, workspace string) (admission.PolicySnapshot, error) {
	const operation = "admission.postgres.Snapshot"
	if err := store.validate(ctx, operation); err != nil {
		return admission.PolicySnapshot{}, err
	}
	var entitlementDocument []byte
	err := store.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT document FROM %s WHERE subject=$1 AND workspace=$2`, store.entitlements),
		subject, workspace).Scan(&entitlementDocument)
	if errors.Is(err, sql.ErrNoRows) {
		return admission.PolicySnapshot{}, domainError(ctx, faults.CodeNotFound, "entitlement_not_found", "entitlement was not found", operation)
	}
	if err != nil {
		return admission.PolicySnapshot{}, provider(ctx, err, operation)
	}
	entitlement, err := decodeDocument[admission.Entitlement](ctx, entitlementDocument, operation)
	if err != nil {
		return admission.PolicySnapshot{}, err
	}
	var budgetDocument []byte
	err = store.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT document FROM %s WHERE workspace=$1`, store.budgets), workspace).Scan(&budgetDocument)
	if errors.Is(err, sql.ErrNoRows) {
		return admission.PolicySnapshot{}, domainError(ctx, faults.CodeNotFound, "budget_not_found", "budget was not found", operation)
	}
	if err != nil {
		return admission.PolicySnapshot{}, provider(ctx, err, operation)
	}
	budget, err := decodeDocument[admission.Budget](ctx, budgetDocument, operation)
	if err != nil {
		return admission.PolicySnapshot{}, err
	}
	snapshot := admission.PolicySnapshot{Entitlement: entitlement, Budget: budget}
	if err := snapshot.Validate(); err != nil {
		return admission.PolicySnapshot{}, internal(ctx, err, operation, "admission_policy_snapshot_invalid")
	}
	return snapshot, nil
}

func (store *Store) PutEntitlement(ctx context.Context, entitlement admission.Entitlement) error {
	const operation = "admission.postgres.PutEntitlement"
	if err := entitlement.Validate(); err != nil {
		return err
	}
	generation, err := sqlUint(entitlement.Version.Generation(), "resource_generation")
	if err != nil {
		return err
	}
	policyEpoch, err := sqlUint(entitlement.PolicyEpoch, "policy_epoch")
	if err != nil {
		return err
	}
	document, err := marshalDocument(ctx, entitlement, operation)
	if err != nil {
		return err
	}
	_, err = runMutation(ctx, store, operation, func(txContext context.Context) (struct{}, error) {
		query := fmt.Sprintf(`INSERT INTO %s (
subject,workspace,entitlement_id,policy_epoch,resource_version,resource_generation,
not_before,expires_at,document,written_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10)
ON CONFLICT (subject,workspace) DO UPDATE SET
entitlement_id=EXCLUDED.entitlement_id,policy_epoch=EXCLUDED.policy_epoch,
resource_version=EXCLUDED.resource_version,resource_generation=EXCLUDED.resource_generation,
not_before=EXCLUDED.not_before,expires_at=EXCLUDED.expires_at,
document=EXCLUDED.document,written_at=EXCLUDED.written_at
WHERE %s.resource_generation < EXCLUDED.resource_generation`, store.entitlements, store.entitlements)
		result, execErr := store.executor(txContext).ExecContext(txContext, query,
			entitlement.Subject, entitlement.Workspace, entitlement.ID.String(), policyEpoch,
			entitlement.Version.String(), generation, entitlement.NotBefore, entitlement.ExpiresAt,
			document, store.clock.Now().Round(0).UTC())
		if execErr != nil {
			return struct{}{}, provider(txContext, execErr, operation)
		}
		affected, affectedErr := result.RowsAffected()
		if affectedErr != nil {
			return struct{}{}, provider(txContext, affectedErr, operation)
		}
		if affected != 1 {
			var storedVersion string
			lookupErr := store.executor(txContext).QueryRowContext(txContext,
				fmt.Sprintf(`SELECT resource_version FROM %s WHERE subject=$1 AND workspace=$2`, store.entitlements),
				entitlement.Subject, entitlement.Workspace).Scan(&storedVersion)
			if lookupErr != nil {
				return struct{}{}, provider(txContext, lookupErr, operation+".Replay")
			}
			if storedVersion == entitlement.Version.String() {
				return struct{}{}, nil
			}
			return struct{}{}, domainError(txContext, faults.CodeConflict, "entitlement_version_not_monotonic", "entitlement version must increase", operation)
		}
		return struct{}{}, store.emitPolicy(txContext, "ai_gateway.entitlement.put", "gateway_entitlement",
			entitlement.ID, entitlement.Workspace, "control.admission.entitlement.v1", document)
	})
	return err
}

func (store *Store) PutBudget(ctx context.Context, budget admission.Budget) error {
	const operation = "admission.postgres.PutBudget"
	if err := budget.Validate(); err != nil {
		return err
	}
	generation, err := sqlUint(budget.Version.Generation(), "resource_generation")
	if err != nil {
		return err
	}
	document, err := marshalDocument(ctx, budget, operation)
	if err != nil {
		return err
	}
	_, err = runMutation(ctx, store, operation, func(txContext context.Context) (struct{}, error) {
		query := fmt.Sprintf(`INSERT INTO %s (
workspace,budget_id,resource_version,resource_generation,starts_at,expires_at,document,written_at
) VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,$8)
ON CONFLICT (workspace) DO UPDATE SET
budget_id=EXCLUDED.budget_id,resource_version=EXCLUDED.resource_version,
resource_generation=EXCLUDED.resource_generation,starts_at=EXCLUDED.starts_at,
expires_at=EXCLUDED.expires_at,document=EXCLUDED.document,written_at=EXCLUDED.written_at
WHERE %s.resource_generation < EXCLUDED.resource_generation`, store.budgets, store.budgets)
		result, execErr := store.executor(txContext).ExecContext(txContext, query,
			budget.Workspace, budget.ID.String(), budget.Version.String(), generation,
			budget.StartsAt, budget.ExpiresAt, document, store.clock.Now().Round(0).UTC())
		if execErr != nil {
			return struct{}{}, provider(txContext, execErr, operation)
		}
		affected, affectedErr := result.RowsAffected()
		if affectedErr != nil {
			return struct{}{}, provider(txContext, affectedErr, operation)
		}
		if affected != 1 {
			var storedVersion string
			lookupErr := store.executor(txContext).QueryRowContext(txContext,
				fmt.Sprintf(`SELECT resource_version FROM %s WHERE workspace=$1`, store.budgets), budget.Workspace).Scan(&storedVersion)
			if lookupErr != nil {
				return struct{}{}, provider(txContext, lookupErr, operation+".Replay")
			}
			if storedVersion == budget.Version.String() {
				return struct{}{}, nil
			}
			return struct{}{}, domainError(txContext, faults.CodeConflict, "budget_version_not_monotonic", "budget version must increase", operation)
		}
		return struct{}{}, store.emitPolicy(txContext, "ai_gateway.budget.put", "gateway_budget",
			budget.ID, budget.Workspace, "control.admission.budget.v1", document)
	})
	return err
}
