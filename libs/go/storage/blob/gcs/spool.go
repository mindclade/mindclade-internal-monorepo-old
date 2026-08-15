// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package gcs

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"

	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/identifiers"
	"mindclade.internal/libs/go/storage/blob"
)

type spool struct {
	file   *os.File
	size   int64
	digest identifiers.Digest
}

func createSpool(ctx context.Context, reader io.Reader, directory string, maximum int64) (_ *spool, err error) {
	if ctx == nil || reader == nil || maximum <= 0 {
		return nil, faults.New(faults.CodeInvalidArgument, "invalid blob upload", faults.WithReason("invalid_blob_upload"), faults.WithOperation("storage.blob.gcs.spool"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	if err := ctx.Err(); err != nil {
		return nil, qualifySpoolContext(ctx, err)
	}
	file, err := os.CreateTemp(directory, "mindclade-blob-*.upload")
	if err != nil {
		return nil, faults.Wrap(err, faults.CodeUnavailable, "unable to stage blob upload", faults.WithReason("blob_spool_create_failed"), faults.WithOperation("storage.blob.gcs.spool"), faults.WithRetryPolicy(faults.BackoffRetry(3)))
	}
	defer func() {
		if err != nil {
			_ = file.Close()
			_ = os.Remove(file.Name())
		}
	}()

	hash := sha256.New()
	limited := io.LimitReader(contextReader{ctx: ctx, reader: reader}, maximum+1)
	size, copyErr := io.Copy(io.MultiWriter(file, hash), limited)
	if copyErr != nil {
		if cause := ctx.Err(); cause != nil {
			return nil, qualifySpoolContext(ctx, cause)
		}
		return nil, faults.Wrap(copyErr, faults.CodeUnavailable, "unable to stage blob upload", faults.WithReason("blob_spool_write_failed"), faults.WithOperation("storage.blob.gcs.spool"), faults.WithRetryPolicy(faults.BackoffRetry(3)))
	}
	if size > maximum {
		return nil, faults.Wrap(blob.ErrObjectTooLarge, faults.CodeResourceExhausted, "blob object exceeds configured limit", faults.WithReason("blob_object_too_large"), faults.WithOperation("storage.blob.gcs.spool"), faults.WithField("maximum_bytes", maximum), faults.WithRetryPolicy(faults.NoRetry()))
	}
	if err := file.Sync(); err != nil {
		return nil, faults.Wrap(err, faults.CodeUnavailable, "unable to stage blob upload", faults.WithReason("blob_spool_sync_failed"), faults.WithOperation("storage.blob.gcs.spool"), faults.WithRetryPolicy(faults.BackoffRetry(3)))
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, faults.Wrap(err, faults.CodeInternal, "unable to prepare blob upload", faults.WithReason("blob_spool_seek_failed"), faults.WithOperation("storage.blob.gcs.spool"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	digest, err := identifiers.DigestFromBytes(hash.Sum(nil))
	if err != nil {
		return nil, faults.Wrap(err, faults.CodeInternal, "unable to compute blob digest", faults.WithReason("blob_digest_generation_failed"), faults.WithOperation("storage.blob.gcs.spool"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return &spool{file: file, size: size, digest: digest}, nil
}

func (value *spool) Close() error {
	if value == nil || value.file == nil {
		return nil
	}
	name := value.file.Name()
	closeErr := value.file.Close()
	removeErr := os.Remove(name)
	if closeErr != nil && removeErr != nil {
		return errors.Join(closeErr, removeErr)
	}
	if closeErr != nil {
		return closeErr
	}
	return removeErr
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader contextReader) Read(value []byte) (int, error) {
	select {
	case <-reader.ctx.Done():
		return 0, reader.ctx.Err()
	default:
		return reader.reader.Read(value)
	}
}

func qualifySpoolContext(ctx context.Context, err error) error {
	code := faults.CodeCanceled
	message := "blob upload staging was canceled"
	reason := "blob_spool_canceled"
	if errors.Is(err, context.DeadlineExceeded) {
		code = faults.CodeDeadlineExceeded
		message = "blob upload staging exceeded its deadline"
		reason = "blob_spool_deadline_exceeded"
	}
	return faults.Wrap(err, code, message,
		faults.WithReason(reason),
		faults.WithOperation("storage.blob.gcs.spool"),
		faults.WithContextMetadata(ctx),
		faults.WithRetryPolicy(faults.NoRetry()),
	)
}
