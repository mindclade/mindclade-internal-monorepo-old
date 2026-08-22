// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package admission

import (
	"context"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"go.mindclade.dev/libs/go/auth"
	"go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/resourceversion"
	"go.mindclade.dev/libs/go/signing"
)

const (
	DefaultProposalTTL      = 24 * time.Hour
	MaximumGatewayBodyBytes = 16 << 20
)

// GatewayOperation is one supported OpenAI-compatible operation. Native
// provider passthrough is intentionally absent.
type GatewayOperation string

const (
	OperationChatCompletions GatewayOperation = "chat.completions"
	OperationResponses       GatewayOperation = "responses"
	OperationEmbeddings      GatewayOperation = "embeddings"
)

func (operation GatewayOperation) Valid() bool {
	switch operation {
	case OperationChatCompletions, OperationResponses, OperationEmbeddings:
		return true
	default:
		return false
	}
}

// EndpointPolicy binds one public alias to exactly one provider/model and one
// logical credential connection. Split routing and silent failover are not
// representable in the v1 contract.
type EndpointPolicy struct {
	Name                   string             `json:"name"`
	Route                  GatewayRoute       `json:"route"`
	Operations             []GatewayOperation `json:"operations"`
	ConnectionRef          string             `json:"connection_ref"`
	GuardrailRefs          []string           `json:"guardrail_refs,omitempty"`
	MaximumBodyBytes       uint64             `json:"maximum_body_bytes"`
	MaximumRequest         Quota              `json:"maximum_request"`
	PricingVersion         uint64             `json:"pricing_version"`
	RequestMicros          uint64             `json:"request_micros"`
	InputMicrosPerMillion  uint64             `json:"input_micros_per_million"`
	OutputMicrosPerMillion uint64             `json:"output_micros_per_million"`
	MetadataOnlyTracing    bool               `json:"metadata_only_tracing"`
	UsageTracking          bool               `json:"usage_tracking"`
}

func (policy EndpointPolicy) Validate() error {
	if err := validateName(policy.Name, "endpoint"); err != nil {
		return err
	}
	if err := policy.Route.Validate(); err != nil {
		return err
	}
	if policy.Route.Endpoint != policy.Name {
		return invalid("endpoint_route_alias_mismatch", "endpoint route alias does not match its name", nil)
	}
	if err := validateName(policy.ConnectionRef, "connection_ref"); err != nil {
		return err
	}
	if len(policy.Operations) == 0 || len(policy.Operations) > 3 {
		return invalid("endpoint_operations_invalid", "endpoint operations are outside bounds", nil)
	}
	seenOperations := make(map[GatewayOperation]struct{}, len(policy.Operations))
	for _, operation := range policy.Operations {
		if !operation.Valid() {
			return invalid("endpoint_operation_invalid", "endpoint operation is invalid", nil)
		}
		if _, exists := seenOperations[operation]; exists {
			return invalid("endpoint_operation_duplicate", "endpoint operation is duplicated", nil)
		}
		seenOperations[operation] = struct{}{}
	}
	if len(policy.GuardrailRefs) != 0 {
		return invalid("endpoint_guardrails_unsupported", "endpoint guardrails are fail-closed until the guardrail execution path is qualified", nil)
	}
	seenGuardrails := make(map[string]struct{}, len(policy.GuardrailRefs))
	for _, guardrail := range policy.GuardrailRefs {
		if err := validateName(guardrail, "guardrail_ref"); err != nil {
			return err
		}
		if _, exists := seenGuardrails[guardrail]; exists {
			return invalid("endpoint_guardrail_duplicate", "endpoint guardrail is duplicated", nil)
		}
		seenGuardrails[guardrail] = struct{}{}
	}
	if policy.MaximumBodyBytes == 0 || policy.MaximumBodyBytes > MaximumGatewayBodyBytes {
		return invalid("endpoint_body_limit_invalid", "endpoint body limit is outside bounds", nil)
	}
	if err := policy.MaximumRequest.Validate(true); err != nil {
		return err
	}
	if policy.MaximumRequest[UnitRequests] != 1 {
		return invalid("endpoint_request_count_invalid", "endpoint policy must authorize exactly one request", nil)
	}
	if policy.PricingVersion == 0 {
		return invalid("endpoint_pricing_version_invalid", "endpoint pricing version is required", nil)
	}
	if !policy.MetadataOnlyTracing || policy.UsageTracking {
		return invalid("endpoint_observability_policy_invalid", "governed endpoints require metadata-only tracing and disabled MLflow usage tracking", nil)
	}
	return nil
}

func (policy EndpointPolicy) Allows(operation GatewayOperation) bool {
	return slices.Contains(policy.Operations, operation)
}

// SubjectPolicy grants a service identity a bounded subset of workspace
// endpoint aliases. Subject values are stable authenticated token subjects.
type SubjectPolicy struct {
	Subject        string   `json:"subject"`
	Endpoints      []string `json:"endpoints"`
	MaximumRequest Quota    `json:"maximum_request"`
}

func (policy SubjectPolicy) Validate() error {
	if err := validateName(policy.Subject, "subject"); err != nil {
		return err
	}
	if len(policy.Endpoints) == 0 || len(policy.Endpoints) > 128 {
		return invalid("subject_endpoints_invalid", "subject endpoints are outside bounds", nil)
	}
	seen := make(map[string]struct{}, len(policy.Endpoints))
	for _, endpoint := range policy.Endpoints {
		if err := validateName(endpoint, "endpoint"); err != nil {
			return err
		}
		if _, exists := seen[endpoint]; exists {
			return invalid("subject_endpoint_duplicate", "subject endpoint is duplicated", nil)
		}
		seen[endpoint] = struct{}{}
	}
	if err := policy.MaximumRequest.Validate(true); err != nil {
		return err
	}
	if policy.MaximumRequest[UnitRequests] != 1 {
		return invalid("subject_request_count_invalid", "subject policy must authorize exactly one request", nil)
	}
	return nil
}

type BundleBudgetSpec struct {
	Limit     Quota     `json:"limit"`
	StartsAt  time.Time `json:"starts_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// WorkspacePolicyBundleSpec is the complete atomic policy change unit.
type WorkspacePolicyBundleSpec struct {
	TenantID        identifiers.ID   `json:"tenant_id"`
	WorkspaceID     identifiers.ID   `json:"workspace_id"`
	MLflowWorkspace string           `json:"mlflow_workspace"`
	PolicyEpoch     uint64           `json:"policy_epoch"`
	EffectiveAt     time.Time        `json:"effective_at"`
	ExpiresAt       time.Time        `json:"expires_at"`
	Endpoints       []EndpointPolicy `json:"endpoints"`
	Subjects        []SubjectPolicy  `json:"subjects"`
	Budget          BundleBudgetSpec `json:"budget"`
}

func (spec WorkspacePolicyBundleSpec) Validate() error {
	if err := validateID(spec.TenantID, "tenant", "tenant_id"); err != nil {
		return err
	}
	if err := validateID(spec.WorkspaceID, "workspace", "workspace_id"); err != nil {
		return err
	}
	if err := validateName(spec.MLflowWorkspace, "mlflow_workspace"); err != nil {
		return err
	}
	if spec.PolicyEpoch == 0 {
		return invalid("bundle_policy_epoch_invalid", "bundle policy epoch is required", nil)
	}
	if spec.EffectiveAt.IsZero() || !spec.ExpiresAt.After(spec.EffectiveAt) {
		return invalid("bundle_window_invalid", "bundle window is invalid", nil)
	}
	if len(spec.Endpoints) == 0 || len(spec.Endpoints) > 128 {
		return invalid("bundle_endpoints_invalid", "bundle endpoints are outside bounds", nil)
	}
	endpoints := make(map[string]EndpointPolicy, len(spec.Endpoints))
	for _, endpoint := range spec.Endpoints {
		if err := endpoint.Validate(); err != nil {
			return err
		}
		if _, exists := endpoints[endpoint.Name]; exists {
			return invalid("bundle_endpoint_duplicate", "bundle endpoint is duplicated", nil)
		}
		endpoints[endpoint.Name] = endpoint
	}
	if len(spec.Subjects) == 0 || len(spec.Subjects) > 1024 {
		return invalid("bundle_subjects_invalid", "bundle subjects are outside bounds", nil)
	}
	seenSubjects := make(map[string]struct{}, len(spec.Subjects))
	for _, subject := range spec.Subjects {
		if err := subject.Validate(); err != nil {
			return err
		}
		if _, exists := seenSubjects[subject.Subject]; exists {
			return invalid("bundle_subject_duplicate", "bundle subject is duplicated", nil)
		}
		seenSubjects[subject.Subject] = struct{}{}
		for _, endpoint := range subject.Endpoints {
			configured, exists := endpoints[endpoint]
			if !exists {
				return invalid("bundle_subject_endpoint_unknown", "subject references an unknown endpoint", nil)
			}
			if !subject.MaximumRequest.Fits(configured.MaximumRequest) {
				return invalid("bundle_subject_limit_exceeds_endpoint", "subject limit exceeds an endpoint limit", nil)
			}
		}
	}
	if err := spec.Budget.Limit.Validate(true); err != nil {
		return err
	}
	if spec.Budget.StartsAt.IsZero() || !spec.Budget.ExpiresAt.After(spec.Budget.StartsAt) {
		return invalid("bundle_budget_window_invalid", "bundle budget window is invalid", nil)
	}
	if spec.Budget.StartsAt.Before(spec.EffectiveAt) || spec.Budget.ExpiresAt.After(spec.ExpiresAt) {
		return invalid("bundle_budget_outside_policy_window", "bundle budget window must be inside the policy window", nil)
	}
	return nil
}

func canonicalizeBundleSpec(spec WorkspacePolicyBundleSpec) WorkspacePolicyBundleSpec {
	spec.EffectiveAt = spec.EffectiveAt.Round(0).UTC()
	spec.ExpiresAt = spec.ExpiresAt.Round(0).UTC()
	spec.Budget.StartsAt = spec.Budget.StartsAt.Round(0).UTC()
	spec.Budget.ExpiresAt = spec.Budget.ExpiresAt.Round(0).UTC()
	spec.Budget.Limit = spec.Budget.Limit.Clone()
	spec.Endpoints = append([]EndpointPolicy(nil), spec.Endpoints...)
	for index := range spec.Endpoints {
		spec.Endpoints[index].Operations = append([]GatewayOperation(nil), spec.Endpoints[index].Operations...)
		slices.Sort(spec.Endpoints[index].Operations)
		spec.Endpoints[index].GuardrailRefs = append([]string(nil), spec.Endpoints[index].GuardrailRefs...)
		slices.Sort(spec.Endpoints[index].GuardrailRefs)
		spec.Endpoints[index].MaximumRequest = spec.Endpoints[index].MaximumRequest.Clone()
	}
	slices.SortFunc(spec.Endpoints, func(left, right EndpointPolicy) int { return strings.Compare(left.Name, right.Name) })
	spec.Subjects = append([]SubjectPolicy(nil), spec.Subjects...)
	for index := range spec.Subjects {
		spec.Subjects[index].Endpoints = append([]string(nil), spec.Subjects[index].Endpoints...)
		slices.Sort(spec.Subjects[index].Endpoints)
		spec.Subjects[index].MaximumRequest = spec.Subjects[index].MaximumRequest.Clone()
	}
	slices.SortFunc(spec.Subjects, func(left, right SubjectPolicy) int { return strings.Compare(left.Subject, right.Subject) })
	return spec
}

// WorkspacePolicyBundle is the effective, server-sealed policy aggregate.
type WorkspacePolicyBundle struct {
	ID      identifiers.ID            `json:"id"`
	Spec    WorkspacePolicyBundleSpec `json:"spec"`
	Version resourceversion.Version   `json:"resource_version"`
}

func (bundle WorkspacePolicyBundle) Validate() error {
	if err := validateID(bundle.ID, "policybundle", "policy_bundle_id"); err != nil {
		return err
	}
	if err := bundle.Spec.Validate(); err != nil {
		return err
	}
	if err := bundle.Version.Validate(); err != nil {
		return invalid("policy_bundle_version_invalid", "policy bundle version is invalid", err)
	}
	if !bundle.Version.Digest().Equal(bundleDigest(bundle.ID, bundle.Spec)) {
		return invalid("policy_bundle_seal_mismatch", "policy bundle version does not seal its content", nil)
	}
	return nil
}

func (bundle WorkspacePolicyBundle) Endpoint(name string, operation GatewayOperation) (EndpointPolicy, bool) {
	for _, endpoint := range bundle.Spec.Endpoints {
		if endpoint.Name == name && endpoint.Allows(operation) {
			return endpoint, true
		}
	}
	return EndpointPolicy{}, false
}

type PolicyProposalState string

const (
	ProposalPending   PolicyProposalState = "pending"
	ProposalApplied   PolicyProposalState = "applied"
	ProposalRejected  PolicyProposalState = "rejected"
	ProposalCancelled PolicyProposalState = "cancelled"
)

func (state PolicyProposalState) Valid() bool {
	switch state {
	case ProposalPending, ProposalApplied, ProposalRejected, ProposalCancelled:
		return true
	default:
		return false
	}
}

// PolicyProposal binds an exact canonical bundle and exact base bundle version
// to one proposer identity key.
type PolicyProposal struct {
	ID          identifiers.ID            `json:"id"`
	Spec        WorkspacePolicyBundleSpec `json:"spec"`
	BaseVersion resourceversion.Version   `json:"base_resource_version,omitempty"`
	ProposerKey string                    `json:"proposer_key"`
	ProposedAt  time.Time                 `json:"proposed_at"`
	ExpiresAt   time.Time                 `json:"expires_at"`
	State       PolicyProposalState       `json:"state"`
	DecisionKey string                    `json:"decision_key,omitempty"`
	DecidedAt   time.Time                 `json:"decided_at,omitempty"`
	Version     resourceversion.Version   `json:"resource_version"`
}

func (proposal PolicyProposal) Validate() error {
	if err := validateID(proposal.ID, "policyproposal", "policy_proposal_id"); err != nil {
		return err
	}
	if err := proposal.Spec.Validate(); err != nil {
		return err
	}
	if proposal.BaseVersion.IsZero() == (proposal.Spec.PolicyEpoch != 1) {
		return invalid("policy_proposal_base_invalid", "policy proposal base and epoch do not agree", nil)
	}
	if !proposal.BaseVersion.IsZero() {
		if err := proposal.BaseVersion.Validate(); err != nil {
			return invalid("policy_proposal_base_invalid", "policy proposal base version is invalid", err)
		}
	}
	if !validActorKey(proposal.ProposerKey) || !proposal.State.Valid() || proposal.ProposedAt.IsZero() || !proposal.ExpiresAt.After(proposal.ProposedAt) {
		return invalid("policy_proposal_invalid", "policy proposal is invalid", nil)
	}
	if proposal.State == ProposalPending {
		if proposal.DecisionKey != "" || !proposal.DecidedAt.IsZero() {
			return invalid("policy_proposal_decision_unexpected", "pending proposal has decision metadata", nil)
		}
	} else if !validActorKey(proposal.DecisionKey) || proposal.DecidedAt.Before(proposal.ProposedAt) {
		return invalid("policy_proposal_decision_invalid", "terminal proposal decision metadata is invalid", nil)
	}
	if err := proposal.Version.Validate(); err != nil {
		return invalid("policy_proposal_version_invalid", "policy proposal version is invalid", err)
	}
	if !proposal.Version.Digest().Equal(proposalDigest(proposal)) {
		return invalid("policy_proposal_seal_mismatch", "policy proposal version does not seal its content", nil)
	}
	return nil
}

// PolicyApprovalReceipt proves the distinct approval and exact published
// bundle. The detached signature covers every field except Signature.
type PolicyApprovalReceipt struct {
	ID            identifiers.ID          `json:"id"`
	ProposalID    identifiers.ID          `json:"proposal_id"`
	BundleID      identifiers.ID          `json:"bundle_id"`
	BundleVersion resourceversion.Version `json:"bundle_resource_version"`
	ProposerKey   string                  `json:"proposer_key"`
	ApproverKey   string                  `json:"approver_key"`
	AppliedAt     time.Time               `json:"applied_at"`
	Signature     signing.Signature       `json:"signature"`
}

func (receipt PolicyApprovalReceipt) Validate() error {
	if err := validateID(receipt.ID, "policyreceipt", "policy_receipt_id"); err != nil {
		return err
	}
	if err := validateID(receipt.ProposalID, "policyproposal", "policy_proposal_id"); err != nil {
		return err
	}
	if err := validateID(receipt.BundleID, "policybundle", "policy_bundle_id"); err != nil {
		return err
	}
	if err := receipt.BundleVersion.Validate(); err != nil {
		return err
	}
	if !validActorKey(receipt.ProposerKey) || !validActorKey(receipt.ApproverKey) || receipt.ProposerKey == receipt.ApproverKey || receipt.AppliedAt.IsZero() {
		return invalid("policy_receipt_identity_invalid", "policy receipt identities are invalid", nil)
	}
	if err := receipt.Signature.Validate(); err != nil {
		return invalid("policy_receipt_signature_invalid", "policy receipt signature is invalid", err)
	}
	return nil
}

func (receipt PolicyApprovalReceipt) signingPayload() []byte {
	return []byte(canonicalJoin("gateway-policy-receipt-v1", receipt.ID.String(), receipt.ProposalID.String(),
		receipt.BundleID.String(), receipt.BundleVersion.String(), receipt.ProposerKey, receipt.ApproverKey,
		receipt.AppliedAt.UTC().Format(time.RFC3339Nano)))
}

// GovernanceRepository atomically owns proposal state and approved policy
// publication. ApplyApproved must persist bundle, primitive policy, receipt,
// audit, and outbox in one transaction.
type GovernanceRepository interface {
	PolicyRepository
	CurrentBundle(context.Context, identifiers.ID) (WorkspacePolicyBundle, error)
	ListEntitlements(context.Context, string) ([]Entitlement, error)
	CreateProposal(context.Context, PolicyProposal) error
	GetProposal(context.Context, identifiers.ID) (PolicyProposal, error)
	ApplyApproved(context.Context, PolicyProposal, resourceversion.Version, WorkspacePolicyBundle, []Entitlement, Budget, PolicyApprovalReceipt, time.Time) error
	TransitionProposal(context.Context, PolicyProposal, resourceversion.Version) error
}

type GovernanceService struct {
	Repository  GovernanceRepository
	IDs         *identifiers.Generator
	Clock       clock.Clock
	Signer      signing.Signer
	ProposalTTL time.Duration
}

func (service GovernanceService) Propose(ctx context.Context, proposer auth.Principal, spec WorkspacePolicyBundleSpec, precondition resourceversion.Precondition) (PolicyProposal, error) {
	const operation = "admission.GovernanceService.Propose"
	if err := service.validate(ctx, operation); err != nil {
		return PolicyProposal{}, err
	}
	if err := proposer.Validate(); err != nil {
		return PolicyProposal{}, faults.Wrap(err, faults.CodeUnauthenticated, "authenticated proposer is invalid", faults.WithReason("policy_proposer_invalid"), faults.WithOperation(operation), faults.WithRetryPolicy(faults.NoRetry()))
	}
	if err := exactPolicyPrecondition(precondition); err != nil {
		return PolicyProposal{}, err
	}
	spec = canonicalizeBundleSpec(spec)
	if err := spec.Validate(); err != nil {
		return PolicyProposal{}, err
	}
	current, err := service.Repository.CurrentBundle(ctx, spec.WorkspaceID)
	exists := err == nil
	if err != nil && !faults.IsCode(err, faults.CodeNotFound) {
		return PolicyProposal{}, err
	}
	currentVersion := resourceversion.Version{}
	if exists {
		currentVersion = current.Version
	}
	if err := precondition.Check(exists, currentVersion); err != nil {
		return PolicyProposal{}, err
	}
	if exists {
		if current.Spec.TenantID != spec.TenantID || current.Spec.PolicyEpoch == math.MaxUint64 || spec.PolicyEpoch != current.Spec.PolicyEpoch+1 {
			return PolicyProposal{}, conflict("policy_bundle_transition_invalid", "policy bundle tenant and epoch must advance exactly")
		}
	} else if spec.PolicyEpoch != 1 {
		return PolicyProposal{}, conflict("policy_bundle_initial_epoch_invalid", "initial policy bundle epoch must be one")
	}
	now := service.now()
	if spec.EffectiveAt.After(now) || !spec.ExpiresAt.After(now) || spec.Budget.StartsAt.After(now) || !spec.Budget.ExpiresAt.After(now) {
		return PolicyProposal{}, failedPrecondition("policy_bundle_window_inactive", "proposed policy and budget must be active at approval time")
	}
	id, err := service.IDs.ID(identifiers.MustParseKind("policyproposal"))
	if err != nil {
		return PolicyProposal{}, unavailable("policy_proposal_id_unavailable", "policy proposal ID is unavailable", err)
	}
	proposal := PolicyProposal{
		ID: id, Spec: spec, BaseVersion: currentVersion, ProposerKey: proposer.Key(), ProposedAt: now,
		ExpiresAt: now.Add(service.proposalTTL()), State: ProposalPending,
	}
	proposal, err = versionProposal(proposal, 1)
	if err != nil {
		return PolicyProposal{}, unavailable("policy_proposal_version_unavailable", "policy proposal version is unavailable", err)
	}
	if err := service.Repository.CreateProposal(ctx, proposal); err != nil {
		return PolicyProposal{}, err
	}
	return proposal, nil
}

func (service GovernanceService) Proposal(ctx context.Context, id identifiers.ID) (PolicyProposal, error) {
	if err := service.validate(ctx, "admission.GovernanceService.Proposal"); err != nil {
		return PolicyProposal{}, err
	}
	return service.Repository.GetProposal(ctx, id)
}

func (service GovernanceService) Bundle(ctx context.Context, workspaceID identifiers.ID) (WorkspacePolicyBundle, error) {
	if err := service.validate(ctx, "admission.GovernanceService.Bundle"); err != nil {
		return WorkspacePolicyBundle{}, err
	}
	if err := validateID(workspaceID, "workspace", "workspace_id"); err != nil {
		return WorkspacePolicyBundle{}, err
	}
	return service.Repository.CurrentBundle(ctx, workspaceID)
}

func (service GovernanceService) Approve(ctx context.Context, approver auth.Principal, id identifiers.ID, expected resourceversion.Version) (WorkspacePolicyBundle, PolicyApprovalReceipt, error) {
	const operation = "admission.GovernanceService.Approve"
	if err := service.validate(ctx, operation); err != nil {
		return WorkspacePolicyBundle{}, PolicyApprovalReceipt{}, err
	}
	if err := approver.Validate(); err != nil {
		return WorkspacePolicyBundle{}, PolicyApprovalReceipt{}, faults.Wrap(err, faults.CodeUnauthenticated, "authenticated approver is invalid", faults.WithReason("policy_approver_invalid"), faults.WithOperation(operation), faults.WithRetryPolicy(faults.NoRetry()))
	}
	proposal, err := service.Repository.GetProposal(ctx, id)
	if err != nil {
		return WorkspacePolicyBundle{}, PolicyApprovalReceipt{}, err
	}
	now := service.now()
	if proposal.State != ProposalPending {
		return WorkspacePolicyBundle{}, PolicyApprovalReceipt{}, conflict("policy_proposal_terminal", "policy proposal is already terminal")
	}
	if !now.Before(proposal.ExpiresAt) {
		return WorkspacePolicyBundle{}, PolicyApprovalReceipt{}, failedPrecondition("policy_proposal_expired", "policy proposal has expired")
	}
	if proposal.Spec.EffectiveAt.After(now) || !proposal.Spec.ExpiresAt.After(now) ||
		proposal.Spec.Budget.StartsAt.After(now) || !proposal.Spec.Budget.ExpiresAt.After(now) {
		return WorkspacePolicyBundle{}, PolicyApprovalReceipt{}, failedPrecondition("policy_bundle_window_inactive", "approved policy and budget must be active at publication time")
	}
	if proposal.Version != expected {
		return WorkspacePolicyBundle{}, PolicyApprovalReceipt{}, conflict("policy_proposal_version_stale", "policy proposal version is stale")
	}
	if approver.Key() == proposal.ProposerKey {
		return WorkspacePolicyBundle{}, PolicyApprovalReceipt{}, denied("policy_self_approval_forbidden", "proposal approval requires a different administrator")
	}
	current, currentErr := service.Repository.CurrentBundle(ctx, proposal.Spec.WorkspaceID)
	exists := currentErr == nil
	if currentErr != nil && !faults.IsCode(currentErr, faults.CodeNotFound) {
		return WorkspacePolicyBundle{}, PolicyApprovalReceipt{}, currentErr
	}
	if exists && current.Version != proposal.BaseVersion || !exists && !proposal.BaseVersion.IsZero() {
		return WorkspacePolicyBundle{}, PolicyApprovalReceipt{}, conflict("policy_proposal_base_stale", "policy proposal base bundle is stale")
	}
	var bundleID identifiers.ID
	generation := uint64(1)
	if exists {
		bundleID = current.ID
		generation = current.Version.Generation() + 1
		if generation == 0 {
			return WorkspacePolicyBundle{}, PolicyApprovalReceipt{}, unavailable("policy_bundle_version_exhausted", "policy bundle version is exhausted", nil)
		}
	} else {
		bundleID, err = service.IDs.ID(identifiers.MustParseKind("policybundle"))
		if err != nil {
			return WorkspacePolicyBundle{}, PolicyApprovalReceipt{}, unavailable("policy_bundle_id_unavailable", "policy bundle ID is unavailable", err)
		}
	}
	bundle, err := sealBundle(bundleID, proposal.Spec, generation)
	if err != nil {
		return WorkspacePolicyBundle{}, PolicyApprovalReceipt{}, err
	}
	entitlements, err := service.buildEntitlements(ctx, bundle, now)
	if err != nil {
		return WorkspacePolicyBundle{}, PolicyApprovalReceipt{}, err
	}
	budget, err := service.buildBudget(ctx, bundle, now)
	if err != nil {
		return WorkspacePolicyBundle{}, PolicyApprovalReceipt{}, err
	}
	receiptID, err := service.IDs.ID(identifiers.MustParseKind("policyreceipt"))
	if err != nil {
		return WorkspacePolicyBundle{}, PolicyApprovalReceipt{}, unavailable("policy_receipt_id_unavailable", "policy receipt ID is unavailable", err)
	}
	receipt := PolicyApprovalReceipt{
		ID: receiptID, ProposalID: proposal.ID, BundleID: bundle.ID, BundleVersion: bundle.Version,
		ProposerKey: proposal.ProposerKey, ApproverKey: approver.Key(), AppliedAt: now,
	}
	receipt.Signature, err = service.Signer.Sign(ctx, receipt.signingPayload())
	if err != nil {
		return WorkspacePolicyBundle{}, PolicyApprovalReceipt{}, unavailable("policy_receipt_signing_failed", "policy receipt could not be signed", err)
	}
	if err := receipt.Validate(); err != nil {
		return WorkspacePolicyBundle{}, PolicyApprovalReceipt{}, err
	}
	appliedProposal := proposal
	appliedProposal.State = ProposalApplied
	appliedProposal.DecisionKey = receipt.ApproverKey
	appliedProposal.DecidedAt = now
	appliedProposal, err = versionProposal(appliedProposal, proposal.Version.Generation()+1)
	if err != nil {
		return WorkspacePolicyBundle{}, PolicyApprovalReceipt{}, unavailable("policy_proposal_version_unavailable", "policy proposal version is unavailable", err)
	}
	if err := service.Repository.ApplyApproved(ctx, appliedProposal, expected, bundle, entitlements, budget, receipt, now); err != nil {
		return WorkspacePolicyBundle{}, PolicyApprovalReceipt{}, err
	}
	return bundle, receipt, nil
}

func (service GovernanceService) Reject(ctx context.Context, actor auth.Principal, id identifiers.ID, expected resourceversion.Version) (PolicyProposal, error) {
	return service.transition(ctx, actor, id, expected, ProposalRejected, false)
}

func (service GovernanceService) Cancel(ctx context.Context, actor auth.Principal, id identifiers.ID, expected resourceversion.Version) (PolicyProposal, error) {
	return service.transition(ctx, actor, id, expected, ProposalCancelled, true)
}

func (service GovernanceService) transition(ctx context.Context, actor auth.Principal, id identifiers.ID, expected resourceversion.Version, state PolicyProposalState, proposerOnly bool) (PolicyProposal, error) {
	if err := service.validate(ctx, "admission.GovernanceService.Transition"); err != nil {
		return PolicyProposal{}, err
	}
	if err := actor.Validate(); err != nil {
		return PolicyProposal{}, err
	}
	proposal, err := service.Repository.GetProposal(ctx, id)
	if err != nil {
		return PolicyProposal{}, err
	}
	if proposerOnly && actor.Key() != proposal.ProposerKey {
		return PolicyProposal{}, denied("policy_proposal_cancel_forbidden", "only the proposer may cancel a proposal")
	}
	if proposal.State != ProposalPending || proposal.Version != expected {
		return PolicyProposal{}, conflict("policy_proposal_version_stale", "policy proposal version is stale")
	}
	proposal.State = state
	proposal.DecisionKey = actor.Key()
	proposal.DecidedAt = service.now()
	proposal, err = versionProposal(proposal, proposal.Version.Generation()+1)
	if err != nil {
		return PolicyProposal{}, unavailable("policy_proposal_version_unavailable", "policy proposal version is unavailable", err)
	}
	if err := service.Repository.TransitionProposal(ctx, proposal, expected); err != nil {
		return PolicyProposal{}, err
	}
	return proposal, nil
}

func (service GovernanceService) buildEntitlements(ctx context.Context, bundle WorkspacePolicyBundle, now time.Time) ([]Entitlement, error) {
	workspace := bundle.Spec.WorkspaceID.String()
	current, err := service.Repository.ListEntitlements(ctx, workspace)
	if err != nil {
		return nil, err
	}
	bySubject := make(map[string]Entitlement, len(current))
	for _, entitlement := range current {
		bySubject[entitlement.Subject] = entitlement
	}
	endpoints := make(map[string]GatewayRoute, len(bundle.Spec.Endpoints))
	for _, endpoint := range bundle.Spec.Endpoints {
		endpoints[endpoint.Name] = endpoint.Route
	}
	result := make([]Entitlement, 0, len(bundle.Spec.Subjects)+len(current))
	active := make(map[string]struct{}, len(bundle.Spec.Subjects))
	for _, subject := range bundle.Spec.Subjects {
		active[subject.Subject] = struct{}{}
		routes := make([]GatewayRoute, 0, len(subject.Endpoints))
		for _, endpoint := range subject.Endpoints {
			routes = append(routes, endpoints[endpoint])
		}
		candidate := Entitlement{
			Subject: subject.Subject, Workspace: workspace, PolicyEpoch: bundle.Spec.PolicyEpoch,
			Routes: routes, MaximumRequest: subject.MaximumRequest,
			NotBefore: bundle.Spec.EffectiveAt, ExpiresAt: bundle.Spec.ExpiresAt,
		}
		generation := uint64(1)
		if existing, exists := bySubject[subject.Subject]; exists {
			candidate.ID = existing.ID
			generation = existing.Version.Generation() + 1
		} else {
			candidate.ID, err = service.IDs.ID(identifiers.MustParseKind("entitlement"))
			if err != nil {
				return nil, unavailable("entitlement_id_unavailable", "entitlement ID is unavailable", err)
			}
		}
		candidate, err = candidate.Seal(generation)
		if err != nil {
			return nil, err
		}
		result = append(result, candidate)
	}
	for _, existing := range current {
		if _, remains := active[existing.Subject]; remains || !now.Before(existing.ExpiresAt) && existing.PolicyEpoch >= bundle.Spec.PolicyEpoch {
			continue
		}
		revoked := existing
		revoked.PolicyEpoch = bundle.Spec.PolicyEpoch
		revoked.ExpiresAt = now
		if !revoked.ExpiresAt.After(revoked.NotBefore) {
			revoked.NotBefore = now.Add(-time.Nanosecond)
		}
		revoked, err = revoked.Seal(existing.Version.Generation() + 1)
		if err != nil {
			return nil, err
		}
		result = append(result, revoked)
	}
	return result, nil
}

func (service GovernanceService) buildBudget(ctx context.Context, bundle WorkspacePolicyBundle, now time.Time) (Budget, error) {
	workspace := bundle.Spec.WorkspaceID.String()
	current, err := service.Repository.GetBudget(ctx, workspace)
	exists := err == nil
	if err != nil && !faults.IsCode(err, faults.CodeNotFound) {
		return Budget{}, err
	}
	candidate := Budget{Workspace: workspace, Limit: bundle.Spec.Budget.Limit, StartsAt: bundle.Spec.Budget.StartsAt, ExpiresAt: bundle.Spec.Budget.ExpiresAt}
	generation := uint64(1)
	if exists {
		generation = current.Version.Generation() + 1
		sameWindow := current.StartsAt.Equal(candidate.StartsAt) && current.ExpiresAt.Equal(candidate.ExpiresAt)
		if sameWindow {
			candidate.ID = current.ID
		} else {
			if now.Before(current.ExpiresAt) || candidate.StartsAt.Before(current.ExpiresAt) {
				return Budget{}, failedPrecondition("budget_window_active", "an active or overlapping budget window cannot be replaced")
			}
			candidate.ID, err = service.IDs.ID(identifiers.MustParseKind("budget"))
		}
	} else {
		candidate.ID, err = service.IDs.ID(identifiers.MustParseKind("budget"))
	}
	if err != nil {
		return Budget{}, unavailable("budget_id_unavailable", "budget ID is unavailable", err)
	}
	return candidate.Seal(generation)
}

func (service GovernanceService) validate(ctx context.Context, operation string) error {
	if ctx == nil {
		return invalid("context_nil", "context is required", nil)
	}
	if nilInterface(service.Repository) || service.IDs == nil || nilInterface(service.Clock) || nilInterface(service.Signer) {
		return unavailable("governance_service_unconfigured", "policy governance service is unavailable", nil)
	}
	if err := ctx.Err(); err != nil {
		return faults.Wrap(err, faults.CodeOf(err), faults.PublicMessageOf(err), faults.WithReason("policy_context_done"), faults.WithOperation(operation), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return nil
}

func (service GovernanceService) now() time.Time { return service.Clock.Now().Round(0).UTC() }

func (service GovernanceService) proposalTTL() time.Duration {
	if service.ProposalTTL <= 0 || service.ProposalTTL > DefaultProposalTTL {
		return DefaultProposalTTL
	}
	return service.ProposalTTL
}

func validActorKey(value string) bool {
	if !strings.HasPrefix(value, "sha256:") || len(value) != len("sha256:")+64 {
		return false
	}
	for _, character := range value[len("sha256:"):] {
		if character < '0' || character > '9' && character < 'a' || character > 'f' {
			return false
		}
	}
	return true
}

func sealBundle(id identifiers.ID, spec WorkspacePolicyBundleSpec, generation uint64) (WorkspacePolicyBundle, error) {
	spec = canonicalizeBundleSpec(spec)
	if err := spec.Validate(); err != nil {
		return WorkspacePolicyBundle{}, err
	}
	version, err := resourceversion.New(generation, bundleDigest(id, spec))
	if err != nil {
		return WorkspacePolicyBundle{}, err
	}
	bundle := WorkspacePolicyBundle{ID: id, Spec: spec, Version: version}
	return bundle, bundle.Validate()
}

func bundleDigest(id identifiers.ID, spec WorkspacePolicyBundleSpec) identifiers.Digest {
	spec = canonicalizeBundleSpec(spec)
	values := []string{"gateway-policy-bundle-v1", id.String(), spec.TenantID.String(), spec.WorkspaceID.String(), spec.MLflowWorkspace,
		strconv.FormatUint(spec.PolicyEpoch, 10), spec.EffectiveAt.Format(time.RFC3339Nano), spec.ExpiresAt.Format(time.RFC3339Nano),
		spec.Budget.Limit.canonical(), spec.Budget.StartsAt.Format(time.RFC3339Nano), spec.Budget.ExpiresAt.Format(time.RFC3339Nano)}
	for _, endpoint := range spec.Endpoints {
		values = append(values, endpoint.Name, endpoint.Route.Provider, endpoint.Route.Model, endpoint.ConnectionRef,
			strconv.FormatUint(endpoint.MaximumBodyBytes, 10), endpoint.MaximumRequest.canonical(), strconv.FormatUint(endpoint.PricingVersion, 10),
			strconv.FormatUint(endpoint.RequestMicros, 10), strconv.FormatUint(endpoint.InputMicrosPerMillion, 10), strconv.FormatUint(endpoint.OutputMicrosPerMillion, 10),
			strconv.FormatBool(endpoint.MetadataOnlyTracing), strconv.FormatBool(endpoint.UsageTracking))
		for _, operation := range endpoint.Operations {
			values = append(values, string(operation))
		}
		values = append(values, endpoint.GuardrailRefs...)
	}
	for _, subject := range spec.Subjects {
		values = append(values, subject.Subject, subject.MaximumRequest.canonical())
		values = append(values, subject.Endpoints...)
	}
	return identifiers.SHA256String(canonicalJoin(values...))
}

func proposalDigest(proposal PolicyProposal) identifiers.Digest {
	return identifiers.SHA256String(canonicalJoin("gateway-policy-proposal-v1", proposal.ID.String(),
		bundleDigest(identifiers.ID{}, proposal.Spec).String(), proposal.BaseVersion.String(), proposal.ProposerKey,
		proposal.ProposedAt.UTC().Format(time.RFC3339Nano), proposal.ExpiresAt.UTC().Format(time.RFC3339Nano),
		string(proposal.State), proposal.DecisionKey, proposal.DecidedAt.UTC().Format(time.RFC3339Nano)))
}

func versionProposal(proposal PolicyProposal, generation uint64) (PolicyProposal, error) {
	version, err := resourceversion.New(generation, proposalDigest(proposal))
	if err != nil {
		return PolicyProposal{}, err
	}
	proposal.Version = version
	return proposal, proposal.Validate()
}
