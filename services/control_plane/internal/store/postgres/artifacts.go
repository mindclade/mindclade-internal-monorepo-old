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
	"math"
	"strings"

	"go.mindclade.dev/control/artifacts"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
)

var _ artifacts.Catalog = (*Store)(nil)

// artifactRegisterFixedArguments is the count of placeholders the register
// statement uses before the first location. Locations occupy four each after
// it, so this constant and the builder below must move together.
const artifactRegisterFixedArguments = 8

func (store *Store) Put(ctx context.Context, ref artifacts.Ref) error {
	const operation = "registry.postgres.PutArtifact"
	if err := store.validate(ctx, operation); err != nil {
		return err
	}
	if err := store.validArtifact(ctx, ref, operation); err != nil {
		return err
	}
	return store.putArtifactIdentity(ctx, ref, operation)
}

func (store *Store) Get(ctx context.Context, digest identifiers.Digest) (artifacts.Ref, error) {
	const operation = "registry.postgres.GetArtifact"
	if err := store.validate(ctx, operation); err != nil {
		return artifacts.Ref{}, err
	}
	if !digest.Valid() {
		return artifacts.Ref{}, invalidArtifact(ctx, artifacts.ErrNotFound, operation, "artifact_digest_required")
	}
	var document []byte
	err := store.executor(ctx).QueryRowContext(ctx,
		fmt.Sprintf(`SELECT document FROM %s WHERE digest=$1`, store.artifactIdentities), digest.String(),
	).Scan(&document)
	if errors.Is(err, sql.ErrNoRows) {
		return artifacts.Ref{}, artifactMissing(ctx, operation, digest)
	}
	if err != nil {
		return artifacts.Ref{}, provider(ctx, err, operation)
	}
	var ref artifacts.Ref
	if err := json.Unmarshal(document, &ref); err != nil {
		return artifacts.Ref{}, internal(ctx, err, operation, "artifact_decoding_failed")
	}
	// The document is the authority and the projected columns are derived, so a
	// drifted projection can only skew a query, never a returned Ref. What a
	// caller cannot tolerate is a row filed under the wrong key: it would answer
	// a lookup with somebody else's artifact.
	if err := ref.Validate(); err != nil {
		return artifacts.Ref{}, internal(ctx, err, operation, "stored_artifact_invalid")
	}
	if !ref.Digest.Equal(digest) {
		return artifacts.Ref{}, internal(ctx, artifacts.ErrIdentityConflict, operation, "stored_artifact_digest_mismatch")
	}
	return ref, nil
}

// PutLocation attaches one placement to an artifact that is already registered
// with exactly this identity.
//
// The guard is in the statement rather than in a preceding read. Checking the
// identity first and inserting afterwards is two round trips with a window
// between them, and the window is the whole rule: the location must never
// outlive a check that no longer holds.
func (store *Store) PutLocation(ctx context.Context, location artifacts.Location) error {
	const operation = "registry.postgres.PutArtifactLocation"
	if err := store.validate(ctx, operation); err != nil {
		return err
	}
	if err := location.Validate(); err != nil {
		return invalidArtifact(ctx, err, operation, faults.ReasonOf(err))
	}
	if err := store.validArtifact(ctx, location.Artifact, operation); err != nil {
		return err
	}
	ref := location.Artifact
	query := fmt.Sprintf(`INSERT INTO %s (digest, provider, uri, generation, region, written_at)
SELECT $1,$2,$3,$4,$5,$6 FROM %s identity
WHERE identity.digest=$1 AND identity.size_bytes=$7 AND identity.media_type=$8
  AND identity.logical_kind=$9 AND identity.schema_version=$10
  AND (SELECT count(*) FROM %s existing WHERE existing.digest=$1) < $11
ON CONFLICT (digest, provider, uri, generation) DO NOTHING`,
		store.artifactLocations, store.artifactIdentities, store.artifactLocations)
	result, err := store.executor(ctx).ExecContext(ctx, query,
		ref.Digest.String(), location.Provider, location.URI, location.Generation, location.Region,
		store.clock.Now().Round(0).UTC(),
		int64(ref.SizeBytes), ref.MediaType, ref.LogicalKind, int64(ref.SchemaVersion),
		int64(artifacts.MaximumLocationsPerArtifact),
	)
	if err != nil {
		return provider(ctx, err, operation)
	}
	if affected, rowsErr := result.RowsAffected(); rowsErr == nil && affected == 1 {
		return nil
	}
	// Zero rows is ambiguous: the identity may be absent or different, the
	// placement may already be stored, or the budget may be full. Only the
	// first is an error the caller must see, and a replay of an identical
	// placement must succeed rather than look like a conflict.
	return store.explainAbsentLocation(ctx, location, operation)
}

func (store *Store) explainAbsentLocation(ctx context.Context, location artifacts.Location, operation string) error {
	ref := location.Artifact
	query := fmt.Sprintf(`SELECT
 (SELECT count(*) FROM %s identity WHERE identity.digest=$1 AND identity.size_bytes=$2
    AND identity.media_type=$3 AND identity.logical_kind=$4 AND identity.schema_version=$5),
 (SELECT count(*) FROM %s existing WHERE existing.digest=$1 AND existing.provider=$6
    AND existing.uri=$7 AND existing.generation=$8),
 (SELECT count(*) FROM %s existing WHERE existing.digest=$1)`,
		store.artifactIdentities, store.artifactLocations, store.artifactLocations)
	var matching, stored, total int64
	if err := store.executor(ctx).QueryRowContext(ctx, query,
		ref.Digest.String(), int64(ref.SizeBytes), ref.MediaType, ref.LogicalKind, int64(ref.SchemaVersion),
		location.Provider, location.URI, location.Generation,
	).Scan(&matching, &stored, &total); err != nil {
		return provider(ctx, err, operation)
	}
	switch {
	case matching == 0:
		return unknownArtifactIdentity(ctx, operation, ref.Digest)
	case stored > 0:
		return nil
	case total >= int64(artifacts.MaximumLocationsPerArtifact):
		return artifactLocationBudget(ctx, operation, ref.Digest, total)
	default:
		return internal(ctx, artifacts.ErrLocationUnknownIdentity, operation, "artifact_location_write_lost")
	}
}

func (store *Store) Locations(ctx context.Context, digest identifiers.Digest) ([]artifacts.Location, error) {
	const operation = "registry.postgres.ArtifactLocations"
	if err := store.validate(ctx, operation); err != nil {
		return nil, err
	}
	if !digest.Valid() {
		return nil, invalidArtifact(ctx, artifacts.ErrNotFound, operation, "artifact_digest_required")
	}
	// The write path caps the placement set, so LIMIT here is a second bound
	// rather than the only one, and it reads one row past the cap so an
	// over-full set is reported instead of silently truncated. A page that
	// dropped replicas would make a garbage collector believe an object had
	// fewer copies than it does.
	query := fmt.Sprintf(`SELECT identity.document, placement.provider, placement.uri, placement.generation, placement.region
FROM %s placement JOIN %s identity ON identity.digest=placement.digest
WHERE placement.digest=$1
ORDER BY placement.provider, placement.uri, placement.generation
LIMIT $2`, store.artifactLocations, store.artifactIdentities)
	rows, err := store.executor(ctx).QueryContext(ctx, query, digest.String(), int64(artifacts.MaximumLocationsPerArtifact)+1)
	if err != nil {
		return nil, provider(ctx, err, operation)
	}
	defer rows.Close()
	locations := make([]artifacts.Location, 0, artifacts.MaximumLocationsPerArtifact)
	for rows.Next() {
		var document []byte
		var location artifacts.Location
		if err := rows.Scan(&document, &location.Provider, &location.URI, &location.Generation, &location.Region); err != nil {
			return nil, provider(ctx, err, operation)
		}
		if err := json.Unmarshal(document, &location.Artifact); err != nil {
			return nil, internal(ctx, err, operation, "artifact_decoding_failed")
		}
		if err := location.Validate(); err != nil {
			return nil, internal(ctx, err, operation, "stored_artifact_location_invalid")
		}
		locations = append(locations, location)
	}
	if err := rows.Err(); err != nil {
		return nil, provider(ctx, err, operation)
	}
	if len(locations) > artifacts.MaximumLocationsPerArtifact {
		return nil, internal(ctx, artifacts.ErrLocationBudget, operation, "artifact_location_page_overflow")
	}
	return locations, nil
}

// Register binds the identity and every placement in one statement.
//
// One statement is the entire point. Put followed by N PutLocation calls has no
// commit boundary, so a crash between them leaves a registered identity whose
// bytes nothing can find -- and because the digest binding is permanent, that
// half-state is durable. PostgreSQL executes a single statement atomically
// whether or not a caller wrapped it in a transaction, so the seam is safe even
// where no transaction context is installed, and joins the caller's commit
// where one is.
//
// It deliberately does not open its own transaction: a repository that did
// could not be composed into a wider commit, which is the same reason every
// other repository in this package resolves its executor per call.
func (store *Store) Register(ctx context.Context, ref artifacts.Ref, locations []artifacts.Location) error {
	const operation = "registry.postgres.RegisterArtifact"
	if err := store.validate(ctx, operation); err != nil {
		return err
	}
	if err := store.validArtifact(ctx, ref, operation); err != nil {
		return err
	}
	if len(locations) > artifacts.MaximumLocationsPerArtifact {
		return artifactLocationBudget(ctx, operation, ref.Digest, int64(len(locations)))
	}
	for _, location := range locations {
		if err := location.Validate(); err != nil {
			return invalidArtifact(ctx, err, operation, faults.ReasonOf(err))
		}
		if !location.Artifact.EqualIdentity(ref) {
			return invalidArtifact(ctx, artifacts.ErrLocationUnknownIdentity, operation, "artifact_location_identity_mismatch")
		}
	}
	// Duplicates within one batch would collide with each other inside the
	// statement, and they must not consume budget twice.
	placements := distinctPlacements(locations)
	if len(placements) == 0 {
		return store.putArtifactIdentity(ctx, ref, operation)
	}

	arguments := make([]any, 0, artifactRegisterFixedArguments+len(placements)*4)
	document, err := json.Marshal(ref)
	if err != nil {
		return internal(ctx, err, operation, "artifact_encoding_failed")
	}
	arguments = append(arguments,
		ref.Digest.String(), int64(ref.SizeBytes), ref.MediaType, ref.LogicalKind, int64(ref.SchemaVersion),
		document, store.clock.Now().Round(0).UTC(), int64(artifacts.MaximumLocationsPerArtifact),
	)
	for _, placement := range placements {
		arguments = append(arguments, placement.Provider, placement.URI, placement.Generation, placement.Region)
	}
	result, err := store.executor(ctx).ExecContext(ctx, store.registerStatement(len(placements)), arguments...)
	if err != nil {
		return provider(ctx, err, operation)
	}
	// Every placement landing proves the identity matched: the statement can
	// only insert a location through the identity CTE, which is empty when the
	// digest is bound to different metadata.
	if affected, rowsErr := result.RowsAffected(); rowsErr == nil && affected == int64(len(placements)) {
		return nil
	}
	return store.explainIncompleteRegistration(ctx, ref, placements, operation)
}

func (store *Store) registerStatement(placements int) string {
	values := make([]string, 0, placements)
	for index := range placements {
		base := artifactRegisterFixedArguments + index*4
		// The first row carries the casts that give the CTE its column types.
		if index == 0 {
			values = append(values, fmt.Sprintf("($%d::text,$%d::text,$%d::text,$%d::text)", base+1, base+2, base+3, base+4))
			continue
		}
		values = append(values, fmt.Sprintf("($%d,$%d,$%d,$%d)", base+1, base+2, base+3, base+4))
	}
	// identity_write inserts the binding; identity re-reads it when the insert
	// conflicted. The re-read is guarded on the immutable columns, so a digest
	// already bound to different metadata yields no identity row and therefore
	// no location rows -- the conflict blocks the placements rather than
	// attaching them to somebody else's artifact. A data-modifying CTE is not
	// visible to the rest of its own statement, which is why the union is
	// needed at all.
	return fmt.Sprintf(`WITH identity_write AS (
INSERT INTO %[1]s (digest, size_bytes, media_type, logical_kind, schema_version, document, written_at)
VALUES ($1,$2,$3,$4,$5,$6,$7)
ON CONFLICT (digest) DO NOTHING
RETURNING digest
), identity AS (
SELECT digest FROM identity_write
UNION ALL
SELECT digest FROM %[1]s
WHERE digest=$1 AND size_bytes=$2 AND media_type=$3 AND logical_kind=$4 AND schema_version=$5
LIMIT 1
), incoming (provider, uri, generation, region) AS (
VALUES %[3]s
)
INSERT INTO %[2]s (digest, provider, uri, generation, region, written_at)
SELECT identity.digest, incoming.provider, incoming.uri, incoming.generation, incoming.region, $7
FROM identity CROSS JOIN incoming
WHERE (SELECT count(*) FROM %[2]s existing WHERE existing.digest=$1)
    + (SELECT count(*) FROM incoming candidate WHERE NOT EXISTS (
        SELECT 1 FROM %[2]s existing WHERE existing.digest=$1
          AND existing.provider=candidate.provider AND existing.uri=candidate.uri
          AND existing.generation=candidate.generation)) <= $8
ON CONFLICT (digest, provider, uri, generation) DO NOTHING`,
		store.artifactIdentities, store.artifactLocations, strings.Join(values, ","))
}

// explainIncompleteRegistration turns "fewer rows than requested" into the one
// reason the caller can act on. The identity row is immutable once written, so
// re-reading it here cannot race with a writer that changes the answer.
func (store *Store) explainIncompleteRegistration(ctx context.Context, ref artifacts.Ref, placements []artifacts.Location, operation string) error {
	present, matching, err := store.artifactIdentityState(ctx, ref, operation)
	if err != nil {
		return err
	}
	if !present {
		return internal(ctx, artifacts.ErrNotFound, operation, "artifact_identity_write_lost")
	}
	if !matching {
		return artifactIdentityConflict(ctx, operation, ref.Digest)
	}
	landed, err := store.countStoredPlacements(ctx, ref.Digest, placements, operation)
	if err != nil {
		return err
	}
	if landed != len(placements) {
		return artifactLocationBudget(ctx, operation, ref.Digest, int64(landed))
	}
	return nil
}

func (store *Store) putArtifactIdentity(ctx context.Context, ref artifacts.Ref, operation string) error {
	document, err := json.Marshal(ref)
	if err != nil {
		return internal(ctx, err, operation, "artifact_encoding_failed")
	}
	query := fmt.Sprintf(`INSERT INTO %s (digest, size_bytes, media_type, logical_kind, schema_version, document, written_at)
VALUES ($1,$2,$3,$4,$5,$6,$7)
ON CONFLICT (digest) DO NOTHING`, store.artifactIdentities)
	result, err := store.executor(ctx).ExecContext(ctx, query,
		ref.Digest.String(), int64(ref.SizeBytes), ref.MediaType, ref.LogicalKind, int64(ref.SchemaVersion),
		document, store.clock.Now().Round(0).UTC(),
	)
	if err != nil {
		return provider(ctx, err, operation)
	}
	if affected, rowsErr := result.RowsAffected(); rowsErr == nil && affected == 1 {
		return nil
	}
	present, matching, err := store.artifactIdentityState(ctx, ref, operation)
	if err != nil {
		return err
	}
	if !present {
		return internal(ctx, artifacts.ErrNotFound, operation, "artifact_identity_write_lost")
	}
	if !matching {
		return artifactIdentityConflict(ctx, operation, ref.Digest)
	}
	return nil
}

// artifactIdentityState compares the stored immutable metadata against ref.
// The comparison is the projected columns rather than the document bytes,
// because the projection is exactly the domain's EqualIdentity predicate and
// does not depend on an encoder producing identical bytes twice.
func (store *Store) artifactIdentityState(ctx context.Context, ref artifacts.Ref, operation string) (present, matching bool, err error) {
	var size, schema int64
	var mediaType, logicalKind string
	scanErr := store.executor(ctx).QueryRowContext(ctx,
		fmt.Sprintf(`SELECT size_bytes, media_type, logical_kind, schema_version FROM %s WHERE digest=$1`, store.artifactIdentities),
		ref.Digest.String(),
	).Scan(&size, &mediaType, &logicalKind, &schema)
	if errors.Is(scanErr, sql.ErrNoRows) {
		return false, false, nil
	}
	if scanErr != nil {
		return false, false, provider(ctx, scanErr, operation)
	}
	same := size == int64(ref.SizeBytes) && mediaType == ref.MediaType &&
		logicalKind == ref.LogicalKind && schema == int64(ref.SchemaVersion)
	return true, same, nil
}

func (store *Store) countStoredPlacements(ctx context.Context, digest identifiers.Digest, placements []artifacts.Location, operation string) (int, error) {
	values := make([]string, 0, len(placements))
	arguments := make([]any, 0, 1+len(placements)*3)
	arguments = append(arguments, digest.String())
	for index := range placements {
		base := 1 + index*3
		if index == 0 {
			values = append(values, fmt.Sprintf("($%d::text,$%d::text,$%d::text)", base+1, base+2, base+3))
		} else {
			values = append(values, fmt.Sprintf("($%d,$%d,$%d)", base+1, base+2, base+3))
		}
		arguments = append(arguments, placements[index].Provider, placements[index].URI, placements[index].Generation)
	}
	query := fmt.Sprintf(`WITH wanted (provider, uri, generation) AS (VALUES %s)
SELECT count(*) FROM %s existing JOIN wanted
  ON existing.provider=wanted.provider AND existing.uri=wanted.uri AND existing.generation=wanted.generation
WHERE existing.digest=$1`, strings.Join(values, ","), store.artifactLocations)
	var landed int64
	if err := store.executor(ctx).QueryRowContext(ctx, query, arguments...).Scan(&landed); err != nil {
		return 0, provider(ctx, err, operation)
	}
	return int(landed), nil
}

// validArtifact rejects a Ref the schema cannot hold. SizeBytes is uint64 in
// the domain and bigint in the table, so a size past the signed range would
// otherwise be stored negative and silently pass the CHECK on the way back out.
func (store *Store) validArtifact(ctx context.Context, ref artifacts.Ref, operation string) error {
	if err := ref.Validate(); err != nil {
		return invalidArtifact(ctx, err, operation, faults.ReasonOf(err))
	}
	if ref.SizeBytes > math.MaxInt64 || ref.SchemaVersion > math.MaxInt32 {
		return invalidArtifact(ctx, artifacts.ErrIdentityConflict, operation, "artifact_size_out_of_range")
	}
	return nil
}

func distinctPlacements(locations []artifacts.Location) []artifacts.Location {
	distinct := make([]artifacts.Location, 0, len(locations))
	for _, location := range locations {
		duplicate := false
		for _, kept := range distinct {
			if kept.SamePlacement(location) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			distinct = append(distinct, location)
		}
	}
	return distinct
}

func invalidArtifact(ctx context.Context, err error, operation, reason string) error {
	return faults.Wrap(err, faults.CodeInvalidArgument, "artifact catalog record is invalid",
		faults.WithReason(reason), faults.WithOperation(operation), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
}

func artifactMissing(ctx context.Context, operation string, digest identifiers.Digest) error {
	return faults.Wrap(artifacts.ErrNotFound, faults.CodeNotFound, "artifact is not registered",
		faults.WithReason(artifacts.ReasonNotFound), faults.WithOperation(operation),
		faults.WithField("digest", digest.String()), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
}

// artifactIdentityConflict carries the domain's reason and sentinel so a caller
// cannot tell this store from the in-memory catalog by the rejection it gets.
func artifactIdentityConflict(ctx context.Context, operation string, digest identifiers.Digest) error {
	return faults.Wrap(artifacts.ErrIdentityConflict, faults.CodeInvalidArgument,
		"digest is already registered with different immutable metadata",
		faults.WithReason(artifacts.ReasonIdentityConflict), faults.WithOperation(operation),
		faults.WithField("digest", digest.String()), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
}

func unknownArtifactIdentity(ctx context.Context, operation string, digest identifiers.Digest) error {
	return faults.Wrap(artifacts.ErrLocationUnknownIdentity, faults.CodeInvalidArgument,
		"artifact identity must be registered before a location",
		faults.WithReason(artifacts.ReasonLocationUnknownIdentity), faults.WithOperation(operation),
		faults.WithField("digest", digest.String()), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
}

func artifactLocationBudget(ctx context.Context, operation string, digest identifiers.Digest, stored int64) error {
	return faults.Wrap(artifacts.ErrLocationBudget, faults.CodeResourceExhausted,
		"artifact location budget is exhausted",
		faults.WithReason(artifacts.ReasonLocationBudget), faults.WithOperation(operation),
		faults.WithField("digest", digest.String()), faults.WithField("stored_locations", stored),
		faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
}
