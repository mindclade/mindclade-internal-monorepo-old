// Copyright 2026 Mindclade. All rights reserved.
package routing

import (
	"context"
	"mindclade.internal/control/runtime_authority"
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
			return runtime_authority.RouteSnapshot{}, err
		}
	}
	return snapshot, nil
}
