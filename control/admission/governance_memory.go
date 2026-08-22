// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package admission

import (
	"context"
	"time"

	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/resourceversion"
)

var _ GovernanceRepository = (*MemoryRepository)(nil)

func (repository *MemoryRepository) CurrentBundle(ctx context.Context, workspaceID identifiers.ID) (WorkspacePolicyBundle, error) {
	if ctx == nil {
		return WorkspacePolicyBundle{}, invalid("context_nil", "context is required", nil)
	}
	if err := ctx.Err(); err != nil {
		return WorkspacePolicyBundle{}, err
	}
	if err := validateID(workspaceID, "workspace", "workspace_id"); err != nil {
		return WorkspacePolicyBundle{}, err
	}
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	bundle, exists := repository.bundles[workspaceID.String()]
	if !exists {
		return WorkspacePolicyBundle{}, notFound("policy_bundle_not_found", "policy bundle was not found")
	}
	return cloneBundle(bundle), nil
}

func (repository *MemoryRepository) ListEntitlements(ctx context.Context, workspace string) ([]Entitlement, error) {
	if ctx == nil {
		return nil, invalid("context_nil", "context is required", nil)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateName(workspace, "workspace"); err != nil {
		return nil, err
	}
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	result := make([]Entitlement, 0)
	for _, entitlement := range repository.entitlements {
		if entitlement.Workspace == workspace {
			result = append(result, entitlement.clone())
		}
	}
	return result, nil
}

func (repository *MemoryRepository) CreateProposal(ctx context.Context, proposal PolicyProposal) error {
	if ctx == nil {
		return invalid("context_nil", "context is required", nil)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := proposal.Validate(); err != nil {
		return err
	}
	if proposal.State != ProposalPending || proposal.Version.Generation() != 1 {
		return invalid("policy_proposal_initial_state_invalid", "new policy proposal must be pending generation one", nil)
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if _, exists := repository.proposals[proposal.ID.String()]; exists {
		return conflict("policy_proposal_id_conflict", "policy proposal ID already exists")
	}
	current, exists := repository.bundles[proposal.Spec.WorkspaceID.String()]
	if exists && current.Version != proposal.BaseVersion || !exists && !proposal.BaseVersion.IsZero() {
		return conflict("policy_proposal_base_stale", "policy proposal base bundle is stale")
	}
	repository.proposals[proposal.ID.String()] = cloneProposal(proposal)
	return nil
}

func (repository *MemoryRepository) GetProposal(ctx context.Context, id identifiers.ID) (PolicyProposal, error) {
	if ctx == nil {
		return PolicyProposal{}, invalid("context_nil", "context is required", nil)
	}
	if err := ctx.Err(); err != nil {
		return PolicyProposal{}, err
	}
	if err := validateID(id, "policyproposal", "policy_proposal_id"); err != nil {
		return PolicyProposal{}, err
	}
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	proposal, exists := repository.proposals[id.String()]
	if !exists {
		return PolicyProposal{}, notFound("policy_proposal_not_found", "policy proposal was not found")
	}
	return cloneProposal(proposal), nil
}

func (repository *MemoryRepository) ApplyApproved(
	ctx context.Context,
	proposal PolicyProposal,
	expected resourceversion.Version,
	bundle WorkspacePolicyBundle,
	entitlements []Entitlement,
	budget Budget,
	receipt PolicyApprovalReceipt,
	now time.Time,
) error {
	if ctx == nil {
		return invalid("context_nil", "context is required", nil)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := proposal.Validate(); err != nil {
		return err
	}
	if proposal.State != ProposalApplied || proposal.Version.Generation() != expected.Generation()+1 {
		return invalid("policy_proposal_approval_invalid", "approved proposal state is invalid", nil)
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
		return invalid("approved_entitlements_empty", "approved bundle must publish entitlements", nil)
	}
	for _, entitlement := range entitlements {
		if err := entitlement.Validate(); err != nil {
			return err
		}
	}
	now = now.Round(0).UTC()
	repository.mu.Lock()
	defer repository.mu.Unlock()
	stored, exists := repository.proposals[proposal.ID.String()]
	if !exists {
		return notFound("policy_proposal_not_found", "policy proposal was not found")
	}
	if stored.Version != expected || stored.State != ProposalPending || stored.ID != proposal.ID || stored.BaseVersion != proposal.BaseVersion || stored.ProposerKey != proposal.ProposerKey {
		return conflict("policy_proposal_version_stale", "policy proposal version is stale")
	}
	if !now.Before(stored.ExpiresAt) {
		return failedPrecondition("policy_proposal_expired", "policy proposal has expired")
	}
	currentBundle, bundleExists := repository.bundles[bundle.Spec.WorkspaceID.String()]
	if bundleExists && currentBundle.Version != proposal.BaseVersion || !bundleExists && !proposal.BaseVersion.IsZero() {
		return conflict("policy_proposal_base_stale", "policy proposal base bundle is stale")
	}
	if bundleExists {
		if bundle.ID != currentBundle.ID || bundle.Version.Generation() != currentBundle.Version.Generation()+1 {
			return conflict("policy_bundle_update_invalid", "policy bundle identity and version must advance exactly")
		}
	} else if bundle.Version.Generation() != 1 {
		return conflict("policy_bundle_initial_version_invalid", "initial policy bundle version must be one")
	}
	if receipt.ProposalID != proposal.ID || receipt.BundleID != bundle.ID || receipt.BundleVersion != bundle.Version || !receipt.AppliedAt.Equal(now) {
		return invalid("policy_receipt_binding_invalid", "policy receipt does not bind the approved publication", nil)
	}

	for _, entitlement := range entitlements {
		key := policyKey(entitlement.Subject, entitlement.Workspace)
		if current, exists := repository.entitlements[key]; exists {
			if entitlement.ID != current.ID || entitlement.Version.Generation() != current.Version.Generation()+1 || entitlement.PolicyEpoch != current.PolicyEpoch+1 {
				return conflict("entitlement_update_invalid", "entitlement identity, policy epoch, and version must advance exactly")
			}
		} else if entitlement.Version.Generation() != 1 {
			return conflict("entitlement_initial_version_invalid", "initial entitlement version must be one")
		}
	}
	if current, exists := repository.budgets[budget.Workspace]; exists {
		if budget.Version.Generation() != current.Version.Generation()+1 {
			return conflict("budget_version_not_monotonic", "budget version must increase by one")
		}
		sameWindow := budget.StartsAt.Equal(current.StartsAt) && budget.ExpiresAt.Equal(current.ExpiresAt)
		if budget.ID == current.ID && !sameWindow || budget.ID != current.ID && (sameWindow || budget.StartsAt.Before(current.ExpiresAt)) {
			return conflict("budget_update_invalid", "budget identity and window transition is invalid")
		}
		if budget.ID == current.ID {
			used, err := repository.lockedBudgetUsage(current.ID, now)
			if err != nil {
				return err
			}
			if !used.Fits(budget.Limit) {
				return failedPrecondition("budget_limit_below_usage", "budget limit cannot be reduced below durable usage")
			}
		}
	} else if budget.Version.Generation() != 1 {
		return conflict("budget_initial_version_invalid", "initial budget version must be one")
	}

	for _, entitlement := range entitlements {
		repository.entitlements[policyKey(entitlement.Subject, entitlement.Workspace)] = entitlement.clone()
	}
	repository.budgets[budget.Workspace] = budget.clone()
	repository.bundles[bundle.Spec.WorkspaceID.String()] = cloneBundle(bundle)
	repository.proposals[proposal.ID.String()] = cloneProposal(proposal)
	repository.receipts[receipt.ID.String()] = cloneReceipt(receipt)
	return nil
}

func (repository *MemoryRepository) TransitionProposal(ctx context.Context, terminal PolicyProposal, expected resourceversion.Version) error {
	if ctx == nil {
		return invalid("context_nil", "context is required", nil)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := terminal.Validate(); err != nil {
		return err
	}
	if terminal.State != ProposalRejected && terminal.State != ProposalCancelled || terminal.Version.Generation() != expected.Generation()+1 {
		return invalid("policy_proposal_transition_invalid", "policy proposal transition is invalid", nil)
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	proposal, exists := repository.proposals[terminal.ID.String()]
	if !exists {
		return notFound("policy_proposal_not_found", "policy proposal was not found")
	}
	if proposal.State != ProposalPending || proposal.Version != expected {
		return conflict("policy_proposal_version_stale", "policy proposal version is stale")
	}
	if terminal.BaseVersion != proposal.BaseVersion || terminal.ProposerKey != proposal.ProposerKey {
		return conflict("policy_proposal_transition_mismatch", "policy proposal immutable fields changed")
	}
	repository.proposals[terminal.ID.String()] = cloneProposal(terminal)
	return nil
}

func (repository *MemoryRepository) lockedBudgetUsage(budgetID identifiers.ID, now time.Time) (Quota, error) {
	used := make(Quota)
	for _, reservation := range repository.reservations {
		if reservation.BudgetID != budgetID {
			continue
		}
		var amount Quota
		switch {
		case reservation.State == ReservationReserved && now.Before(reservation.ExpiresAt):
			amount = reservation.Reserved
		case reservation.State == ReservationDispatched || reservation.State == ReservationReconciliationPending:
			amount = reservation.Reserved
		case reservation.State == ReservationCommitted:
			amount = reservation.Actual
		default:
			continue
		}
		var err error
		used, err = used.add(amount)
		if err != nil {
			return nil, err
		}
	}
	return used, nil
}

func cloneBundle(bundle WorkspacePolicyBundle) WorkspacePolicyBundle {
	bundle.Spec = canonicalizeBundleSpec(bundle.Spec)
	return bundle
}

func cloneProposal(proposal PolicyProposal) PolicyProposal {
	proposal.Spec = canonicalizeBundleSpec(proposal.Spec)
	return proposal
}

func cloneReceipt(receipt PolicyApprovalReceipt) PolicyApprovalReceipt {
	receipt.Signature = receipt.Signature.Clone()
	return receipt
}
