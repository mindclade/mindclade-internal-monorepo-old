// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

package evidence

import (
	"regexp"
	"sort"
	"strings"
	"time"

	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/signing"
)

const (
	SchemaClaimV1        = "mindclade.dev/evidence-claim/v1"
	SchemaVerificationV1 = "mindclade.dev/evidence-verification/v1"
	SchemaBundleV1       = "mindclade.dev/deployment-bundle/v1"
	SchemaDecisionV1     = "mindclade.dev/production-eligibility/v1"
	MaximumDecisionTTL   = 6 * time.Hour
)

var (
	canonicalName = regexp.MustCompile(`^[a-z][a-z0-9_.-]{1,127}$`)
	commitSHA     = regexp.MustCompile(`^[0-9a-f]{40}$`)
	changeRef     = regexp.MustCompile(`^(?:CHG|SEC|DR)-[A-Za-z0-9._-]+$`)
)

var RequiredRepositories = []string{
	".github",
	".github-private",
	"bootstrap",
	"github-config",
	"gitops",
	"infrastructure-live",
	"mindclade-internal-monorepo",
}

type Result string

const (
	ResultPass          Result = "pass"
	ResultFail          Result = "fail"
	ResultEligible      Result = "eligible"
	ResultWithException Result = "eligible_with_exceptions"
	ResultIneligible    Result = "ineligible"
	ResultIndeterminate Result = "indeterminate"
)

type Artifact struct {
	URI       string             `json:"uri"`
	Digest    identifiers.Digest `json:"digest"`
	MediaType string             `json:"media_type"`
}

func (artifact Artifact) Validate() error {
	if !strings.HasPrefix(artifact.URI, "gs://") && !strings.HasPrefix(artifact.URI, "https://") && !strings.HasPrefix(artifact.URI, "oci://") {
		return invalid("evidence_artifact_uri_invalid", "evidence artifact URI must be immutable-capable", nil)
	}
	if !artifact.Digest.Valid() || !strings.Contains(artifact.MediaType, "/") || len(artifact.MediaType) > 128 {
		return invalid("evidence_artifact_invalid", "evidence artifact metadata is invalid", nil)
	}
	return nil
}

type Claim struct {
	SchemaVersion   string             `json:"schema_version"`
	ClaimDigest     identifiers.Digest `json:"claim_digest"`
	SubjectKind     string             `json:"subject_kind"`
	SubjectDigest   identifiers.Digest `json:"subject_digest"`
	ControlID       string             `json:"control_id"`
	Owner           string             `json:"owner"`
	Scope           string             `json:"scope"`
	Artifact        Artifact           `json:"artifact"`
	SourceAuthority string             `json:"source_authority"`
	IssuedAt        time.Time          `json:"issued_at"`
	ValidUntil      time.Time          `json:"valid_until"`
}

func (claim Claim) CanonicalBytes() ([]byte, error) {
	if err := claim.validate(false); err != nil {
		return nil, err
	}
	encoder := newCanonicalEncoder("evidence-claim/v1")
	encoder.text("schema_version", claim.SchemaVersion)
	encoder.text("subject_kind", claim.SubjectKind)
	encoder.text("subject_digest", claim.SubjectDigest.String())
	encoder.text("control_id", claim.ControlID)
	encoder.text("owner", claim.Owner)
	encoder.text("scope", claim.Scope)
	encoder.text("artifact_uri", claim.Artifact.URI)
	encoder.text("artifact_digest", claim.Artifact.Digest.String())
	encoder.text("artifact_media_type", claim.Artifact.MediaType)
	encoder.text("source_authority", claim.SourceAuthority)
	encoder.timestamp("issued_at", claim.IssuedAt)
	encoder.timestamp("valid_until", claim.ValidUntil)
	return encoder.bytes()
}

func (claim *Claim) Seal() error {
	if claim == nil {
		return invalid("evidence_claim_nil", "evidence claim is required", nil)
	}
	payload, err := claim.CanonicalBytes()
	if err != nil {
		return err
	}
	claim.ClaimDigest = identifiers.SHA256(payload)
	return nil
}

func (claim Claim) Validate() error { return claim.validate(true) }

func (claim Claim) validate(requireDigest bool) error {
	if claim.SchemaVersion != SchemaClaimV1 || !canonicalName.MatchString(claim.SubjectKind) || !claim.SubjectDigest.Valid() ||
		!canonicalName.MatchString(claim.ControlID) || !canonicalName.MatchString(claim.Owner) || claim.Scope == "" || len(claim.Scope) > 512 ||
		!canonicalName.MatchString(claim.SourceAuthority) {
		return invalid("evidence_claim_fields_invalid", "evidence claim fields are invalid", nil)
	}
	if err := claim.Artifact.Validate(); err != nil {
		return err
	}
	if claim.IssuedAt.IsZero() || !claim.ValidUntil.After(claim.IssuedAt) {
		return invalid("evidence_claim_window_invalid", "evidence claim validity window is invalid", nil)
	}
	if requireDigest {
		payload, err := claim.CanonicalBytes()
		if err != nil {
			return err
		}
		if !claim.ClaimDigest.Verify(payload) {
			return invalid("evidence_claim_digest_mismatch", "evidence claim digest does not match canonical content", nil)
		}
	}
	return nil
}

type Verification struct {
	SchemaVersion      string             `json:"schema_version"`
	VerificationDigest identifiers.Digest `json:"verification_digest"`
	ClaimDigest        identifiers.Digest `json:"claim_digest"`
	PolicyDigest       identifiers.Digest `json:"policy_digest"`
	PolicyEpoch        uint64             `json:"policy_epoch"`
	Result             Result             `json:"result"`
	Reasons            []string           `json:"reasons"`
	Artifact           Artifact           `json:"artifact"`
	VerifiedAt         time.Time          `json:"verified_at"`
	ValidUntil         time.Time          `json:"valid_until"`
}

func (verification Verification) CanonicalBytes() ([]byte, error) {
	if err := verification.validate(false); err != nil {
		return nil, err
	}
	encoder := newCanonicalEncoder("evidence-verification/v1")
	encoder.text("schema_version", verification.SchemaVersion)
	encoder.text("claim_digest", verification.ClaimDigest.String())
	encoder.text("policy_digest", verification.PolicyDigest.String())
	encoder.u64("policy_epoch", verification.PolicyEpoch)
	encoder.text("result", string(verification.Result))
	encoder.strings("reasons", verification.Reasons)
	encoder.text("artifact_uri", verification.Artifact.URI)
	encoder.text("artifact_digest", verification.Artifact.Digest.String())
	encoder.text("artifact_media_type", verification.Artifact.MediaType)
	encoder.timestamp("verified_at", verification.VerifiedAt)
	encoder.timestamp("valid_until", verification.ValidUntil)
	return encoder.bytes()
}

func (verification *Verification) Seal() error {
	if verification == nil {
		return invalid("evidence_verification_nil", "evidence verification is required", nil)
	}
	verification.Reasons = append([]string(nil), verification.Reasons...)
	sort.Strings(verification.Reasons)
	payload, err := verification.CanonicalBytes()
	if err != nil {
		return err
	}
	verification.VerificationDigest = identifiers.SHA256(payload)
	return nil
}

func (verification Verification) Validate() error { return verification.validate(true) }

func (verification Verification) validate(requireDigest bool) error {
	if verification.SchemaVersion != SchemaVerificationV1 || !verification.ClaimDigest.Valid() || !verification.PolicyDigest.Valid() || verification.PolicyEpoch == 0 ||
		verification.Result != ResultPass && verification.Result != ResultFail {
		return invalid("evidence_verification_fields_invalid", "evidence verification fields are invalid", nil)
	}
	if err := requireSortedNames(verification.Reasons); err != nil {
		return err
	}
	if err := verification.Artifact.Validate(); err != nil {
		return err
	}
	if verification.VerifiedAt.IsZero() || !verification.ValidUntil.After(verification.VerifiedAt) {
		return invalid("evidence_verification_window_invalid", "evidence verification window is invalid", nil)
	}
	if requireDigest {
		payload, err := verification.CanonicalBytes()
		if err != nil {
			return err
		}
		if !verification.VerificationDigest.Verify(payload) {
			return invalid("evidence_verification_digest_mismatch", "evidence verification digest does not match canonical content", nil)
		}
	}
	return nil
}

type RepositoryRevision struct {
	Repository string `json:"repository"`
	Commit     string `json:"commit"`
}

type DeploymentBundle struct {
	SchemaVersion             string               `json:"schema_version"`
	BundleDigest              identifiers.Digest   `json:"bundle_digest"`
	ChangeReference           string               `json:"change_reference"`
	Environment               string               `json:"environment"`
	Repositories              []RepositoryRevision `json:"repositories"`
	ReleaseDigests            []string             `json:"release_digests"`
	GitOpsRenderDigest        identifiers.Digest   `json:"gitops_render_digest"`
	DeploymentSelectionDigest identifiers.Digest   `json:"deployment_selection_digest"`
	InfrastructureDigest      identifiers.Digest   `json:"infrastructure_handoff_digest"`
	GovernanceAuditDigest     identifiers.Digest   `json:"governance_audit_digest"`
	WorkflowRelease           string               `json:"workflow_release"`
	PolicyBundleDigest        identifiers.Digest   `json:"policy_bundle_digest"`
}

func (bundle DeploymentBundle) CanonicalBytes() ([]byte, error) {
	if err := bundle.validate(false); err != nil {
		return nil, err
	}
	encoder := newCanonicalEncoder("deployment-bundle/v1")
	encoder.text("schema_version", bundle.SchemaVersion)
	encoder.text("change_reference", bundle.ChangeReference)
	encoder.text("environment", bundle.Environment)
	repositories := make([]string, 0, len(bundle.Repositories))
	for _, repository := range bundle.Repositories {
		repositories = append(repositories, repository.Repository+"\x00"+repository.Commit)
	}
	encoder.strings("repositories", repositories)
	encoder.strings("release_digests", bundle.ReleaseDigests)
	encoder.text("gitops_render_digest", bundle.GitOpsRenderDigest.String())
	encoder.text("deployment_selection_digest", bundle.DeploymentSelectionDigest.String())
	encoder.text("infrastructure_handoff_digest", bundle.InfrastructureDigest.String())
	encoder.text("governance_audit_digest", bundle.GovernanceAuditDigest.String())
	encoder.text("workflow_release", bundle.WorkflowRelease)
	encoder.text("policy_bundle_digest", bundle.PolicyBundleDigest.String())
	return encoder.bytes()
}

func (bundle *DeploymentBundle) Seal() error {
	if bundle == nil {
		return invalid("deployment_bundle_nil", "deployment bundle is required", nil)
	}
	bundle.Repositories = append([]RepositoryRevision(nil), bundle.Repositories...)
	sort.Slice(bundle.Repositories, func(left, right int) bool {
		return bundle.Repositories[left].Repository < bundle.Repositories[right].Repository
	})
	bundle.ReleaseDigests = append([]string(nil), bundle.ReleaseDigests...)
	sort.Strings(bundle.ReleaseDigests)
	payload, err := bundle.CanonicalBytes()
	if err != nil {
		return err
	}
	bundle.BundleDigest = identifiers.SHA256(payload)
	return nil
}

func (bundle DeploymentBundle) Validate() error { return bundle.validate(true) }

func (bundle DeploymentBundle) validate(requireDigest bool) error {
	if bundle.SchemaVersion != SchemaBundleV1 || !changeRef.MatchString(bundle.ChangeReference) || bundle.Environment != "production" ||
		!bundle.GitOpsRenderDigest.Valid() || !bundle.DeploymentSelectionDigest.Valid() || !bundle.InfrastructureDigest.Valid() ||
		!bundle.GovernanceAuditDigest.Valid() || !bundle.PolicyBundleDigest.Valid() || !regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(bundle.WorkflowRelease) {
		return invalid("deployment_bundle_fields_invalid", "deployment bundle fields are invalid", nil)
	}
	if len(bundle.Repositories) != len(RequiredRepositories) {
		return invalid("deployment_bundle_repository_count", "deployment bundle must contain every governed repository", nil)
	}
	for index, expected := range RequiredRepositories {
		if bundle.Repositories[index].Repository != expected || !commitSHA.MatchString(bundle.Repositories[index].Commit) {
			return invalid("deployment_bundle_repositories_invalid", "deployment bundle repositories must be exact, sorted, and immutable", nil)
		}
	}
	if len(bundle.ReleaseDigests) == 0 || len(bundle.ReleaseDigests) > 64 {
		return invalid("deployment_bundle_releases_invalid", "deployment bundle release digest inventory is invalid", nil)
	}
	for index, digest := range bundle.ReleaseDigests {
		if _, err := identifiers.ParseDigest(digest); err != nil || index > 0 && bundle.ReleaseDigests[index-1] >= digest {
			return invalid("deployment_bundle_releases_noncanonical", "deployment bundle release digests must be sorted and unique", err)
		}
	}
	if requireDigest {
		payload, err := bundle.CanonicalBytes()
		if err != nil {
			return err
		}
		if !bundle.BundleDigest.Verify(payload) {
			return invalid("deployment_bundle_digest_mismatch", "deployment bundle digest does not match canonical content", nil)
		}
	}
	return nil
}

type Selection struct {
	ControlID          string             `json:"control_id"`
	ClaimDigest        identifiers.Digest `json:"claim_digest"`
	VerificationDigest identifiers.Digest `json:"verification_digest"`
}

type Decision struct {
	SchemaVersion  string             `json:"schema_version"`
	DecisionDigest identifiers.Digest `json:"decision_digest"`
	BundleDigest   identifiers.Digest `json:"bundle_digest"`
	PolicyDigest   identifiers.Digest `json:"policy_digest"`
	PolicyEpoch    uint64             `json:"policy_epoch"`
	Result         Result             `json:"result"`
	Reasons        []string           `json:"reasons"`
	Selections     []Selection        `json:"selections"`
	Exceptions     []string           `json:"exceptions"`
	EvaluatedAt    time.Time          `json:"evaluated_at"`
	ExpiresAt      time.Time          `json:"expires_at"`
}

func (decision Decision) CanonicalBytes() ([]byte, error) {
	if err := decision.validate(false); err != nil {
		return nil, err
	}
	encoder := newCanonicalEncoder("production-eligibility/v1")
	encoder.text("schema_version", decision.SchemaVersion)
	encoder.text("bundle_digest", decision.BundleDigest.String())
	encoder.text("policy_digest", decision.PolicyDigest.String())
	encoder.u64("policy_epoch", decision.PolicyEpoch)
	encoder.text("result", string(decision.Result))
	encoder.strings("reasons", decision.Reasons)
	selections := make([]string, 0, len(decision.Selections))
	for _, selection := range decision.Selections {
		selections = append(selections, selection.ControlID+"|"+selection.ClaimDigest.String()+"|"+selection.VerificationDigest.String())
	}
	encoder.strings("selections", selections)
	encoder.strings("exceptions", decision.Exceptions)
	encoder.timestamp("evaluated_at", decision.EvaluatedAt)
	encoder.timestamp("expires_at", decision.ExpiresAt)
	return encoder.bytes()
}

func (decision *Decision) Seal() error {
	if decision == nil {
		return invalid("eligibility_decision_nil", "eligibility decision is required", nil)
	}
	decision.Reasons = canonicalStrings(decision.Reasons)
	decision.Exceptions = canonicalStrings(decision.Exceptions)
	decision.Selections = append([]Selection(nil), decision.Selections...)
	sort.Slice(decision.Selections, func(left, right int) bool {
		return decision.Selections[left].ControlID < decision.Selections[right].ControlID
	})
	payload, err := decision.CanonicalBytes()
	if err != nil {
		return err
	}
	decision.DecisionDigest = identifiers.SHA256(payload)
	return nil
}

func (decision Decision) Validate() error { return decision.validate(true) }

func (decision Decision) validate(requireDigest bool) error {
	if decision.SchemaVersion != SchemaDecisionV1 || !decision.BundleDigest.Valid() || !decision.PolicyDigest.Valid() || decision.PolicyEpoch == 0 ||
		decision.Result != ResultEligible && decision.Result != ResultWithException && decision.Result != ResultIneligible && decision.Result != ResultIndeterminate {
		return invalid("eligibility_decision_fields_invalid", "eligibility decision fields are invalid", nil)
	}
	if err := requireSortedNames(decision.Reasons); err != nil {
		return err
	}
	if err := requireSortedNames(decision.Exceptions); err != nil {
		return err
	}
	for index, selection := range decision.Selections {
		if !canonicalName.MatchString(selection.ControlID) || !selection.ClaimDigest.Valid() || !selection.VerificationDigest.Valid() ||
			index > 0 && decision.Selections[index-1].ControlID >= selection.ControlID {
			return invalid("eligibility_selection_invalid", "eligibility selections must be sorted, unique, and complete", nil)
		}
	}
	if decision.EvaluatedAt.IsZero() || !decision.ExpiresAt.After(decision.EvaluatedAt) || decision.ExpiresAt.Sub(decision.EvaluatedAt) > MaximumDecisionTTL {
		return invalid("eligibility_decision_window_invalid", "eligibility decision validity window is invalid", nil)
	}
	if requireDigest {
		payload, err := decision.CanonicalBytes()
		if err != nil {
			return err
		}
		if !decision.DecisionDigest.Verify(payload) {
			return invalid("eligibility_decision_digest_mismatch", "eligibility decision digest does not match canonical content", nil)
		}
	}
	return nil
}

type SignedDecision struct {
	Decision  Decision          `json:"decision"`
	Signature signing.Signature `json:"signature"`
}

func (signed SignedDecision) Validate() error {
	if err := signed.Decision.Validate(); err != nil {
		return err
	}
	return signed.Signature.Validate()
}

func canonicalStrings(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	if len(result) < 2 {
		return result
	}
	write := 1
	for read := 1; read < len(result); read++ {
		if result[read] != result[write-1] {
			result[write] = result[read]
			write++
		}
	}
	return result[:write]
}

func requireSortedNames(values []string) error {
	for index, value := range values {
		if !canonicalName.MatchString(value) || index > 0 && values[index-1] >= value {
			return invalid("canonical_names_invalid", "names must be canonical, sorted, and unique", nil)
		}
	}
	return nil
}
