// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package gcs

import (
	"context"
	"errors"
	"net/http"

	gcsapi "cloud.google.com/go/storage"
	"github.com/googleapis/gax-go/v2/apierror"
	"google.golang.org/api/googleapi"
	"google.golang.org/grpc/codes"

	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/storage/blob"
)

type errorIntent int

const (
	intentGeneral errorIntent = iota
	intentCreateOnly
	intentGenerationMatch
)

func qualify(ctx context.Context, err error, operation, bucket string, key blob.Key, intent errorIntent) error {
	if err == nil {
		return nil
	}
	if faults.CodeOf(err) != faults.CodeUnknown {
		return err
	}
	fields := faults.Fields{"gcs_bucket": bucket}
	if !key.IsZero() {
		fields["blob_key"] = key.String()
	}

	if errors.Is(err, context.Canceled) {
		return wrapProvider(ctx, err, faults.CodeCanceled, "blob operation canceled", "gcs_canceled", operation, fields, faults.NoRetry())
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return wrapProvider(ctx, err, faults.CodeDeadlineExceeded, "blob operation timed out", "gcs_deadline_exceeded", operation, fields, faults.NoRetry())
	}
	if errors.Is(err, gcsapi.ErrObjectNotExist) {
		return wrapProvider(ctx, err, faults.CodeNotFound, "blob object not found", "blob_not_found", operation, fields, faults.NoRetry())
	}

	httpCode, grpcCode := providerCodes(err)
	switch {
	case httpCode == http.StatusBadRequest || grpcCode == codes.InvalidArgument:
		return wrapProvider(ctx, err, faults.CodeInvalidArgument, "invalid blob storage request", "gcs_invalid_request", operation, fields, faults.NoRetry())
	case httpCode == http.StatusUnauthorized || grpcCode == codes.Unauthenticated:
		return wrapProvider(ctx, err, faults.CodeUnauthenticated, "blob storage authentication failed", "gcs_unauthenticated", operation, fields, faults.NoRetry())
	case httpCode == http.StatusForbidden || grpcCode == codes.PermissionDenied:
		return wrapProvider(ctx, err, faults.CodePermissionDenied, "blob storage permission denied", "gcs_permission_denied", operation, fields, faults.NoRetry())
	case httpCode == http.StatusNotFound || grpcCode == codes.NotFound:
		return wrapProvider(ctx, err, faults.CodeNotFound, "blob object not found", "blob_not_found", operation, fields, faults.NoRetry())
	case httpCode == http.StatusPreconditionFailed || grpcCode == codes.FailedPrecondition:
		if intent == intentCreateOnly {
			return wrapProvider(ctx, errors.Join(blob.ErrPrecondition, err), faults.CodeAlreadyExists, "blob object already exists", "blob_must_not_exist", operation, fields, faults.NoRetry())
		}
		return wrapProvider(ctx, errors.Join(blob.ErrPrecondition, err), faults.CodeConflict, "blob precondition failed", "blob_generation_mismatch", operation, fields, faults.NoRetry())
	case httpCode == http.StatusConflict || grpcCode == codes.Aborted:
		return wrapProvider(ctx, err, faults.CodeConflict, "blob storage conflict", "gcs_conflict", operation, fields, faults.NoRetry())
	case httpCode == http.StatusRequestedRangeNotSatisfiable || grpcCode == codes.OutOfRange:
		return wrapProvider(ctx, err, faults.CodeOutOfRange, "blob range is out of bounds", "blob_range_out_of_bounds", operation, fields, faults.NoRetry())
	case httpCode == http.StatusTooManyRequests || grpcCode == codes.ResourceExhausted:
		return wrapProvider(ctx, err, faults.CodeResourceExhausted, "blob storage is throttled", "gcs_throttled", operation, fields, faults.BackoffRetry(5))
	case grpcCode == codes.Canceled:
		return wrapProvider(ctx, err, faults.CodeCanceled, "blob operation canceled", "gcs_canceled", operation, fields, faults.NoRetry())
	case grpcCode == codes.DeadlineExceeded:
		return wrapProvider(ctx, err, faults.CodeDeadlineExceeded, "blob operation timed out", "gcs_deadline_exceeded", operation, fields, faults.NoRetry())
	case gcsapi.ShouldRetry(err) || httpCode >= 500 || grpcCode == codes.Unavailable:
		return wrapProvider(ctx, err, faults.CodeUnavailable, "blob storage is unavailable", "gcs_unavailable", operation, fields, faults.BackoffRetry(5))
	default:
		return wrapProvider(ctx, err, faults.CodeInternal, "blob storage operation failed", "gcs_error", operation, fields, faults.NoRetry())
	}
}

func providerCodes(err error) (int, codes.Code) {
	var apiErr *apierror.APIError
	if errors.As(err, &apiErr) && apiErr != nil {
		return apiErr.HTTPCode(), apiErr.GRPCStatus().Code()
	}
	var googleErr *googleapi.Error
	if errors.As(err, &googleErr) && googleErr != nil {
		return googleErr.Code, codes.Unknown
	}
	return 0, codes.Unknown
}

func wrapProvider(ctx context.Context, cause error, code faults.Code, message, reason, operation string, fields faults.Fields, policy faults.RetryPolicy) error {
	return faults.Wrap(cause, code, message,
		faults.WithReason(reason),
		faults.WithOperation(operation),
		faults.WithFields(fields),
		faults.WithContextMetadata(ctx),
		faults.WithRetryPolicy(policy),
	)
}
