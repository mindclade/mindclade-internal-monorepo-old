// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package ingestion

import (
	"context"
	"sync"
)

// DefaultMaximumSnapshots bounds the in-memory adapter. A snapshot store that
// grows with the source is a spool, and an unbounded spool in a long-lived
// ingestion coordinator is an outage waiting for a large upstream window.
const DefaultMaximumSnapshots = 10_000

// SnapshotRepository persists immutable observations. A SnapshotID is the
// durable provenance link to exactly one set of raw bytes, so Put must be
// idempotent for a repeated identical observation and must refuse to rebind an
// identifier that already names different content.
type SnapshotRepository interface {
	Put(context.Context, Snapshot) error
	Get(context.Context, string) (Snapshot, bool, error)
}
type MemorySnapshotRepository struct {
	mu      sync.RWMutex
	items   map[string]Snapshot
	maximum int
}

// NewMemorySnapshotRepository takes the bound explicitly so no caller can
// construct an unbounded one. A non-positive value falls back to
// DefaultMaximumSnapshots, matching admission.NewMemoryRepository, so the two
// in-repository memory adapters do not disagree about the same argument.
func NewMemorySnapshotRepository(maximum int) *MemorySnapshotRepository {
	if maximum <= 0 {
		maximum = DefaultMaximumSnapshots
	}
	return &MemorySnapshotRepository{items: map[string]Snapshot{}, maximum: maximum}
}
func (r *MemorySnapshotRepository) Put(_ context.Context, s Snapshot) error {
	if err := s.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	// A repeated observation is a replay and keeps the original record; anything
	// else under the same identifier is a rebind. Overwriting it silently would
	// break the one thing a snapshot exists to prove — which bytes this run
	// actually read — and would leave nothing behind to find the divergence with.
	if existing, ok := r.items[s.SnapshotID]; ok {
		if !existing.SameObservation(s) {
			return conflict("snapshot_identity_conflict", "snapshot id is already bound to a different observation")
		}
		return nil
	}
	if len(r.items) >= r.maximum {
		return exhausted("snapshot_store_bound", "snapshot store bound was reached")
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
