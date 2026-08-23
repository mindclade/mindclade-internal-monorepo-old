// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	_ "github.com/lib/pq"

	"go.mindclade.dev/control/artifacts"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/storage/sql/transaction"
)

// These run against a real PostgreSQL server. A fake driver can only prove the
// shape of a query string; it cannot prove that ON CONFLICT, the identity CTE,
// the foreign key, or the budget guard behave the way the store believes. Every
// domain rule this package claims is asserted here against real SQL.

func liveArtifactRef(seed string) artifacts.Ref {
	return artifacts.Ref{
		Digest:        identifiers.SHA256String(seed),
		SizeBytes:     4096,
		MediaType:     "application/octet-stream",
		LogicalKind:   "dataset_shard",
		SchemaVersion: 1,
	}
}

func liveArtifactLocation(ref artifacts.Ref, uri string) artifacts.Location {
	return artifacts.Location{Artifact: ref, Provider: "gcs", URI: uri, Generation: "1", Region: "us-central1"}
}

func TestLivePostgresArtifactCatalogRoundTrip(t *testing.T) {
	store, _ := livePostgresStore(t)
	ctx := context.Background()
	ref := liveArtifactRef("round-trip")
	first := liveArtifactLocation(ref, "gs://bucket/a")
	second := liveArtifactLocation(ref, "gs://bucket/b")

	if err := store.Register(ctx, ref, []artifacts.Location{first, second}); err != nil {
		t.Fatal(err)
	}
	stored, err := store.Get(ctx, ref.Digest)
	if err != nil || !stored.EqualIdentity(ref) {
		t.Fatalf("stored=%#v err=%v", stored, err)
	}
	locations, err := store.Locations(ctx, ref.Digest)
	if err != nil || len(locations) != 2 {
		t.Fatalf("locations=%#v err=%v", locations, err)
	}
	for _, location := range locations {
		if !location.Artifact.EqualIdentity(ref) {
			t.Fatalf("a location came back bound to a different identity: %#v", location)
		}
	}

	// Replay is the normal case after a lost response: both writes are
	// content-addressed, so an identical registration must succeed and must not
	// duplicate a placement.
	if err := store.Register(ctx, ref, []artifacts.Location{first, second}); err != nil {
		t.Fatalf("replaying an identical registration was rejected: %v", err)
	}
	locations, err = store.Locations(ctx, ref.Digest)
	if err != nil || len(locations) != 2 {
		t.Fatalf("a replay duplicated placements: locations=%d err=%v", len(locations), err)
	}

	if _, err := store.Get(ctx, identifiers.SHA256String("never-registered")); !errors.Is(err, artifacts.ErrNotFound) {
		t.Fatalf("an unregistered digest resolved: %v", err)
	}
	empty, err := store.Locations(ctx, identifiers.SHA256String("never-registered"))
	if err != nil || len(empty) != 0 {
		t.Fatalf("locations=%#v err=%v", empty, err)
	}
}

// The digest/metadata binding is permanent. This is the property the whole
// package exists to hold, and it is asserted here against a real unique index
// rather than a map.
func TestLivePostgresArtifactIdentityBindingIsImmutable(t *testing.T) {
	store, db := livePostgresStore(t)
	ctx := context.Background()
	ref := liveArtifactRef("immutable")
	if err := store.Register(ctx, ref, []artifacts.Location{liveArtifactLocation(ref, "gs://bucket/original")}); err != nil {
		t.Fatal(err)
	}

	for name, mutate := range map[string]func(*artifacts.Ref){
		"size":           func(r *artifacts.Ref) { r.SizeBytes = 8192 },
		"media_type":     func(r *artifacts.Ref) { r.MediaType = "application/json" },
		"logical_kind":   func(r *artifacts.Ref) { r.LogicalKind = "model_bundle" },
		"schema_version": func(r *artifacts.Ref) { r.SchemaVersion = 2 },
	} {
		t.Run(name, func(t *testing.T) {
			conflicting := ref
			mutate(&conflicting)

			err := store.Put(ctx, conflicting)
			if !errors.Is(err, artifacts.ErrIdentityConflict) {
				t.Fatalf("Put rebound the digest: %v", err)
			}
			if reason := faults.ReasonOf(err); reason != artifacts.ReasonIdentityConflict {
				t.Fatalf("reason=%s want %s", reason, artifacts.ReasonIdentityConflict)
			}

			// The same rejection through Register, and -- the part a
			// non-atomic implementation gets wrong -- the location that came
			// with the rejected registration must not have been written.
			intruder := liveArtifactLocation(conflicting, "gs://bucket/intruder")
			err = store.Register(ctx, conflicting, []artifacts.Location{intruder})
			if !errors.Is(err, artifacts.ErrIdentityConflict) {
				t.Fatalf("Register rebound the digest: %v", err)
			}

			locations, listErr := store.Locations(ctx, ref.Digest)
			if listErr != nil {
				t.Fatal(listErr)
			}
			if len(locations) != 1 || locations[0].URI != "gs://bucket/original" {
				t.Fatalf("a rejected registration wrote its location anyway: %#v", locations)
			}
			var orphans int
			if err := db.QueryRowContext(ctx,
				"SELECT count(*) FROM "+store.ArtifactLocationTable()+" WHERE uri='gs://bucket/intruder'",
			).Scan(&orphans); err != nil {
				t.Fatal(err)
			}
			if orphans != 0 {
				t.Fatalf("%d orphaned location rows survived a rejected registration", orphans)
			}

			// The original binding is intact and still readable.
			stored, getErr := store.Get(ctx, ref.Digest)
			if getErr != nil || !stored.EqualIdentity(ref) {
				t.Fatalf("stored=%#v err=%v", stored, getErr)
			}
		})
	}
}

// A location may not exist without a matching identity. The store guards the
// insert on the immutable columns and the schema carries a foreign key; this
// asserts both, including the case the key alone would let through.
func TestLivePostgresArtifactLocationRequiresItsIdentity(t *testing.T) {
	store, db := livePostgresStore(t)
	ctx := context.Background()
	ref := liveArtifactRef("location-identity")

	err := store.PutLocation(ctx, liveArtifactLocation(ref, "gs://bucket/early"))
	if !errors.Is(err, artifacts.ErrLocationUnknownIdentity) {
		t.Fatalf("a location was written before its identity: %v", err)
	}
	if reason := faults.ReasonOf(err); reason != artifacts.ReasonLocationUnknownIdentity {
		t.Fatalf("reason=%s", reason)
	}

	if err := store.Put(ctx, ref); err != nil {
		t.Fatal(err)
	}
	// The digest row now exists, so a foreign key alone would accept this. The
	// immutable-column guard is what rejects it.
	drifted := ref
	drifted.SizeBytes = 1
	err = store.PutLocation(ctx, liveArtifactLocation(drifted, "gs://bucket/drifted"))
	if !errors.Is(err, artifacts.ErrLocationUnknownIdentity) {
		t.Fatalf("a location attached to a differing identity: %v", err)
	}

	var rows int
	if err := db.QueryRowContext(ctx,
		"SELECT count(*) FROM "+store.ArtifactLocationTable()+" WHERE digest=$1", ref.Digest.String(),
	).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("%d location rows were written without a matching identity", rows)
	}

	matching := liveArtifactLocation(ref, "gs://bucket/matching")
	if err := store.PutLocation(ctx, matching); err != nil {
		t.Fatal(err)
	}
	if err := store.PutLocation(ctx, matching); err != nil {
		t.Fatalf("replaying an identical placement was rejected: %v", err)
	}
	locations, err := store.Locations(ctx, ref.Digest)
	if err != nil || len(locations) != 1 {
		t.Fatalf("locations=%#v err=%v", locations, err)
	}
}

// Register is one statement, so it is atomic on its own. It must also compose
// into a caller's commit: a repository that opened its own transaction would
// commit the registration even when the surrounding unit of work rolled back.
func TestLivePostgresArtifactRegisterJoinsTheCallersTransaction(t *testing.T) {
	store, db := livePostgresStore(t)
	ctx := context.Background()
	ref := liveArtifactRef("rollback")
	sentinel := errors.New("injected failure after the artifact write")

	err := transaction.RunVoid(ctx, db, transaction.Options{Isolation: sql.LevelReadCommitted},
		func(txContext context.Context, _ *sql.Tx) error {
			if registerErr := store.Register(txContext, ref, []artifacts.Location{liveArtifactLocation(ref, "gs://bucket/rolled-back")}); registerErr != nil {
				return registerErr
			}
			// The registration is visible inside the transaction, which is what
			// makes the rollback below a real test rather than a no-op.
			stored, getErr := store.Get(txContext, ref.Digest)
			if getErr != nil || !stored.EqualIdentity(ref) {
				t.Errorf("the registration was not visible inside its own transaction: %#v %v", stored, getErr)
			}
			return sentinel
		})
	if !errors.Is(err, sentinel) {
		t.Fatalf("err=%v", err)
	}

	if _, err := store.Get(ctx, ref.Digest); !errors.Is(err, artifacts.ErrNotFound) {
		t.Fatalf("a rolled-back registration committed the artifact identity anyway: %v", err)
	}
	locations, err := store.Locations(ctx, ref.Digest)
	if err != nil || len(locations) != 0 {
		t.Fatalf("a rolled-back registration committed %d locations: %v", len(locations), err)
	}
}

// The catalog has no Delete and no eviction, so a placement set that grew
// without limit would be a durable leak. The guard lives in the statement, so
// this asserts what PostgreSQL actually enforced, not what the store intended.
func TestLivePostgresArtifactLocationSetIsBounded(t *testing.T) {
	store, db := livePostgresStore(t)
	ctx := context.Background()
	ref := liveArtifactRef("bounded")
	if err := store.Put(ctx, ref); err != nil {
		t.Fatal(err)
	}
	for index := range artifacts.MaximumLocationsPerArtifact {
		if err := store.PutLocation(ctx, liveArtifactLocation(ref, uriForIndex(index))); err != nil {
			t.Fatalf("placement %d was rejected below the bound: %v", index, err)
		}
	}
	// A stored placement consumes no further budget, so a replay at the cap
	// must still succeed.
	if err := store.PutLocation(ctx, liveArtifactLocation(ref, uriForIndex(0))); err != nil {
		t.Fatalf("replaying a stored placement at the cap was rejected: %v", err)
	}

	err := store.PutLocation(ctx, liveArtifactLocation(ref, uriForIndex(artifacts.MaximumLocationsPerArtifact)))
	if !errors.Is(err, artifacts.ErrLocationBudget) {
		t.Fatalf("the placement set grew past its bound: %v", err)
	}
	if code := faults.CodeOf(err); code != faults.CodeResourceExhausted {
		t.Fatalf("code=%v", code)
	}

	// Register must respect the same durable budget, and must land nothing when
	// it cannot land everything.
	overflow := liveArtifactRef("bounded")
	err = store.Register(ctx, overflow, []artifacts.Location{
		liveArtifactLocation(overflow, uriForIndex(artifacts.MaximumLocationsPerArtifact+1)),
	})
	if !errors.Is(err, artifacts.ErrLocationBudget) {
		t.Fatalf("Register grew the placement set past its bound: %v", err)
	}

	var rows int
	if err := db.QueryRowContext(ctx,
		"SELECT count(*) FROM "+store.ArtifactLocationTable()+" WHERE digest=$1", ref.Digest.String(),
	).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != artifacts.MaximumLocationsPerArtifact {
		t.Fatalf("the durable placement set holds %d rows, want %d", rows, artifacts.MaximumLocationsPerArtifact)
	}
	locations, err := store.Locations(ctx, ref.Digest)
	if err != nil || len(locations) != artifacts.MaximumLocationsPerArtifact {
		t.Fatalf("locations=%d err=%v", len(locations), err)
	}
}

func uriForIndex(index int) string {
	return "gs://bucket/object-" + string(rune('a'+index/26)) + string(rune('a'+index%26))
}

// The durable store and the in-memory reference must be interchangeable: a
// caller decides what to do from the reason, and cannot see which one answered.
func TestLivePostgresArtifactCatalogMatchesTheMemoryReference(t *testing.T) {
	store, _ := livePostgresStore(t)
	ctx := context.Background()
	memory := artifacts.NewMemoryCatalog()

	ref := liveArtifactRef("conformance")
	conflicting := ref
	conflicting.SizeBytes = 8192
	unregistered := liveArtifactRef("conformance-absent")

	for _, catalog := range []artifacts.Catalog{store, memory} {
		if err := catalog.Register(ctx, ref, []artifacts.Location{liveArtifactLocation(ref, "gs://bucket/one")}); err != nil {
			t.Fatalf("%T: %v", catalog, err)
		}
		assertReason(t, catalog, "identity conflict",
			catalog.Register(ctx, conflicting, nil), artifacts.ErrIdentityConflict, artifacts.ReasonIdentityConflict)
		assertReason(t, catalog, "unknown identity",
			catalog.PutLocation(ctx, liveArtifactLocation(unregistered, "gs://bucket/absent")),
			artifacts.ErrLocationUnknownIdentity, artifacts.ReasonLocationUnknownIdentity)
		_, err := catalog.Get(ctx, unregistered.Digest)
		assertReason(t, catalog, "not found", err, artifacts.ErrNotFound, artifacts.ReasonNotFound)
	}
}

func assertReason(t *testing.T, catalog artifacts.Catalog, name string, err error, sentinel error, reason string) {
	t.Helper()
	if !errors.Is(err, sentinel) {
		t.Fatalf("%T %s: err=%v does not carry %v", catalog, name, err, sentinel)
	}
	if got := faults.ReasonOf(err); got != reason {
		t.Fatalf("%T %s: reason=%s want %s", catalog, name, got, reason)
	}
}
