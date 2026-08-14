// Copyright 2026 Mindclade. All rights reserved.
package routing

import (
	"context"
	"mindclade.internal/control/runtime_authority"
	"sync"
)

type MemoryRepository struct {
	mu     sync.RWMutex
	values map[string]runtime_authority.RouteSnapshot
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{values: map[string]runtime_authority.RouteSnapshot{}}
}
func (r *MemoryRepository) Current(_ context.Context, region string) (runtime_authority.RouteSnapshot, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.values[region]
	if !ok {
		return runtime_authority.RouteSnapshot{}, invalid("route_snapshot_not_found", "route snapshot is not published", nil)
	}
	return v, nil
}
func (r *MemoryRepository) Put(_ context.Context, region string, v runtime_authority.RouteSnapshot) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if old, ok := r.values[region]; ok && v.Claims.Version <= old.Claims.Version {
		return invalid("route_snapshot_version_not_monotonic", "route snapshot version must increase", nil)
	}
	r.values[region] = v
	return nil
}
