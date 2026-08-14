// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package memory

import (
	"context"
	"errors"
	"sync"

	"mindclade.internal/libs/go/coordination/cursor"
	"mindclade.internal/libs/go/faults"
)

type Store struct {
	mu     sync.RWMutex
	values map[cursor.Key]cursor.Cursor
}

func New() *Store { return &Store{values: make(map[cursor.Key]cursor.Cursor)} }
func (store *Store) Load(ctx context.Context, key cursor.Key) (cursor.Cursor, error) {
	if ctx == nil || store == nil || key.Validate() != nil {
		return cursor.Cursor{}, invalid(ctx)
	}
	store.mu.RLock()
	value, ok := store.values[key]
	store.mu.RUnlock()
	if !ok {
		return cursor.Cursor{}, notFound(ctx, key)
	}
	return value.Clone(), nil
}
func (store *Store) Advance(ctx context.Context, request cursor.AdvanceRequest) (cursor.Cursor, error) {
	if ctx == nil || store == nil {
		return cursor.Cursor{}, invalid(ctx)
	}
	if err := request.Validate(); err != nil {
		return cursor.Cursor{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	current, exists := store.values[request.Key]
	if !exists {
		if request.ExpectedVersion != 0 {
			return cursor.Cursor{}, conflict(ctx, request.Key)
		}
		value, err := cursor.New(request.Key, request.Sequence, request.Opaque, request.Fence, 1, request.UpdatedAt)
		if err != nil {
			return cursor.Cursor{}, err
		}
		store.values[request.Key] = value
		return value.Clone(), nil
	}
	if current.Version != request.ExpectedVersion {
		return cursor.Cursor{}, conflict(ctx, request.Key)
	}
	if request.Fence < current.Fence {
		return cursor.Cursor{}, stale(ctx, request.Key)
	}
	if request.Sequence < current.Sequence {
		return cursor.Cursor{}, regression(ctx, request.Key)
	}
	value, err := cursor.New(request.Key, request.Sequence, request.Opaque, request.Fence, current.Version+1, request.UpdatedAt)
	if err != nil {
		return cursor.Cursor{}, err
	}
	store.values[request.Key] = value
	return value.Clone(), nil
}
func (store *Store) Delete(ctx context.Context, key cursor.Key, expected uint64) error {
	if ctx == nil || store == nil || key.Validate() != nil || expected == 0 {
		return invalid(ctx)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	value, ok := store.values[key]
	if !ok {
		return notFound(ctx, key)
	}
	if value.Version != expected {
		return conflict(ctx, key)
	}
	delete(store.values, key)
	return nil
}
func invalid(ctx context.Context) error {
	return faults.Wrap(cursor.ErrInvalidRequest, faults.CodeInvalidArgument, "invalid cursor store request", faults.WithReason("invalid_cursor_store_request"), faults.WithOperation("cursor.memory"), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
}
func notFound(ctx context.Context, key cursor.Key) error {
	return faults.Wrap(cursor.ErrNotFound, faults.CodeNotFound, "cursor not found", faults.WithReason("cursor_not_found"), faults.WithOperation("cursor.memory"), faults.WithField("cursor", key.String()), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
}
func conflict(ctx context.Context, key cursor.Key) error {
	return faults.Wrap(cursor.ErrConflict, faults.CodeAborted, "cursor compare-and-swap conflict", faults.WithReason("cursor_conflict"), faults.WithOperation("cursor.memory"), faults.WithField("cursor", key.String()), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.ImmediateRetry(3)))
}
func stale(ctx context.Context, key cursor.Key) error {
	return faults.Wrap(cursor.ErrStaleFence, faults.CodeAborted, "cursor fencing token is stale", faults.WithReason("cursor_stale_fence"), faults.WithOperation("cursor.memory"), faults.WithField("cursor", key.String()), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
}
func regression(ctx context.Context, key cursor.Key) error {
	return faults.Wrap(cursor.ErrRegression, faults.CodeFailedPrecondition, "cursor sequence cannot regress", faults.WithReason("cursor_regression"), faults.WithOperation("cursor.memory"), faults.WithField("cursor", key.String()), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
}

var _ = errors.Is
