// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package artifacts

import (
	"context"
	"go.mindclade.dev/libs/go/identifiers"
	"testing"
)

func TestArtifactIdentitySeparateFromLocation(t *testing.T) {
	r := Ref{Digest: identifiers.SHA256([]byte("x")), SizeBytes: 1, MediaType: "application/octet-stream", LogicalKind: "dataset-shard", SchemaVersion: 1}
	s := Service{Catalog: NewMemoryCatalog()}
	l := Location{Artifact: r, Provider: "gcs", URI: "gs://bucket/key", Generation: "1"}
	if err := s.Register(context.Background(), r, l); err != nil {
		t.Fatal(err)
	}
	got, err := s.Catalog.Get(context.Background(), r.Digest)
	if err != nil || !got.EqualIdentity(r) {
		t.Fatalf("got %#v err=%v", got, err)
	}
}

// The catalog binds a digest to immutable metadata permanently. A registration
// the service itself rejects must therefore leave nothing behind: committing the
// identity first and validating the locations afterwards poisoned the digest, so
// the corrected retry failed with an identity conflict against state the caller
// never asked to keep.
func TestRejectedRegistrationCommitsNothing(t *testing.T) {
	ctx := context.Background()
	catalog := NewMemoryCatalog()
	service := Service{Catalog: catalog}
	r := Ref{Digest: identifiers.SHA256([]byte("x")), SizeBytes: 1, MediaType: "application/octet-stream", LogicalKind: "dataset-shard", SchemaVersion: 1}

	mismatched := r
	mismatched.SizeBytes = 2
	if err := service.Register(ctx, r, Location{Artifact: mismatched, Provider: "gcs", URI: "gs://bucket/key", Generation: "1"}); err == nil {
		t.Fatal("expected the mismatched location identity to be rejected")
	}
	if err := service.Register(ctx, r, Location{Artifact: r, Provider: "gcs", URI: "gs://bucket/key"}); err == nil {
		t.Fatal("expected the incomplete location to be rejected")
	}
	if _, err := catalog.Get(ctx, r.Digest); err == nil {
		t.Fatal("a rejected registration durably committed the artifact identity anyway")
	}

	// The corrected registration still succeeds, which it could not do if the
	// rejected attempt had bound the digest.
	corrected := r
	corrected.SizeBytes = 2
	if err := service.Register(ctx, corrected, Location{Artifact: corrected, Provider: "gcs", URI: "gs://bucket/key", Generation: "1"}); err != nil {
		t.Fatal(err)
	}
	locations, err := catalog.Locations(ctx, corrected.Digest)
	if err != nil || len(locations) != 1 {
		t.Fatalf("locations=%#v err=%v", locations, err)
	}
}
