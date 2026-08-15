// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package outbox

import (
	"context"
	"time"
)

// Store persists messages and owns atomic fenced claim transitions.
// Append implementations backed by SQL must use any transaction carried by
// storage/sql/transaction in ctx.
type Store interface {
	Append(context.Context, Message) error
	Claim(context.Context, ClaimRequest) ([]Claim, error)
	Renew(context.Context, Claim, time.Duration) (Claim, error)
	MarkPublished(context.Context, Claim, time.Time) error
	Reschedule(context.Context, Claim, time.Time, string) error
	DeadLetter(context.Context, Claim, time.Time, string) error
	Lookup(context.Context, string) (Record, error)
}

type State string

const (
	StatePending    State = "pending"
	StateClaimed    State = "claimed"
	StatePublished  State = "published"
	StateDeadLetter State = "dead_letter"
)

func (state State) Valid() bool {
	switch state {
	case StatePending, StateClaimed, StatePublished, StateDeadLetter:
		return true
	default:
		return false
	}
}

type Record struct {
	Message      Message
	State        State
	Version      uint64
	Attempts     uint32
	ClaimOwner   string
	ClaimToken   string
	ClaimedAt    time.Time
	ClaimExpires time.Time
	PublishedAt  time.Time
	DeadAt       time.Time
	LastError    string
}

func (value Record) Validate() error {
	if err := value.Message.Validate(); err != nil || !value.State.Valid() || value.Version == 0 || value.Attempts > MaximumAttempts {
		return ErrInvalidMessage
	}
	return nil
}
