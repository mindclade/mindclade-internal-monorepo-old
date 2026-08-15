// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package outbox

import (
	"strings"
	"time"

	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/storage/lease"
)

const (
	MaximumClaimBatch = 1000
	MaximumAttempts   = 1000000
)

type Claim struct {
	message   Message
	token     lease.Token
	owner     string
	version   uint64
	attempts  uint32
	claimedAt time.Time
	expiresAt time.Time
}

func NewClaim(message Message, token lease.Token, owner string, version uint64, attempts uint32, claimedAt, expiresAt time.Time) (Claim, error) {
	value := Claim{message: message, token: token, owner: owner, version: version, attempts: attempts, claimedAt: claimedAt.Round(0).UTC(), expiresAt: expiresAt.Round(0).UTC()}
	if err := value.Validate(); err != nil {
		return Claim{}, err
	}
	return value, nil
}

func (value Claim) Message() Message           { return value.message }
func (value Claim) Token() lease.Token         { return value.token }
func (value Claim) Owner() string              { return value.owner }
func (value Claim) Version() uint64            { return value.version }
func (value Claim) Attempts() uint32           { return value.attempts }
func (value Claim) ClaimedAt() time.Time       { return value.claimedAt }
func (value Claim) ExpiresAt() time.Time       { return value.expiresAt }
func (value Claim) Expired(now time.Time) bool { return !now.Before(value.expiresAt) }

func (value Claim) Validate() error {
	if err := value.message.Validate(); err != nil || value.token.IsZero() || value.version == 0 || value.attempts == 0 || value.attempts > MaximumAttempts || strings.TrimSpace(value.owner) == "" || strings.TrimSpace(value.owner) != value.owner || len(value.owner) > lease.MaximumOwnerLength || value.claimedAt.IsZero() || !value.expiresAt.After(value.claimedAt) {
		return faults.Wrap(ErrInvalidClaim, faults.CodeInvalidArgument, "invalid outbox claim", faults.WithReason(ReasonInvalidClaim), faults.WithOperation("outbox.Claim.Validate"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return nil
}

type ClaimRequest struct {
	Owner         string
	Topics        []string
	Limit         int
	LeaseDuration time.Duration
}

func (request ClaimRequest) Normalized() ClaimRequest {
	request.Owner = strings.TrimSpace(request.Owner)
	if request.Limit == 0 {
		request.Limit = 100
	}
	if request.LeaseDuration == 0 {
		request.LeaseDuration = time.Minute
	}
	captured := make([]string, 0, len(request.Topics))
	seen := map[string]struct{}{}
	for _, topic := range request.Topics {
		topic = strings.TrimSpace(topic)
		if _, exists := seen[topic]; !exists {
			captured = append(captured, topic)
			seen[topic] = struct{}{}
		}
	}
	request.Topics = captured
	return request
}

func (request ClaimRequest) Validate() error {
	request = request.Normalized()
	if request.Owner == "" || len(request.Owner) > lease.MaximumOwnerLength || request.Limit <= 0 || request.Limit > MaximumClaimBatch || request.LeaseDuration <= 0 {
		return faults.Wrap(ErrInvalidRequest, faults.CodeInvalidArgument, "invalid outbox claim request", faults.WithReason("invalid_outbox_claim_request"), faults.WithOperation("outbox.ClaimRequest.Validate"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	for _, topic := range request.Topics {
		if !validToken(topic, MaximumTopicLength) {
			return faults.Wrap(ErrInvalidRequest, faults.CodeInvalidArgument, "invalid outbox claim topic", faults.WithReason("invalid_outbox_claim_topic"), faults.WithOperation("outbox.ClaimRequest.Validate"), faults.WithRetryPolicy(faults.NoRetry()))
		}
	}
	return nil
}
