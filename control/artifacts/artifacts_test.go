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
