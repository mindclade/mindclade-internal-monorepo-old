// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package memory

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	mcclock "mindclade.internal/libs/go/clock"
	"mindclade.internal/libs/go/coordination/outbox"
	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/storage/lease"
)

type Option func(*Store) error

func WithClock(value mcclock.Clock) Option {
	return func(store *Store) error {
		if value == nil {
			return faults.Wrap(outbox.ErrInvalidRequest, faults.CodeInvalidArgument, "outbox clock is required", faults.WithReason("nil_outbox_clock"), faults.WithOperation("storage.outbox.memory.WithClock"), faults.WithRetryPolicy(faults.NoRetry()))
		}
		store.clock = value
		return nil
	}
}

func WithTokenGenerator(value func() (lease.Token, error)) Option {
	return func(store *Store) error {
		if value == nil {
			return faults.Wrap(outbox.ErrInvalidRequest, faults.CodeInvalidArgument, "outbox token generator is required", faults.WithReason("nil_outbox_token_generator"), faults.WithOperation("storage.outbox.memory.WithTokenGenerator"), faults.WithRetryPolicy(faults.NoRetry()))
		}
		store.token = value
		return nil
	}
}

type entry struct {
	record outbox.Record
	token  lease.Token
}

type Store struct {
	mu      sync.Mutex
	clock   mcclock.Clock
	token   func() (lease.Token, error)
	records map[string]*entry
}

var _ outbox.Store = (*Store)(nil)

func New(options ...Option) (*Store, error) {
	store := &Store{clock: mcclock.RealClock{}, token: lease.NewToken, records: make(map[string]*entry)}
	for _, option := range options {
		if option != nil {
			if err := option(store); err != nil {
				return nil, err
			}
		}
	}
	return store, nil
}

func (store *Store) Append(ctx context.Context, message outbox.Message) error {
	if ctx == nil {
		return invalid(ctx, outbox.ErrInvalidRequest, "storage.outbox.memory.Append")
	}
	if store == nil || store.records == nil || store.clock == nil || store.token == nil {
		return unavailable(ctx, outbox.ErrUnavailable, "storage.outbox.memory.Append")
	}
	if err := message.Validate(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return contextFault(ctx, err, "storage.outbox.memory.Append")
	}
	key := message.ID().String()
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, exists := store.records[key]; exists {
		return faults.Wrap(outbox.ErrAlreadyExists, faults.CodeAlreadyExists, "outbox message already exists", faults.WithReason("outbox_message_exists"), faults.WithOperation("storage.outbox.memory.Append"), faults.WithField("outbox_message_id", key), faults.WithRetryPolicy(faults.NoRetry()), faults.WithContextMetadata(ctx))
	}
	store.records[key] = &entry{record: outbox.Record{Message: message, State: outbox.StatePending, Version: 1}}
	return nil
}

func (store *Store) Claim(ctx context.Context, request outbox.ClaimRequest) ([]outbox.Claim, error) {
	if ctx == nil {
		return nil, invalid(ctx, outbox.ErrInvalidRequest, "storage.outbox.memory.Claim")
	}
	if store == nil || store.records == nil || store.clock == nil || store.token == nil {
		return nil, unavailable(ctx, outbox.ErrUnavailable, "storage.outbox.memory.Claim")
	}
	request = request.Normalized()
	if err := request.Validate(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, contextFault(ctx, err, "storage.outbox.memory.Claim")
	}
	now := store.clock.Now().Round(0).UTC()
	topics := make(map[string]struct{}, len(request.Topics))
	for _, topic := range request.Topics {
		topics[topic] = struct{}{}
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	candidates := make([]*entry, 0, len(store.records))
	for _, value := range store.records {
		record := value.record
		if record.State == outbox.StateClaimed && !record.ClaimExpires.After(now) {
			record.State = outbox.StatePending
			record.ClaimOwner = ""
			record.ClaimToken = ""
			record.ClaimedAt = time.Time{}
			record.ClaimExpires = time.Time{}
			record.Version++
			value.record = record
			value.token = lease.Token{}
		}
		if record.State != outbox.StatePending || record.Message.AvailableAt().After(now) {
			continue
		}
		if len(topics) > 0 {
			if _, allowed := topics[record.Message.Topic()]; !allowed {
				continue
			}
		}
		candidates = append(candidates, value)
	}
	sort.Slice(candidates, func(left, right int) bool {
		leftMessage := candidates[left].record.Message
		rightMessage := candidates[right].record.Message
		if !leftMessage.AvailableAt().Equal(rightMessage.AvailableAt()) {
			return leftMessage.AvailableAt().Before(rightMessage.AvailableAt())
		}
		return leftMessage.ID().Less(rightMessage.ID())
	})
	if len(candidates) > request.Limit {
		candidates = candidates[:request.Limit]
	}
	claims := make([]outbox.Claim, 0, len(candidates))
	for _, value := range candidates {
		token, err := store.token()
		if err != nil {
			return nil, faults.Wrap(err, faults.CodeInternal, "unable to generate outbox claim token", faults.WithReason("outbox_token_generation_failed"), faults.WithOperation("storage.outbox.memory.Claim"), faults.WithRetryPolicy(faults.BackoffRetry(3)), faults.WithContextMetadata(ctx))
		}
		record := value.record
		record.State = outbox.StateClaimed
		record.Attempts++
		record.Version++
		record.ClaimOwner = request.Owner
		record.ClaimToken = token.String()
		record.ClaimedAt = now
		record.ClaimExpires = now.Add(request.LeaseDuration)
		value.record = record
		value.token = token
		claim, err := outbox.NewClaim(record.Message, token, request.Owner, record.Version, record.Attempts, record.ClaimedAt, record.ClaimExpires)
		if err != nil {
			return nil, faults.Wrap(err, faults.CodeInternal, "memory outbox produced invalid claim", faults.WithReason("outbox_store_contract_failed"), faults.WithOperation("storage.outbox.memory.Claim"), faults.WithRetryPolicy(faults.NoRetry()))
		}
		claims = append(claims, claim)
	}
	return claims, nil
}

func (store *Store) Renew(ctx context.Context, claim outbox.Claim, ttl time.Duration) (outbox.Claim, error) {
	if ctx == nil || ttl <= 0 {
		return outbox.Claim{}, invalid(ctx, outbox.ErrInvalidRequest, "storage.outbox.memory.Renew")
	}
	if err := claim.Validate(); err != nil {
		return outbox.Claim{}, err
	}
	now := store.clock.Now().Round(0).UTC()
	store.mu.Lock()
	defer store.mu.Unlock()
	value, err := store.claimedLocked(claim, now)
	if err != nil {
		return outbox.Claim{}, err
	}
	record := value.record
	record.Version++
	record.ClaimExpires = now.Add(ttl)
	value.record = record
	return outbox.NewClaim(record.Message, value.token, record.ClaimOwner, record.Version, record.Attempts, record.ClaimedAt, record.ClaimExpires)
}

func (store *Store) MarkPublished(ctx context.Context, claim outbox.Claim, publishedAt time.Time) error {
	if ctx == nil || publishedAt.IsZero() {
		return invalid(ctx, outbox.ErrInvalidRequest, "storage.outbox.memory.MarkPublished")
	}
	if err := claim.Validate(); err != nil {
		return err
	}
	publishedAt = publishedAt.Round(0).UTC()
	store.mu.Lock()
	defer store.mu.Unlock()
	value, err := store.claimedLocked(claim, publishedAt)
	if err != nil {
		return err
	}
	record := value.record
	record.State = outbox.StatePublished
	record.Version++
	record.PublishedAt = publishedAt
	clearClaim(&record)
	value.record = record
	value.token = lease.Token{}
	return nil
}

func (store *Store) Reschedule(ctx context.Context, claim outbox.Claim, availableAt time.Time, reason string) error {
	if ctx == nil || availableAt.IsZero() || strings.TrimSpace(reason) == "" {
		return invalid(ctx, outbox.ErrInvalidRequest, "storage.outbox.memory.Reschedule")
	}
	if err := claim.Validate(); err != nil {
		return err
	}
	availableAt = availableAt.Round(0).UTC()
	store.mu.Lock()
	defer store.mu.Unlock()
	value, err := store.claimedLocked(claim, store.clock.Now())
	if err != nil {
		return err
	}
	record := value.record
	record.State = outbox.StatePending
	record.Version++
	record.LastError = truncate(reason, 256)
	message := record.Message
	updated, createErr := outbox.NewMessage(message.ID(), message.Topic(), message.PartitionKey(), message.ContentType(), message.Payload(), message.Headers(), message.Request(), message.CreatedAt(), availableAt)
	if createErr != nil {
		return createErr
	}
	record.Message = updated
	clearClaim(&record)
	value.record = record
	value.token = lease.Token{}
	return nil
}

func (store *Store) DeadLetter(ctx context.Context, claim outbox.Claim, deadAt time.Time, reason string) error {
	if ctx == nil || deadAt.IsZero() || strings.TrimSpace(reason) == "" {
		return invalid(ctx, outbox.ErrInvalidRequest, "storage.outbox.memory.DeadLetter")
	}
	if err := claim.Validate(); err != nil {
		return err
	}
	deadAt = deadAt.Round(0).UTC()
	store.mu.Lock()
	defer store.mu.Unlock()
	value, err := store.claimedLocked(claim, deadAt)
	if err != nil {
		return err
	}
	record := value.record
	record.State = outbox.StateDeadLetter
	record.Version++
	record.DeadAt = deadAt
	record.LastError = truncate(reason, 256)
	clearClaim(&record)
	value.record = record
	value.token = lease.Token{}
	return nil
}

func (store *Store) Lookup(ctx context.Context, identifier string) (outbox.Record, error) {
	if ctx == nil || strings.TrimSpace(identifier) == "" {
		return outbox.Record{}, invalid(ctx, outbox.ErrInvalidRequest, "storage.outbox.memory.Lookup")
	}
	if err := ctx.Err(); err != nil {
		return outbox.Record{}, contextFault(ctx, err, "storage.outbox.memory.Lookup")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	value, exists := store.records[identifier]
	if !exists {
		return outbox.Record{}, faults.Wrap(outbox.ErrNotFound, faults.CodeNotFound, "outbox message not found", faults.WithReason("outbox_message_not_found"), faults.WithOperation("storage.outbox.memory.Lookup"), faults.WithField("outbox_message_id", identifier), faults.WithRetryPolicy(faults.NoRetry()), faults.WithContextMetadata(ctx))
	}
	return cloneRecord(value.record), nil
}

func (store *Store) claimedLocked(claim outbox.Claim, now time.Time) (*entry, error) {
	value, exists := store.records[claim.Message().ID().String()]
	if !exists {
		return nil, claimLost(nil, claim)
	}
	record := value.record
	if record.State != outbox.StateClaimed || record.Version != claim.Version() || record.ClaimOwner != claim.Owner() || !value.token.Equal(claim.Token()) || !record.ClaimExpires.After(now) {
		return nil, claimLost(nil, claim)
	}
	return value, nil
}

func clearClaim(record *outbox.Record) {
	record.ClaimOwner = ""
	record.ClaimToken = ""
	record.ClaimedAt = time.Time{}
	record.ClaimExpires = time.Time{}
}

func cloneRecord(record outbox.Record) outbox.Record {
	message := record.Message
	cloned, _ := outbox.NewMessage(message.ID(), message.Topic(), message.PartitionKey(), message.ContentType(), message.Payload(), message.Headers(), message.Request(), message.CreatedAt(), message.AvailableAt())
	record.Message = cloned
	return record
}

func invalid(ctx context.Context, cause error, operation string) error {
	return faults.Wrap(cause, faults.CodeInvalidArgument, "invalid outbox store request", faults.WithReason("invalid_outbox_store_request"), faults.WithOperation(operation), faults.WithRetryPolicy(faults.NoRetry()), faults.WithContextMetadata(ctx))
}
func unavailable(ctx context.Context, cause error, operation string) error {
	return faults.Wrap(cause, faults.CodeFailedPrecondition, "outbox store is not configured", faults.WithReason(outbox.ReasonStoreFailed), faults.WithOperation(operation), faults.WithRetryPolicy(faults.NoRetry()), faults.WithContextMetadata(ctx))
}
func claimLost(ctx context.Context, claim outbox.Claim) error {
	return faults.Wrap(outbox.ErrClaimLost, faults.CodeConflict, "outbox claim is stale or expired", faults.WithReason(outbox.ReasonClaimLost), faults.WithOperation("storage.outbox.memory"), faults.WithField("outbox_message_id", claim.Message().ID().String()), faults.WithRetryPolicy(faults.NoRetry()), faults.WithContextMetadata(ctx))
}
func contextFault(ctx context.Context, cause error, operation string) error {
	code := faults.CodeCanceled
	reason := "outbox_operation_canceled"
	if errors.Is(cause, context.DeadlineExceeded) {
		code = faults.CodeDeadlineExceeded
		reason = "outbox_operation_deadline_exceeded"
	}
	return faults.Wrap(cause, code, "outbox operation interrupted", faults.WithReason(reason), faults.WithOperation(operation), faults.WithRetryPolicy(faults.NoRetry()), faults.WithContextMetadata(ctx))
}
func truncate(value string, maximum int) string {
	value = strings.TrimSpace(value)
	if len(value) > maximum {
		return value[:maximum]
	}
	return value
}
