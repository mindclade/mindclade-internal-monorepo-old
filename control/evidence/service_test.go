// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

package evidence

import (
	"context"
	"crypto/ed25519"
	"testing"
	"time"

	"go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/signing"
)

type memoryRepository struct {
	claims        map[string]Claim
	verifications map[string]Verification
	decisions     map[string]SignedDecision
	revoked       map[string]bool
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{claims: map[string]Claim{}, verifications: map[string]Verification{}, decisions: map[string]SignedDecision{}, revoked: map[string]bool{}}
}

func evidenceKey(subject identifiers.Digest, control string) string {
	return subject.String() + "|" + control
}

func (repository *memoryRepository) AppendClaim(_ context.Context, claim Claim, verification Verification) error {
	key := evidenceKey(claim.SubjectDigest, claim.ControlID)
	repository.claims[key] = claim
	repository.verifications[key] = verification
	return nil
}

func (repository *memoryRepository) Current(_ context.Context, subject identifiers.Digest, control string) (Claim, Verification, error) {
	key := evidenceKey(subject, control)
	claim, found := repository.claims[key]
	if !found {
		return Claim{}, Verification{}, ErrNotFound
	}
	return claim, repository.verifications[key], nil
}

func (repository *memoryRepository) Evidence(_ context.Context, subject identifiers.Digest) ([]Record, error) {
	records := make([]Record, 0)
	for key, claim := range repository.claims {
		if claim.SubjectDigest.Equal(subject) {
			records = append(records, Record{Claim: claim, Verification: repository.verifications[key]})
		}
	}
	if len(records) == 0 {
		return nil, ErrNotFound
	}
	return records, nil
}

func (repository *memoryRepository) PutDecision(_ context.Context, decision SignedDecision) error {
	repository.decisions[decision.Decision.DecisionDigest.String()] = decision
	return nil
}

func (repository *memoryRepository) Decision(_ context.Context, digest identifiers.Digest) (SignedDecision, bool, error) {
	decision, found := repository.decisions[digest.String()]
	if !found {
		return SignedDecision{}, false, ErrNotFound
	}
	return decision, repository.revoked[digest.String()], nil
}

func (repository *memoryRepository) Revoke(_ context.Context, digest identifiers.Digest, _ string, _ time.Time) error {
	if _, found := repository.decisions[digest.String()]; !found {
		return ErrNotFound
	}
	repository.revoked[digest.String()] = true
	return nil
}

type verifierFunc func(context.Context, Claim, Control, time.Time) (Verification, error)

func (function verifierFunc) Verify(ctx context.Context, claim Claim, control Control, now time.Time) (Verification, error) {
	return function(ctx, claim, control, now)
}

func fixture(t *testing.T) (Service, DeploymentBundle, *memoryRepository) {
	t.Helper()
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	policy := Policy{ID: "production-controls", Version: "v1.0.0", Epoch: 7, ValidUntil: now.Add(24 * time.Hour), Controls: []Control{
		{ID: "dr_evidence", Owner: "security", MaximumAge: 90 * 24 * time.Hour},
		{ID: "source_ci", Owner: "platform", MaximumAge: 6 * time.Hour},
	}}
	if err := policy.Seal(); err != nil {
		t.Fatal(err)
	}
	_, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	keyID, err := signing.ParseKeyID("production-eligibility-v1")
	if err != nil {
		t.Fatal(err)
	}
	signer, err := signing.NewEd25519Signer(keyID, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	repository := newMemoryRepository()
	service := Service{Repository: repository, Policy: policy, Signer: signer, Clock: clock.NewFake(now), Verifier: verifierFunc(func(_ context.Context, claim Claim, _ Control, now time.Time) (Verification, error) {
		return Verification{Result: ResultPass, Reasons: []string{"verified"}, Artifact: claim.Artifact, VerifiedAt: now, ValidUntil: claim.ValidUntil}, nil
	})}
	repositories := make([]RepositoryRevision, 0, len(RequiredRepositories))
	for _, repository := range RequiredRepositories {
		repositories = append(repositories, RepositoryRevision{Repository: repository, Commit: "0123456789abcdef0123456789abcdef01234567"})
	}
	digest := identifiers.SHA256String("fixture")
	bundle := DeploymentBundle{SchemaVersion: SchemaBundleV1, ChangeReference: "CHG-123", Environment: "production", Repositories: repositories, ReleaseDigests: []string{digest.String()}, GitOpsRenderDigest: digest, DeploymentSelectionDigest: digest, InfrastructureDigest: digest, GovernanceAuditDigest: digest, WorkflowRelease: "v5.0.0", PolicyBundleDigest: digest}
	if err := bundle.Seal(); err != nil {
		t.Fatal(err)
	}
	return service, bundle, repository
}

func TestEligibilityRequiresEveryControl(t *testing.T) {
	service, bundle, repository := fixture(t)
	decision, err := service.Evaluate(context.Background(), bundle)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Decision.Result != ResultIndeterminate || len(decision.Decision.Reasons) != 2 {
		t.Fatalf("unexpected decision: %#v", decision.Decision)
	}
	if _, found := repository.decisions[decision.Decision.DecisionDigest.String()]; !found {
		t.Fatal("decision was not stored")
	}
}

func TestEligibilityRejectsUnsealedOrTamperedBundle(t *testing.T) {
	service, bundle, _ := fixture(t)
	bundle.ChangeReference = "CHG-tampered"
	if _, err := service.Evaluate(context.Background(), bundle); err == nil {
		t.Fatal("eligibility evaluation accepted a bundle whose digest no longer matched")
	}
}

func TestEligibilityPassesExactFreshEvidenceAndRevokes(t *testing.T) {
	service, bundle, repository := fixture(t)
	for _, control := range service.Policy.Controls {
		claim := Claim{SchemaVersion: SchemaClaimV1, SubjectKind: "deployment_bundle", SubjectDigest: bundle.BundleDigest, ControlID: control.ID, Scope: "production", Artifact: Artifact{URI: "gs://evidence/" + control.ID, Digest: identifiers.SHA256String(control.ID), MediaType: "application/json"}, SourceAuthority: "gitops", IssuedAt: service.now(), ValidUntil: service.now().Add(time.Hour)}
		if _, _, err := service.Submit(context.Background(), claim); err != nil {
			t.Fatal(err)
		}
	}
	decision, err := service.Evaluate(context.Background(), bundle)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Decision.Result != ResultEligible || len(decision.Decision.Selections) != 2 {
		t.Fatalf("unexpected decision: %#v", decision.Decision)
	}
	if err := service.Revoke(context.Background(), decision.Decision.DecisionDigest, "artifact_compromised"); err != nil {
		t.Fatal(err)
	}
	_, revoked, err := repository.Decision(context.Background(), decision.Decision.DecisionDigest)
	if err != nil || !revoked {
		t.Fatalf("revocation missing: revoked=%v err=%v", revoked, err)
	}
}

func TestFailedVerificationIsIneligible(t *testing.T) {
	service, bundle, _ := fixture(t)
	service.Verifier = verifierFunc(func(_ context.Context, claim Claim, _ Control, now time.Time) (Verification, error) {
		return Verification{Result: ResultFail, Reasons: []string{"policy_failed"}, Artifact: claim.Artifact, VerifiedAt: now, ValidUntil: claim.ValidUntil}, nil
	})
	for _, control := range service.Policy.Controls {
		claim := Claim{SchemaVersion: SchemaClaimV1, SubjectKind: "deployment_bundle", SubjectDigest: bundle.BundleDigest, ControlID: control.ID, Scope: "production", Artifact: Artifact{URI: "gs://evidence/" + control.ID, Digest: identifiers.SHA256String(control.ID), MediaType: "application/json"}, SourceAuthority: "gitops", IssuedAt: service.now(), ValidUntil: service.now().Add(time.Hour)}
		if _, _, err := service.Submit(context.Background(), claim); err != nil {
			t.Fatal(err)
		}
	}
	decision, err := service.Evaluate(context.Background(), bundle)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Decision.Result != ResultIneligible {
		t.Fatalf("result=%s", decision.Decision.Result)
	}
}

func TestRecordRequiresActivePolicyBinding(t *testing.T) {
	service, bundle, repository := fixture(t)
	control := service.Policy.Controls[0]
	claim := Claim{SchemaVersion: SchemaClaimV1, SubjectKind: "deployment_bundle", SubjectDigest: bundle.BundleDigest, ControlID: control.ID, Owner: control.Owner, Scope: "production", Artifact: Artifact{URI: "gs://evidence/record", Digest: identifiers.SHA256String("record"), MediaType: "application/json"}, SourceAuthority: "gitops", IssuedAt: service.now(), ValidUntil: service.now().Add(time.Hour)}
	if err := claim.Seal(); err != nil {
		t.Fatal(err)
	}
	verification := Verification{SchemaVersion: SchemaVerificationV1, ClaimDigest: claim.ClaimDigest, PolicyDigest: service.Policy.Digest, PolicyEpoch: service.Policy.Epoch, Result: ResultPass, Reasons: []string{"verified"}, Artifact: claim.Artifact, VerifiedAt: service.now(), ValidUntil: claim.ValidUntil}
	if err := verification.Seal(); err != nil {
		t.Fatal(err)
	}
	if err := service.Record(context.Background(), claim, verification); err != nil {
		t.Fatal(err)
	}
	records, err := service.Evidence(context.Background(), bundle.BundleDigest)
	if err != nil || len(records) != 1 {
		t.Fatalf("records=%d err=%v", len(records), err)
	}
	verification.PolicyEpoch++
	if err := verification.Seal(); err != nil {
		t.Fatal(err)
	}
	if err := service.Record(context.Background(), claim, verification); err == nil {
		t.Fatal("record accepted evidence from another policy epoch")
	}
	if len(repository.claims) != 1 {
		t.Fatalf("stored claims=%d", len(repository.claims))
	}
}

func TestCanonicalEligibilityVectorsMatchGitOps(t *testing.T) {
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	policy := Policy{
		ID:         "production-controls",
		Version:    "v1.0.0",
		Epoch:      1,
		ValidUntil: time.Date(2027, 8, 22, 0, 0, 0, 0, time.UTC),
		Controls: []Control{
			{ID: "cost_capacity", Owner: "finops", MaximumAge: 24 * time.Hour, ExceptionAllowed: true},
			{ID: "deployment_health", Owner: "sre", MaximumAge: time.Hour},
			{ID: "dr_evidence", Owner: "sre", MaximumAge: 90 * 24 * time.Hour, ExceptionAllowed: true},
			{ID: "github_protections", Owner: "platform", MaximumAge: 6 * time.Hour},
			{ID: "policy_results", Owner: "security", MaximumAge: 6 * time.Hour},
			{ID: "provenance", Owner: "security", MaximumAge: 24 * time.Hour},
			{ID: "sbom", Owner: "security", MaximumAge: 24 * time.Hour, ExceptionAllowed: true},
			{ID: "source_ci", Owner: "platform", MaximumAge: 6 * time.Hour},
			{ID: "tenant_isolation", Owner: "security", MaximumAge: 6 * time.Hour},
			{ID: "vulnerability_posture", Owner: "security", MaximumAge: 6 * time.Hour, ExceptionAllowed: true},
		},
	}
	if err := policy.Seal(); err != nil {
		t.Fatal(err)
	}
	if got, want := policy.Digest.String(), "sha256:affcc099bf4e423232e9176d2f77c9d08e0d4fd4eac6d4fb926a9abe85aa5326"; got != want {
		t.Fatalf("policy digest = %s, want %s", got, want)
	}

	repositories := make([]RepositoryRevision, 0, len(RequiredRepositories))
	for _, repository := range RequiredRepositories {
		repositories = append(repositories, RepositoryRevision{Repository: repository, Commit: "0000000000000000000000000000000000000000"})
	}
	bundle := DeploymentBundle{
		SchemaVersion:             SchemaBundleV1,
		ChangeReference:           "CHG-123",
		Environment:               "production",
		Repositories:              repositories,
		ReleaseDigests:            []string{"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		GitOpsRenderDigest:        mustEvidenceDigest(t, "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
		DeploymentSelectionDigest: mustEvidenceDigest(t, "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"),
		InfrastructureDigest:      mustEvidenceDigest(t, "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"),
		GovernanceAuditDigest:     mustEvidenceDigest(t, "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"),
		WorkflowRelease:           "v1.0.0",
		PolicyBundleDigest:        mustEvidenceDigest(t, "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"),
	}
	if err := bundle.Seal(); err != nil {
		t.Fatal(err)
	}
	if got, want := bundle.BundleDigest.String(), "sha256:8790176a9a3f68f6830e4fea9a5132d4e2c1166a46e0e0d51f694a62657eac6e"; got != want {
		t.Fatalf("bundle digest = %s, want %s", got, want)
	}

	artifact := Artifact{
		URI:       "gs://fixture/root/connected-evidence/connected-control-plane.zip",
		Digest:    mustEvidenceDigest(t, "sha256:1111111111111111111111111111111111111111111111111111111111111111"),
		MediaType: "application/zip",
	}
	decision := Decision{
		SchemaVersion: SchemaDecisionV1,
		BundleDigest:  bundle.BundleDigest,
		PolicyDigest:  policy.Digest,
		PolicyEpoch:   policy.Epoch,
		Result:        ResultEligible,
		EvaluatedAt:   now,
		ExpiresAt:     now.Add(time.Hour),
	}
	for _, control := range policy.Controls {
		claim := Claim{
			SchemaVersion:   SchemaClaimV1,
			SubjectKind:     "deployment_bundle",
			SubjectDigest:   bundle.BundleDigest,
			ControlID:       control.ID,
			Owner:           control.Owner,
			Scope:           "production",
			Artifact:        artifact,
			SourceAuthority: "production_qualification",
			IssuedAt:        now,
			ValidUntil:      now.Add(control.MaximumAge),
		}
		if err := claim.Seal(); err != nil {
			t.Fatal(err)
		}
		verification := Verification{
			SchemaVersion: SchemaVerificationV1,
			ClaimDigest:   claim.ClaimDigest,
			PolicyDigest:  policy.Digest,
			PolicyEpoch:   policy.Epoch,
			Result:        ResultPass,
			Reasons:       []string{"protected_qualification_passed"},
			Artifact:      artifact,
			VerifiedAt:    now,
			ValidUntil:    claim.ValidUntil,
		}
		if err := verification.Seal(); err != nil {
			t.Fatal(err)
		}
		if control.ID == "source_ci" {
			if got, want := claim.ClaimDigest.String(), "sha256:d6d7560f3f24232414aa92831d0912be51b7dce7f2b6e5f0b6bda1248b79828c"; got != want {
				t.Fatalf("claim digest = %s, want %s", got, want)
			}
			if got, want := verification.VerificationDigest.String(), "sha256:d68fe9f18b012170a6f4d62c3bb0047e95e4669959bd83d5f12f068f80c8d175"; got != want {
				t.Fatalf("verification digest = %s, want %s", got, want)
			}
		}
		decision.Selections = append(decision.Selections, Selection{
			ControlID:          control.ID,
			ClaimDigest:        claim.ClaimDigest,
			VerificationDigest: verification.VerificationDigest,
		})
	}
	if err := decision.Seal(); err != nil {
		t.Fatal(err)
	}
	if got, want := decision.DecisionDigest.String(), "sha256:5f41137dc4380fbb543420c62a515397915fb4956e74cdc6fca1956434141fbd"; got != want {
		t.Fatalf("decision digest = %s, want %s", got, want)
	}
}

func mustEvidenceDigest(t *testing.T, value string) identifiers.Digest {
	t.Helper()
	digest, err := identifiers.ParseDigest(value)
	if err != nil {
		t.Fatal(err)
	}
	return digest
}
