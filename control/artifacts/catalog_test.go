// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package artifacts

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
)

func testRef() Ref {
	return Ref{
		Digest:        identifiers.SHA256([]byte("artifact")),
		SizeBytes:     4096,
		MediaType:     "application/octet-stream",
		LogicalKind:   "dataset_shard",
		SchemaVersion: 1,
	}
}

func testLocation(ref Ref, index int) Location {
	return Location{
		Artifact:   ref,
		Provider:   "gcs",
		URI:        fmt.Sprintf("gs://bucket/object-%d", index),
		Generation: "1",
		Region:     "us-central1",
	}
}

// The digest/metadata binding is permanent. Re-registering a digest with
// different immutable metadata is the one rejection every Catalog
// implementation must make identically, because a caller decides what to do
// from the reason and cannot see which implementation answered.
func TestIdentityBindingIsPermanentAndImmutable(t *testing.T) {
	ctx := context.Background()
	catalog := NewMemoryCatalog()
	ref := testRef()
	if err := catalog.Register(ctx, ref, []Location{testLocation(ref, 0)}); err != nil {
		t.Fatal(err)
	}

	// Replaying the identical registration must succeed: the write is
	// content-addressed, so a retry after a lost response is the normal case.
	if err := catalog.Register(ctx, ref, []Location{testLocation(ref, 0)}); err != nil {
		t.Fatalf("replaying an identical registration was rejected: %v", err)
	}

	for name, mutate := range map[string]func(*Ref){
		"size":           func(r *Ref) { r.SizeBytes = 8192 },
		"media_type":     func(r *Ref) { r.MediaType = "application/json" },
		"logical_kind":   func(r *Ref) { r.LogicalKind = "model_bundle" },
		"schema_version": func(r *Ref) { r.SchemaVersion = 2 },
	} {
		t.Run(name, func(t *testing.T) {
			conflicting := ref
			mutate(&conflicting)
			err := catalog.Register(ctx, conflicting, nil)
			if err == nil {
				t.Fatal("a digest was rebound to different immutable metadata")
			}
			if !errors.Is(err, ErrIdentityConflict) {
				t.Fatalf("err=%v does not carry ErrIdentityConflict", err)
			}
			if reason := faults.ReasonOf(err); reason != ReasonIdentityConflict {
				t.Fatalf("reason=%s want %s", reason, ReasonIdentityConflict)
			}
			if err := (Service{Catalog: catalog}).Register(ctx, conflicting); !errors.Is(err, ErrIdentityConflict) {
				t.Fatalf("the service did not surface the conflict: %v", err)
			}
			// The original binding survives the rejected rebind.
			stored, getErr := catalog.Get(ctx, ref.Digest)
			if getErr != nil || !stored.EqualIdentity(ref) {
				t.Fatalf("stored=%#v err=%v", stored, getErr)
			}
		})
	}
}

// recordingCatalog fails every decomposed write. Service.Register must reach
// the catalog exactly once: the defect this replaces was a Put followed by N
// PutLocation calls with no commit boundary between them, and a caller cannot
// tell that window is gone by reading the result of a successful call.
type recordingCatalog struct {
	t          *testing.T
	inner      *MemoryCatalog
	registered int
}

func (c *recordingCatalog) Put(context.Context, Ref) error {
	c.t.Fatal("Register decomposed into a separate identity write; the crash window is back")
	return nil
}

func (c *recordingCatalog) PutLocation(context.Context, Location) error {
	c.t.Fatal("Register decomposed into a separate location write; the crash window is back")
	return nil
}

func (c *recordingCatalog) Get(ctx context.Context, d identifiers.Digest) (Ref, error) {
	return c.inner.Get(ctx, d)
}

func (c *recordingCatalog) Locations(ctx context.Context, d identifiers.Digest) ([]Location, error) {
	return c.inner.Locations(ctx, d)
}

func (c *recordingCatalog) Register(ctx context.Context, r Ref, locations []Location) error {
	c.registered++
	return c.inner.Register(ctx, r, locations)
}

func TestRegisterIsOneWriteAtTheCatalogSeam(t *testing.T) {
	ctx := context.Background()
	ref := testRef()
	catalog := &recordingCatalog{t: t, inner: NewMemoryCatalog()}
	service := Service{Catalog: catalog}
	if err := service.Register(ctx, ref, testLocation(ref, 0), testLocation(ref, 1)); err != nil {
		t.Fatal(err)
	}
	if catalog.registered != 1 {
		t.Fatalf("Register reached the catalog %d times, want 1", catalog.registered)
	}
	locations, err := catalog.Locations(ctx, ref.Digest)
	if err != nil || len(locations) != 2 {
		t.Fatalf("locations=%#v err=%v", locations, err)
	}
}

// A rejected registration must leave nothing behind. The identity write used to
// land before the locations were validated, which poisoned the digest
// permanently and made the corrected retry fail against state nobody asked to
// keep.
func TestRejectedRegistrationLeavesNoIdentityBehind(t *testing.T) {
	ctx := context.Background()
	catalog := NewMemoryCatalog()
	ref := testRef()

	mismatched := ref
	mismatched.SizeBytes = 2
	rejections := []struct {
		name      string
		locations []Location
	}{
		{"identity_mismatch", []Location{{Artifact: mismatched, Provider: "gcs", URI: "gs://bucket/key", Generation: "1"}}},
		{"incomplete_location", []Location{{Artifact: ref, Provider: "gcs", URI: "gs://bucket/key"}}},
		{"budget", oversizedLocations(ref)},
	}
	for _, rejection := range rejections {
		if err := catalog.Register(ctx, ref, rejection.locations); err == nil {
			t.Fatalf("%s was accepted", rejection.name)
		}
		if _, err := catalog.Get(ctx, ref.Digest); !errors.Is(err, ErrNotFound) {
			t.Fatalf("%s durably committed the artifact identity anyway: %v", rejection.name, err)
		}
	}

	// The corrected registration still succeeds, which it could not do if a
	// rejected attempt had bound the digest.
	corrected := ref
	corrected.SizeBytes = 2
	if err := catalog.Register(ctx, corrected, []Location{testLocation(corrected, 0)}); err != nil {
		t.Fatal(err)
	}
}

func oversizedLocations(ref Ref) []Location {
	locations := make([]Location, 0, MaximumLocationsPerArtifact+1)
	for index := range MaximumLocationsPerArtifact + 1 {
		locations = append(locations, testLocation(ref, index))
	}
	return locations
}

// The catalog has no Delete and no eviction, so an unbounded location set is a
// durable leak rather than a transient overload.
func TestLocationSetIsBounded(t *testing.T) {
	ctx := context.Background()
	ref := testRef()

	t.Run("batch", func(t *testing.T) {
		catalog := NewMemoryCatalog()
		err := catalog.Register(ctx, ref, oversizedLocations(ref))
		if !errors.Is(err, ErrLocationBudget) {
			t.Fatalf("an oversized batch was accepted: %v", err)
		}
		if reason := faults.ReasonOf(err); reason != ReasonLocationBudget {
			t.Fatalf("reason=%s", reason)
		}
		if err := (Service{Catalog: catalog}).Register(ctx, ref, oversizedLocations(ref)...); !errors.Is(err, ErrLocationBudget) {
			t.Fatalf("the service accepted an oversized batch: %v", err)
		}
	})

	t.Run("incremental", func(t *testing.T) {
		catalog := NewMemoryCatalog()
		if err := catalog.Put(ctx, ref); err != nil {
			t.Fatal(err)
		}
		for index := range MaximumLocationsPerArtifact {
			if err := catalog.PutLocation(ctx, testLocation(ref, index)); err != nil {
				t.Fatalf("location %d was rejected below the bound: %v", index, err)
			}
		}
		// Replaying a stored placement must stay free: it consumes no budget.
		if err := catalog.PutLocation(ctx, testLocation(ref, 0)); err != nil {
			t.Fatalf("replaying a stored placement was rejected: %v", err)
		}
		err := catalog.PutLocation(ctx, testLocation(ref, MaximumLocationsPerArtifact))
		if !errors.Is(err, ErrLocationBudget) {
			t.Fatalf("the location set grew past its bound: %v", err)
		}
		locations, err := catalog.Locations(ctx, ref.Digest)
		if err != nil || len(locations) != MaximumLocationsPerArtifact {
			t.Fatalf("locations=%d err=%v", len(locations), err)
		}
	})
}

// A location may not exist without a matching identity. The digest alone is not
// enough: an identity bound to different metadata is a different artifact.
func TestLocationRequiresAMatchingRegisteredIdentity(t *testing.T) {
	ctx := context.Background()
	catalog := NewMemoryCatalog()
	ref := testRef()

	err := catalog.PutLocation(ctx, testLocation(ref, 0))
	if !errors.Is(err, ErrLocationUnknownIdentity) {
		t.Fatalf("a location was written before its identity: %v", err)
	}
	if reason := faults.ReasonOf(err); reason != ReasonLocationUnknownIdentity {
		t.Fatalf("reason=%s", reason)
	}

	if err := catalog.Put(ctx, ref); err != nil {
		t.Fatal(err)
	}
	drifted := ref
	drifted.SizeBytes = 1
	if err := catalog.PutLocation(ctx, testLocation(drifted, 0)); !errors.Is(err, ErrLocationUnknownIdentity) {
		t.Fatalf("a location attached to a differing identity: %v", err)
	}
}

func TestRegisterRequiresACatalog(t *testing.T) {
	if err := (Service{}).Register(context.Background(), testRef()); err == nil {
		t.Fatal("a registration without a catalog was accepted")
	}
}
