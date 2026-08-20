// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package coordination

import (
	"strings"
	"time"

	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
)

const MaximumOwnerLength = 256

// Claim is an immutable ownership grant for one durable resource. Fence must
// monotonically increase whenever ownership is acquired. Store mutations must
// compare resource ID, token, and fence so a stale process cannot commit after
// another process has reclaimed the resource.
type Claim struct {
	ResourceID identifiers.ID   `json:"resource_id"`
	Token      identifiers.UUID `json:"token"`
	Owner      string           `json:"owner"`
	Fence      uint64           `json:"fence"`
	AcquiredAt time.Time        `json:"acquired_at"`
	ExpiresAt  time.Time        `json:"expires_at"`
}

// NewClaim generates a cryptographically random ownership token.
func NewClaim(resourceID identifiers.ID, owner string, fence uint64, acquiredAt, expiresAt time.Time) (Claim, error) {
	token, err := identifiers.NewUUIDv4()
	if err != nil {
		return Claim{}, faults.Wrap(err, faults.CodeInternal, "unable to generate coordination claim token",
			faults.WithReason("coordination_claim_token_generation_failed"),
			faults.WithOperation("coordination.NewClaim"),
			faults.WithRetryPolicy(faults.BackoffRetry(3)),
		)
	}
	return ClaimFromToken(resourceID, token, owner, fence, acquiredAt, expiresAt)
}

// ClaimFromToken reconstructs a claim read from durable storage.
func ClaimFromToken(resourceID identifiers.ID, token identifiers.UUID, owner string, fence uint64, acquiredAt, expiresAt time.Time) (Claim, error) {
	value := Claim{
		ResourceID: resourceID,
		Token:      token,
		Owner:      strings.TrimSpace(owner),
		Fence:      fence,
		AcquiredAt: acquiredAt.Round(0).UTC(),
		ExpiresAt:  expiresAt.Round(0).UTC(),
	}
	if err := value.Validate(); err != nil {
		return Claim{}, err
	}
	return value, nil
}

func (claim Claim) IsZero() bool {
	return claim.ResourceID.IsZero() && claim.Token.IsZero() && claim.Owner == "" &&
		claim.Fence == 0 && claim.AcquiredAt.IsZero() && claim.ExpiresAt.IsZero()
}

func (claim Claim) Validate() error {
	if claim.ResourceID.IsZero() || claim.ResourceID.Validate() != nil ||
		claim.Token.IsZero() || claim.Token.Version() != 4 ||
		claim.Owner == "" || claim.Owner != strings.TrimSpace(claim.Owner) || len(claim.Owner) > MaximumOwnerLength ||
		claim.Fence == 0 || claim.AcquiredAt.IsZero() || !claim.ExpiresAt.After(claim.AcquiredAt) {
		return faults.Wrap(ErrInvalidClaim, faults.CodeInvalidArgument, "invalid coordination claim",
			faults.WithReason("invalid_coordination_claim"),
			faults.WithOperation("coordination.Claim.Validate"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	return nil
}

func (claim Claim) Expired(now time.Time) bool {
	return claim.ExpiresAt.IsZero() || !now.Before(claim.ExpiresAt)
}

func (claim Claim) OwnedBy(owner string) bool {
	return claim.Owner != "" && claim.Owner == strings.TrimSpace(owner)
}

// SameEpoch reports whether both claims identify the same ownership epoch.
func (claim Claim) SameEpoch(other Claim) bool {
	return claim.ResourceID == other.ResourceID && claim.Token == other.Token && claim.Fence == other.Fence
}
