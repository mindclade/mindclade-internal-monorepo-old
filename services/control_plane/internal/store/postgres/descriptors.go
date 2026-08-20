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

	"go.mindclade.dev/control/registry/models"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
)

var _ models.Repository = (*Store)(nil)

// PutDescriptor stores a sealed descriptor.
//
// Descriptors are content-addressed, so this is insert-if-absent rather than
// an upsert. Republishing byte-identical content is a no-op and reports
// success, which is what makes Publish safe to retry. Republishing different
// content under a digest that is already taken cannot be a concurrent write --
// the digest covers every field -- so it is reported as a seal failure and
// never retried.
//
// The descriptor's own seal is verified before the write. The domain Service
// seals immediately before calling, so this is not a redundant check of the
// happy path: it is the check that keeps an unsealed or hand-constructed
// descriptor reaching this repository through some other caller from being
// stored under a digest that does not describe it.
func (store *Store) PutDescriptor(ctx context.Context, descriptor models.Descriptor) error {
	const operation = "registry.postgres.PutDescriptor"
	if err := store.validate(ctx, operation); err != nil {
		return err
	}
	if err := descriptor.VerifyDigest(); err != nil {
		return faults.Wrap(err, faults.CodeInvalidArgument, "model descriptor is not correctly sealed",
			faults.WithReason("model_descriptor_unsealed"),
			faults.WithOperation(operation),
			faults.WithContextMetadata(ctx),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	document, err := json.Marshal(descriptor)
	if err != nil {
		return internal(ctx, err, operation, "descriptor_encoding_failed")
	}
	content := identifiers.SHA256(document)
	now := store.clock.Now().Round(0).UTC()

	query := fmt.Sprintf(`INSERT INTO %s (
descriptor_digest, content_digest, model_id, family, version, lifecycle,
model_bundle_digest, schema_version, policy_epoch, created, expires,
document, written_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
ON CONFLICT (descriptor_digest) DO NOTHING`, store.descriptors)
	result, err := store.executor(ctx).ExecContext(ctx, query,
		descriptor.DescriptorDigest.String(), content.String(), descriptor.ModelID,
		descriptor.Family, descriptor.Version, string(descriptor.Lifecycle),
		descriptor.ModelBundleDigest.String(), int64(descriptor.SchemaVersion),
		int64(descriptor.PolicyEpoch), descriptor.Created.UTC(), descriptor.Expires.UTC(),
		document, now,
	)
	if err != nil {
		return provider(ctx, err, operation)
	}
	if affected, affectedErr := result.RowsAffected(); affectedErr == nil && affected == 1 {
		return nil
	}

	// The digest is taken. Either this is an idempotent republish of identical
	// content, or the seal does not uniquely determine the descriptor.
	var stored string
	row := store.executor(ctx).QueryRowContext(ctx,
		fmt.Sprintf(`SELECT content_digest FROM %s WHERE descriptor_digest=$1`, store.descriptors),
		descriptor.DescriptorDigest.String(),
	)
	if err := row.Scan(&stored); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// The row was deleted between the insert and this read. Nothing
			// retains descriptors today, so treat it as the transient race it
			// is rather than reporting a collision that did not happen.
			return faults.Wrap(err, faults.CodeAborted, "model descriptor write raced a concurrent delete",
				faults.WithReason("model_descriptor_write_raced"),
				faults.WithOperation(operation),
				faults.WithField("descriptor_digest", descriptor.DescriptorDigest.String()),
				faults.WithContextMetadata(ctx),
				faults.WithRetryPolicy(faults.BackoffRetry(3)),
			)
		}
		return provider(ctx, err, operation)
	}
	if stored == content.String() {
		return nil
	}
	return faults.Wrap(ErrDigestCollision, faults.CodeFailedPrecondition,
		"model descriptor digest is already stored against different content",
		faults.WithReason("model_descriptor_digest_collision"),
		faults.WithOperation(operation),
		faults.WithField("descriptor_digest", descriptor.DescriptorDigest.String()),
		faults.WithContextMetadata(ctx),
		faults.WithRetryPolicy(faults.NoRetry()),
	)
}

// GetDescriptor loads a descriptor by its sealed digest.
//
// A missing row is a faults.CodeNotFound error rather than a zero value, so a
// caller cannot mistake absence for a descriptor with empty fields. The seal
// is not re-verified here: models.Service.Resolve verifies it and compares the
// returned identity against the digest it asked for, and duplicating that
// check would put the same decision in two places.
func (store *Store) GetDescriptor(ctx context.Context, digest identifiers.Digest) (models.Descriptor, error) {
	const operation = "registry.postgres.GetDescriptor"
	if err := store.validate(ctx, operation); err != nil {
		return models.Descriptor{}, err
	}
	if !digest.Valid() {
		return models.Descriptor{}, faults.Wrap(ErrDescriptorMissing, faults.CodeInvalidArgument,
			"model descriptor digest is invalid",
			faults.WithReason("model_descriptor_digest_invalid"),
			faults.WithOperation(operation),
			faults.WithContextMetadata(ctx),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	var document []byte
	row := store.executor(ctx).QueryRowContext(ctx,
		fmt.Sprintf(`SELECT document FROM %s WHERE descriptor_digest=$1`, store.descriptors),
		digest.String(),
	)
	if err := row.Scan(&document); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.Descriptor{}, faults.Wrap(ErrDescriptorMissing, faults.CodeNotFound,
				"model descriptor was not found",
				faults.WithReason("model_descriptor_not_found"),
				faults.WithOperation(operation),
				faults.WithField("descriptor_digest", digest.String()),
				faults.WithContextMetadata(ctx),
				faults.WithRetryPolicy(faults.NoRetry()),
			)
		}
		return models.Descriptor{}, provider(ctx, err, operation)
	}
	var descriptor models.Descriptor
	if err := json.Unmarshal(document, &descriptor); err != nil {
		return models.Descriptor{}, internal(ctx, err, operation, "descriptor_decoding_failed")
	}
	return descriptor, nil
}
