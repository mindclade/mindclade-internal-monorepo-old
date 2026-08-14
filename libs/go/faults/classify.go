// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package faults

import (
	"context"
	stderrors "errors"
	"io/fs"
	"strings"
)

type codeProvider interface {
	Code() Code
}

type messageProvider interface {
	Message() string
}

type reasonProvider interface {
	Reason() string
}

type operationProvider interface {
	Operation() string
}

type fieldsProvider interface {
	Fields() Fields
}

type retryPolicyProvider interface {
	RetryPolicy() RetryPolicy
}

// AsFault finds the outermost *Fault in err's wrapping chain.
func AsFault(err error) (*Fault, bool) {
	if err == nil {
		return nil, false
	}

	var fault *Fault
	if !stderrors.As(err, &fault) || fault == nil {
		return nil, false
	}
	return fault, true
}

// CodeOf returns the outermost structured code. For otherwise unstructured
// errors, it applies a deliberately small set of standard-library mappings.
func CodeOf(err error) Code {
	if err == nil {
		return CodeUnknown
	}

	var provider codeProvider
	if stderrors.As(err, &provider) && provider != nil {
		return NormalizeCode(provider.Code())
	}

	switch {
	case stderrors.Is(err, context.Canceled):
		return CodeCanceled
	case stderrors.Is(err, context.DeadlineExceeded):
		return CodeDeadlineExceeded
	case stderrors.Is(err, fs.ErrNotExist):
		return CodeNotFound
	case stderrors.Is(err, fs.ErrExist):
		return CodeAlreadyExists
	case stderrors.Is(err, fs.ErrPermission):
		return CodePermissionDenied
	case stderrors.Is(err, fs.ErrInvalid):
		return CodeInvalidArgument
	default:
		return CodeUnknown
	}
}

// MessageOf returns the structured message when available and otherwise the
// raw error string. This helper is for trusted diagnostics; external responses
// should use PublicMessageOf.
func MessageOf(err error) string {
	if err == nil {
		return ""
	}

	var provider messageProvider
	if stderrors.As(err, &provider) && provider != nil {
		return strings.TrimSpace(provider.Message())
	}
	return strings.TrimSpace(err.Error())
}

// PublicMessageOf returns a message suitable for an external response. It
// never appends a wrapped cause. Unstructured errors receive a canonical
// message derived from their inferred code.
func PublicMessageOf(err error) string {
	if err == nil {
		return ""
	}

	var provider messageProvider
	if stderrors.As(err, &provider) && provider != nil {
		message := strings.TrimSpace(provider.Message())
		if message != "" {
			return message
		}
	}
	return defaultMessage(CodeOf(err))
}

// ReasonOf returns the outermost machine-readable reason.
func ReasonOf(err error) string {
	if err == nil {
		return ""
	}

	var provider reasonProvider
	if !stderrors.As(err, &provider) || provider == nil {
		return ""
	}
	return strings.TrimSpace(provider.Reason())
}

// OperationOf returns the outermost logical operation.
func OperationOf(err error) string {
	if err == nil {
		return ""
	}

	var provider operationProvider
	if !stderrors.As(err, &provider) || provider == nil {
		return ""
	}
	return strings.TrimSpace(provider.Operation())
}

// FieldsOf returns a defensive copy of the outermost structured fields.
func FieldsOf(err error) Fields {
	if err == nil {
		return nil
	}

	var provider fieldsProvider
	if !stderrors.As(err, &provider) || provider == nil {
		return nil
	}
	return cloneFields(provider.Fields())
}

// RetryPolicyOf returns the normalized outermost retry policy.
func RetryPolicyOf(err error) RetryPolicy {
	if err == nil {
		return RetryPolicy{}
	}

	var provider retryPolicyProvider
	if !stderrors.As(err, &provider) || provider == nil {
		return RetryPolicy{}
	}
	return provider.RetryPolicy().Normalized()
}

// IsCode reports whether the outermost classification equals code.
func IsCode(err error, code Code) bool {
	return err != nil && code.Valid() && CodeOf(err) == code
}

// IsReason reports whether the outermost machine-readable reason equals reason.
func IsReason(err error, reason string) bool {
	normalized := strings.TrimSpace(reason)
	return err != nil && normalized != "" && ReasonOf(err) == normalized
}

// IsRetryable reports whether an explicit retry policy permits another attempt.
func IsRetryable(err error) bool {
	return err != nil && RetryPolicyOf(err).Retryable()
}
