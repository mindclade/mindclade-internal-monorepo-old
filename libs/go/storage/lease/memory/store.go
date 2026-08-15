// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package memory

import (
	"context"
	"errors"
	"sync"
	"time"

	"mindclade.internal/libs/go/clock"
	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/storage/lease"
)

type Option func(*Store) error

func WithClock(value clock.Clock) Option {
	return func(store *Store) error {
		if value == nil {
			return errors.New("memory lease: nil clock")
		}
		store.clock = value
		return nil
	}
}
func WithTokenGenerator(generator func() (lease.Token, error)) Option {
	return func(store *Store) error {
		if generator == nil {
			return errors.New("memory lease: nil token generator")
		}
		store.generateToken = generator
		return nil
	}
}

type Store struct {
	mu            sync.Mutex
	clock         clock.Clock
	generateToken func() (lease.Token, error)
	leases        map[lease.Key]lease.Lease
}

var _ lease.Store = (*Store)(nil)

func New(options ...Option) (*Store, error) {
	store := &Store{clock: clock.RealClock{}, generateToken: lease.NewToken, leases: make(map[lease.Key]lease.Lease)}
	for _, option := range options {
		if option != nil {
			if err := option(store); err != nil {
				return nil, err
			}
		}
	}
	return store, nil
}

func (store *Store) Acquire(ctx context.Context, request lease.AcquireRequest) (lease.Lease, error) {
	if ctx == nil || store == nil {
		return lease.Lease{}, faults.New(faults.CodeInvalidArgument, "invalid lease acquire request", faults.WithReason("invalid_lease_acquire_request"), faults.WithOperation("storage.lease.memory.Acquire"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	if err := request.Validate(); err != nil {
		return lease.Lease{}, err
	}
	if err := ctx.Err(); err != nil {
		return lease.Lease{}, contextError(ctx, err, "storage.lease.memory.Acquire")
	}
	now := store.clock.Now().Round(0)
	store.mu.Lock()
	defer store.mu.Unlock()
	current, exists := store.leases[request.Key]
	if exists && !current.Expired(now) {
		delay := current.ExpiresAt.Sub(now)
		return lease.Lease{}, faults.Wrap(lease.ErrHeld, faults.CodeConflict, "lease is already held", faults.WithReason("lease_held"), faults.WithOperation("storage.lease.memory.Acquire"), faults.WithField("lease_key", request.Key.String()), faults.WithRetryPolicy(faults.DelayedRetry(delay, 0)))
	}
	token, err := store.generateToken()
	if err != nil {
		return lease.Lease{}, faults.Wrap(err, faults.CodeInternal, "unable to generate lease token",
			faults.WithReason("lease_token_generation_failed"),
			faults.WithOperation("storage.lease.memory.Acquire"),
			faults.WithContextMetadata(ctx),
			faults.WithRetryPolicy(faults.BackoffRetry(3)),
		)
	}
	if token.IsZero() {
		return lease.Lease{}, faults.New(faults.CodeInternal, "lease token generator returned an empty token",
			faults.WithReason("empty_lease_token"),
			faults.WithOperation("storage.lease.memory.Acquire"),
			faults.WithContextMetadata(ctx),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	version := uint64(1)
	if exists {
		version = current.Version + 1
	}
	value := lease.Lease{Key: request.Key, Token: token, Owner: request.Owner, Version: version, AcquiredAt: now, ExpiresAt: now.Add(request.TTL)}
	store.leases[request.Key] = value
	return value, nil
}

func (store *Store) Renew(ctx context.Context, current lease.Lease, ttl time.Duration) (lease.Lease, error) {
	if ctx == nil || store == nil || ttl <= 0 {
		return lease.Lease{}, faults.New(faults.CodeInvalidArgument, "invalid lease renew request", faults.WithReason("invalid_lease_renew_request"), faults.WithOperation("storage.lease.memory.Renew"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	if err := current.Validate(); err != nil {
		return lease.Lease{}, err
	}
	if err := ctx.Err(); err != nil {
		return lease.Lease{}, contextError(ctx, err, "storage.lease.memory.Renew")
	}
	now := store.clock.Now().Round(0)
	store.mu.Lock()
	defer store.mu.Unlock()
	stored, exists := store.leases[current.Key]
	if !exists {
		return lease.Lease{}, faults.Wrap(lease.ErrNotFound, faults.CodeNotFound, "lease not found", faults.WithReason("lease_not_found"), faults.WithOperation("storage.lease.memory.Renew"), faults.WithField("lease_key", current.Key.String()), faults.WithRetryPolicy(faults.NoRetry()))
	}
	if stored.Expired(now) || !stored.Token.Equal(current.Token) || stored.Version != current.Version {
		return lease.Lease{}, faults.Wrap(lease.ErrStale, faults.CodeConflict, "lease ownership is stale", faults.WithReason("stale_lease"), faults.WithOperation("storage.lease.memory.Renew"), faults.WithField("lease_key", current.Key.String()), faults.WithRetryPolicy(faults.NoRetry()))
	}
	stored.Version++
	stored.ExpiresAt = now.Add(ttl)
	store.leases[current.Key] = stored
	return stored, nil
}

func (store *Store) Release(ctx context.Context, current lease.Lease) error {
	if ctx == nil || store == nil {
		return faults.New(faults.CodeInvalidArgument, "invalid lease release request", faults.WithReason("invalid_lease_release_request"), faults.WithOperation("storage.lease.memory.Release"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	if err := current.Validate(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return contextError(ctx, err, "storage.lease.memory.Release")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	stored, exists := store.leases[current.Key]
	if !exists {
		return faults.Wrap(lease.ErrNotFound, faults.CodeNotFound, "lease not found", faults.WithReason("lease_not_found"), faults.WithOperation("storage.lease.memory.Release"), faults.WithField("lease_key", current.Key.String()), faults.WithRetryPolicy(faults.NoRetry()))
	}
	if !stored.Token.Equal(current.Token) || stored.Version != current.Version {
		return faults.Wrap(lease.ErrStale, faults.CodeConflict, "lease ownership is stale", faults.WithReason("stale_lease"), faults.WithOperation("storage.lease.memory.Release"), faults.WithField("lease_key", current.Key.String()), faults.WithRetryPolicy(faults.NoRetry()))
	}
	delete(store.leases, current.Key)
	return nil
}

func (store *Store) Lookup(ctx context.Context, key lease.Key) (lease.Lease, error) {
	if ctx == nil || store == nil {
		return lease.Lease{}, faults.New(faults.CodeInvalidArgument, "invalid lease lookup request", faults.WithReason("invalid_lease_lookup_request"), faults.WithOperation("storage.lease.memory.Lookup"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	if err := key.Validate(); err != nil {
		return lease.Lease{}, err
	}
	if err := ctx.Err(); err != nil {
		return lease.Lease{}, contextError(ctx, err, "storage.lease.memory.Lookup")
	}
	now := store.clock.Now()
	store.mu.Lock()
	defer store.mu.Unlock()
	value, exists := store.leases[key]
	if exists && value.Expired(now) {
		delete(store.leases, key)
		exists = false
	}
	if !exists {
		return lease.Lease{}, faults.Wrap(lease.ErrNotFound, faults.CodeNotFound, "lease not found", faults.WithReason("lease_not_found"), faults.WithOperation("storage.lease.memory.Lookup"), faults.WithField("lease_key", key.String()), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return value, nil
}

func contextError(ctx context.Context, cause error, operation string) error {
	code := faults.CodeCanceled
	message := "lease operation canceled"
	reason := "lease_operation_canceled"
	if errors.Is(cause, context.DeadlineExceeded) {
		code = faults.CodeDeadlineExceeded
		message = "lease operation exceeded its deadline"
		reason = "lease_operation_deadline_exceeded"
	}
	return faults.Wrap(cause, code, message,
		faults.WithReason(reason),
		faults.WithOperation(operation),
		faults.WithContextMetadata(ctx),
		faults.WithRetryPolicy(faults.NoRetry()),
	)
}
