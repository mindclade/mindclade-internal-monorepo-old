// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package memory

import (
	"context"
	"errors"
	"sync"

	"mindclade.internal/libs/go/clock"
	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/storage/cache"
)

const DefaultMaximumEntryBytes = 4 << 20

type Option func(*Store) error

func WithClock(value clock.Clock) Option {
	return func(store *Store) error {
		if value == nil {
			return errors.New("memory cache: nil clock")
		}
		store.clock = value
		return nil
	}
}
func WithMaximumEntryBytes(value int) Option {
	return func(store *Store) error {
		if value <= 0 {
			return errors.New("memory cache: maximum entry bytes must be positive")
		}
		store.maximumEntryBytes = value
		return nil
	}
}

type Store struct {
	mu                sync.Mutex
	clock             clock.Clock
	maximumEntryBytes int
	entries           map[cache.Key]cache.Entry
}

var _ cache.Store = (*Store)(nil)

func New(options ...Option) (*Store, error) {
	store := &Store{clock: clock.RealClock{}, maximumEntryBytes: DefaultMaximumEntryBytes, entries: make(map[cache.Key]cache.Entry)}
	for _, option := range options {
		if option != nil {
			if err := option(store); err != nil {
				return nil, err
			}
		}
	}
	return store, nil
}

func (store *Store) Get(ctx context.Context, key cache.Key) (cache.Entry, error) {
	if ctx == nil || store == nil {
		return cache.Entry{}, faults.New(faults.CodeInvalidArgument, "invalid cache get request", faults.WithReason("invalid_cache_get_request"), faults.WithOperation("storage.cache.memory.Get"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	if err := key.Validate(); err != nil {
		return cache.Entry{}, err
	}
	if err := ctx.Err(); err != nil {
		return cache.Entry{}, contextError(ctx, err, "storage.cache.memory.Get")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	entry, ok := store.entries[key]
	if ok && entry.Expired(store.clock.Now()) {
		delete(store.entries, key)
		ok = false
	}
	if !ok {
		return cache.Entry{}, faults.Wrap(cache.ErrMiss, faults.CodeNotFound, "cache entry not found", faults.WithReason("cache_miss"), faults.WithOperation("storage.cache.memory.Get"), faults.WithField("cache_key", key.String()), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return entry.Clone(), nil
}

func (store *Store) Set(ctx context.Context, key cache.Key, value []byte, options cache.SetOptions) (cache.Entry, error) {
	if ctx == nil || store == nil {
		return cache.Entry{}, faults.New(faults.CodeInvalidArgument, "invalid cache set request", faults.WithReason("invalid_cache_set_request"), faults.WithOperation("storage.cache.memory.Set"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	if err := key.Validate(); err != nil {
		return cache.Entry{}, err
	}
	if err := options.Validate(); err != nil {
		return cache.Entry{}, err
	}
	if err := ctx.Err(); err != nil {
		return cache.Entry{}, contextError(ctx, err, "storage.cache.memory.Set")
	}
	if len(value) > store.maximumEntryBytes {
		return cache.Entry{}, faults.Wrap(cache.ErrEntryTooLarge, faults.CodeResourceExhausted, "cache entry exceeds configured limit", faults.WithReason("cache_entry_too_large"), faults.WithOperation("storage.cache.memory.Set"), faults.WithField("maximum_bytes", store.maximumEntryBytes), faults.WithRetryPolicy(faults.NoRetry()))
	}
	now := store.clock.Now().Round(0)
	store.mu.Lock()
	defer store.mu.Unlock()
	current, exists := store.entries[key]
	if exists && current.Expired(now) {
		delete(store.entries, key)
		current = cache.Entry{}
		exists = false
	}
	if options.IfAbsent && exists {
		return cache.Entry{}, faults.Wrap(cache.ErrVersionMismatch, faults.CodeAlreadyExists, "cache entry already exists", faults.WithReason("cache_entry_exists"), faults.WithOperation("storage.cache.memory.Set"), faults.WithField("cache_key", key.String()), faults.WithRetryPolicy(faults.NoRetry()))
	}
	if expected := options.IfVersion; expected != nil && (!exists || current.Version != *expected) {
		return cache.Entry{}, faults.Wrap(cache.ErrVersionMismatch, faults.CodeConflict, "cache version does not match", faults.WithReason("cache_version_mismatch"), faults.WithOperation("storage.cache.memory.Set"), faults.WithField("cache_key", key.String()), faults.WithRetryPolicy(faults.NoRetry()))
	}
	version := uint64(1)
	if exists {
		if current.Version == ^uint64(0) {
			return cache.Entry{}, faults.New(
				faults.CodeDataLoss,
				"cache entry version is exhausted",
				faults.WithReason("cache_version_exhausted"),
				faults.WithOperation("storage.cache.memory.Set"),
				faults.WithField("cache_key", key.String()),
				faults.WithContextMetadata(ctx),
				faults.WithRetryPolicy(faults.NoRetry()),
			)
		}
		version = current.Version + 1
	}
	entry := cache.Entry{Key: key, Value: append([]byte(nil), value...), Version: version}
	if options.TTL > 0 {
		entry.ExpiresAt = now.Add(options.TTL)
	}
	store.entries[key] = entry
	return entry.Clone(), nil
}

func (store *Store) Delete(ctx context.Context, key cache.Key, options cache.DeleteOptions) error {
	if ctx == nil || store == nil {
		return faults.New(faults.CodeInvalidArgument, "invalid cache delete request", faults.WithReason("invalid_cache_delete_request"), faults.WithOperation("storage.cache.memory.Delete"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	if err := key.Validate(); err != nil {
		return err
	}
	if err := options.Validate(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return contextError(ctx, err, "storage.cache.memory.Delete")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	current, exists := store.entries[key]
	if exists && current.Expired(store.clock.Now()) {
		delete(store.entries, key)
		exists = false
	}
	if !exists {
		return faults.Wrap(cache.ErrMiss, faults.CodeNotFound, "cache entry not found", faults.WithReason("cache_miss"), faults.WithOperation("storage.cache.memory.Delete"), faults.WithField("cache_key", key.String()), faults.WithRetryPolicy(faults.NoRetry()))
	}
	if expected := options.IfVersion; expected != nil && current.Version != *expected {
		return faults.Wrap(cache.ErrVersionMismatch, faults.CodeConflict, "cache version does not match", faults.WithReason("cache_version_mismatch"), faults.WithOperation("storage.cache.memory.Delete"), faults.WithField("cache_key", key.String()), faults.WithRetryPolicy(faults.NoRetry()))
	}
	delete(store.entries, key)
	return nil
}

func contextError(ctx context.Context, cause error, operation string) error {
	code := faults.CodeCanceled
	message := "cache operation canceled"
	reason := "cache_operation_canceled"
	if errors.Is(cause, context.DeadlineExceeded) {
		code = faults.CodeDeadlineExceeded
		message = "cache operation exceeded its deadline"
		reason = "cache_operation_deadline_exceeded"
	}
	return faults.Wrap(cause, code, message,
		faults.WithReason(reason),
		faults.WithOperation(operation),
		faults.WithContextMetadata(ctx),
		faults.WithRetryPolicy(faults.NoRetry()),
	)
}
