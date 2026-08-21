// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package s3

import (
	"context"
	"testing"
	"time"

	"go.mindclade.dev/data/connectors"
)

type fakeLister struct{}

func (fakeLister) ListVersions(_ context.Context, _, _, _ string, _ int) ([]ObjectVersion, string, error) {
	return []ObjectVersion{{Key: "object", VersionID: "version-1", Size: 4, Updated: time.Unix(1, 0).UTC()}}, "", nil
}

func TestDiscoverRequiresImmutableObjectVersion(t *testing.T) {
	client, err := NewClient(fakeLister{})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := client.Discover(
		context.Background(), "bucket", "prefix", "v1", "internal",
		connectors.Cursor{Value: "cursor", Sequence: 1}, time.Unix(2, 0).UTC(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Objects[0].Generation != "version-1" {
		t.Fatalf("version not retained: %#v", snapshot.Objects[0])
	}
}
