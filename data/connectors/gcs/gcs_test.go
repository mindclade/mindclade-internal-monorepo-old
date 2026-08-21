// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package gcs

import (
	"context"
	"testing"
	"time"

	"go.mindclade.dev/data/connectors"
)

type fakeLister struct{ calls int }

func (f *fakeLister) List(_ context.Context, _, _, token string, _ int) ([]ObjectAttrs, string, error) {
	f.calls++
	if token == "" {
		return []ObjectAttrs{{Name: "z", Generation: 2, Size: 2, Updated: time.Unix(2, 0).UTC()}}, "next", nil
	}
	return []ObjectAttrs{{Name: "a", Generation: 1, Size: 1, Updated: time.Unix(1, 0).UTC()}}, "", nil
}

func TestDiscoverBuildsSortedGenerationBoundSnapshot(t *testing.T) {
	lister := &fakeLister{}
	client, err := NewClient(lister)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := client.Discover(
		context.Background(), "bucket", "prefix", "v1", "internal",
		connectors.Cursor{Value: "cursor", Sequence: 1}, time.Unix(3, 0).UTC(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if lister.calls != 2 || snapshot.Objects[0].URI != "gs://bucket/a" || snapshot.Objects[0].Generation != "1" {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}
	if _, err := snapshot.Digest(); err != nil {
		t.Fatal(err)
	}
}
