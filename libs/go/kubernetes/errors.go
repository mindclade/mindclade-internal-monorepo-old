// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package kubernetes

import (
	"context"
	stderrors "errors"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"mindclade.internal/libs/go/faults"
)

const (
	FieldAPIReason  = "kubernetes.api_reason"
	FieldHTTPStatus = "kubernetes.http_status"
	FieldAPIVersion = "kubernetes.api_version"
	FieldGroup      = "kubernetes.group"
	FieldKind       = "kubernetes.kind"
	FieldNamespace  = "kubernetes.namespace"
	FieldName       = "kubernetes.name"
	FieldUID        = "kubernetes.uid"
)

// Qualify converts a Kubernetes API error into a transport-neutral Mindclade
// fault. Existing structured faults are returned unchanged. Provider error
// text is retained only as the wrapped diagnostic cause and is never selected
// as the public message.
func Qualify(ctx context.Context, err error, operation string, fields faults.Fields) error {
	if err == nil {
		return nil
	}
	if _, ok := faults.AsFault(err); ok {
		return err
	}

	code := faults.CodeInternal
	reason := "kubernetes_operation_failed"
	message := "Kubernetes operation failed"
	retryPolicy := faults.NoRetry()

	switch {
	case stderrors.Is(err, context.Canceled):
		code = faults.CodeCanceled
		reason = "kubernetes_operation_canceled"
		message = "Kubernetes operation was canceled"
	case stderrors.Is(err, context.DeadlineExceeded):
		code = faults.CodeDeadlineExceeded
		reason = "kubernetes_operation_deadline_exceeded"
		message = "Kubernetes operation exceeded its deadline"
	case apierrors.IsNotFound(err):
		code = faults.CodeNotFound
		reason = "kubernetes_resource_not_found"
		message = "Kubernetes resource was not found"
	case apierrors.IsAlreadyExists(err):
		code = faults.CodeAlreadyExists
		reason = "kubernetes_resource_already_exists"
		message = "Kubernetes resource already exists"
	case apierrors.IsConflict(err):
		code = faults.CodeConflict
		reason = "kubernetes_resource_conflict"
		message = "Kubernetes resource changed concurrently"
		retryPolicy = faults.BackoffRetry(5)
	case apierrors.IsInvalid(err), apierrors.IsBadRequest(err), apierrors.IsUnsupportedMediaType(err), apierrors.IsNotAcceptable(err):
		code = faults.CodeInvalidArgument
		reason = "kubernetes_resource_invalid"
		message = "Kubernetes request is invalid"
	case apierrors.IsForbidden(err):
		code = faults.CodePermissionDenied
		reason = "kubernetes_operation_forbidden"
		message = "Kubernetes operation is not permitted"
	case apierrors.IsUnauthorized(err):
		code = faults.CodeUnauthenticated
		reason = "kubernetes_authentication_required"
		message = "Kubernetes authentication is required"
	case apierrors.IsTooManyRequests(err):
		code = faults.CodeResourceExhausted
		reason = "kubernetes_rate_limited"
		message = "Kubernetes API request was rate limited"
		retryPolicy = retryPolicyFromStatus(err, 5)
	case apierrors.IsRequestEntityTooLargeError(err):
		code = faults.CodeResourceExhausted
		reason = "kubernetes_request_too_large"
		message = "Kubernetes request exceeds the server limit"
	case apierrors.IsServerTimeout(err), apierrors.IsTimeout(err), apierrors.IsServiceUnavailable(err), apierrors.IsInternalError(err), apierrors.IsUnexpectedServerError(err):
		code = faults.CodeUnavailable
		reason = "kubernetes_api_unavailable"
		message = "Kubernetes API is unavailable"
		retryPolicy = retryPolicyFromStatus(err, 5)
	case apierrors.IsGone(err), apierrors.IsResourceExpired(err):
		code = faults.CodeAborted
		reason = "kubernetes_resource_version_expired"
		message = "Kubernetes resource version expired"
		retryPolicy = faults.ImmediateRetry(5)
	case apierrors.IsMethodNotSupported(err):
		code = faults.CodeNotImplemented
		reason = "kubernetes_method_not_supported"
		message = "Kubernetes operation is not supported"
	case apierrors.IsStoreReadError(err), apierrors.IsUnexpectedObjectError(err):
		code = faults.CodeDataLoss
		reason = "kubernetes_response_corrupt"
		message = "Kubernetes API returned invalid data"
	}

	qualifiedFields := fields.Clone()
	if qualifiedFields == nil {
		qualifiedFields = faults.Fields{}
	}
	appendAPIStatusFields(qualifiedFields, err)

	operation = strings.TrimSpace(operation)
	if operation == "" {
		operation = "kubernetes.Operation"
	}
	options := []faults.Option{
		faults.WithCause(err),
		faults.WithReason(reason),
		faults.WithOperation(operation),
		faults.WithFields(qualifiedFields),
		faults.WithRetryPolicy(retryPolicy),
	}
	if ctx != nil {
		options = append(options, faults.WithContextMetadata(ctx))
	}
	return faults.New(code, message, options...)
}

// QualifyObject is Qualify with diagnostic object identity attached.
func QualifyObject(ctx context.Context, err error, operation string, reference ObjectReference, additional faults.Fields) error {
	return Qualify(ctx, err, operation, reference.Fields().Merge(additional))
}

func retryPolicyFromStatus(err error, maximumAttempts int) faults.RetryPolicy {
	if seconds, ok := apierrors.SuggestsClientDelay(err); ok && seconds > 0 {
		return faults.DelayedRetry(time.Duration(seconds)*time.Second, maximumAttempts)
	}
	return faults.BackoffRetry(maximumAttempts)
}

func appendAPIStatusFields(fields faults.Fields, err error) {
	var status apierrors.APIStatus
	if !stderrors.As(err, &status) || status == nil {
		return
	}
	apiStatus := status.Status()
	if apiStatus.Reason != "" {
		fields[FieldAPIReason] = string(apiStatus.Reason)
	}
	if apiStatus.Code != 0 {
		fields[FieldHTTPStatus] = apiStatus.Code
	}
	if apiStatus.Details == nil {
		return
	}
	if apiStatus.Details.Group != "" {
		fields[FieldGroup] = apiStatus.Details.Group
	}
	if apiStatus.Details.Kind != "" {
		fields[FieldKind] = apiStatus.Details.Kind
	}
	if apiStatus.Details.Name != "" {
		fields[FieldName] = apiStatus.Details.Name
	}
}
