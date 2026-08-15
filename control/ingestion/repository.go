// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package ingestion

import (
	"context"
	"sync"
)

type SnapshotRepository interface {
	Put(context.Context, Snapshot) error
	Get(context.Context, string) (Snapshot, bool, error)
}
type MemorySnapshotRepository struct {
	mu    sync.RWMutex
	items map[string]Snapshot
}

func NewMemorySnapshotRepository() *MemorySnapshotRepository {
	return &MemorySnapshotRepository{items: map[string]Snapshot{}}
}
func (r *MemorySnapshotRepository) Put(_ context.Context, s Snapshot) error {
	if err := s.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.items[s.SnapshotID]; ok && existing.RawArtifact.EqualIdentity(s.RawArtifact) {
		return nil
	}
	r.items[s.SnapshotID] = s
	return nil
}
func (r *MemorySnapshotRepository) Get(_ context.Context, id string) (Snapshot, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.items[id]
	return v, ok, nil
}
