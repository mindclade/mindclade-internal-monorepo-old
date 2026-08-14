// Copyright 2026 Mindclade. All rights reserved.
package artifacts

import (
	"context"
	"mindclade.internal/libs/go/identifiers"
	"sync"
)

type MemoryCatalog struct {
	mu        sync.RWMutex
	refs      map[string]Ref
	locations map[string][]Location
}

func NewMemoryCatalog() *MemoryCatalog {
	return &MemoryCatalog{refs: map[string]Ref{}, locations: map[string][]Location{}}
}
func (c *MemoryCatalog) Put(_ context.Context, r Ref) error {
	if err := r.Validate(); err != nil {
		return err
	}
	k := r.Digest.String()
	c.mu.Lock()
	defer c.mu.Unlock()
	if old, ok := c.refs[k]; ok && !old.EqualIdentity(r) {
		return invalid("artifact_identity_conflict", "digest is already registered with different immutable metadata", nil)
	}
	c.refs[k] = r
	return nil
}
func (c *MemoryCatalog) Get(_ context.Context, d identifiers.Digest) (Ref, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	r, ok := c.refs[d.String()]
	if !ok {
		return Ref{}, notFound("artifact_not_found", "artifact is not registered")
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
		return invalid("artifact_location_unknown_identity", "artifact identity must be registered before a location", nil)
	}
	for _, x := range c.locations[k] {
		if x.Provider == l.Provider && x.URI == l.URI && x.Generation == l.Generation {
			return nil
		}
	}
	c.locations[k] = append(c.locations[k], l)
	return nil
}
func (c *MemoryCatalog) Locations(_ context.Context, d identifiers.Digest) ([]Location, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v := append([]Location(nil), c.locations[d.String()]...)
	return v, nil
}
