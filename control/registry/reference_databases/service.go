// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package reference_databases

import (
	"context"

	"go.mindclade.dev/control/artifacts"
	"go.mindclade.dev/libs/go/identifiers"
)

type Repository interface {
	// Put creates a release and is insert-if-absent on ReleaseID: a replay of
	// the identical sealed body is a no-op, and a different body under an
	// existing ReleaseID is refused with a CodeConflict fault.
	//
	// It is deliberately not an upsert. A retried registration job carries the
	// body it was handed, which is a pre-promotion one; letting it overwrite
	// would silently demote a production release back to qualified and restore
	// its old snapshot digest, so a superseded promotion still holding that
	// digest would then pass the compare below.
	Put(context.Context, Release) error
	Get(context.Context, string) (Release, error)
	// CompareAndSwap replaces the durable release named by updated.ReleaseID only
	// while its stored SnapshotDigest still equals expected, and must refuse the
	// write with a CodeConflict fault otherwise. It has no insert path.
	//
	// It takes an already-resealed release rather than a field to patch because
	// SnapshotDigest binds Status: a storage adapter that set the status column
	// in place would leave a record whose digest no longer covers its content,
	// which fails Release.Validate forever and can neither be resolved nor
	// re-registered. Resealing is domain sealing, so it stays on this side of the
	// seam. The digest doubles as the optimistic-concurrency token for the same
	// reason — it changes on every lifecycle write, so a promotion decided
	// against a superseded record cannot commit.
	CompareAndSwap(ctx context.Context, updated Release, expected identifiers.Digest) error
}
type Service struct {
	Repository Repository
	Policy     PromotionPolicy
}

func (s Service) Register(ctx context.Context, r Release) error {
	if s.Repository == nil {
		return invalid("reference_repository_unavailable", "reference database repository is unavailable", nil)
	}
	if err := r.Validate(); err != nil {
		return err
	}
	return s.Repository.Put(ctx, r)
}

// Promote moves an already durable release to production. It is a
// read-modify-write, so it re-verifies the seal it read, reseals the status it
// writes, and compare-and-swaps against the exact record the promotion policy
// was evaluated on.
func (s Service) Promote(ctx context.Context, id string) error {
	if s.Repository == nil {
		return invalid("reference_repository_unavailable", "reference database repository is unavailable", nil)
	}
	current, err := s.Repository.Get(ctx, id)
	if err != nil {
		return err
	}
	// A store that hands back content its digest does not cover is corrupt. Fail
	// closed rather than resealing the corruption under a production status.
	if err = current.Validate(); err != nil {
		return err
	}
	if current.ReleaseID != id {
		return invalid("reference_release_identity_mismatch", "reference database repository returned a different release", nil)
	}
	if current.Status == StatusProduction {
		return nil
	}
	// The lifecycle rule is checked before the configurable policy, because a
	// zero-value PromotionPolicy permits everything and must not be able to
	// resurrect a retired release or ship a never-qualified draft.
	if !current.Status.PromotableToProduction() {
		return conflict("reference_release_status_not_promotable", "reference release status cannot enter production")
	}
	if err = s.Policy.Allows(current); err != nil {
		return err
	}
	observed := current.SnapshotDigest
	promoted := current
	// Seal sorts in place, so the promoted copy must not share backing arrays
	// with the record the caller's repository still holds.
	promoted.Shards = append([]artifacts.Ref(nil), current.Shards...)
	promoted.CompatibleSearchTools = append([]string(nil), current.CompatibleSearchTools...)
	promoted.Status = StatusProduction
	if err = promoted.Seal(); err != nil {
		return err
	}
	return s.Repository.CompareAndSwap(ctx, promoted, observed)
}
