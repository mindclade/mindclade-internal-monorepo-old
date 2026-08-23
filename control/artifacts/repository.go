// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package artifacts

import (
	"context"
	"sync"

	"go.mindclade.dev/libs/go/identifiers"
)

// MemoryCatalog is the reference implementation of Catalog: it defines the
// behaviour a durable catalog must reproduce, and is what a caller gets in a
// test that has no database. It is not a production store -- it has no
// eviction and no persistence, so a process restart loses every binding.
type MemoryCatalog struct {
	mu        sync.RWMutex
	refs      map[string]Ref
	locations map[string][]Location
}

var _ Catalog = (*MemoryCatalog)(nil)

func NewMemoryCatalog() *MemoryCatalog {
	return &MemoryCatalog{refs: map[string]Ref{}, locations: map[string][]Location{}}
}
func (c *MemoryCatalog) Put(_ context.Context, r Ref) error {
	if err := r.Validate(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.put(r)
}
func (c *MemoryCatalog) Get(_ context.Context, d identifiers.Digest) (Ref, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	r, ok := c.refs[d.String()]
	if !ok {
		return Ref{}, notRegistered()
	}
	return r, nil
}
func (c *MemoryCatalog) PutLocation(_ context.Context, l Location) error {
	if err := l.Validate(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	k := l.Artifact.Digest.String()
	if old, ok := c.refs[k]; !ok || !old.EqualIdentity(l.Artifact) {
		return locationUnknownIdentity()
	}
	return c.putLocation(k, l)
}
func (c *MemoryCatalog) Locations(_ context.Context, d identifiers.Digest) ([]Location, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v := append([]Location(nil), c.locations[d.String()]...)
	return v, nil
}

// Register binds the identity and every location under one mutex hold, which
// is this implementation's whole commit boundary: an observer either sees the
// registration or does not, never an identity with a missing replica.
func (c *MemoryCatalog) Register(_ context.Context, r Ref, locations []Location) error {
	if err := validateRegistration(r, locations); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	// Every rejection is decided against a snapshot of the durable state before
	// the first mutation, so a rejected Register leaves nothing behind. Mutating
	// as we validate would commit the identity and then fail on a location,
	// which is exactly the partial state this method exists to prevent.
	k := r.Digest.String()
	if old, ok := c.refs[k]; ok && !old.EqualIdentity(r) {
		return identityConflict()
	}
	// The batch is already bounded by MaximumLocationsPerArtifact, so this
	// allocation is bounded too. Duplicates within the batch and placements
	// already stored do not consume budget, which is what makes replaying an
	// identical Register succeed rather than exhaust it.
	pending := make([]Location, 0, len(locations))
	for _, l := range locations {
		if c.hasLocation(k, l) || containsPlacement(pending, l) {
			continue
		}
		pending = append(pending, l)
	}
	if len(c.locations[k])+len(pending) > MaximumLocationsPerArtifact {
		return locationBudgetExhausted()
	}
	if err := c.put(r); err != nil {
		return err
	}
	for _, l := range locations {
		if err := c.putLocation(k, l); err != nil {
			return err
		}
	}
	return nil
}

// put and putLocation assume c.mu is held and every domain rejection has
// already been decided.
func (c *MemoryCatalog) put(r Ref) error {
	k := r.Digest.String()
	if old, ok := c.refs[k]; ok && !old.EqualIdentity(r) {
		return identityConflict()
	}
	c.refs[k] = r
	return nil
}

func (c *MemoryCatalog) putLocation(k string, l Location) error {
	if c.hasLocation(k, l) {
		return nil
	}
	if len(c.locations[k]) >= MaximumLocationsPerArtifact {
		return locationBudgetExhausted()
	}
	c.locations[k] = append(c.locations[k], l)
	return nil
}

func (c *MemoryCatalog) hasLocation(k string, l Location) bool {
	return containsPlacement(c.locations[k], l)
}

func containsPlacement(set []Location, l Location) bool {
	for _, x := range set {
		if x.SamePlacement(l) {
			return true
		}
	}
	return false
}
