// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

package evidence

import (
	"context"
	"errors"
	"time"

	"go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/signing"
)

type Repository interface {
	AppendClaim(context.Context, Claim, Verification) error
	Current(context.Context, identifiers.Digest, string) (Claim, Verification, error)
	Evidence(context.Context, identifiers.Digest) ([]Record, error)
	PutDecision(context.Context, SignedDecision) error
	Decision(context.Context, identifiers.Digest) (SignedDecision, bool, error)
	Revoke(context.Context, identifiers.Digest, string, time.Time) error
}

type Record struct {
	Claim        Claim        `json:"claim"`
	Verification Verification `json:"verification"`
}

type Verifier interface {
	Verify(context.Context, Claim, Control, time.Time) (Verification, error)
}

type Service struct {
	Repository Repository
	Verifier   Verifier
	Policy     Policy
	Signer     signing.Signer
	Clock      clock.Clock
}

func (service Service) Submit(ctx context.Context, claim Claim) (Claim, Verification, error) {
	if err := service.validate(ctx, true, false); err != nil {
		return Claim{}, Verification{}, err
	}
	control, found := service.control(claim.ControlID)
	if !found {
		return Claim{}, Verification{}, invalid("evidence_control_unknown", "evidence claim names an unknown control", nil)
	}
	if claim.Owner != "" && claim.Owner != control.Owner {
		return Claim{}, Verification{}, invalid("evidence_owner_mismatch", "evidence claim owner differs from active policy", nil)
	}
	claim.Owner = control.Owner
	if err := claim.Seal(); err != nil {
		return Claim{}, Verification{}, err
	}
	now := service.now()
	verification, err := service.Verifier.Verify(ctx, claim, control, now)
	if err != nil {
		return Claim{}, Verification{}, err
	}
	verification.SchemaVersion = SchemaVerificationV1
	verification.ClaimDigest = claim.ClaimDigest
	verification.PolicyDigest = service.Policy.Digest
	verification.PolicyEpoch = service.Policy.Epoch
	verification.VerifiedAt = now
	maximum := now.Add(control.MaximumAge)
	if verification.ValidUntil.IsZero() || verification.ValidUntil.After(maximum) {
		verification.ValidUntil = maximum
	}
	if verification.ValidUntil.After(claim.ValidUntil) {
		verification.ValidUntil = claim.ValidUntil
	}
	if err := verification.Seal(); err != nil {
		return Claim{}, Verification{}, err
	}
	if err := service.Repository.AppendClaim(ctx, claim, verification); err != nil {
		return Claim{}, Verification{}, err
	}
	return claim, verification, nil
}

// Record appends independently produced evidence after binding it to the
// active policy. It does not trust caller-supplied ownership or validity caps.
func (service Service) Record(ctx context.Context, claim Claim, verification Verification) error {
	if err := service.validate(ctx, false, false); err != nil {
		return err
	}
	if err := claim.Validate(); err != nil {
		return err
	}
	if err := verification.Validate(); err != nil {
		return err
	}
	control, found := service.control(claim.ControlID)
	if !found {
		return invalid("evidence_control_unknown", "evidence claim names an unknown control", nil)
	}
	if claim.Owner != control.Owner || !verification.ClaimDigest.Equal(claim.ClaimDigest) ||
		!verification.PolicyDigest.Equal(service.Policy.Digest) || verification.PolicyEpoch != service.Policy.Epoch {
		return invalid("evidence_policy_binding_invalid", "evidence is not bound to the active policy", nil)
	}
	if verification.ValidUntil.After(claim.ValidUntil) || verification.ValidUntil.Sub(verification.VerifiedAt) > control.MaximumAge {
		return invalid("evidence_validity_exceeds_policy", "evidence validity exceeds the active policy", nil)
	}
	return service.Repository.AppendClaim(ctx, claim, verification)
}

func (service Service) Evidence(ctx context.Context, subject identifiers.Digest) ([]Record, error) {
	if err := service.validate(ctx, false, false); err != nil {
		return nil, err
	}
	if !subject.Valid() {
		return nil, invalid("evidence_subject_invalid", "evidence subject digest is invalid", nil)
	}
	return service.Repository.Evidence(ctx, subject)
}

func (service Service) Evaluate(ctx context.Context, bundle DeploymentBundle) (SignedDecision, error) {
	if err := service.validate(ctx, false, true); err != nil {
		return SignedDecision{}, err
	}
	if err := bundle.Validate(); err != nil {
		return SignedDecision{}, err
	}
	now := service.now()
	decision := Decision{
		SchemaVersion: SchemaDecisionV1,
		BundleDigest:  bundle.BundleDigest,
		PolicyDigest:  service.Policy.Digest,
		PolicyEpoch:   service.Policy.Epoch,
		Result:        ResultEligible,
		EvaluatedAt:   now,
		ExpiresAt:     now.Add(MaximumDecisionTTL),
	}
	if service.Policy.ValidUntil.Before(decision.ExpiresAt) {
		decision.ExpiresAt = service.Policy.ValidUntil
	}
	for _, control := range service.Policy.Controls {
		claim, verification, err := service.Repository.Current(ctx, bundle.BundleDigest, control.ID)
		if err != nil {
			if faults.CodeOf(err) == faults.CodeNotFound {
				decision.Result = ResultIndeterminate
				decision.Reasons = append(decision.Reasons, "missing_"+control.ID)
				continue
			}
			return SignedDecision{}, err
		}
		if err := claim.Validate(); err != nil {
			return SignedDecision{}, err
		}
		if err := verification.Validate(); err != nil {
			return SignedDecision{}, err
		}
		decision.Selections = append(decision.Selections, Selection{
			ControlID:          control.ID,
			ClaimDigest:        claim.ClaimDigest,
			VerificationDigest: verification.VerificationDigest,
		})
		if claim.ValidUntil.Before(decision.ExpiresAt) {
			decision.ExpiresAt = claim.ValidUntil
		}
		if verification.ValidUntil.Before(decision.ExpiresAt) {
			decision.ExpiresAt = verification.ValidUntil
		}
		if !claim.ValidUntil.After(now) || !verification.ValidUntil.After(now) || verification.Result != ResultPass {
			decision.Result = ResultIneligible
			decision.Reasons = append(decision.Reasons, "failed_"+control.ID)
		}
	}
	if !decision.ExpiresAt.After(now) {
		decision.Result = ResultIneligible
		decision.ExpiresAt = now.Add(time.Millisecond)
		decision.Reasons = append(decision.Reasons, "evidence_expired")
	}
	if err := decision.Seal(); err != nil {
		return SignedDecision{}, err
	}
	payload, err := decision.CanonicalBytes()
	if err != nil {
		return SignedDecision{}, err
	}
	signature, err := service.Signer.Sign(ctx, payload)
	if err != nil {
		return SignedDecision{}, err
	}
	signed := SignedDecision{Decision: decision, Signature: signature}
	if err := signed.Validate(); err != nil {
		return SignedDecision{}, err
	}
	if err := service.Repository.PutDecision(ctx, signed); err != nil {
		return SignedDecision{}, err
	}
	return signed, nil
}

func (service Service) GetDecision(ctx context.Context, digest identifiers.Digest) (SignedDecision, bool, error) {
	if err := service.validate(ctx, false, false); err != nil {
		return SignedDecision{}, false, err
	}
	return service.Repository.Decision(ctx, digest)
}

func (service Service) Revoke(ctx context.Context, digest identifiers.Digest, reason string) error {
	if err := service.validate(ctx, false, false); err != nil {
		return err
	}
	if !digest.Valid() || !canonicalName.MatchString(reason) {
		return invalid("eligibility_revocation_invalid", "eligibility revocation is invalid", nil)
	}
	return service.Repository.Revoke(ctx, digest, reason, service.now())
}

func (service Service) validate(ctx context.Context, requireVerifier, requireSigner bool) error {
	if ctx == nil {
		return invalid("evidence_context_nil", "evidence context is required", nil)
	}
	if service.Repository == nil || service.Clock == nil || requireVerifier && service.Verifier == nil || requireSigner && service.Signer == nil {
		return unavailable("evidence_service_unconfigured", "evidence service is unavailable")
	}
	if err := service.Policy.Validate(); err != nil {
		return err
	}
	if !service.Policy.ValidUntil.After(service.now()) {
		return unavailable("evidence_policy_expired", "active evidence policy is expired")
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(err, unavailable("evidence_context_done", "evidence request was canceled"))
	}
	return nil
}

func (service Service) control(id string) (Control, bool) {
	for _, control := range service.Policy.Controls {
		if control.ID == id {
			return control, true
		}
	}
	return Control{}, false
}

func (service Service) now() time.Time { return service.Clock.Now().Round(0).UTC() }

var ErrNotFound = notFound("evidence_not_found", "evidence was not found")
