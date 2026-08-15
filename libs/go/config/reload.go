// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package config

import (
	"sync"

	"mindclade.internal/libs/go/faults"
)

// Atomic stores the last-known-good snapshot and accepts reloads only when all
// changed fields are explicitly marked Reloadable.
type Atomic struct {
	mu      sync.RWMutex
	current Snapshot
}

func NewAtomic(initial Snapshot) (*Atomic, error) {
	if initial.Digest().IsZero() {
		return nil, invalid(ErrSnapshotMismatch, "invalid_initial_snapshot", "config.NewAtomic", "", "")
	}
	return &Atomic{current: initial}, nil
}
func (atomic *Atomic) Snapshot() Snapshot {
	if atomic == nil {
		return Snapshot{}
	}
	atomic.mu.RLock()
	value := cloneSnapshot(atomic.current)
	atomic.mu.RUnlock()
	return value
}
func (atomic *Atomic) Apply(next Snapshot) error {
	if atomic == nil || next.Digest().IsZero() {
		return invalid(ErrSnapshotMismatch, "invalid_reload_snapshot", "config.Atomic.Apply", "", "")
	}
	atomic.mu.Lock()
	defer atomic.mu.Unlock()
	for key, current := range atomic.current.values {
		nextValue, exists := next.values[key]
		if exists && nextValue == current {
			continue
		}
		origin := atomic.current.origins[key]
		if nextOrigin, ok := next.origins[key]; ok {
			origin.Reloadable = origin.Reloadable && nextOrigin.Reloadable
		}
		if !origin.Reloadable {
			return faults.Wrap(ErrRestartRequired, faults.CodeFailedPrecondition, "configuration change requires restart", faults.WithReason("config_restart_required"), faults.WithOperation("config.Atomic.Apply"), faults.WithField("key", key), faults.WithRetryPolicy(faults.NoRetry()))
		}
	}
	for key := range next.values {
		if _, exists := atomic.current.values[key]; !exists && !next.origins[key].Reloadable {
			return faults.Wrap(ErrRestartRequired, faults.CodeFailedPrecondition, "configuration change requires restart", faults.WithReason("config_restart_required"), faults.WithOperation("config.Atomic.Apply"), faults.WithField("key", key), faults.WithRetryPolicy(faults.NoRetry()))
		}
	}
	atomic.current = cloneSnapshot(next)
	return nil
}
func cloneSnapshot(snapshot Snapshot) Snapshot {
	return Snapshot{values: snapshot.Values(), origins: cloneOrigins(snapshot.origins), digest: snapshot.digest, loadedAt: snapshot.loadedAt}
}
func cloneOrigins(values map[string]Origin) map[string]Origin {
	result := make(map[string]Origin, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}
