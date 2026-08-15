// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package runtime_authority

import (
	"context"
	"mindclade.internal/libs/go/clock"
	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/identifiers"
	"mindclade.internal/libs/go/signing"
	"time"
)

func invalid(reason, message string, cause error) error {
	if cause == nil {
		return faults.New(faults.CodeInvalidArgument, message, faults.WithReason(reason), faults.WithOperation("control.runtime_authority"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return faults.Wrap(cause, faults.CodeInvalidArgument, message, faults.WithReason(reason), faults.WithOperation("control.runtime_authority"), faults.WithRetryPolicy(faults.NoRetry()))
}
func denied(reason, message string) error {
	return faults.New(faults.CodePermissionDenied, message, faults.WithReason(reason), faults.WithOperation("control.runtime_authority"), faults.WithRetryPolicy(faults.NoRetry()))
}
func validateCanonicalID(value, expectedKind string) error {
	id, err := identifiers.ParseID(value)
	if err != nil {
		return invalid("invalid_"+expectedKind+"_id", expectedKind+" id must be canonical", err)
	}
	if expectedKind != "" && id.Kind().String() != expectedKind {
		return invalid("invalid_"+expectedKind+"_kind", expectedKind+" id has unexpected kind", nil)
	}
	return nil
}

type ValidationContext struct {
	Clock               clock.Clock
	Verifier            signing.Verifier
	Revocations         Revocations
	Fencing             FencingFloor
	MinimumPolicyEpoch  uint64
	MinimumRouteVersion uint64
	ExpectedIssuer      string
	ExpectedRegion      string
}

func (v ValidationContext) now() time.Time {
	if v.Clock != nil {
		return v.Clock.Now().UTC()
	}
	return time.Now().UTC()
}
func (v ValidationContext) verify(ctx context.Context, payload []byte, sig signing.Signature) error {
	if v.Verifier == nil {
		return denied("signature_verifier_unavailable", "signature verifier is unavailable")
	}
	if err := v.Verifier.Verify(ctx, payload, sig); err != nil {
		return err
	}
	return nil
}
func (v ValidationContext) ValidateExecutionTicket(ctx context.Context, t ExecutionTicket) error {
	b, err := t.Claims.CanonicalBytes()
	if err != nil {
		return err
	}
	if err = v.verify(ctx, b, t.Signature); err != nil {
		return err
	}
	now := v.now()
	if v.ExpectedIssuer != "" && t.Claims.Issuer != v.ExpectedIssuer {
		return denied("ticket_issuer_mismatch", "execution ticket issuer is not accepted")
	}
	if now.Before(t.Claims.NotBefore) || !now.Before(t.Claims.Expires) || now.After(t.Claims.Deadline) {
		return denied("ticket_inactive", "execution ticket is not active")
	}
	if t.Claims.PolicyEpoch < v.MinimumPolicyEpoch {
		return denied("ticket_policy_stale", "execution ticket policy epoch is stale")
	}
	if t.Claims.RouteSnapshotVersion < v.MinimumRouteVersion {
		return denied("ticket_route_stale", "execution ticket route snapshot is stale")
	}
	if v.Revocations != nil {
		if t.Claims.RevocationEpoch < v.Revocations.MinimumEpoch() {
			return denied("ticket_revocation_state_stale", "execution ticket predates required revocation state")
		}
		if v.Revocations.TicketRevoked(t.Claims.TicketID) {
			return denied("ticket_revoked", "execution ticket is revoked")
		}
		for _, d := range []identifiers.Digest{t.Claims.ModelBundleDigest, t.Claims.EngineBundleDigest} {
			if d.Valid() && v.Revocations.BundleRevoked(d.String()) {
				return denied("ticket_bundle_revoked", "execution ticket references a revoked bundle")
			}
		}
	}
	return ValidateFencing(t.Claims.StageID, t.Claims.FencingToken, v.Fencing)
}
func (v ValidationContext) ValidateAdmissionGrant(ctx context.Context, g AdmissionGrant) error {
	b, err := g.Claims.CanonicalBytes()
	if err != nil {
		return err
	}
	if err = v.verify(ctx, b, g.Signature); err != nil {
		return err
	}
	now := v.now()
	if now.Before(g.Claims.NotBefore) || !now.Before(g.Claims.Expires) {
		return denied("grant_inactive", "admission grant is not active")
	}
	if g.Claims.PolicyEpoch < v.MinimumPolicyEpoch {
		return denied("grant_policy_stale", "admission grant policy epoch is stale")
	}
	if v.ExpectedRegion != "" && g.Claims.Region != v.ExpectedRegion {
		return denied("grant_region_mismatch", "admission grant does not authorize this region")
	}
	if v.Revocations != nil {
		if g.Claims.RevocationEpoch < v.Revocations.MinimumEpoch() {
			return denied("grant_revocation_state_stale", "admission grant predates required revocation state")
		}
		if v.Revocations.GrantRevoked(g.Claims.GrantID) {
			return denied("grant_revoked", "admission grant is revoked")
		}
	}
	return nil
}
func (v ValidationContext) ValidateRouteSnapshot(ctx context.Context, s RouteSnapshot) error {
	if err := s.Claims.VerifyDigest(); err != nil {
		return err
	}
	b, err := s.Claims.CanonicalBytes()
	if err != nil {
		return err
	}
	if err = v.verify(ctx, b, s.Signature); err != nil {
		return err
	}
	now := v.now()
	if !now.Before(s.Claims.Expires) {
		return denied("route_snapshot_expired", "route snapshot has expired")
	}
	if s.Claims.PolicyEpoch < v.MinimumPolicyEpoch || s.Claims.Version < v.MinimumRouteVersion {
		return denied("route_snapshot_stale", "route snapshot is stale")
	}
	if v.Revocations != nil && s.Claims.RevocationEpoch < v.Revocations.MinimumEpoch() {
		return denied("route_revocation_state_stale", "route snapshot predates required revocation state")
	}
	return nil
}
