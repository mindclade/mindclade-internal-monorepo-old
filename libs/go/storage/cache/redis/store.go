// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package redis

import (
	"context"
	"errors"
	"fmt"
	"net"
	"reflect"
	"strconv"
	"time"

	redisapi "github.com/redis/go-redis/v9"

	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/storage/cache"
)

type Store struct {
	client            redisapi.Scripter
	prefix            string
	maximumEntryBytes int
	getScript         scriptRunner
	setScript         scriptRunner
	deleteScript      scriptRunner
}

var _ cache.Store = (*Store)(nil)

func New(client redisapi.Scripter, options ...Option) (*Store, error) {
	if nilInterface(client) {
		return nil, faults.New(faults.CodeInvalidArgument, "Redis client must not be nil", faults.WithReason("nil_redis_client"), faults.WithOperation("storage.cache.redis.New"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	store := &Store{client: client, prefix: DefaultPrefix, maximumEntryBytes: DefaultMaximumEntryBytes, getScript: defaultGetScript, setScript: defaultSetScript, deleteScript: defaultDeleteScript}
	for _, option := range options {
		if option != nil {
			if err := option(store); err != nil {
				return nil, faults.Wrap(err, faults.CodeInvalidArgument, "invalid Redis cache configuration", faults.WithReason("invalid_redis_cache_option"), faults.WithOperation("storage.cache.redis.New"), faults.WithRetryPolicy(faults.NoRetry()))
			}
		}
	}
	return store, nil
}

func (store *Store) Get(ctx context.Context, key cache.Key) (cache.Entry, error) {
	const operation = "storage.cache.redis.Get"
	if ctx == nil || store == nil || nilInterface(store.client) {
		return cache.Entry{}, invalidRequest(operation)
	}
	if err := key.Validate(); err != nil {
		return cache.Entry{}, err
	}
	result, err := store.getScript.Run(ctx, store.client, []string{store.redisKey(key)})
	if err != nil {
		return cache.Entry{}, qualify(ctx, err, operation, key)
	}
	values, err := arrayResult(result)
	if err != nil {
		return cache.Entry{}, corrupt(ctx, err, operation, key)
	}
	if len(values) == 0 {
		return cache.Entry{}, corrupt(ctx, errors.New("empty Redis script result"), operation, key)
	}
	switch values[0] {
	case "miss":
		return cache.Entry{}, miss(ctx, operation, key)
	case "corrupt":
		return cache.Entry{}, corrupt(ctx, errors.New("corrupt Redis cache record"), operation, key)
	case "ok":
		if len(values) != 4 {
			return cache.Entry{}, corrupt(ctx, errors.New("invalid Redis cache record shape"), operation, key)
		}
		value := []byte(values[1])
		if len(value) > store.maximumEntryBytes {
			return cache.Entry{}, corrupt(ctx, cache.ErrEntryTooLarge, operation, key)
		}
		version, parseErr := strconv.ParseUint(values[2], 10, 64)
		if parseErr != nil || version == 0 {
			return cache.Entry{}, corrupt(ctx, errors.Join(errors.New("invalid Redis cache version"), parseErr), operation, key)
		}
		expiresAt, parseErr := parseExpiration(values[3])
		if parseErr != nil {
			return cache.Entry{}, corrupt(ctx, parseErr, operation, key)
		}
		entry := cache.Entry{Key: key, Value: value, Version: version, ExpiresAt: expiresAt}
		if err := entry.Validate(); err != nil {
			return cache.Entry{}, corrupt(ctx, err, operation, key)
		}
		return entry, nil
	default:
		return cache.Entry{}, corrupt(ctx, fmt.Errorf("unknown Redis script status %q", values[0]), operation, key)
	}
}

func (store *Store) Set(ctx context.Context, key cache.Key, value []byte, options cache.SetOptions) (cache.Entry, error) {
	const operation = "storage.cache.redis.Set"
	if ctx == nil || store == nil || nilInterface(store.client) {
		return cache.Entry{}, invalidRequest(operation)
	}
	if err := key.Validate(); err != nil {
		return cache.Entry{}, err
	}
	if err := options.Validate(); err != nil {
		return cache.Entry{}, err
	}
	if len(value) > store.maximumEntryBytes {
		return cache.Entry{}, faults.Wrap(cache.ErrEntryTooLarge, faults.CodeResourceExhausted, "cache entry exceeds configured limit", faults.WithReason("cache_entry_too_large"), faults.WithOperation(operation), faults.WithField("maximum_bytes", store.maximumEntryBytes), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
	}
	expected := uint64(0)
	if options.IfVersion != nil {
		expected = *options.IfVersion
	}
	ttlMilliseconds := int64(0)
	if options.TTL > 0 {
		ttlMilliseconds = options.TTL.Milliseconds()
		if ttlMilliseconds == 0 {
			ttlMilliseconds = 1
		}
	}
	result, err := store.setScript.Run(ctx, store.client, []string{store.redisKey(key)}, boolString(options.IfAbsent), strconv.FormatUint(expected, 10), value, strconv.FormatInt(ttlMilliseconds, 10))
	if err != nil {
		return cache.Entry{}, qualify(ctx, err, operation, key)
	}
	values, err := arrayResult(result)
	if err != nil || len(values) == 0 {
		return cache.Entry{}, corrupt(ctx, errors.Join(errors.New("invalid Redis set result"), err), operation, key)
	}
	switch values[0] {
	case "exists":
		return cache.Entry{}, faults.Wrap(cache.ErrVersionMismatch, faults.CodeAlreadyExists, "cache entry already exists", faults.WithReason("cache_entry_exists"), faults.WithOperation(operation), faults.WithField("cache_key", key.String()), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
	case "mismatch":
		return cache.Entry{}, faults.Wrap(cache.ErrVersionMismatch, faults.CodeConflict, "cache version does not match", faults.WithReason("cache_version_mismatch"), faults.WithOperation(operation), faults.WithField("cache_key", key.String()), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
	case "corrupt":
		return cache.Entry{}, corrupt(ctx, errors.New("corrupt Redis cache record"), operation, key)
	case "invalid_ttl":
		return cache.Entry{}, corrupt(ctx, errors.New("Redis rejected cache TTL"), operation, key)
	case "ok":
		if len(values) != 3 {
			return cache.Entry{}, corrupt(ctx, errors.New("invalid Redis set result shape"), operation, key)
		}
		version, parseErr := strconv.ParseUint(values[1], 10, 64)
		if parseErr != nil || version == 0 {
			return cache.Entry{}, corrupt(ctx, errors.Join(errors.New("invalid Redis cache version"), parseErr), operation, key)
		}
		expiresAt, parseErr := parseExpiration(values[2])
		if parseErr != nil {
			return cache.Entry{}, corrupt(ctx, parseErr, operation, key)
		}
		entry := cache.Entry{Key: key, Value: append([]byte(nil), value...), Version: version, ExpiresAt: expiresAt}
		if validateErr := entry.Validate(); validateErr != nil {
			return cache.Entry{}, corrupt(ctx, validateErr, operation, key)
		}
		return entry, nil
	default:
		return cache.Entry{}, corrupt(ctx, fmt.Errorf("unknown Redis script status %q", values[0]), operation, key)
	}
}

func (store *Store) Delete(ctx context.Context, key cache.Key, options cache.DeleteOptions) error {
	const operation = "storage.cache.redis.Delete"
	if ctx == nil || store == nil || nilInterface(store.client) {
		return invalidRequest(operation)
	}
	if err := key.Validate(); err != nil {
		return err
	}
	if err := options.Validate(); err != nil {
		return err
	}
	expected := uint64(0)
	if options.IfVersion != nil {
		expected = *options.IfVersion
	}
	result, err := store.deleteScript.Run(ctx, store.client, []string{store.redisKey(key)}, strconv.FormatUint(expected, 10))
	if err != nil {
		return qualify(ctx, err, operation, key)
	}
	values, err := arrayResult(result)
	if err != nil || len(values) == 0 {
		return corrupt(ctx, errors.Join(errors.New("invalid Redis delete result"), err), operation, key)
	}
	switch values[0] {
	case "ok":
		return nil
	case "miss":
		return miss(ctx, operation, key)
	case "mismatch":
		return faults.Wrap(cache.ErrVersionMismatch, faults.CodeConflict, "cache version does not match", faults.WithReason("cache_version_mismatch"), faults.WithOperation(operation), faults.WithField("cache_key", key.String()), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
	case "corrupt":
		return corrupt(ctx, errors.New("corrupt Redis cache record"), operation, key)
	default:
		return corrupt(ctx, fmt.Errorf("unknown Redis script status %q", values[0]), operation, key)
	}
}

func (store *Store) redisKey(key cache.Key) string { return store.prefix + key.String() }

func invalidRequest(operation string) error {
	return faults.New(faults.CodeInvalidArgument, "invalid Redis cache request", faults.WithReason("invalid_redis_cache_request"), faults.WithOperation(operation), faults.WithRetryPolicy(faults.NoRetry()))
}
func miss(ctx context.Context, operation string, key cache.Key) error {
	return faults.Wrap(cache.ErrMiss, faults.CodeNotFound, "cache entry not found", faults.WithReason("cache_miss"), faults.WithOperation(operation), faults.WithField("cache_key", key.String()), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
}
func corrupt(ctx context.Context, cause error, operation string, key cache.Key) error {
	return faults.Wrap(cause, faults.CodeDataLoss, "cache entry is corrupt", faults.WithReason("redis_cache_corrupt"), faults.WithOperation(operation), faults.WithField("cache_key", key.String()), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
}
func qualify(ctx context.Context, err error, operation string, key cache.Key) error {
	if err == nil {
		return nil
	}
	if faults.CodeOf(err) != faults.CodeUnknown {
		return err
	}
	if errors.Is(err, context.Canceled) {
		return faults.Wrap(err, faults.CodeCanceled, "cache operation canceled",
			faults.WithReason("redis_cache_canceled"),
			faults.WithOperation(operation),
			faults.WithField("cache_key", key.String()),
			faults.WithContextMetadata(ctx),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return faults.Wrap(err, faults.CodeDeadlineExceeded, "cache operation timed out",
			faults.WithReason("redis_cache_deadline_exceeded"),
			faults.WithOperation(operation),
			faults.WithField("cache_key", key.String()),
			faults.WithContextMetadata(ctx),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	if errors.Is(err, redisapi.Nil) {
		return miss(ctx, operation, key)
	}
	var networkError net.Error
	policy := faults.BackoffRetry(5)
	code := faults.CodeUnavailable
	reason := "redis_cache_unavailable"
	if errors.As(err, &networkError) && networkError.Timeout() {
		code = faults.CodeDeadlineExceeded
		reason = "redis_cache_timeout"
		policy = faults.NoRetry()
	}
	return faults.Wrap(err, code, "cache storage is unavailable", faults.WithReason(reason), faults.WithOperation(operation), faults.WithField("cache_key", key.String()), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(policy))
}

func arrayResult(value any) ([]string, error) {
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("expected Redis array result, got %T", value)
	}
	output := make([]string, len(items))
	for index, item := range items {
		switch typed := item.(type) {
		case string:
			output[index] = typed
		case []byte:
			output[index] = string(typed)
		case int64:
			output[index] = strconv.FormatInt(typed, 10)
		case nil:
			output[index] = ""
		default:
			return nil, fmt.Errorf("unexpected Redis array value %T", item)
		}
	}
	return output, nil
}

func parseExpiration(value string) (time.Time, error) {
	if value == "" || value == "0" {
		return time.Time{}, nil
	}
	milliseconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil || milliseconds <= 0 {
		return time.Time{}, errors.Join(errors.New("invalid Redis cache expiration"), err)
	}
	return time.UnixMilli(milliseconds).UTC(), nil
}

func boolString(value bool) string {
	if value {
		return "1"
	}
	return "0"
}
func nilInterface(value any) bool {
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
