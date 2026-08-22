// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"go.mindclade.dev/control/registry/releases"
	"go.mindclade.dev/libs/go/faults"
)

var _ releases.Repository = (*Store)(nil)

// PutGraph stores an evidence graph.
//
// A graph is sealed by the digest the release quotes, so it is immutable:
// storing the same graph again is a no-op, and storing a different graph under
// a release identifier that already holds one is refused. The production
// composition joins this write and PromoteRelease in one serializable
// transaction, so a rejected promotion cannot leave a torn graph write.
func (store *Store) PutGraph(ctx context.Context, graph releases.EvidenceGraph) error {
	const operation = "registry.postgres.PutGraph"
	if err := store.validate(ctx, operation); err != nil {
		return err
	}
	// Digest validates the graph as its first step, so this rejects a
	// malformed graph and computes its identity in one call.
	digest, err := graph.Digest()
	if err != nil {
		return faults.Wrap(err, faults.CodeInvalidArgument, "evidence graph is not storable",
			faults.WithReason("evidence_graph_invalid"),
			faults.WithOperation(operation),
			faults.WithContextMetadata(ctx),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	document, err := json.Marshal(graph)
	if err != nil {
		return internal(ctx, err, operation, "evidence_graph_encoding_failed")
	}
	now := store.clock.Now().Round(0).UTC()

	query := fmt.Sprintf(`INSERT INTO %s (
release_id, graph_digest, subject_digest, policy_digest, policy_epoch,
document, written_at
) VALUES ($1,$2,$3,$4,$5,$6,$7)
ON CONFLICT (release_id) DO NOTHING`, store.graphs)
	result, err := store.executor(ctx).ExecContext(ctx, query,
		graph.ReleaseID, digest.String(), graph.SubjectDigest.String(),
		graph.PolicyDigest.String(), int64(graph.PolicyEpoch), document, now,
	)
	if err != nil {
		return provider(ctx, err, operation)
	}
	if affected, affectedErr := result.RowsAffected(); affectedErr == nil && affected == 1 {
		return nil
	}

	var stored string
	row := store.executor(ctx).QueryRowContext(ctx,
		fmt.Sprintf(`SELECT graph_digest FROM %s WHERE release_id=$1`, store.graphs),
		graph.ReleaseID,
	)
	if err := row.Scan(&stored); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return faults.Wrap(err, faults.CodeAborted, "evidence graph write raced a concurrent delete",
				faults.WithReason("evidence_graph_write_raced"),
				faults.WithOperation(operation),
				faults.WithField("release_id", graph.ReleaseID),
				faults.WithContextMetadata(ctx),
				faults.WithRetryPolicy(faults.BackoffRetry(3)),
			)
		}
		return provider(ctx, err, operation)
	}
	if stored == digest.String() {
		return nil
	}
	return faults.Wrap(ErrGraphImmutable, faults.CodeFailedPrecondition,
		"evidence graph for this release is already sealed with different content",
		faults.WithReason("evidence_graph_immutable"),
		faults.WithOperation(operation),
		faults.WithField("release_id", graph.ReleaseID),
		faults.WithContextMetadata(ctx),
		faults.WithRetryPolicy(faults.NoRetry()),
	)
}

// PutRelease stores a release under compare-and-swap on its ResourceVersion.
//
// The release carries its own optimistic-concurrency counter, so that is what
// this swaps on rather than layering libs/go/resourceversion beside it: two
// version fields on one record is two things to keep agreeing.
//
// A zero ResourceVersion means "this release does not exist yet" and inserts
// at version 1. Any other value updates the row only if the stored version
// still matches, bumping it. Both a lost swap and an insert onto an existing
// identifier surface as faults.CodeConflict, because from the caller's side
// they are the same event: someone else advanced this release first.
//
// The stored ResourceVersion is not reflected back to the caller. The domain
// Release is passed by value and Promote does not re-read it, so a returned
// version would have nowhere to go; a caller that needs the new version reads
// the release back.
func (store *Store) PutRelease(ctx context.Context, release releases.Release) error {
	const operation = "registry.postgres.PutRelease"
	if err := store.validate(ctx, operation); err != nil {
		return err
	}
	if release.ReleaseID == "" {
		return faults.Wrap(ErrReleaseConflict, faults.CodeInvalidArgument, "release identifier is required",
			faults.WithReason("release_identifier_required"),
			faults.WithOperation(operation),
			faults.WithContextMetadata(ctx),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	if !release.ModelBundleDigest.Valid() || !release.EvidenceGraphDigest.Valid() {
		return faults.Wrap(ErrReleaseConflict, faults.CodeInvalidArgument,
			"release subject and evidence graph digests are required",
			faults.WithReason("release_digests_required"),
			faults.WithOperation(operation),
			faults.WithField("release_id", release.ReleaseID),
			faults.WithContextMetadata(ctx),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	now := store.clock.Now().Round(0).UTC()

	if release.ResourceVersion == 0 {
		query := fmt.Sprintf(`INSERT INTO %s (
release_id, model_bundle_digest, evidence_graph_digest, channel, status,
resource_version, written_at
) VALUES ($1,$2,$3,$4,$5,1,$6)
ON CONFLICT (release_id) DO NOTHING`, store.releases)
		result, err := store.executor(ctx).ExecContext(ctx, query,
			release.ReleaseID, release.ModelBundleDigest.String(),
			release.EvidenceGraphDigest.String(), release.Channel, release.Status, now,
		)
		if err != nil {
			return provider(ctx, err, operation)
		}
		affected, err := result.RowsAffected()
		if err != nil || affected != 1 {
			return conflict(ctx, release.ReleaseID, 0, operation,
				"release already exists at a later resource version")
		}
		return nil
	}

	query := fmt.Sprintf(`UPDATE %s SET
model_bundle_digest=$2, evidence_graph_digest=$3, channel=$4, status=$5,
resource_version=resource_version+1, written_at=$6
WHERE release_id=$1 AND resource_version=$7`, store.releases)
	result, err := store.executor(ctx).ExecContext(ctx, query,
		release.ReleaseID, release.ModelBundleDigest.String(),
		release.EvidenceGraphDigest.String(), release.Channel, release.Status, now,
		int64(release.ResourceVersion),
	)
	if err != nil {
		return provider(ctx, err, operation)
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return conflict(ctx, release.ReleaseID, release.ResourceVersion, operation,
			"release was modified concurrently")
	}
	return nil
}

// PromoteRelease compare-and-swaps a durable qualified candidate to promoted.
// Unlike PutRelease, this operation has no insert path: callers cannot turn
// verified evidence into a newly invented release or skip the durable
// candidate/qualification lifecycle. The surrounding registry composition
// executes this update and PutGraph in one serializable transaction.
func (store *Store) PromoteRelease(ctx context.Context, release releases.Release) error {
	const operation = "registry.postgres.PromoteRelease"
	if err := store.validate(ctx, operation); err != nil {
		return err
	}
	if err := release.ValidateQualifiedCandidate(); err != nil {
		return faults.Wrap(err, faults.CodeInvalidArgument, "release is not promotable",
			faults.WithReason("release_not_qualified_candidate"),
			faults.WithOperation(operation),
			faults.WithField("release_id", release.ReleaseID),
			faults.WithContextMetadata(ctx),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	now := store.clock.Now().Round(0).UTC()
	query := fmt.Sprintf(`UPDATE %s SET
status='promoted', resource_version=resource_version+1, written_at=$4
WHERE release_id=$1
  AND model_bundle_digest=$2
  AND evidence_graph_digest=$3
  AND channel='candidate'
  AND status='qualified'
  AND resource_version=$5`, store.releases)
	result, err := store.executor(ctx).ExecContext(ctx, query,
		release.ReleaseID, release.ModelBundleDigest.String(),
		release.EvidenceGraphDigest.String(), now, int64(release.ResourceVersion),
	)
	if err != nil {
		return provider(ctx, err, operation)
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return conflict(ctx, release.ReleaseID, release.ResourceVersion, operation,
			"release is absent, stale, or no longer the qualified candidate admitted for promotion")
	}
	return nil
}

func conflict(ctx context.Context, releaseID string, version uint64, operation, message string) error {
	return faults.Wrap(ErrReleaseConflict, faults.CodeConflict, message,
		faults.WithReason("release_resource_version_conflict"),
		faults.WithOperation(operation),
		faults.WithField("release_id", releaseID),
		faults.WithField("resource_version", version),
		faults.WithContextMetadata(ctx),
		faults.WithRetryPolicy(faults.NoRetry()),
	)
}
