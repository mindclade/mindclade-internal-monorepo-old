// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package outbox

import (
	"time"

	coordination "mindclade.internal/libs/go/coordination/outbox"
	"mindclade.internal/libs/go/storage/lease"
)

type Record = coordination.Record
type Claim = coordination.Claim
type ClaimRequest = coordination.ClaimRequest

const (
	MaximumClaimBatch = coordination.MaximumClaimBatch
	MaximumAttempts   = coordination.MaximumAttempts
)

func NewClaim(message Message, token lease.Token, owner string, version uint64, attempts uint32, claimedAt, expiresAt time.Time) (Claim, error) {
	return coordination.NewClaim(message, token, owner, version, attempts, claimedAt, expiresAt)
}
