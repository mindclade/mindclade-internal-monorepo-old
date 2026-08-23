// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package reference_databases

import (
	"context"
	"go.mindclade.dev/control/artifacts"
	"go.mindclade.dev/libs/go/identifiers"
	"testing"
	"time"
)

func TestSealMakesSnapshotContentBound(t *testing.T) {
	id, _ := identifiers.NewID(identifiers.MustParseKind("refdb"))
	r := Release{ReleaseID: id.String(), Name: "uniref", Version: "2026-08", Kind: KindSequence, SourceCutoff: time.Now(), Shards: []artifacts.Ref{{Digest: identifiers.SHA256([]byte("s")), SizeBytes: 1, MediaType: "application/octet-stream", LogicalKind: "reference-shard", SchemaVersion: 1}}, IndexFormat: "mmseqs2", IndexTool: "mmseqs", IndexToolVersion: "1", SourceProvenanceDigest: identifiers.SHA256([]byte("p")), LicensePolicyDigest: identifiers.SHA256([]byte("l")), CompatibleSearchTools: []string{"mmseqs"}, Status: StatusQualified, Created: time.Now()}
	if err := r.Seal(); err != nil {
		t.Fatal(err)
	}
	old := r.SnapshotDigest
	r.IndexToolVersion = "2"
	if old.Equal(identifiers.SHA256([]byte(r.canonical(false)))) {
		t.Fatal("expected content change")
	}
}

// Status is one of the fields SnapshotDigest binds, so a lifecycle change is a
// reseal, not a field patch. These tests exist because a promotion that wrote
// the status straight through the storage seam left a durable record whose
// digest no longer matched its content: it failed Validate forever, could not be
// resolved, and could not be re-registered.
func TestPromoteKeepsThePromotedReleaseVerifiable(t *testing.T) {
	ctx := context.Background()
	repository := newMemoryRepository()
	service := Service{Repository: repository, Policy: PromotionPolicy{RequireQualified: true}}
	release := qualifiedRelease(t)
	if err := service.Register(ctx, release); err != nil {
		t.Fatal(err)
	}
	if err := service.Promote(ctx, release.ReleaseID); err != nil {
		t.Fatal(err)
	}
	stored, err := repository.Get(ctx, release.ReleaseID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != StatusProduction {
		t.Fatalf("release was not promoted: %q", stored.Status)
	}
	if err := stored.Validate(); err != nil {
		t.Fatalf("promoted release no longer verifies its own content seal: %v", err)
	}
}

// Re-promoting an already production release is a replay, not a second write.
func TestPromoteIsIdempotent(t *testing.T) {
	ctx := context.Background()
	repository := newMemoryRepository()
	service := Service{Repository: repository, Policy: PromotionPolicy{RequireQualified: true}}
	release := qualifiedRelease(t)
	if err := service.Register(ctx, release); err != nil {
		t.Fatal(err)
	}
	if err := service.Promote(ctx, release.ReleaseID); err != nil {
		t.Fatal(err)
	}
	first, err := repository.Get(ctx, release.ReleaseID)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Promote(ctx, release.ReleaseID); err != nil {
		t.Fatalf("replayed promotion must succeed: %v", err)
	}
	second, err := repository.Get(ctx, release.ReleaseID)
	if err != nil {
		t.Fatal(err)
	}
	if !first.SnapshotDigest.Equal(second.SnapshotDigest) || second.Status != StatusProduction {
		t.Fatalf("replayed promotion changed the durable record: %#v -> %#v", first, second)
	}
}

// Promotion is a read-modify-write. It must compare-and-swap the exact record it
// evaluated, so a concurrent lifecycle change is refused rather than silently
// overwritten by a decision made against a record that no longer exists.
func TestPromoteRefusesAStaleObservation(t *testing.T) {
	ctx := context.Background()
	repository := newMemoryRepository()
	release := qualifiedRelease(t)
	if err := (Service{Repository: repository}).Register(ctx, release); err != nil {
		t.Fatal(err)
	}
	racing := &racingRepository{memoryRepository: repository, onGet: func() {
		retired := release
		retired.Status = StatusRetired
		if err := retired.Seal(); err != nil {
			t.Error(err)
			return
		}
		repository.items[retired.ReleaseID] = retired
	}}
	service := Service{Repository: racing, Policy: PromotionPolicy{RequireQualified: true}}
	if err := service.Promote(ctx, release.ReleaseID); err == nil {
		t.Fatal("a promotion decided against a superseded record must not commit")
	}
	stored, err := repository.Get(ctx, release.ReleaseID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != StatusRetired {
		t.Fatalf("the concurrent retirement was silently lost: %q", stored.Status)
	}
}

// A store that hands back content its digest does not cover is corrupt. Promote
// fails closed on it instead of resealing the corruption under a new status.
func TestPromoteRefusesAnUnsealedStoredRelease(t *testing.T) {
	ctx := context.Background()
	repository := newMemoryRepository()
	release := qualifiedRelease(t)
	if err := (Service{Repository: repository}).Register(ctx, release); err != nil {
		t.Fatal(err)
	}
	corrupt := release
	corrupt.IndexToolVersion = "2"
	repository.items[corrupt.ReleaseID] = corrupt
	if err := (Service{Repository: repository, Policy: PromotionPolicy{RequireQualified: true}}).Promote(ctx, release.ReleaseID); err == nil {
		t.Fatal("expected a corrupt stored release to fail closed")
	}
}

// Registration is create-only. A retried registration job replays the body it
// was handed, which is the pre-promotion one: an upsert here would demote a
// production release back to qualified and restore its old snapshot digest,
// letting a promotion that was already superseded pass the compare afterwards.
func TestRegisterDoesNotOverwriteADurableRelease(t *testing.T) {
	ctx := context.Background()
	repository := newMemoryRepository()
	service := Service{Repository: repository, Policy: PromotionPolicy{RequireQualified: true}}
	release := qualifiedRelease(t)
	if err := service.Register(ctx, release); err != nil {
		t.Fatal(err)
	}
	if err := service.Promote(ctx, release.ReleaseID); err != nil {
		t.Fatal(err)
	}
	if err := service.Register(ctx, release); err == nil {
		t.Fatal("a replayed registration must not rewrite a durable release")
	}
	stored, err := repository.Get(ctx, release.ReleaseID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != StatusProduction {
		t.Fatalf("the promotion was silently demoted by a replayed registration: %q", stored.Status)
	}
}

// The lifecycle rule holds even under a zero-value PromotionPolicy, which
// permits everything. A retired release is not a promotion candidate, and a
// draft has never been qualified.
func TestPromoteRefusesANonPromotableStatus(t *testing.T) {
	ctx := context.Background()
	for _, status := range []Status{StatusDraft, StatusRetired} {
		repository := newMemoryRepository()
		release := qualifiedRelease(t)
		release.Status = status
		if err := release.Seal(); err != nil {
			t.Fatal(err)
		}
		if err := (Service{Repository: repository}).Register(ctx, release); err != nil {
			t.Fatal(err)
		}
		if err := (Service{Repository: repository}).Promote(ctx, release.ReleaseID); err == nil {
			t.Fatalf("a %q release must not reach production", status)
		}
		stored, err := repository.Get(ctx, release.ReleaseID)
		if err != nil {
			t.Fatal(err)
		}
		if stored.Status != status {
			t.Fatalf("status changed to %q", stored.Status)
		}
	}
}

func qualifiedRelease(t *testing.T) Release {
	t.Helper()
	id, err := identifiers.NewID(identifiers.MustParseKind("refdb"))
	if err != nil {
		t.Fatal(err)
	}
	created := time.UnixMilli(1800000000000).UTC()
	release := Release{
		ReleaseID: id.String(), Name: "uniref", Version: "2026-08", Kind: KindSequence, SourceCutoff: created,
		Shards:                 []artifacts.Ref{{Digest: identifiers.SHA256([]byte("shard")), SizeBytes: 1, MediaType: "application/octet-stream", LogicalKind: "reference-shard", SchemaVersion: 1}},
		IndexFormat:            "mmseqs2",
		IndexTool:              "mmseqs",
		IndexToolVersion:       "1",
		SourceProvenanceDigest: identifiers.SHA256([]byte("provenance")),
		LicensePolicyDigest:    identifiers.SHA256([]byte("license")),
		CompatibleSearchTools:  []string{"mmseqs"},
		Status:                 StatusQualified,
		Created:                created,
	}
	if err := release.Seal(); err != nil {
		t.Fatal(err)
	}
	return release
}

// memoryRepository is a faithful implementation of the storage seam: it inserts
// if absent, replays an identical body, and refuses a stale observation. It
// exists so the promotion path can be exercised without a provider, and is the
// worked example an adapter author is expected to follow.
type memoryRepository struct{ items map[string]Release }

func newMemoryRepository() *memoryRepository { return &memoryRepository{items: map[string]Release{}} }

func (m *memoryRepository) Put(_ context.Context, release Release) error {
	if current, exists := m.items[release.ReleaseID]; exists {
		if !current.SnapshotDigest.Equal(release.SnapshotDigest) {
			return conflict("reference_release_identity_conflict", "release id is already bound to different content")
		}
		return nil
	}
	m.items[release.ReleaseID] = release
	return nil
}

func (m *memoryRepository) Get(_ context.Context, id string) (Release, error) {
	release, exists := m.items[id]
	if !exists {
		return Release{}, invalid("reference_release_not_found", "reference release was not found", nil)
	}
	return release, nil
}

func (m *memoryRepository) CompareAndSwap(_ context.Context, updated Release, expected identifiers.Digest) error {
	current, exists := m.items[updated.ReleaseID]
	if !exists {
		return invalid("reference_release_not_found", "reference release was not found", nil)
	}
	if !current.SnapshotDigest.Equal(expected) {
		return conflict("reference_release_observation_stale", "reference release observation is stale")
	}
	m.items[updated.ReleaseID] = updated
	return nil
}

// racingRepository runs exactly one interleaving between the read a promotion
// decides on and the write it issues.
type racingRepository struct {
	*memoryRepository
	onGet func()
}

func (r *racingRepository) Get(ctx context.Context, id string) (Release, error) {
	release, err := r.memoryRepository.Get(ctx, id)
	if r.onGet != nil {
		interleave := r.onGet
		r.onGet = nil
		interleave()
	}
	return release, err
}
