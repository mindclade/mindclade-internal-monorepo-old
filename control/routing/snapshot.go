// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package routing

import (
	"context"
	"go.mindclade.dev/control/runtime_authority"
	"go.mindclade.dev/libs/go/identifiers"
	"time"
)

type SnapshotRepository interface {
	Current(context.Context, string) (runtime_authority.RouteSnapshot, error)
	Put(context.Context, string, runtime_authority.RouteSnapshot) error
}
type SnapshotBuilder struct {
	Issuer runtime_authority.Issuer
	TTL    time.Duration
}

func (b SnapshotBuilder) Build(ctx context.Context, region string, version uint64, policy Policy, routes []Deployment, now time.Time) (runtime_authority.RouteSnapshot, error) {
	if region == "" || version == 0 {
		return runtime_authority.RouteSnapshot{}, invalid("route_snapshot_scope_invalid", "region and version are required", nil)
	}
	if err := policy.Validate(); err != nil {
		return runtime_authority.RouteSnapshot{}, err
	}
	canonical, err := CanonicalDeployments(routes)
	if err != nil {
		return runtime_authority.RouteSnapshot{}, err
	}
	filtered := canonical[:0]
	for _, r := range canonical {
		if r.Region == region {
			filtered = append(filtered, r)
		}
	}
	if len(filtered) == 0 {
		return runtime_authority.RouteSnapshot{}, invalid("route_snapshot_empty", "route snapshot has no deployments in region", nil)
	}
	ttl := b.TTL
	if ttl <= 0 {
		ttl = 2 * time.Minute
	}
	id, err := identifiers.NewID(identifiers.MustParseKind("routesnap"))
	if err != nil {
		return runtime_authority.RouteSnapshot{}, err
	}
	claims := runtime_authority.RouteSnapshotClaims{SnapshotID: id.String(), Version: version, PolicyEpoch: policy.PolicyEpoch, RevocationEpoch: policy.RevocationEpoch, Created: now.UTC(), Expires: now.UTC().Add(ttl), Routes: filtered, MinimumRuntimeVersion: policy.MinimumRuntimeVersion}
	return b.Issuer.IssueRouteSnapshot(ctx, claims)
}
