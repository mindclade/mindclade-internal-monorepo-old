// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package admission

import (
	"context"
	"testing"
	"time"

	"go.mindclade.dev/libs/go/auth"
	"go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/idempotency"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/resourceversion"
	"go.mindclade.dev/libs/go/signing"
)

type governanceFixture struct {
	service     GovernanceService
	repository  *MemoryRepository
	clock       *clock.FakeClock
	proposer    auth.Principal
	approver    auth.Principal
	tenantID    identifiers.ID
	workspaceID identifiers.ID
}

func newGovernanceFixture(t *testing.T) governanceFixture {
	t.Helper()
	value := clock.NewFake(testStart)
	ids, err := identifiers.NewGenerator(identifiers.WithTimeSource(value.Now))
	if err != nil {
		t.Fatal(err)
	}
	key := make([]byte, 32)
	for index := range key {
		key[index] = byte(index + 1)
	}
	signer, err := signing.NewHMACSigner(signing.MustParseKeyID("policy-test"), key)
	if err != nil {
		t.Fatal(err)
	}
	proposer, err := auth.NewPrincipal(auth.PrincipalKindUser, "admin-a", auth.WithIssuer("https://cloud.google.com/iap"))
	if err != nil {
		t.Fatal(err)
	}
	approver, err := auth.NewPrincipal(auth.PrincipalKindUser, "admin-b", auth.WithIssuer("https://cloud.google.com/iap"))
	if err != nil {
		t.Fatal(err)
	}
	tenantID, err := ids.ID(identifiers.MustParseKind("tenant"))
	if err != nil {
		t.Fatal(err)
	}
	workspaceID, err := ids.ID(identifiers.MustParseKind("workspace"))
	if err != nil {
		t.Fatal(err)
	}
	repository := NewMemoryRepository(100)
	return governanceFixture{
		service:    GovernanceService{Repository: repository, IDs: ids, Clock: value, Signer: signer},
		repository: repository, clock: value, proposer: proposer, approver: approver,
		tenantID: tenantID, workspaceID: workspaceID,
	}
}

func (fixture governanceFixture) spec(epoch uint64, requests uint64) WorkspacePolicyBundleSpec {
	maximum := Quota{UnitRequests: 1, UnitInputTokens: 2_000, UnitOutputTokens: 1_000, UnitCostMicros: 25_000}
	return WorkspacePolicyBundleSpec{
		TenantID: fixture.tenantID, WorkspaceID: fixture.workspaceID, MLflowWorkspace: "research-team", PolicyEpoch: epoch,
		EffectiveAt: testStart.Add(-time.Minute), ExpiresAt: testStart.Add(2 * time.Hour),
		Endpoints: []EndpointPolicy{{
			Name: "chat-primary", Route: GatewayRoute{Endpoint: "chat-primary", Provider: "openai-compatible", Model: "qualified-model"},
			Operations: []GatewayOperation{OperationChatCompletions, OperationResponses}, ConnectionRef: "provider-primary",
			MaximumBodyBytes: 1 << 20, MaximumRequest: maximum,
			PricingVersion: 1, RequestMicros: 100, InputMicrosPerMillion: 1_000_000, OutputMicrosPerMillion: 2_000_000,
			MetadataOnlyTracing: true, UsageTracking: false,
		}},
		Subjects: []SubjectPolicy{{Subject: "gateway-client", Endpoints: []string{"chat-primary"}, MaximumRequest: maximum}},
		Budget: BundleBudgetSpec{
			Limit:    Quota{UnitRequests: requests, UnitInputTokens: 20_000, UnitOutputTokens: 10_000, UnitCostMicros: 250_000},
			StartsAt: testStart.Add(-time.Minute), ExpiresAt: testStart.Add(2 * time.Hour),
		},
	}
}

func TestGovernanceRequiresDistinctApprovalAndPublishesAtomicBundle(t *testing.T) {
	fixture := newGovernanceFixture(t)
	ctx := context.Background()
	proposal, err := fixture.service.Propose(ctx, fixture.proposer, fixture.spec(1, 10), resourceversion.RequireAbsence())
	if err != nil {
		t.Fatal(err)
	}
	if proposal.State != ProposalPending || proposal.Version.Generation() != 1 || proposal.ProposerKey != fixture.proposer.Key() {
		t.Fatalf("unexpected proposal: %+v", proposal)
	}
	if _, _, err := fixture.service.Approve(ctx, fixture.proposer, proposal.ID, proposal.Version); !faults.IsReason(err, "policy_self_approval_forbidden") {
		t.Fatalf("expected self-approval denial, got %v", err)
	}
	bundle, receipt, err := fixture.service.Approve(ctx, fixture.approver, proposal.ID, proposal.Version)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Spec.PolicyEpoch != 1 || bundle.Version.Generation() != 1 || receipt.ProposerKey == receipt.ApproverKey {
		t.Fatalf("unexpected approval: bundle=%+v receipt=%+v", bundle, receipt)
	}
	stored, err := fixture.service.Proposal(ctx, proposal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.State != ProposalApplied || stored.DecisionKey != fixture.approver.Key() || stored.Version.Generation() != 2 {
		t.Fatalf("proposal was not atomically applied: %+v", stored)
	}
	entitlement, err := fixture.repository.GetEntitlement(ctx, "gateway-client", fixture.workspaceID.String())
	if err != nil || entitlement.PolicyEpoch != 1 || !entitlement.Allows(bundle.Spec.Endpoints[0].Route) {
		t.Fatalf("effective entitlement=%+v error=%v", entitlement, err)
	}
	budget, err := fixture.repository.GetBudget(ctx, fixture.workspaceID.String())
	if err != nil || budget.Limit[UnitRequests] != 10 {
		t.Fatalf("effective budget=%+v error=%v", budget, err)
	}
	resolved, err := (Service{Repository: fixture.repository, Clock: fixture.clock}).ResolveEndpoint(
		ctx, "gateway-client", fixture.workspaceID.String(), "chat-primary", OperationChatCompletions,
	)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.BundleVersion != bundle.Version || resolved.PolicyEpoch != 1 || resolved.Endpoint.Route != bundle.Spec.Endpoints[0].Route ||
		!resolved.SubjectMaximumRequest.Equal(bundle.Spec.Subjects[0].MaximumRequest) {
		t.Fatalf("resolved endpoint does not bind the effective bundle: %+v", resolved)
	}
}

func TestGovernanceRejectsStaleBaseAndProtectsCommittedUsage(t *testing.T) {
	fixture := newGovernanceFixture(t)
	ctx := context.Background()
	first, err := fixture.service.Propose(ctx, fixture.proposer, fixture.spec(1, 2), resourceversion.RequireAbsence())
	if err != nil {
		t.Fatal(err)
	}
	bundle, _, err := fixture.service.Approve(ctx, fixture.approver, first.ID, first.Version)
	if err != nil {
		t.Fatal(err)
	}
	stale, err := fixture.service.Propose(ctx, fixture.proposer, fixture.spec(2, 2), resourceversion.MatchVersion(bundle.Version))
	if err != nil {
		t.Fatal(err)
	}
	rotatedSpec := fixture.spec(2, 3)
	other, err := fixture.service.Propose(ctx, fixture.approver, rotatedSpec, resourceversion.MatchVersion(bundle.Version))
	if err != nil {
		t.Fatal(err)
	}
	third, err := auth.NewPrincipal(auth.PrincipalKindUser, "admin-c", auth.WithIssuer("https://cloud.google.com/iap"))
	if err != nil {
		t.Fatal(err)
	}
	updated, _, err := fixture.service.Approve(ctx, third, other.ID, other.Version)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := fixture.service.Approve(ctx, fixture.approver, stale.ID, stale.Version); !faults.IsReason(err, "policy_proposal_base_stale") {
		t.Fatalf("expected stale-base denial after %s, got %v", updated.Version, err)
	}

	decision, err := (Service{Repository: fixture.repository, Clock: fixture.clock, MaximumTTL: time.Minute}).Admit(ctx, AdmitRequest{
		Idempotency:   testIdempotency(t, fixture.workspaceID.String()+"/mlflow-gateway/gateway-client", "governed-usage"),
		RequestDigest: identifiers.SHA256String("governed request"), Subject: "gateway-client", Workspace: fixture.workspaceID.String(),
		Route: updated.Spec.Endpoints[0].Route, PolicyEpoch: updated.Spec.PolicyEpoch,
		Requested: Quota{UnitRequests: 1, UnitInputTokens: 100, UnitOutputTokens: 50, UnitCostMicros: 1_000}, TTL: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	dispatched, err := (Service{Repository: fixture.repository, Clock: fixture.clock}).Dispatch(ctx, decision.Reservation.ID, decision.Reservation.Version, decision.Reservation.RequestDigest, "gateway-client")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (Service{Repository: fixture.repository, Clock: fixture.clock}).Commit(ctx, decision.Reservation.ID, dispatched.Reservation.Version, decision.Reservation.RequestDigest, "gateway-client", decision.Reservation.Reserved); err != nil {
		t.Fatal(err)
	}
	reduced := fixture.spec(3, 0)
	proposal, err := fixture.service.Propose(ctx, fixture.proposer, reduced, resourceversion.MatchVersion(updated.Version))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := fixture.service.Approve(ctx, fixture.approver, proposal.ID, proposal.Version); !faults.IsReason(err, "budget_limit_below_usage") {
		t.Fatalf("expected atomic usage-floor rejection, got %v", err)
	}
	current, err := fixture.service.Bundle(ctx, fixture.workspaceID)
	if err != nil || current.Version != updated.Version {
		t.Fatalf("failed approval changed bundle: %+v error=%v", current, err)
	}
}

func TestGovernanceValidatesObservabilityAndCancellation(t *testing.T) {
	fixture := newGovernanceFixture(t)
	invalid := fixture.spec(1, 10)
	invalid.Endpoints[0].UsageTracking = true
	if _, err := fixture.service.Propose(context.Background(), fixture.proposer, invalid, resourceversion.RequireAbsence()); !faults.IsReason(err, "endpoint_observability_policy_invalid") {
		t.Fatalf("expected usage-tracking denial, got %v", err)
	}
	invalid = fixture.spec(1, 10)
	invalid.Endpoints[0].GuardrailRefs = []string{"not-yet-qualified"}
	if _, err := fixture.service.Propose(context.Background(), fixture.proposer, invalid, resourceversion.RequireAbsence()); !faults.IsReason(err, "endpoint_guardrails_unsupported") {
		t.Fatalf("expected unqualified-guardrail denial, got %v", err)
	}
	proposal, err := fixture.service.Propose(context.Background(), fixture.proposer, fixture.spec(1, 10), resourceversion.RequireAbsence())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.Cancel(context.Background(), fixture.approver, proposal.ID, proposal.Version); !faults.IsReason(err, "policy_proposal_cancel_forbidden") {
		t.Fatalf("expected non-proposer cancellation denial, got %v", err)
	}
	cancelled, err := fixture.service.Cancel(context.Background(), fixture.proposer, proposal.ID, proposal.Version)
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.State != ProposalCancelled || cancelled.Version.Generation() != 2 {
		t.Fatalf("unexpected cancelled proposal: %+v", cancelled)
	}
}

func testIdempotency(t *testing.T, scope, key string) idempotency.Identity {
	t.Helper()
	parsedScope, err := idempotency.ParseScope(scope)
	if err != nil {
		t.Fatal(err)
	}
	parsedKey, err := idempotency.ParseKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return idempotency.Identity{Scope: parsedScope, Key: parsedKey}
}
