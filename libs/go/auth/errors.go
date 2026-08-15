// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package auth

import (
	"errors"

	"mindclade.internal/libs/go/faults"
)

var (
	ErrInvalidCredential     = errors.New("auth: invalid credential")
	ErrUnsupportedCredential = errors.New("auth: unsupported credential")
	ErrInvalidClaims         = errors.New("auth: invalid claims")
	ErrInvalidPrincipal      = errors.New("auth: invalid principal")
	ErrUnauthenticated       = errors.New("auth: unauthenticated")
	ErrInvalidPermission     = errors.New("auth: invalid permission")
	ErrInvalidResource       = errors.New("auth: invalid resource")
	ErrInvalidDecision       = errors.New("auth: invalid decision")
	ErrAuthorizationDenied   = errors.New("auth: authorization denied")
	ErrNilAuthenticator      = errors.New("auth: nil authenticator")
	ErrNilAuthorizer         = errors.New("auth: nil authorizer")
	ErrNilContext            = errors.New("auth: nil context")
)

func newFault(cause error, code faults.Code, message, reason, operation string, fields faults.Fields) error {
	return faults.Wrap(
		cause,
		code,
		message,
		faults.WithReason(reason),
		faults.WithOperation(operation),
		faults.WithFields(fields),
		faults.WithRetryPolicy(faults.NoRetry()),
	)
}

func preserveFault(err error, message, operation string) error {
	if err == nil {
		return nil
	}
	code := faults.CodeOf(err)
	if code == faults.CodeUnknown {
		code = faults.CodeInternal
		message = ""
	}
	return faults.Wrap(
		err,
		code,
		message,
		faults.WithReason(faults.ReasonOf(err)),
		faults.WithOperation(operation),
		faults.WithFields(faults.FieldsOf(err)),
		faults.WithRetryPolicy(faults.RetryPolicyOf(err)),
	)
}
