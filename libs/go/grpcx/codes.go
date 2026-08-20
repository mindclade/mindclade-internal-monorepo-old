// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package grpcx

import (
	"go.mindclade.dev/libs/go/faults"
	"google.golang.org/grpc/codes"
)

func CodeFromFault(code faults.Code) codes.Code {
	switch faults.NormalizeCode(code) {
	case faults.CodeCanceled:
		return codes.Canceled
	case faults.CodeInvalidArgument:
		return codes.InvalidArgument
	case faults.CodeDeadlineExceeded:
		return codes.DeadlineExceeded
	case faults.CodeNotFound:
		return codes.NotFound
	case faults.CodeAlreadyExists:
		return codes.AlreadyExists
	case faults.CodePermissionDenied:
		return codes.PermissionDenied
	case faults.CodeUnauthenticated:
		return codes.Unauthenticated
	case faults.CodeResourceExhausted:
		return codes.ResourceExhausted
	case faults.CodeFailedPrecondition:
		return codes.FailedPrecondition
	case faults.CodeConflict, faults.CodeAborted:
		return codes.Aborted
	case faults.CodeOutOfRange:
		return codes.OutOfRange
	case faults.CodeNotImplemented:
		return codes.Unimplemented
	case faults.CodeInternal:
		return codes.Internal
	case faults.CodeUnavailable:
		return codes.Unavailable
	case faults.CodeDataLoss:
		return codes.DataLoss
	default:
		return codes.Unknown
	}
}
func FaultCode(code codes.Code) faults.Code {
	switch code {
	case codes.Canceled:
		return faults.CodeCanceled
	case codes.InvalidArgument:
		return faults.CodeInvalidArgument
	case codes.DeadlineExceeded:
		return faults.CodeDeadlineExceeded
	case codes.NotFound:
		return faults.CodeNotFound
	case codes.AlreadyExists:
		return faults.CodeAlreadyExists
	case codes.PermissionDenied:
		return faults.CodePermissionDenied
	case codes.Unauthenticated:
		return faults.CodeUnauthenticated
	case codes.ResourceExhausted:
		return faults.CodeResourceExhausted
	case codes.FailedPrecondition:
		return faults.CodeFailedPrecondition
	case codes.Aborted:
		return faults.CodeAborted
	case codes.OutOfRange:
		return faults.CodeOutOfRange
	case codes.Unimplemented:
		return faults.CodeNotImplemented
	case codes.Internal:
		return faults.CodeInternal
	case codes.Unavailable:
		return faults.CodeUnavailable
	case codes.DataLoss:
		return faults.CodeDataLoss
	default:
		return faults.CodeUnknown
	}
}
