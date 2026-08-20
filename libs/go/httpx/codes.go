// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package httpx

import (
	"net/http"

	"go.mindclade.dev/libs/go/faults"
)

// StatusFromCode maps transport-neutral fault codes to HTTP status codes.
func StatusFromCode(code faults.Code) int {
	switch faults.NormalizeCode(code) {
	case faults.CodeCanceled:
		return http.StatusRequestTimeout
	case faults.CodeInvalidArgument, faults.CodeOutOfRange:
		return http.StatusBadRequest
	case faults.CodeDeadlineExceeded:
		return http.StatusGatewayTimeout
	case faults.CodeNotFound:
		return http.StatusNotFound
	case faults.CodeAlreadyExists, faults.CodeConflict, faults.CodeAborted:
		return http.StatusConflict
	case faults.CodePermissionDenied:
		return http.StatusForbidden
	case faults.CodeUnauthenticated:
		return http.StatusUnauthorized
	case faults.CodeResourceExhausted:
		return http.StatusTooManyRequests
	case faults.CodeFailedPrecondition:
		return http.StatusPreconditionFailed
	case faults.CodeNotImplemented:
		return http.StatusNotImplemented
	case faults.CodeUnavailable:
		return http.StatusServiceUnavailable
	case faults.CodeDataLoss, faults.CodeInternal, faults.CodeUnknown:
		return http.StatusInternalServerError
	default:
		return http.StatusInternalServerError
	}
}

// CodeFromStatus maps HTTP status codes back to the closest fault code.
func CodeFromStatus(status int) faults.Code {
	switch status {
	case http.StatusRequestTimeout:
		return faults.CodeCanceled
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return faults.CodeInvalidArgument
	case http.StatusGatewayTimeout:
		return faults.CodeDeadlineExceeded
	case http.StatusNotFound:
		return faults.CodeNotFound
	case http.StatusConflict:
		return faults.CodeConflict
	case http.StatusForbidden:
		return faults.CodePermissionDenied
	case http.StatusUnauthorized:
		return faults.CodeUnauthenticated
	case http.StatusTooManyRequests:
		return faults.CodeResourceExhausted
	case http.StatusPreconditionFailed:
		return faults.CodeFailedPrecondition
	case http.StatusNotImplemented:
		return faults.CodeNotImplemented
	case http.StatusBadGateway, http.StatusServiceUnavailable:
		return faults.CodeUnavailable
	case http.StatusInternalServerError:
		return faults.CodeInternal
	default:
		if status >= 400 && status < 500 {
			return faults.CodeInvalidArgument
		}
		return faults.CodeUnknown
	}
}
