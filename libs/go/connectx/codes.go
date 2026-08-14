// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package connectx

import (
	"connectrpc.com/connect"

	"mindclade.internal/libs/go/faults"
)

// CodeFromFault maps a transport-neutral Mindclade code to Connect's canonical
// code space.
func CodeFromFault(code faults.Code) connect.Code {
	switch faults.NormalizeCode(code) {
	case faults.CodeCanceled:
		return connect.CodeCanceled
	case faults.CodeInvalidArgument:
		return connect.CodeInvalidArgument
	case faults.CodeDeadlineExceeded:
		return connect.CodeDeadlineExceeded
	case faults.CodeNotFound:
		return connect.CodeNotFound
	case faults.CodeAlreadyExists:
		return connect.CodeAlreadyExists
	case faults.CodePermissionDenied:
		return connect.CodePermissionDenied
	case faults.CodeUnauthenticated:
		return connect.CodeUnauthenticated
	case faults.CodeResourceExhausted:
		return connect.CodeResourceExhausted
	case faults.CodeFailedPrecondition:
		return connect.CodeFailedPrecondition
	case faults.CodeConflict, faults.CodeAborted:
		return connect.CodeAborted
	case faults.CodeOutOfRange:
		return connect.CodeOutOfRange
	case faults.CodeNotImplemented:
		return connect.CodeUnimplemented
	case faults.CodeInternal:
		return connect.CodeInternal
	case faults.CodeUnavailable:
		return connect.CodeUnavailable
	case faults.CodeDataLoss:
		return connect.CodeDataLoss
	default:
		return connect.CodeUnknown
	}
}

// FaultCode maps a Connect code into Mindclade's canonical code space.
func FaultCode(code connect.Code) faults.Code {
	switch code {
	case connect.CodeCanceled:
		return faults.CodeCanceled
	case connect.CodeInvalidArgument:
		return faults.CodeInvalidArgument
	case connect.CodeDeadlineExceeded:
		return faults.CodeDeadlineExceeded
	case connect.CodeNotFound:
		return faults.CodeNotFound
	case connect.CodeAlreadyExists:
		return faults.CodeAlreadyExists
	case connect.CodePermissionDenied:
		return faults.CodePermissionDenied
	case connect.CodeUnauthenticated:
		return faults.CodeUnauthenticated
	case connect.CodeResourceExhausted:
		return faults.CodeResourceExhausted
	case connect.CodeFailedPrecondition:
		return faults.CodeFailedPrecondition
	case connect.CodeAborted:
		return faults.CodeAborted
	case connect.CodeOutOfRange:
		return faults.CodeOutOfRange
	case connect.CodeUnimplemented:
		return faults.CodeNotImplemented
	case connect.CodeInternal:
		return faults.CodeInternal
	case connect.CodeUnavailable:
		return faults.CodeUnavailable
	case connect.CodeDataLoss:
		return faults.CodeDataLoss
	default:
		return faults.CodeUnknown
	}
}
