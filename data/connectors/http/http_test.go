// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package http

import (
	"encoding/json"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.mindclade.dev/data/connectors"
)

func TestFetchSnapshotEnforcesHostTLSAndBoundedSchema(t *testing.T) {
	snapshot := connectors.Snapshot{
		Source: "catalog", Version: "v1", Cursor: connectors.Cursor{Value: "cursor", Sequence: 1},
		Objects: []connectors.Object{{
			URI: "https://example.invalid/data/object", Generation: "etag-1", SizeBytes: 2,
			UpdatedAt: time.Unix(1, 0).UTC(),
		}},
		ObservedAt: time.Unix(2, 0).UTC(), LicenseRef: "approved",
	}
	server := httptest.NewTLSServer(nethttp.HandlerFunc(func(response nethttp.ResponseWriter, _ *nethttp.Request) {
		_ = json.NewEncoder(response).Encode(snapshot)
	}))
	defer server.Close()
	host := strings.TrimPrefix(server.URL, "https://")
	client, err := NewClient(server.Client(), []string{host}, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.FetchSnapshot(t.Context(), server.URL+"/snapshot")
	if err != nil {
		t.Fatal(err)
	}
	if result.Version != "v1" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if _, err := client.FetchSnapshot(t.Context(), "https://not-allowed.invalid/snapshot"); err == nil {
		t.Fatal("expected host allow-list rejection")
	}
}
