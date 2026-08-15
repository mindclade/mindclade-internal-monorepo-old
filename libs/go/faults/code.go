// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package faults

import (
	"fmt"
	"strings"
)

// Code is a stable, transport-neutral classification for a failure.
//
// Codes are intentionally broad. Domain-specific machine-readable detail
// belongs in Fault.Reason rather than in an ever-growing code taxonomy.
type Code string

const (
	CodeUnknown            Code = "unknown"
	CodeCanceled           Code = "canceled"
	CodeInvalidArgument    Code = "invalid_argument"
	CodeDeadlineExceeded   Code = "deadline_exceeded"
	CodeNotFound           Code = "not_found"
	CodeAlreadyExists      Code = "already_exists"
	CodePermissionDenied   Code = "permission_denied"
	CodeUnauthenticated    Code = "unauthenticated"
	CodeResourceExhausted  Code = "resource_exhausted"
	CodeFailedPrecondition Code = "failed_precondition"
	CodeConflict           Code = "conflict"
	CodeAborted            Code = "aborted"
	CodeOutOfRange         Code = "out_of_range"
	CodeNotImplemented     Code = "not_implemented"
	CodeInternal           Code = "internal"
	CodeUnavailable        Code = "unavailable"
	CodeDataLoss           Code = "data_loss"
)

var validCodes = map[Code]struct{}{
	CodeUnknown:            {},
	CodeCanceled:           {},
	CodeInvalidArgument:    {},
	CodeDeadlineExceeded:   {},
	CodeNotFound:           {},
	CodeAlreadyExists:      {},
	CodePermissionDenied:   {},
	CodeUnauthenticated:    {},
	CodeResourceExhausted:  {},
	CodeFailedPrecondition: {},
	CodeConflict:           {},
	CodeAborted:            {},
	CodeOutOfRange:         {},
	CodeNotImplemented:     {},
	CodeInternal:           {},
	CodeUnavailable:        {},
	CodeDataLoss:           {},
}

// String returns the wire representation of the code.
func (c Code) String() string {
	return string(c)
}

// Valid reports whether c is one of the package's canonical codes.
func (c Code) Valid() bool {
	_, ok := validCodes[c]
	return ok
}

// ParseCode parses a canonical code. Leading and trailing whitespace and ASCII
// letter case are normalized. Unknown values return CodeUnknown and an error.
func ParseCode(value string) (Code, error) {
	code := Code(strings.ToLower(strings.TrimSpace(value)))
	if !code.Valid() {
		return CodeUnknown, fmt.Errorf("faults: invalid code %q", value)
	}
	return code, nil
}

// NormalizeCode returns c when it is valid and CodeUnknown otherwise.
func NormalizeCode(c Code) Code {
	if c.Valid() {
		return c
	}
	return CodeUnknown
}

func defaultMessage(code Code) string {
	switch NormalizeCode(code) {
	case CodeCanceled:
		return "request canceled"
	case CodeInvalidArgument:
		return "invalid argument"
	case CodeDeadlineExceeded:
		return "deadline exceeded"
	case CodeNotFound:
		return "resource not found"
	case CodeAlreadyExists:
		return "resource already exists"
	case CodePermissionDenied:
		return "permission denied"
	case CodeUnauthenticated:
		return "authentication required"
	case CodeResourceExhausted:
		return "resource exhausted"
	case CodeFailedPrecondition:
		return "failed precondition"
	case CodeConflict:
		return "resource conflict"
	case CodeAborted:
		return "operation aborted"
	case CodeOutOfRange:
		return "value out of range"
	case CodeNotImplemented:
		return "operation not implemented"
	case CodeInternal:
		return "internal error"
	case CodeUnavailable:
		return "service unavailable"
	case CodeDataLoss:
		return "data loss"
	case CodeUnknown:
		fallthrough
	default:
		return "operation failed"
	}
}
