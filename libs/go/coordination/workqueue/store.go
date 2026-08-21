// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package workqueue

import (
	"context"
	"strings"
	"time"

	"go.mindclade.dev/libs/go/identifiers"
)

// MaximumPruneLimit caps terminal records removed by one store operation.
const MaximumPruneLimit = 1000

// PruneRequest bounds deletion to old terminal records in one queue. Pending
// and leased work is never eligible for retention pruning. Deleting a terminal
// record also removes its duplicate-ID tombstone, so CompletedBefore must be
// later than the producer's retry/replay horizon unless the effect is
// independently idempotent.
type PruneRequest struct {
	Queue           string
	CompletedBefore time.Time
	Limit           int
}

func (request PruneRequest) Validate() error {
	if request.Queue == "" || request.Queue != strings.TrimSpace(request.Queue) ||
		len(request.Queue) > MaximumQueueBytes || request.CompletedBefore.IsZero() ||
		request.Limit <= 0 || request.Limit > MaximumPruneLimit {
		return invalid("invalid_work_prune_request", "invalid work prune request", "workqueue.PruneRequest.Validate")
	}
	return nil
}

type ClaimRequest struct {
	Owner         string
	Queues        []string
	Limit         int
	LeaseDuration time.Duration
}

func (request ClaimRequest) Validate() error {
	if request.Owner == "" || request.Limit <= 0 || request.Limit > 1000 || request.LeaseDuration <= 0 {
		return invalid("invalid_work_claim_request", "invalid work claim request", "workqueue.ClaimRequest.Validate")
	}
	for _, q := range request.Queues {
		if q == "" || len(q) > MaximumQueueBytes {
			return invalid("invalid_work_queue", "invalid work queue", "workqueue.ClaimRequest.Validate")
		}
	}
	return nil
}

type Failure struct {
	Reason   string
	RetryAt  time.Time
	Terminal bool
}

func (failure Failure) Validate() error {
	if failure.Reason == "" || len(failure.Reason) > MaximumReasonBytes || !failure.Terminal && failure.RetryAt.IsZero() {
		return invalid("invalid_work_failure", "invalid work failure", "workqueue.Failure.Validate")
	}
	return nil
}

type Store interface {
	Enqueue(context.Context, Item) error
	Claim(context.Context, ClaimRequest) ([]Claim, error)
	Renew(context.Context, Claim, time.Duration) (Claim, error)
	Complete(context.Context, Claim, Result, time.Time) error
	Fail(context.Context, Claim, Failure, time.Time) error
	Cancel(context.Context, identifiers.ID, string, time.Time) error
	Lookup(context.Context, identifiers.ID) (Record, error)
	PruneTerminal(context.Context, PruneRequest) (int, error)
}
