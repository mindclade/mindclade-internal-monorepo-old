// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package admissionpostgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"go.mindclade.dev/control/admission"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/resourceversion"
)

var _ admission.GovernanceRepository = (*Store)(nil)

func (store *Store) CurrentBundle(ctx context.Context, workspaceID identifiers.ID) (admission.WorkspacePolicyBundle, error) {
	const operation = "admission.postgres.CurrentBundle"
	if err := store.validate(ctx, operation); err != nil {
		return admission.WorkspacePolicyBundle{}, err
	}
	if err := workspaceID.Validate(); err != nil || workspaceID.Kind().String() != "workspace" {
		return admission.WorkspacePolicyBundle{}, domainError(ctx, faults.CodeInvalidArgument, "workspace_id_invalid", "workspace ID is invalid", operation)
	}
	var document []byte
	err := store.executor(ctx).QueryRowContext(ctx,
		fmt.Sprintf(`SELECT document FROM %s WHERE workspace_id=$1`, store.bundles), workspaceID.String()).Scan(&document)
	if errors.Is(err, sql.ErrNoRows) {
		return admission.WorkspacePolicyBundle{}, domainError(ctx, faults.CodeNotFound, "policy_bundle_not_found", "policy bundle was not found", operation)
	}
	if err != nil {
		return admission.WorkspacePolicyBundle{}, provider(ctx, err, operation)
	}
	return decodeDocument[admission.WorkspacePolicyBundle](ctx, document, operation)
}

func (store *Store) ListEntitlements(ctx context.Context, workspace string) ([]admission.Entitlement, error) {
	const operation = "admission.postgres.ListEntitlements"
	if err := store.validate(ctx, operation); err != nil {
		return nil, err
	}
	rows, err := store.executor(ctx).QueryContext(ctx,
		fmt.Sprintf(`SELECT document FROM %s WHERE workspace=$1 ORDER BY subject`, store.entitlements), workspace)
	if err != nil {
		return nil, provider(ctx, err, operation)
	}
	defer rows.Close()
	result := make([]admission.Entitlement, 0)
	for rows.Next() {
		var document []byte
		if err := rows.Scan(&document); err != nil {
			return nil, provider(ctx, err, operation)
		}
		value, err := decodeDocument[admission.Entitlement](ctx, document, operation)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, provider(ctx, err, operation)
	}
	return result, nil
}

func (store *Store) CreateProposal(ctx context.Context, proposal admission.PolicyProposal) error {
	const operation = "admission.postgres.CreateProposal"
	if err := proposal.Validate(); err != nil {
		return err
	}
	if proposal.State != admission.ProposalPending || proposal.Version.Generation() != 1 {
		return domainError(ctx, faults.CodeInvalidArgument, "policy_proposal_initial_state_invalid", "new policy proposal must be pending generation one", operation)
	}
	document, err := marshalDocument(ctx, proposal, operation)
	if err != nil {
		return err
	}
	generation, err := sqlUint(proposal.Version.Generation(), "resource_generation")
	if err != nil {
		return err
	}
	_, err = runMutation(ctx, store, operation, func(txContext context.Context) (struct{}, error) {
		if err := store.checkBundleBase(txContext, proposal.Spec.WorkspaceID, proposal.BaseVersion); err != nil {
			return struct{}{}, err
		}
		var base any
		if !proposal.BaseVersion.IsZero() {
			base = proposal.BaseVersion.String()
		}
		_, execErr := store.executor(txContext).ExecContext(txContext, fmt.Sprintf(`INSERT INTO %s (
proposal_id,workspace_id,tenant_id,base_resource_version,proposer_key,state,decision_key,
proposed_at,expires_at,decided_at,resource_version,resource_generation,document,written_at
) VALUES ($1,$2,$3,$4,$5,$6,NULL,$7,$8,NULL,$9,$10,$11::jsonb,$12)`, store.proposals),
			proposal.ID.String(), proposal.Spec.WorkspaceID.String(), proposal.Spec.TenantID.String(), base,
			proposal.ProposerKey, string(proposal.State), proposal.ProposedAt, proposal.ExpiresAt,
			proposal.Version.String(), generation, document, store.clock.Now().Round(0).UTC())
		if execErr != nil {
			return struct{}{}, provider(txContext, execErr, operation)
		}
		return struct{}{}, store.emitPolicy(txContext, "ai_gateway.policy_proposals.create", "gateway_policy_proposal",
			proposal.ID, proposal.Spec.WorkspaceID.String(), "control.admission.policy_proposal.v1", document)
	})
	return err
}

func (store *Store) GetProposal(ctx context.Context, id identifiers.ID) (admission.PolicyProposal, error) {
	const operation = "admission.postgres.GetProposal"
	if err := store.validate(ctx, operation); err != nil {
		return admission.PolicyProposal{}, err
	}
	if err := id.Validate(); err != nil || id.Kind().String() != "policyproposal" {
		return admission.PolicyProposal{}, domainError(ctx, faults.CodeInvalidArgument, "policy_proposal_id_invalid", "policy proposal ID is invalid", operation)
	}
	var document []byte
	err := store.executor(ctx).QueryRowContext(ctx,
		fmt.Sprintf(`SELECT document FROM %s WHERE proposal_id=$1`, store.proposals), id.String()).Scan(&document)
	if errors.Is(err, sql.ErrNoRows) {
		return admission.PolicyProposal{}, domainError(ctx, faults.CodeNotFound, "policy_proposal_not_found", "policy proposal was not found", operation)
	}
	if err != nil {
		return admission.PolicyProposal{}, provider(ctx, err, operation)
	}
	return decodeDocument[admission.PolicyProposal](ctx, document, operation)
}

func (store *Store) TransitionProposal(ctx context.Context, terminal admission.PolicyProposal, expected resourceversion.Version) error {
	const operation = "admission.postgres.TransitionProposal"
	if err := terminal.Validate(); err != nil {
		return err
	}
	if terminal.State != admission.ProposalRejected && terminal.State != admission.ProposalCancelled || terminal.Version.Generation() != expected.Generation()+1 {
		return domainError(ctx, faults.CodeInvalidArgument, "policy_proposal_transition_invalid", "policy proposal transition is invalid", operation)
	}
	document, err := marshalDocument(ctx, terminal, operation)
	if err != nil {
		return err
	}
	generation, err := sqlUint(terminal.Version.Generation(), "resource_generation")
	if err != nil {
		return err
	}
	_, err = runMutation(ctx, store, operation, func(txContext context.Context) (struct{}, error) {
		result, execErr := store.executor(txContext).ExecContext(txContext, fmt.Sprintf(`UPDATE %s SET
state=$1,decision_key=$2,decided_at=$3,resource_version=$4,resource_generation=$5,document=$6::jsonb,written_at=$7
WHERE proposal_id=$8 AND state='pending' AND resource_version=$9`, store.proposals),
			string(terminal.State), terminal.DecisionKey, terminal.DecidedAt, terminal.Version.String(), generation,
			document, store.clock.Now().Round(0).UTC(), terminal.ID.String(), expected.String())
		if execErr != nil {
			return struct{}{}, provider(txContext, execErr, operation)
		}
		affected, affectedErr := result.RowsAffected()
		if affectedErr != nil {
			return struct{}{}, provider(txContext, affectedErr, operation)
		}
		if affected != 1 {
			return struct{}{}, domainError(txContext, faults.CodeConflict, "policy_proposal_version_stale", "policy proposal version is stale", operation)
		}
		return struct{}{}, store.emitPolicy(txContext, "ai_gateway.policy_proposals."+string(terminal.State), "gateway_policy_proposal",
			terminal.ID, terminal.Spec.WorkspaceID.String(), "control.admission.policy_proposal.v1", document)
	})
	return err
}

func (store *Store) ApplyApproved(
	ctx context.Context,
	proposal admission.PolicyProposal,
	expected resourceversion.Version,
	bundle admission.WorkspacePolicyBundle,
	entitlements []admission.Entitlement,
	budget admission.Budget,
	receipt admission.PolicyApprovalReceipt,
	now time.Time,
) error {
	const operation = "admission.postgres.ApplyApproved"
	if err := proposal.Validate(); err != nil {
		return err
	}
	if proposal.State != admission.ProposalApplied || proposal.Version.Generation() != expected.Generation()+1 {
		return domainError(ctx, faults.CodeInvalidArgument, "policy_proposal_approval_invalid", "approved proposal state is invalid", operation)
	}
	if err := bundle.Validate(); err != nil {
		return err
	}
	if err := budget.Validate(); err != nil {
		return err
	}
	if err := receipt.Validate(); err != nil {
		return err
	}
	if len(entitlements) == 0 {
		return domainError(ctx, faults.CodeInvalidArgument, "approved_entitlements_empty", "approved bundle must publish entitlements", operation)
	}
	for _, entitlement := range entitlements {
		if err := entitlement.Validate(); err != nil {
			return err
		}
	}
	now = now.Round(0).UTC()
	if now.IsZero() || !budget.ActiveAt(now) || proposal.ID != receipt.ProposalID || bundle.ID != receipt.BundleID ||
		bundle.Version != receipt.BundleVersion || !receipt.AppliedAt.Equal(now) {
		return domainError(ctx, faults.CodeInvalidArgument, "policy_approval_binding_invalid", "approved policy publication is not internally consistent", operation)
	}

	_, err := runMutation(ctx, store, operation, func(txContext context.Context) (struct{}, error) {
		stored, lockErr := store.lockProposal(txContext, proposal.ID)
		if lockErr != nil {
			return struct{}{}, lockErr
		}
		if stored.State != admission.ProposalPending || stored.Version != expected || stored.BaseVersion != proposal.BaseVersion ||
			stored.ProposerKey != proposal.ProposerKey || stored.Spec.WorkspaceID != proposal.Spec.WorkspaceID {
			return struct{}{}, domainError(txContext, faults.CodeConflict, "policy_proposal_version_stale", "policy proposal version is stale", operation)
		}
		if !now.Before(stored.ExpiresAt) {
			return struct{}{}, domainError(txContext, faults.CodeFailedPrecondition, "policy_proposal_expired", "policy proposal has expired", operation)
		}
		if err := store.checkBundleBaseLocked(txContext, bundle.Spec.WorkspaceID, proposal.BaseVersion); err != nil {
			return struct{}{}, err
		}
		if err := store.writeApprovedBundle(txContext, bundle, now); err != nil {
			return struct{}{}, err
		}
		for _, entitlement := range entitlements {
			if err := store.writeApprovedEntitlement(txContext, entitlement, now); err != nil {
				return struct{}{}, err
			}
		}
		if err := store.writeApprovedBudget(txContext, budget, now); err != nil {
			return struct{}{}, err
		}
		proposalDocument, err := marshalDocument(txContext, proposal, operation)
		if err != nil {
			return struct{}{}, err
		}
		proposalGeneration, err := sqlUint(proposal.Version.Generation(), "resource_generation")
		if err != nil {
			return struct{}{}, err
		}
		result, err := store.executor(txContext).ExecContext(txContext, fmt.Sprintf(`UPDATE %s SET
state=$1,decision_key=$2,decided_at=$3,resource_version=$4,resource_generation=$5,document=$6::jsonb,written_at=$7
WHERE proposal_id=$8 AND state='pending' AND resource_version=$9`, store.proposals),
			string(proposal.State), proposal.DecisionKey, proposal.DecidedAt, proposal.Version.String(), proposalGeneration,
			proposalDocument, now, proposal.ID.String(), expected.String())
		if err != nil {
			return struct{}{}, provider(txContext, err, operation)
		}
		if err := requireOneRow(txContext, result, operation, "policy_proposal_version_stale", "policy proposal version is stale"); err != nil {
			return struct{}{}, err
		}
		receiptDocument, err := marshalDocument(txContext, receipt, operation)
		if err != nil {
			return struct{}{}, err
		}
		_, err = store.executor(txContext).ExecContext(txContext, fmt.Sprintf(`INSERT INTO %s (
receipt_id,proposal_id,workspace_id,bundle_id,bundle_resource_version,applied_at,document,written_at
) VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,$8)`, store.receipts), receipt.ID.String(), receipt.ProposalID.String(),
			bundle.Spec.WorkspaceID.String(), receipt.BundleID.String(), receipt.BundleVersion.String(), receipt.AppliedAt, receiptDocument, now)
		if err != nil {
			return struct{}{}, provider(txContext, err, operation)
		}
		bundleDocument, err := marshalDocument(txContext, bundle, operation)
		if err != nil {
			return struct{}{}, err
		}
		if err := store.emitPolicy(txContext, "ai_gateway.policy_bundles.apply", "gateway_policy_bundle", bundle.ID,
			bundle.Spec.WorkspaceID.String(), "control.admission.policy_bundle.v1", bundleDocument); err != nil {
			return struct{}{}, err
		}
		if err := store.emitPolicy(txContext, "ai_gateway.policy_proposals.applied", "gateway_policy_proposal", proposal.ID,
			bundle.Spec.WorkspaceID.String(), "control.admission.policy_proposal.v1", proposalDocument); err != nil {
			return struct{}{}, err
		}
		return struct{}{}, store.emitPolicy(txContext, "ai_gateway.policy_receipts.create", "gateway_policy_receipt", receipt.ID,
			bundle.Spec.WorkspaceID.String(), "control.admission.policy_receipt.v1", receiptDocument)
	})
	return err
}

func (store *Store) lockProposal(ctx context.Context, id identifiers.ID) (admission.PolicyProposal, error) {
	const operation = "admission.postgres.lockProposal"
	var document []byte
	err := store.executor(ctx).QueryRowContext(ctx,
		fmt.Sprintf(`SELECT document FROM %s WHERE proposal_id=$1 FOR UPDATE`, store.proposals), id.String()).Scan(&document)
	if errors.Is(err, sql.ErrNoRows) {
		return admission.PolicyProposal{}, domainError(ctx, faults.CodeNotFound, "policy_proposal_not_found", "policy proposal was not found", operation)
	}
	if err != nil {
		return admission.PolicyProposal{}, provider(ctx, err, operation)
	}
	return decodeDocument[admission.PolicyProposal](ctx, document, operation)
}

func (store *Store) checkBundleBase(ctx context.Context, workspaceID identifiers.ID, expected resourceversion.Version) error {
	const operation = "admission.postgres.checkBundleBase"
	var stored string
	err := store.executor(ctx).QueryRowContext(ctx,
		fmt.Sprintf(`SELECT resource_version FROM %s WHERE workspace_id=$1`, store.bundles), workspaceID.String()).Scan(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		if expected.IsZero() {
			return nil
		}
		return domainError(ctx, faults.CodeConflict, "policy_proposal_base_stale", "policy proposal base bundle is stale", operation)
	}
	if err != nil {
		return provider(ctx, err, operation)
	}
	if expected.IsZero() || stored != expected.String() {
		return domainError(ctx, faults.CodeConflict, "policy_proposal_base_stale", "policy proposal base bundle is stale", operation)
	}
	return nil
}

func (store *Store) checkBundleBaseLocked(ctx context.Context, workspaceID identifiers.ID, expected resourceversion.Version) error {
	const operation = "admission.postgres.checkBundleBaseLocked"
	var stored string
	err := store.executor(ctx).QueryRowContext(ctx,
		fmt.Sprintf(`SELECT resource_version FROM %s WHERE workspace_id=$1 FOR UPDATE`, store.bundles), workspaceID.String()).Scan(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		if expected.IsZero() {
			return nil
		}
		return domainError(ctx, faults.CodeConflict, "policy_proposal_base_stale", "policy proposal base bundle is stale", operation)
	}
	if err != nil {
		return provider(ctx, err, operation)
	}
	if expected.IsZero() || stored != expected.String() {
		return domainError(ctx, faults.CodeConflict, "policy_proposal_base_stale", "policy proposal base bundle is stale", operation)
	}
	return nil
}

func (store *Store) writeApprovedBundle(ctx context.Context, bundle admission.WorkspacePolicyBundle, now time.Time) error {
	const operation = "admission.postgres.writeApprovedBundle"
	document, err := marshalDocument(ctx, bundle, operation)
	if err != nil {
		return err
	}
	generation, err := sqlUint(bundle.Version.Generation(), "resource_generation")
	if err != nil {
		return err
	}
	epoch, err := sqlUint(bundle.Spec.PolicyEpoch, "policy_epoch")
	if err != nil {
		return err
	}
	result, err := store.executor(ctx).ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s (
workspace_id,tenant_id,bundle_id,policy_epoch,resource_version,resource_generation,effective_at,expires_at,document,written_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10)
ON CONFLICT (workspace_id) DO UPDATE SET tenant_id=EXCLUDED.tenant_id,bundle_id=EXCLUDED.bundle_id,
policy_epoch=EXCLUDED.policy_epoch,resource_version=EXCLUDED.resource_version,resource_generation=EXCLUDED.resource_generation,
effective_at=EXCLUDED.effective_at,expires_at=EXCLUDED.expires_at,document=EXCLUDED.document,written_at=EXCLUDED.written_at
WHERE %s.tenant_id=EXCLUDED.tenant_id AND %s.bundle_id=EXCLUDED.bundle_id
AND %s.resource_generation=EXCLUDED.resource_generation-1 AND %s.policy_epoch=EXCLUDED.policy_epoch-1`,
		store.bundles, store.bundles, store.bundles, store.bundles, store.bundles), bundle.Spec.WorkspaceID.String(),
		bundle.Spec.TenantID.String(), bundle.ID.String(), epoch, bundle.Version.String(), generation,
		bundle.Spec.EffectiveAt, bundle.Spec.ExpiresAt, document, now)
	if err != nil {
		return provider(ctx, err, operation)
	}
	return requireOneRow(ctx, result, operation, "policy_bundle_update_invalid", "policy bundle identity, tenant, epoch, and version must advance exactly")
}

func (store *Store) writeApprovedEntitlement(ctx context.Context, entitlement admission.Entitlement, now time.Time) error {
	const operation = "admission.postgres.writeApprovedEntitlement"
	document, err := marshalDocument(ctx, entitlement, operation)
	if err != nil {
		return err
	}
	generation, err := sqlUint(entitlement.Version.Generation(), "resource_generation")
	if err != nil {
		return err
	}
	epoch, err := sqlUint(entitlement.PolicyEpoch, "policy_epoch")
	if err != nil {
		return err
	}
	result, err := store.executor(ctx).ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s (
subject,workspace,entitlement_id,policy_epoch,resource_version,resource_generation,not_before,expires_at,document,written_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10)
ON CONFLICT (subject,workspace) DO UPDATE SET entitlement_id=EXCLUDED.entitlement_id,policy_epoch=EXCLUDED.policy_epoch,
resource_version=EXCLUDED.resource_version,resource_generation=EXCLUDED.resource_generation,not_before=EXCLUDED.not_before,
expires_at=EXCLUDED.expires_at,document=EXCLUDED.document,written_at=EXCLUDED.written_at
WHERE %s.entitlement_id=EXCLUDED.entitlement_id AND %s.policy_epoch=EXCLUDED.policy_epoch-1
AND %s.resource_generation=EXCLUDED.resource_generation-1`, store.entitlements, store.entitlements, store.entitlements, store.entitlements),
		entitlement.Subject, entitlement.Workspace, entitlement.ID.String(), epoch, entitlement.Version.String(), generation,
		entitlement.NotBefore, entitlement.ExpiresAt, document, now)
	if err != nil {
		return provider(ctx, err, operation)
	}
	return requireOneRow(ctx, result, operation, "entitlement_update_invalid", "entitlement identity, policy epoch, and version must advance exactly")
}

func (store *Store) writeApprovedBudget(ctx context.Context, budget admission.Budget, now time.Time) error {
	const operation = "admission.postgres.writeApprovedBudget"
	if !budget.ActiveAt(now) {
		return domainError(ctx, faults.CodeFailedPrecondition, "budget_publication_window_inactive", "budget must be active at publication time", operation)
	}
	document, err := marshalDocument(ctx, budget, operation)
	if err != nil {
		return err
	}
	generation, err := sqlUint(budget.Version.Generation(), "resource_generation")
	if err != nil {
		return err
	}
	limits := make([]int64, 0, 4)
	for _, unit := range []admission.Unit{admission.UnitRequests, admission.UnitInputTokens, admission.UnitOutputTokens, admission.UnitCostMicros} {
		limit, err := sqlUint(budget.Limit[unit], "budget_limit")
		if err != nil {
			return err
		}
		limits = append(limits, limit)
	}
	result, err := store.executor(ctx).ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s (
workspace,budget_id,resource_version,resource_generation,starts_at,expires_at,document,written_at
) VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,$8)
ON CONFLICT (workspace) DO UPDATE SET budget_id=EXCLUDED.budget_id,resource_version=EXCLUDED.resource_version,
resource_generation=EXCLUDED.resource_generation,starts_at=EXCLUDED.starts_at,expires_at=EXCLUDED.expires_at,
document=EXCLUDED.document,written_at=EXCLUDED.written_at
WHERE %s.resource_generation=EXCLUDED.resource_generation-1
AND ((%s.budget_id<>EXCLUDED.budget_id AND %s.expires_at<=$8 AND EXCLUDED.starts_at>=%s.expires_at)
 OR (%s.budget_id=EXCLUDED.budget_id AND %s.starts_at=EXCLUDED.starts_at AND %s.expires_at=EXCLUDED.expires_at
 AND (SELECT
 COALESCE(SUM(CASE WHEN ledger.state='reserved' AND ledger.expires_at>$8 OR ledger.state IN ('dispatched','reconciliation_pending') THEN ledger.reserved_requests WHEN ledger.state='committed' THEN ledger.actual_requests ELSE 0 END),0)<=$9 AND
 COALESCE(SUM(CASE WHEN ledger.state='reserved' AND ledger.expires_at>$8 OR ledger.state IN ('dispatched','reconciliation_pending') THEN ledger.reserved_input_tokens WHEN ledger.state='committed' THEN ledger.actual_input_tokens ELSE 0 END),0)<=$10 AND
 COALESCE(SUM(CASE WHEN ledger.state='reserved' AND ledger.expires_at>$8 OR ledger.state IN ('dispatched','reconciliation_pending') THEN ledger.reserved_output_tokens WHEN ledger.state='committed' THEN ledger.actual_output_tokens ELSE 0 END),0)<=$11 AND
 COALESCE(SUM(CASE WHEN ledger.state='reserved' AND ledger.expires_at>$8 OR ledger.state IN ('dispatched','reconciliation_pending') THEN ledger.reserved_cost_micros WHEN ledger.state='committed' THEN ledger.actual_cost_micros ELSE 0 END),0)<=$12
 FROM %s AS ledger WHERE ledger.budget_id=%s.budget_id)))`, store.budgets, store.budgets, store.budgets,
		store.budgets, store.budgets, store.budgets, store.budgets, store.budgets, store.reservations, store.budgets),
		budget.Workspace, budget.ID.String(), budget.Version.String(), generation, budget.StartsAt, budget.ExpiresAt,
		document, now, limits[0], limits[1], limits[2], limits[3])
	if err != nil {
		return provider(ctx, err, operation)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return provider(ctx, err, operation)
	}
	if affected == 1 {
		return nil
	}
	used, err := store.budgetUsage(ctx, budget.ID, now)
	if err != nil {
		return err
	}
	if !used.Fits(budget.Limit) {
		return domainError(ctx, faults.CodeFailedPrecondition, "budget_limit_below_usage", "budget limit cannot be reduced below durable usage", operation)
	}
	return domainError(ctx, faults.CodeConflict, "budget_update_invalid", "budget identity, window, and version transition is invalid", operation)
}

func requireOneRow(ctx context.Context, result sql.Result, operation, reason, message string) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return provider(ctx, err, operation)
	}
	if affected != 1 {
		return domainError(ctx, faults.CodeConflict, reason, message, operation)
	}
	return nil
}
