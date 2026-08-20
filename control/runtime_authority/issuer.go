// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package runtime_authority

import (
	"context"
	"go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/signing"
)

type Issuer struct {
	Name   string
	Signer signing.Signer
	Clock  clock.Clock
}

func (i Issuer) validate() error {
	if i.Name == "" || i.Signer == nil {
		return invalid("issuer_unconfigured", "runtime authority issuer is not configured", nil)
	}
	return nil
}
func (i Issuer) IssueExecutionTicket(ctx context.Context, claims ExecutionTicketClaims) (ExecutionTicket, error) {
	if err := i.validate(); err != nil {
		return ExecutionTicket{}, err
	}
	claims.Issuer = i.Name
	b, err := claims.CanonicalBytes()
	if err != nil {
		return ExecutionTicket{}, err
	}
	sig, err := i.Signer.Sign(ctx, b)
	if err != nil {
		return ExecutionTicket{}, err
	}
	return ExecutionTicket{claims, sig}, nil
}
func (i Issuer) IssueAdmissionGrant(ctx context.Context, claims AdmissionGrantClaims) (AdmissionGrant, error) {
	if err := i.validate(); err != nil {
		return AdmissionGrant{}, err
	}
	b, err := claims.CanonicalBytes()
	if err != nil {
		return AdmissionGrant{}, err
	}
	sig, err := i.Signer.Sign(ctx, b)
	if err != nil {
		return AdmissionGrant{}, err
	}
	return AdmissionGrant{claims, sig}, nil
}
func (i Issuer) IssueRouteSnapshot(ctx context.Context, claims RouteSnapshotClaims) (RouteSnapshot, error) {
	if err := i.validate(); err != nil {
		return RouteSnapshot{}, err
	}
	if err := claims.SealDigest(); err != nil {
		return RouteSnapshot{}, err
	}
	b, err := claims.CanonicalBytes()
	if err != nil {
		return RouteSnapshot{}, err
	}
	sig, err := i.Signer.Sign(ctx, b)
	if err != nil {
		return RouteSnapshot{}, err
	}
	return RouteSnapshot{claims, sig}, nil
}
func (i Issuer) IssueRevocations(ctx context.Context, claims RevocationSnapshotClaims) (RevocationSnapshot, error) {
	if err := i.validate(); err != nil {
		return RevocationSnapshot{}, err
	}
	b, err := claims.CanonicalBytes()
	if err != nil {
		return RevocationSnapshot{}, err
	}
	sig, err := i.Signer.Sign(ctx, b)
	if err != nil {
		return RevocationSnapshot{}, err
	}
	return RevocationSnapshot{claims, sig}, nil
}
