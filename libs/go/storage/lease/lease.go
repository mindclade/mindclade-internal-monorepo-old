// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package lease

import (
	"strings"
	"time"

	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/identifiers"
)

const MaximumOwnerLength = 256

type Token struct{ value identifiers.UUID }

func NewToken() (Token, error) {
	uuid, err := identifiers.NewUUIDv4()
	if err != nil {
		return Token{}, faults.Wrap(err, faults.CodeInternal, "unable to generate lease token", faults.WithReason("lease_token_generation_failed"), faults.WithOperation("lease.NewToken"), faults.WithRetryPolicy(faults.BackoffRetry(3)))
	}
	return Token{value: uuid}, nil
}
func ParseToken(value string) (Token, error) {
	uuid, err := identifiers.ParseUUID(value)
	if err != nil || uuid.Version() != 4 {
		return Token{}, faults.Wrap(ErrInvalidLease, faults.CodeInvalidArgument, "invalid lease token", faults.WithReason("invalid_lease_token"), faults.WithOperation("lease.ParseToken"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return Token{value: uuid}, nil
}
func (token Token) String() string {
	if token.value.IsZero() {
		return ""
	}
	return token.value.String()
}
func (token Token) IsZero() bool           { return token.value.IsZero() }
func (token Token) Equal(other Token) bool { return token.value == other.value }

type Lease struct {
	Key        Key
	Token      Token
	Owner      string
	Version    uint64
	AcquiredAt time.Time
	ExpiresAt  time.Time
}

func (value Lease) Validate() error {
	if err := value.Key.Validate(); err != nil {
		return err
	}
	if value.Token.IsZero() || value.Version == 0 || strings.TrimSpace(value.Owner) == "" || len(value.Owner) > MaximumOwnerLength || !value.ExpiresAt.After(value.AcquiredAt) {
		return faults.Wrap(ErrInvalidLease, faults.CodeInvalidArgument, "invalid lease", faults.WithReason("invalid_lease"), faults.WithOperation("lease.Lease.Validate"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return nil
}
func (value Lease) Expired(now time.Time) bool { return !now.Before(value.ExpiresAt) }

type AcquireRequest struct {
	Key   Key
	Owner string
	TTL   time.Duration
}

func (request AcquireRequest) Validate() error {
	if err := request.Key.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(request.Owner) == "" || strings.TrimSpace(request.Owner) != request.Owner || len(request.Owner) > MaximumOwnerLength {
		return faults.Wrap(ErrInvalidOwner, faults.CodeInvalidArgument, "invalid lease owner", faults.WithReason("invalid_lease_owner"), faults.WithOperation("lease.AcquireRequest.Validate"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	if request.TTL <= 0 {
		return faults.Wrap(ErrInvalidLease, faults.CodeInvalidArgument, "invalid lease TTL", faults.WithReason("invalid_lease_ttl"), faults.WithOperation("lease.AcquireRequest.Validate"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return nil
}
