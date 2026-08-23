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
//
// The canonical set mirrors mindclade.common.v1.ErrorCode in
// protocols/proto/mindclade/common/v1/errors.proto, which is the wire
// authority. tests/integration/cross_language/test_error_codes.py fails if this
// file, that proto, and libs/rust/faults drift apart.
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

// canonicalCodes is the taxonomy itself, ordered by the tag its counterpart
// carries in mindclade.common.v1.ErrorCode. The order is part of what Codes
// documents, so the two files can be read side by side and a reordering shows
// up as a reviewable diff instead of disappearing into map iteration.
//
// This slice, not the const block above, is what makes a code canonical.
// validCodes used to be a second hand-maintained literal naming the same
// seventeen codes, so declaring a constant and forgetting its map entry left a
// code that every constructor silently rewrote to CodeUnknown — a spelling the
// compiler accepted and no test could see. Deriving the set removes that second
// place to be wrong.
var canonicalCodes = []Code{
	CodeInvalidArgument,
	CodeUnauthenticated,
	CodePermissionDenied,
	CodeNotFound,
	CodeConflict,
	CodeResourceExhausted,
	CodeDeadlineExceeded,
	CodeUnavailable,
	CodeInternal,
	CodeCanceled,
	CodeAlreadyExists,
	CodeFailedPrecondition,
	CodeAborted,
	CodeOutOfRange,
	CodeNotImplemented,
	CodeDataLoss,
	CodeUnknown,
}

var validCodes = func() map[Code]struct{} {
	set := make(map[Code]struct{}, len(canonicalCodes))
	for _, code := range canonicalCodes {
		set[code] = struct{}{}
	}
	return set
}()

// Codes returns the canonical taxonomy in the tag order of
// mindclade.common.v1.ErrorCode, excluding that enum's UNSPECIFIED value, which
// this package represents as the absence of a code rather than as a constant.
//
// It exists so the taxonomy can be compared against the generated protobuf
// descriptor by a program rather than by a regular expression over this file:
// protocols/consumers/_bazel/faults_conformance_test.go is that comparison, and
// it can only enumerate what this package exports. The returned slice is a
// fresh copy, so a caller cannot shorten the set a conformance check is about
// to read.
func Codes() []Code {
	return append([]Code(nil), canonicalCodes...)
}

// codeAliases are non-canonical spellings that must still resolve on ingestion.
//
// libs/rust/faults emitted these two spellings before the taxonomy was
// reconciled, so they are already present in peer responses, telemetry
// attributes, and stored diagnostics. Accepting them costs nothing; rejecting
// them would silently reclassify a canceled or unimplemented operation as
// CodeUnknown and discard the retry behavior the sender chose. They are
// deliberately not Valid: this package accepts both spellings and emits one.
var codeAliases = map[string]Code{
	"cancelled":     CodeCanceled,
	"unimplemented": CodeNotImplemented,
}

// CodeAliases returns the accepted non-canonical spellings and the code each
// resolves to, as a fresh map.
//
// Exported for the same reason as Codes: a conformance check has to be able to
// assert that no alias has become a spelling the wire authority also declares.
// If that ever happened, one concept would parse to two different codes
// depending on which side wrote the message, which is the exact failure the
// canceled/cancelled split already caused once.
func CodeAliases() map[string]Code {
	aliases := make(map[string]Code, len(codeAliases))
	for spelling, code := range codeAliases {
		aliases[spelling] = code
	}
	return aliases
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

// ParseCode parses a canonical code or a recognized legacy spelling. Leading
// and trailing whitespace and ASCII letter case are normalized. Unrecognized
// values return CodeUnknown and an error.
func ParseCode(value string) (Code, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if code := Code(normalized); code.Valid() {
		return code, nil
	}
	if code, ok := codeAliases[normalized]; ok {
		return code, nil
	}
	return CodeUnknown, fmt.Errorf("faults: invalid code %q", value)
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
