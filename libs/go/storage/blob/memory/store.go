// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package memory

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"

	"go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/storage/blob"
)

const DefaultMaximumObjectBytes int64 = 64 << 20

type Option func(*Store) error

func WithClock(value clock.Clock) Option {
	return func(store *Store) error {
		if value == nil {
			return errors.New("memory blob: nil clock")
		}
		store.clock = value
		return nil
	}
}
func WithMaximumObjectBytes(value int64) Option {
	return func(store *Store) error {
		if value <= 0 {
			return errors.New("memory blob: maximum object bytes must be positive")
		}
		store.maximumObjectBytes = value
		return nil
	}
}

type record struct {
	data       []byte
	attributes blob.Attributes
}

type Store struct {
	mu                 sync.RWMutex
	clock              clock.Clock
	maximumObjectBytes int64
	nextGeneration     int64
	objects            map[blob.Key]record
}

var _ blob.Store = (*Store)(nil)

func New(options ...Option) (*Store, error) {
	store := &Store{clock: clock.RealClock{}, maximumObjectBytes: DefaultMaximumObjectBytes, objects: make(map[blob.Key]record)}
	for _, option := range options {
		if option != nil {
			if err := option(store); err != nil {
				return nil, err
			}
		}
	}
	return store, nil
}

func (store *Store) Put(ctx context.Context, key blob.Key, reader io.Reader, options blob.PutOptions) (blob.Attributes, error) {
	if ctx == nil {
		return blob.Attributes{}, faults.New(faults.CodeInvalidArgument, "blob context must not be nil", faults.WithReason("nil_context"), faults.WithOperation("storage.blob.memory.Put"))
	}
	if store == nil || reader == nil {
		return blob.Attributes{}, faults.New(faults.CodeInvalidArgument, "invalid blob put request", faults.WithReason("invalid_blob_put_request"), faults.WithOperation("storage.blob.memory.Put"))
	}
	if err := key.Validate(); err != nil {
		return blob.Attributes{}, err
	}
	if err := options.Validate(); err != nil {
		return blob.Attributes{}, err
	}
	if err := ctx.Err(); err != nil {
		return blob.Attributes{}, contextError(ctx, err, "storage.blob.memory.Put")
	}
	limited := io.LimitReader(reader, store.maximumObjectBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return blob.Attributes{}, faults.Wrap(err, faults.CodeUnavailable, "unable to read blob content", faults.WithReason("blob_read_failed"), faults.WithOperation("storage.blob.memory.Put"), faults.WithRetryPolicy(faults.BackoffRetry(3)))
	}
	if err := ctx.Err(); err != nil {
		return blob.Attributes{}, contextError(ctx, err, "storage.blob.memory.Put")
	}
	if int64(len(data)) > store.maximumObjectBytes {
		return blob.Attributes{}, faults.Wrap(blob.ErrObjectTooLarge, faults.CodeResourceExhausted, "blob object exceeds configured limit", faults.WithReason("blob_object_too_large"), faults.WithOperation("storage.blob.memory.Put"), faults.WithField("maximum_bytes", store.maximumObjectBytes), faults.WithRetryPolicy(faults.NoRetry()))
	}
	digest := identifiers.SHA256(data)
	if !options.Digest.IsZero() && !options.Digest.Equal(digest) {
		return blob.Attributes{}, faults.Wrap(blob.ErrDigestMismatch, faults.CodeDataLoss, "blob digest does not match content", faults.WithReason("blob_digest_mismatch"), faults.WithOperation("storage.blob.memory.Put"), faults.WithField("blob_key", key.String()), faults.WithRetryPolicy(faults.NoRetry()))
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	existing, exists := store.objects[key]
	if options.Preconditions.IfNotExists && exists {
		return blob.Attributes{}, faults.Wrap(blob.ErrPrecondition, faults.CodeAlreadyExists, "blob object already exists", faults.WithReason("blob_must_not_exist"), faults.WithOperation("storage.blob.memory.Put"), faults.WithField("blob_key", key.String()), faults.WithRetryPolicy(faults.NoRetry()))
	}
	if generation := options.Preconditions.IfGenerationMatch; generation != nil && (!exists || existing.attributes.Generation != *generation) {
		return blob.Attributes{}, faults.Wrap(blob.ErrPrecondition, faults.CodeConflict, "blob generation does not match", faults.WithReason("blob_generation_mismatch"), faults.WithOperation("storage.blob.memory.Put"), faults.WithField("blob_key", key.String()), faults.WithRetryPolicy(faults.NoRetry()))
	}
	store.nextGeneration++
	now := store.clock.Now().Round(0)
	created := now
	if exists {
		created = existing.attributes.CreatedAt
	}
	attributes := blob.Attributes{Key: key, Size: int64(len(data)), Digest: digest, ContentType: options.ContentType, ETag: fmt.Sprintf("%s:%d", digest.String(), store.nextGeneration), Generation: store.nextGeneration, CreatedAt: created, UpdatedAt: now, Metadata: options.Metadata.Clone()}
	store.objects[key] = record{data: append([]byte(nil), data...), attributes: attributes}
	return attributes.Clone(), nil
}

func (store *Store) Open(ctx context.Context, key blob.Key, options blob.GetOptions) (blob.Object, error) {
	if ctx == nil {
		return blob.Object{}, faults.New(faults.CodeInvalidArgument, "blob context must not be nil", faults.WithReason("nil_context"), faults.WithOperation("storage.blob.memory.Open"))
	}
	if store == nil {
		return blob.Object{}, faults.New(faults.CodeFailedPrecondition, "blob store is not initialized", faults.WithReason("nil_blob_store"), faults.WithOperation("storage.blob.memory.Open"))
	}
	if err := key.Validate(); err != nil {
		return blob.Object{}, err
	}
	if err := options.Validate(); err != nil {
		return blob.Object{}, err
	}
	if err := ctx.Err(); err != nil {
		return blob.Object{}, contextError(ctx, err, "storage.blob.memory.Open")
	}
	store.mu.RLock()
	current, ok := store.objects[key]
	store.mu.RUnlock()
	if !ok {
		return blob.Object{}, faults.Wrap(errors.New("missing object"), faults.CodeNotFound, "blob object not found", faults.WithReason("blob_not_found"), faults.WithOperation("storage.blob.memory.Open"), faults.WithField("blob_key", key.String()), faults.WithRetryPolicy(faults.NoRetry()))
	}
	if options.Generation != nil && current.attributes.Generation != *options.Generation {
		return blob.Object{}, faults.Wrap(blob.ErrPrecondition, faults.CodeNotFound, "blob generation not found", faults.WithReason("blob_generation_not_found"), faults.WithOperation("storage.blob.memory.Open"), faults.WithField("blob_key", key.String()), faults.WithRetryPolicy(faults.NoRetry()))
	}
	start := options.Offset
	if start > int64(len(current.data)) {
		return blob.Object{}, faults.Wrap(blob.ErrInvalidOptions, faults.CodeOutOfRange, "blob range starts beyond object", faults.WithReason("blob_range_out_of_bounds"), faults.WithOperation("storage.blob.memory.Open"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	end := int64(len(current.data))
	if options.Length > 0 && options.Length < end-start {
		end = start + options.Length
	}
	body := io.NopCloser(bytes.NewReader(append([]byte(nil), current.data[start:end]...)))
	return blob.Object{Attributes: current.attributes.Clone(), Body: body}, nil
}

func (store *Store) Stat(ctx context.Context, key blob.Key) (blob.Attributes, error) {
	object, err := store.Open(ctx, key, blob.GetOptions{})
	if err != nil {
		return blob.Attributes{}, err
	}
	_ = object.Close()
	return object.Attributes, nil
}

func (store *Store) Delete(ctx context.Context, key blob.Key, options blob.DeleteOptions) error {
	if ctx == nil {
		return faults.New(faults.CodeInvalidArgument, "blob context must not be nil", faults.WithReason("nil_context"), faults.WithOperation("storage.blob.memory.Delete"))
	}
	if store == nil {
		return faults.New(faults.CodeFailedPrecondition, "blob store is not initialized", faults.WithReason("nil_blob_store"), faults.WithOperation("storage.blob.memory.Delete"))
	}
	if err := key.Validate(); err != nil {
		return err
	}
	if err := options.Validate(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return contextError(ctx, err, "storage.blob.memory.Delete")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	current, ok := store.objects[key]
	if !ok {
		return faults.Wrap(errors.New("missing object"), faults.CodeNotFound, "blob object not found", faults.WithReason("blob_not_found"), faults.WithOperation("storage.blob.memory.Delete"), faults.WithField("blob_key", key.String()), faults.WithRetryPolicy(faults.NoRetry()))
	}
	if options.Preconditions.IfNotExists {
		return faults.Wrap(blob.ErrPrecondition, faults.CodeFailedPrecondition, "invalid delete precondition", faults.WithReason("delete_if_not_exists_invalid"), faults.WithOperation("storage.blob.memory.Delete"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	if generation := options.Preconditions.IfGenerationMatch; generation != nil && current.attributes.Generation != *generation {
		return faults.Wrap(blob.ErrPrecondition, faults.CodeConflict, "blob generation does not match", faults.WithReason("blob_generation_mismatch"), faults.WithOperation("storage.blob.memory.Delete"), faults.WithField("blob_key", key.String()), faults.WithRetryPolicy(faults.NoRetry()))
	}
	delete(store.objects, key)
	return nil
}

func (store *Store) List(ctx context.Context, options blob.ListOptions) (blob.Page, error) {
	if ctx == nil {
		return blob.Page{}, faults.New(faults.CodeInvalidArgument, "blob context must not be nil", faults.WithReason("nil_context"), faults.WithOperation("storage.blob.memory.List"))
	}
	if store == nil {
		return blob.Page{}, faults.New(faults.CodeFailedPrecondition, "blob store is not initialized", faults.WithReason("nil_blob_store"), faults.WithOperation("storage.blob.memory.List"))
	}
	normalized, err := options.Normalized()
	if err != nil {
		return blob.Page{}, err
	}
	if err := ctx.Err(); err != nil {
		return blob.Page{}, contextError(ctx, err, "storage.blob.memory.List")
	}
	store.mu.RLock()
	values := make([]blob.Attributes, 0, len(store.objects))
	for key, current := range store.objects {
		if strings.HasPrefix(key.String(), normalized.Prefix) && (normalized.Cursor == "" || key.String() > normalized.Cursor) {
			values = append(values, current.attributes.Clone())
		}
	}
	store.mu.RUnlock()
	sort.Slice(values, func(i, j int) bool { return values[i].Key.String() < values[j].Key.String() })
	page := blob.Page{}
	if len(values) > normalized.Limit {
		page.Objects = values[:normalized.Limit]
		page.NextCursor = page.Objects[len(page.Objects)-1].Key.String()
	} else {
		page.Objects = values
	}
	return page.Clone(), nil
}

func contextError(ctx context.Context, cause error, operation string) error {
	code := faults.CodeCanceled
	reason := "blob_operation_canceled"
	message := "blob operation canceled"
	if errors.Is(cause, context.DeadlineExceeded) {
		code = faults.CodeDeadlineExceeded
		reason = "blob_operation_deadline_exceeded"
		message = "blob operation timed out"
	}
	return faults.Wrap(cause, code, message,
		faults.WithReason(reason),
		faults.WithOperation(operation),
		faults.WithContextMetadata(ctx),
		faults.WithRetryPolicy(faults.NoRetry()),
	)
}
