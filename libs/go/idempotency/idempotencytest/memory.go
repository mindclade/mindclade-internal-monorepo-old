// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package idempotencytest

import (
	"context"
	"reflect"
	"sync"
	"time"

	mcclock "mindclade.internal/libs/go/clock"
	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/idempotency"
	"mindclade.internal/libs/go/identifiers"
)

type MemoryStore struct {
	mu        sync.Mutex
	clock     mcclock.Clock
	generator *identifiers.Generator
	entries   map[string]entry
}
type entry struct {
	record idempotency.Record
	token  identifiers.UUID
}
type Option func(*MemoryStore) error

func WithClock(clock mcclock.Clock) Option {
	return func(store *MemoryStore) error {
		if nilValue(clock) {
			return faults.New(faults.CodeInvalidArgument, "clock must not be nil")
		}
		store.clock = clock
		return nil
	}
}
func WithGenerator(generator *identifiers.Generator) Option {
	return func(store *MemoryStore) error {
		if generator == nil {
			return faults.New(faults.CodeInvalidArgument, "generator must not be nil")
		}
		store.generator = generator
		return nil
	}
}
func NewMemoryStore(options ...Option) (*MemoryStore, error) {
	store := &MemoryStore{clock: mcclock.RealClock{}, entries: map[string]entry{}}
	for _, option := range options {
		if option != nil {
			if err := option(store); err != nil {
				return nil, err
			}
		}
	}
	if store.generator == nil {
		generator, err := identifiers.NewGenerator(identifiers.WithTimeSource(store.clock.Now))
		if err != nil {
			return nil, err
		}
		store.generator = generator
	}
	return store, nil
}
func (store *MemoryStore) key(identity idempotency.Identity) string {
	return identity.Digest().String()
}
func (store *MemoryStore) Acquire(ctx context.Context, request idempotency.AcquireRequest) (idempotency.Acquisition, error) {
	if err := contextError(ctx); err != nil {
		return idempotency.Acquisition{}, err
	}
	if err := store.validate(); err != nil {
		return idempotency.Acquisition{}, err
	}
	request = request.Normalized()
	if err := request.Validate(); err != nil {
		return idempotency.Acquisition{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	now := store.clock.Now().Round(0).UTC()
	key := store.key(request.Identity)
	existing, ok := store.entries[key]
	if ok && existing.record.Expired(now) {
		delete(store.entries, key)
		ok = false
	}
	if ok {
		record := existing.record
		if !record.Fingerprint().Equal(request.Fingerprint) {
			return idempotency.Acquisition{Disposition: idempotency.DispositionConflict, Record: record}, nil
		}
		if record.State() == idempotency.StateCompleted {
			return idempotency.Acquisition{Disposition: idempotency.DispositionReplay, Record: record}, nil
		}
		if !record.LeaseExpired(now) {
			return idempotency.Acquisition{Disposition: idempotency.DispositionInProgress, Record: record}, nil
		}
		return store.reclaimLocked(key, record, request, now)
	}
	return store.createLocked(key, request, now)
}
func (store *MemoryStore) createLocked(key string, request idempotency.AcquireRequest, now time.Time) (idempotency.Acquisition, error) {
	recordID, err := store.generator.IDAt(idempotency.RecordIDKind, now)
	if err != nil {
		return idempotency.Acquisition{}, err
	}
	token, err := store.generator.UUIDv4()
	if err != nil {
		return idempotency.Acquisition{}, err
	}
	data := idempotency.RecordData{ID: recordID, Identity: request.Identity, Fingerprint: request.Fingerprint, State: idempotency.StateInProgress, RequestID: request.RequestID, CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(request.TTL), LeaseExpiresAt: now.Add(request.LeaseDuration), Version: 1}
	record, err := idempotency.NewRecord(data)
	if err != nil {
		return idempotency.Acquisition{}, err
	}
	lease := idempotency.Lease{RecordID: recordID, Identity: request.Identity, Fingerprint: request.Fingerprint, Token: token, ExpiresAt: data.LeaseExpiresAt, Version: data.Version}
	store.entries[key] = entry{record: record, token: token}
	return idempotency.Acquisition{Disposition: idempotency.DispositionAcquired, Record: record, Lease: lease}, nil
}
func (store *MemoryStore) reclaimLocked(key string, record idempotency.Record, request idempotency.AcquireRequest, now time.Time) (idempotency.Acquisition, error) {
	token, err := store.generator.UUIDv4()
	if err != nil {
		return idempotency.Acquisition{}, err
	}
	data := record.Data()
	data.RequestID = request.RequestID
	data.UpdatedAt = now
	data.LeaseExpiresAt = minimumTime(now.Add(request.LeaseDuration), data.ExpiresAt)
	data.Version++
	reclaimed, err := idempotency.NewRecord(data)
	if err != nil {
		return idempotency.Acquisition{}, err
	}
	lease := idempotency.Lease{RecordID: data.ID, Identity: data.Identity, Fingerprint: data.Fingerprint, Token: token, ExpiresAt: data.LeaseExpiresAt, Version: data.Version}
	store.entries[key] = entry{record: reclaimed, token: token}
	return idempotency.Acquisition{Disposition: idempotency.DispositionAcquired, Record: reclaimed, Lease: lease}, nil
}
func (store *MemoryStore) Complete(ctx context.Context, request idempotency.CompleteRequest) (idempotency.Record, error) {
	if err := contextError(ctx); err != nil {
		return idempotency.Record{}, err
	}
	if err := store.validate(); err != nil {
		return idempotency.Record{}, err
	}
	if err := request.Validate(); err != nil {
		return idempotency.Record{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	key := store.key(request.Lease.Identity)
	current, ok := store.entries[key]
	if !ok {
		return idempotency.Record{}, notFound(request.Lease.Identity)
	}
	if !owns(current, request.Lease) {
		return idempotency.Record{}, lost(request.Lease)
	}
	now := store.clock.Now().Round(0).UTC()
	if !now.Before(current.record.LeaseExpiresAt()) {
		return idempotency.Record{}, lost(request.Lease)
	}
	data := current.record.Data()
	data.State = idempotency.StateCompleted
	data.Result = request.Result
	data.UpdatedAt = now
	data.LeaseExpiresAt = time.Time{}
	data.Version++
	completed, err := idempotency.NewRecord(data)
	if err != nil {
		return idempotency.Record{}, err
	}
	store.entries[key] = entry{record: completed}
	return completed, nil
}
func (store *MemoryStore) Release(ctx context.Context, request idempotency.ReleaseRequest) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if err := store.validate(); err != nil {
		return err
	}
	if err := request.Validate(); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	key := store.key(request.Lease.Identity)
	current, ok := store.entries[key]
	if !ok {
		return notFound(request.Lease.Identity)
	}
	if !owns(current, request.Lease) {
		return lost(request.Lease)
	}
	if !store.clock.Now().Round(0).UTC().Before(current.record.LeaseExpiresAt()) {
		return lost(request.Lease)
	}
	delete(store.entries, key)
	return nil
}
func (store *MemoryStore) Renew(ctx context.Context, request idempotency.RenewRequest) (idempotency.Lease, error) {
	if err := contextError(ctx); err != nil {
		return idempotency.Lease{}, err
	}
	if err := store.validate(); err != nil {
		return idempotency.Lease{}, err
	}
	if err := request.Validate(); err != nil {
		return idempotency.Lease{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	key := store.key(request.Lease.Identity)
	current, ok := store.entries[key]
	if !ok {
		return idempotency.Lease{}, notFound(request.Lease.Identity)
	}
	if !owns(current, request.Lease) {
		return idempotency.Lease{}, lost(request.Lease)
	}
	now := store.clock.Now().Round(0).UTC()
	if !now.Before(current.record.LeaseExpiresAt()) {
		return idempotency.Lease{}, lost(request.Lease)
	}
	data := current.record.Data()
	data.UpdatedAt = now
	data.LeaseExpiresAt = minimumTime(now.Add(request.ExtendBy), data.ExpiresAt)
	if !data.LeaseExpiresAt.After(now) {
		return idempotency.Lease{}, lost(request.Lease)
	}
	data.Version++
	renewedRecord, err := idempotency.NewRecord(data)
	if err != nil {
		return idempotency.Lease{}, err
	}
	renewed := idempotency.Lease{RecordID: data.ID, Identity: data.Identity, Fingerprint: data.Fingerprint, Token: current.token, ExpiresAt: data.LeaseExpiresAt, Version: data.Version}
	store.entries[key] = entry{record: renewedRecord, token: current.token}
	return renewed, nil
}
func (store *MemoryStore) Lookup(ctx context.Context, identity idempotency.Identity) (idempotency.Record, error) {
	if err := contextError(ctx); err != nil {
		return idempotency.Record{}, err
	}
	if err := store.validate(); err != nil {
		return idempotency.Record{}, err
	}
	if err := identity.Validate(); err != nil {
		return idempotency.Record{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	key := store.key(identity)
	current, ok := store.entries[key]
	if !ok {
		return idempotency.Record{}, notFound(identity)
	}
	if current.record.Expired(store.clock.Now().Round(0).UTC()) {
		delete(store.entries, key)
		return idempotency.Record{}, notFound(identity)
	}
	return current.record, nil
}
func owns(current entry, lease idempotency.Lease) bool {
	return current.record.ID() == lease.RecordID &&
		current.record.Identity() == lease.Identity &&
		current.record.Fingerprint().Equal(lease.Fingerprint) &&
		current.record.LeaseExpiresAt().Equal(lease.ExpiresAt) &&
		current.record.Version() == lease.Version &&
		current.token == lease.Token &&
		current.record.State() == idempotency.StateInProgress
}
func notFound(identity idempotency.Identity) error {
	return faults.Wrap(idempotency.ErrNotFound, faults.CodeNotFound, "idempotency record was not found", faults.WithReason(idempotency.ReasonNotFound), faults.WithOperation("idempotencytest.MemoryStore"), faults.WithField("identity_digest", identity.Digest().String()), faults.WithRetryPolicy(faults.NoRetry()))
}
func lost(lease idempotency.Lease) error {
	return faults.Wrap(idempotency.ErrLeaseLost, faults.CodeConflict, "idempotency lease is no longer owned", faults.WithReason(idempotency.ReasonLeaseLost), faults.WithOperation("idempotencytest.MemoryStore"), faults.WithField("record_id", lease.RecordID.String()), faults.WithRetryPolicy(faults.NoRetry()))
}

func (store *MemoryStore) validate() error {
	if store == nil || nilValue(store.clock) || store.generator == nil || store.entries == nil {
		return faults.Wrap(
			idempotency.ErrNilStore,
			faults.CodeFailedPrecondition,
			"in-memory idempotency store is not configured",
			faults.WithReason(idempotency.ReasonStoreFailed),
			faults.WithOperation("idempotencytest.MemoryStore"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	return nil
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return faults.Wrap(
			idempotency.ErrNilContext,
			faults.CodeInvalidArgument,
			"context must not be nil",
			faults.WithReason(idempotency.ReasonInvalidRequest),
			faults.WithOperation("idempotencytest.MemoryStore"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	if err := ctx.Err(); err != nil {
		return faults.Wrap(
			err,
			faults.CodeOf(err),
			faults.PublicMessageOf(err),
			faults.WithReason("idempotency_context_done"),
			faults.WithOperation("idempotencytest.MemoryStore"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	return nil
}

func minimumTime(left, right time.Time) time.Time {
	if left.Before(right) {
		return left
	}
	return right
}

func nilValue(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
