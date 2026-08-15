// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package faults

import "strings"

// Fault is an immutable structured error.
//
// Construction occurs through New or Wrap. Accessors return values or
// defensive copies so the classification cannot be changed after publication.
type Fault struct {
	code      Code
	message   string
	reason    string
	operation string
	cause     error
	fields    Fields
	retry     RetryPolicy
}

var _ error = (*Fault)(nil)

// New constructs a structured fault.
func New(code Code, message string, options ...Option) error {
	return newFault(code, message, nil, options...)
}

// Wrap constructs a structured fault around cause. A nil cause returns nil,
// matching the convention used by common Go wrapping helpers.
func Wrap(cause error, code Code, message string, options ...Option) error {
	if cause == nil {
		return nil
	}
	return newFault(code, message, cause, options...)
}

func newFault(code Code, message string, cause error, options ...Option) *Fault {
	normalizedCode := NormalizeCode(code)
	normalizedMessage := strings.TrimSpace(message)
	if normalizedMessage == "" {
		normalizedMessage = defaultMessage(normalizedCode)
	}

	fault := &Fault{
		code:    normalizedCode,
		message: normalizedMessage,
		cause:   cause,
	}

	for _, option := range options {
		if option != nil {
			option(fault)
		}
	}

	fault.reason = strings.TrimSpace(fault.reason)
	fault.operation = strings.TrimSpace(fault.operation)
	fault.fields = cloneFields(fault.fields)
	fault.retry = fault.retry.Normalized()

	return fault
}

// Error returns a diagnostic string. It may include the wrapped cause and must
// not be serialized directly to an external client.
func (fault *Fault) Error() string {
	if fault == nil {
		return "<nil>"
	}

	message := fault.message
	if message == "" {
		message = defaultMessage(fault.code)
	}
	if fault.operation != "" {
		message = fault.operation + ": " + message
	}
	if fault.cause != nil {
		message += ": " + fault.cause.Error()
	}
	return message
}

// Unwrap returns the diagnostic cause, when present.
func (fault *Fault) Unwrap() error {
	if fault == nil {
		return nil
	}
	return fault.cause
}

// Code returns the stable machine-readable code.
func (fault *Fault) Code() Code {
	if fault == nil {
		return CodeUnknown
	}
	return NormalizeCode(fault.code)
}

// Message returns the client-safe message supplied at construction time.
func (fault *Fault) Message() string {
	if fault == nil {
		return ""
	}
	return fault.message
}

// Reason returns optional domain-specific machine-readable detail.
func (fault *Fault) Reason() string {
	if fault == nil {
		return ""
	}
	return fault.reason
}

// Operation returns the logical operation in which the fault occurred.
func (fault *Fault) Operation() string {
	if fault == nil {
		return ""
	}
	return fault.operation
}

// Fields returns a defensive copy of the structured diagnostic metadata.
func (fault *Fault) Fields() Fields {
	if fault == nil {
		return nil
	}
	return cloneFields(fault.fields)
}

// RetryPolicy returns the normalized retry intent.
func (fault *Fault) RetryPolicy() RetryPolicy {
	if fault == nil {
		return RetryPolicy{}
	}
	return fault.retry.Normalized()
}
