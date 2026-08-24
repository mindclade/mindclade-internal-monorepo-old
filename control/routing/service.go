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

type Service struct {
	Repository SnapshotRepository
	Publisher  Publisher
	Builder    SnapshotBuilder
}

func (s Service) PublishAt(ctx context.Context, region string, version uint64, p Policy, routes []Deployment, now time.Time) (runtime_authority.RouteSnapshot, error) {
	if s.Repository == nil {
		return runtime_authority.RouteSnapshot{}, invalid("route_repository_unavailable", "route repository is unavailable", nil)
	}
	snapshot, err := s.Builder.Build(ctx, region, version, p, routes, now)
	if err != nil {
		return runtime_authority.RouteSnapshot{}, err
	}
	if err = s.Repository.Put(ctx, region, snapshot); err != nil {
		return runtime_authority.RouteSnapshot{}, err
	}
	if s.Publisher != nil {
		if err = s.Publisher.PublishRouteSnapshot(ctx, region, snapshot); err != nil {
			// The repository already owns this exact signed snapshot. Return it with
			// the delivery error so callers can retain its immutable identity and use
			// Republish rather than manufacturing a second snapshot at the same version.
			return snapshot, err
		}
	}
	return snapshot, nil
}

// Republish redelivers the repository-owned immutable snapshot for a region.
// It does not create a new version or signature and is therefore safe after a
// transient publisher failure from PublishAt.
func (s Service) Republish(ctx context.Context, region string, expected identifiers.Digest) (runtime_authority.RouteSnapshot, error) {
	if s.Repository == nil {
		return runtime_authority.RouteSnapshot{}, invalid("route_repository_unavailable", "route repository is unavailable", nil)
	}
	if s.Publisher == nil {
		return runtime_authority.RouteSnapshot{}, invalid("route_publisher_unavailable", "route publisher is unavailable", nil)
	}
	if !expected.Valid() {
		return runtime_authority.RouteSnapshot{}, invalid("route_snapshot_digest_invalid", "expected route snapshot digest is required", nil)
	}
	snapshot, err := s.Repository.Current(ctx, region)
	if err != nil {
		return runtime_authority.RouteSnapshot{}, err
	}
	if !snapshot.Claims.SnapshotDigest.Equal(expected) {
		return runtime_authority.RouteSnapshot{}, invalid("route_snapshot_retry_stale", "current route snapshot differs from the requested retry", nil)
	}
	if err := s.Publisher.PublishRouteSnapshot(ctx, region, snapshot); err != nil {
		return snapshot, err
	}
	return snapshot, nil
}
