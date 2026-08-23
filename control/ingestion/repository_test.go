// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package ingestion

import (
	"context"
	"testing"
	"time"

	"go.mindclade.dev/control/artifacts"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
)

func snapshotOf(t *testing.T, body string) Snapshot {
	t.Helper()
	return Snapshot{
		SnapshotID: id(t, "snapshot"),
		Source: SourceSpec{
			SourceID: id(t, "source"), Kind: SourcePDB, Locator: "https://files.rcsb.org/pub",
			Version: "2026-08-01", License: "cc0",
		},
		RawArtifact:  artifacts.Ref{Digest: identifiers.SHA256([]byte(body)), SizeBytes: uint64(len(body)), MediaType: "application/octet-stream", LogicalKind: "raw_source", SchemaVersion: 1},
		SourceCursor: "2026-08-01T00:00:00Z",
		ObservedAt:   time.UnixMilli(1800000000000).UTC(),
	}
}

// A SnapshotID is the durable provenance link to exactly one observation. A
// second Put under that identifier is either a replay of the same fact or a
// rebind, and a rebind must not be accepted: overwriting it destroys the only
// record of which bytes the run that quoted this snapshot actually read, and
// leaves no error behind to find the divergence with.
func TestSnapshotPutRefusesRebindingAnIdentifier(t *testing.T) {
	ctx := context.Background()
	repository := NewMemorySnapshotRepository(DefaultMaximumSnapshots)
	first := snapshotOf(t, "raw-a")
	if err := repository.Put(ctx, first); err != nil {
		t.Fatal(err)
	}

	replay := first
	replay.ObservedAt = first.ObservedAt.Add(time.Minute)
	if err := repository.Put(ctx, replay); err != nil {
		t.Fatalf("a replayed observation must be idempotent: %v", err)
	}

	for name, rebind := range map[string]func(Snapshot) Snapshot{
		"raw artifact": func(s Snapshot) Snapshot {
			s.RawArtifact = snapshotOf(t, "raw-b").RawArtifact
			return s
		},
		"source cursor":  func(s Snapshot) Snapshot { s.SourceCursor = "2026-09-01T00:00:00Z"; return s },
		"source version": func(s Snapshot) Snapshot { s.Source.Version = "2026-09-01"; return s },
	} {
		candidate := rebind(first)
		err := repository.Put(ctx, candidate)
		if !faults.IsReason(err, "snapshot_identity_conflict") {
			t.Fatalf("rebinding the %s was accepted: %v", name, err)
		}
	}

	stored, found, err := repository.Get(ctx, first.SnapshotID)
	if err != nil || !found {
		t.Fatalf("stored=%v found=%v err=%v", stored, found, err)
	}
	if !stored.SameObservation(first) || !stored.ObservedAt.Equal(first.ObservedAt) {
		t.Fatalf("the durable snapshot was silently rebound: %#v", stored)
	}
}

// The store is a spool that grows with the source. An unbounded one in a
// long-lived ingestion coordinator is an outage waiting for a large upstream
// window, so it fails closed at its bound instead.
func TestSnapshotStoreIsBounded(t *testing.T) {
	ctx := context.Background()
	repository := NewMemorySnapshotRepository(2)
	for index := range 2 {
		if err := repository.Put(ctx, snapshotOf(t, string(rune('a'+index)))); err != nil {
			t.Fatal(err)
		}
	}
	if err := repository.Put(ctx, snapshotOf(t, "overflow")); !faults.IsReason(err, "snapshot_store_bound") {
		t.Fatalf("expected the store bound to be enforced, got %v", err)
	}
}
