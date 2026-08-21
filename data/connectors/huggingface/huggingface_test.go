// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package huggingface

import (
	"context"
	"testing"
	"time"

	"go.mindclade.dev/data/connectors"
)

type fetcher struct{ snapshot connectors.Snapshot }

func (f fetcher) FetchSnapshot(context.Context, string) (connectors.Snapshot, error) {
	return f.snapshot, nil
}

func TestDiscoverRequiresSourceBoundImmutableRevision(t *testing.T) {
	snapshot := connectors.Snapshot{
		Source: "huggingface", Version: "commit-012345", Cursor: connectors.Cursor{Value: "commit-012345", Sequence: 1},
		Objects: []connectors.Object{{
			URI: "https://example.invalid/repo/resolve/commit-012345/data.parquet", Generation: "commit-012345",
			SizeBytes: 1, UpdatedAt: time.Unix(1, 0).UTC(),
		}},
		ObservedAt: time.Unix(2, 0).UTC(), LicenseRef: "approved",
	}
	client, err := NewClient(fetcher{snapshot})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Discover(t.Context(), "https://example.invalid/manifest"); err != nil {
		t.Fatal(err)
	}
	snapshot.Source = "other"
	client, _ = NewClient(fetcher{snapshot})
	if _, err := client.Discover(t.Context(), "https://example.invalid/manifest"); err == nil {
		t.Fatal("expected source mismatch")
	}
}
