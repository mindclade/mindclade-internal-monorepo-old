// Copyright 2026 Mindclade. All rights reserved.
package memory

import (
	"context"
	"errors"
	"mindclade.internal/libs/go/coordination"
	"mindclade.internal/libs/go/coordination/workqueue"
	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/identifiers"
	"sort"
	"sync"
	"time"
)

type entry struct {
	record workqueue.Record
	claim  coordination.Claim
}
type Store struct {
	mu     sync.Mutex
	values map[identifiers.ID]entry
}

func New() *Store { return &Store{values: make(map[identifiers.ID]entry)} }
func (store *Store) Enqueue(ctx context.Context, item workqueue.Item) error {
	if ctx == nil || store == nil {
		return invalid(ctx)
	}
	if err := item.Validate(); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, ok := store.values[item.ID]; ok {
		return exists(ctx, item.ID)
	}
	store.values[item.ID] = entry{record: workqueue.Record{Item: item.Clone(), State: workqueue.StatePending, UpdatedAt: item.CreatedAt}}
	return nil
}
func (store *Store) Claim(ctx context.Context, request workqueue.ClaimRequest) ([]workqueue.Claim, error) {
	if ctx == nil || store == nil {
		return nil, invalid(ctx)
	}
	if err := request.Validate(); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	allowed := map[string]bool{}
	for _, q := range request.Queues {
		allowed[q] = true
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	ids := make([]identifiers.ID, 0, len(store.values))
	for id := range store.values {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		a, b := store.values[ids[i]].record, store.values[ids[j]].record
		if a.Item.Priority != b.Item.Priority {
			return a.Item.Priority > b.Item.Priority
		}
		if !a.Item.AvailableAt.Equal(b.Item.AvailableAt) {
			return a.Item.AvailableAt.Before(b.Item.AvailableAt)
		}
		return a.Item.ID.Less(b.Item.ID)
	})
	result := make([]workqueue.Claim, 0, request.Limit)
	for _, id := range ids {
		if len(result) >= request.Limit {
			break
		}
		value := store.values[id]
		if len(allowed) > 0 && !allowed[value.record.Item.Queue] {
			continue
		}
		eligible := value.record.State == workqueue.StatePending && !value.record.Item.AvailableAt.After(now) || value.record.State == workqueue.StateLeased && value.claim.Expired(now)
		if !eligible {
			continue
		}
		fence := value.record.Fence + 1
		if fence == 0 {
			fence = 1
		}
		ownership, err := coordination.NewClaim(id, request.Owner, fence, now, now.Add(request.LeaseDuration))
		if err != nil {
			return nil, err
		}
		value.record.State = workqueue.StateLeased
		value.record.Attempts++
		value.record.Fence = fence
		value.record.UpdatedAt = now
		value.claim = ownership
		store.values[id] = value
		result = append(result, workqueue.Claim{Record: value.record.Clone(), Ownership: ownership})
	}
	return result, nil
}
func (store *Store) Renew(ctx context.Context, claim workqueue.Claim, duration time.Duration) (workqueue.Claim, error) {
	if ctx == nil || store == nil || duration <= 0 {
		return workqueue.Claim{}, invalid(ctx)
	}
	if err := claim.Validate(); err != nil {
		return workqueue.Claim{}, err
	}
	now := time.Now().UTC()
	store.mu.Lock()
	defer store.mu.Unlock()
	value, ok := store.values[claim.Record.Item.ID]
	if !ok {
		return workqueue.Claim{}, notFound(ctx, claim.Record.Item.ID)
	}
	if !value.claim.SameEpoch(claim.Ownership) || value.claim.Expired(now) {
		return workqueue.Claim{}, lost(ctx, claim.Record.Item.ID)
	}
	next, err := coordination.ClaimFromToken(value.claim.ResourceID, value.claim.Token, value.claim.Owner, value.claim.Fence, value.claim.AcquiredAt, now.Add(duration))
	if err != nil {
		return workqueue.Claim{}, err
	}
	value.claim = next
	value.record.UpdatedAt = now
	store.values[claim.Record.Item.ID] = value
	return workqueue.Claim{Record: value.record.Clone(), Ownership: next}, nil
}
func (store *Store) Complete(ctx context.Context, claim workqueue.Claim, result workqueue.Result, at time.Time) error {
	if err := result.Validate(); err != nil {
		return err
	}
	return store.finish(ctx, claim, at, func(record *workqueue.Record) {
		record.State = workqueue.StateCompleted
		record.Result = result
		record.CompletedAt = at
		record.LastError = ""
	})
}
func (store *Store) Fail(ctx context.Context, claim workqueue.Claim, failure workqueue.Failure, at time.Time) error {
	if err := failure.Validate(); err != nil {
		return err
	}
	return store.finish(ctx, claim, at, func(record *workqueue.Record) {
		record.LastError = failure.Reason
		if failure.Terminal {
			record.State = workqueue.StateFailed
			record.CompletedAt = at
		} else {
			record.State = workqueue.StatePending
			record.Item.AvailableAt = failure.RetryAt
		}
	})
}
func (store *Store) finish(ctx context.Context, claim workqueue.Claim, at time.Time, apply func(*workqueue.Record)) error {
	if ctx == nil || store == nil || at.IsZero() {
		return invalid(ctx)
	}
	if err := claim.Validate(); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	value, ok := store.values[claim.Record.Item.ID]
	if !ok {
		return notFound(ctx, claim.Record.Item.ID)
	}
	if !value.claim.SameEpoch(claim.Ownership) || value.claim.Expired(at) {
		return lost(ctx, claim.Record.Item.ID)
	}
	apply(&value.record)
	value.record.UpdatedAt = at
	value.claim = coordination.Claim{}
	store.values[claim.Record.Item.ID] = value
	return nil
}
func (store *Store) Cancel(ctx context.Context, id identifiers.ID, reason string, at time.Time) error {
	if ctx == nil || store == nil || id.IsZero() || reason == "" || at.IsZero() {
		return invalid(ctx)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	value, ok := store.values[id]
	if !ok {
		return notFound(ctx, id)
	}
	if value.record.State.Terminal() {
		return terminal(ctx, id)
	}
	value.record.State = workqueue.StateCancelled
	value.record.CompletedAt = at
	value.record.UpdatedAt = at
	value.record.LastError = reason
	value.claim = coordination.Claim{}
	store.values[id] = value
	return nil
}
func (store *Store) Lookup(ctx context.Context, id identifiers.ID) (workqueue.Record, error) {
	if ctx == nil || store == nil || id.IsZero() {
		return workqueue.Record{}, invalid(ctx)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	value, ok := store.values[id]
	if !ok {
		return workqueue.Record{}, notFound(ctx, id)
	}
	return value.record.Clone(), nil
}
func invalid(ctx context.Context) error {
	return faults.Wrap(workqueue.ErrInvalidRequest, faults.CodeInvalidArgument, "invalid in-memory work request", faults.WithReason("invalid_work_request"), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
}
func exists(ctx context.Context, id identifiers.ID) error {
	return faults.Wrap(workqueue.ErrAlreadyExists, faults.CodeAlreadyExists, "work item already exists", faults.WithReason("work_item_exists"), faults.WithField("work_item_id", id.String()), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
}
func notFound(ctx context.Context, id identifiers.ID) error {
	return faults.Wrap(workqueue.ErrNotFound, faults.CodeNotFound, "work item not found", faults.WithReason("work_item_not_found"), faults.WithField("work_item_id", id.String()), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
}
func lost(ctx context.Context, id identifiers.ID) error {
	return faults.Wrap(workqueue.ErrLeaseLost, faults.CodeAborted, "work item lease was lost", faults.WithReason("work_lease_lost"), faults.WithField("work_item_id", id.String()), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
}
func terminal(ctx context.Context, id identifiers.ID) error {
	return faults.Wrap(workqueue.ErrTerminal, faults.CodeFailedPrecondition, "work item is terminal", faults.WithReason("work_item_terminal"), faults.WithField("work_item_id", id.String()), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
}

var _ = errors.Is
