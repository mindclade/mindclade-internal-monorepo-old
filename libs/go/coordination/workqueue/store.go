// Copyright 2026 Mindclade. All rights reserved.
package workqueue

import (
	"context"
	"mindclade.internal/libs/go/identifiers"
	"time"
)

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
}
