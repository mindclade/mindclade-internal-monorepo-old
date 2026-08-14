// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package cache

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"mindclade.internal/libs/go/faults"
)

const MaximumKeyLength = 512

type Key string

func ParseKey(value string) (Key, error) {
	key := Key(value)
	if err := key.Validate(); err != nil {
		return "", err
	}
	return key, nil
}
func MustParseKey(value string) Key {
	key, err := ParseKey(value)
	if err != nil {
		panic(err)
	}
	return key
}
func (key Key) String() string { return string(key) }
func (key Key) IsZero() bool   { return key == "" }
func (key Key) Validate() error {
	value := string(key)
	if value == "" || len(value) > MaximumKeyLength || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return faults.Wrap(ErrInvalidKey, faults.CodeInvalidArgument, "invalid cache key", faults.WithReason("invalid_cache_key"), faults.WithOperation("cache.Key.Validate"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	for _, character := range value {
		if character == 0 || unicode.IsControl(character) {
			return faults.Wrap(ErrInvalidKey, faults.CodeInvalidArgument, "invalid cache key", faults.WithReason("invalid_cache_key_character"), faults.WithOperation("cache.Key.Validate"), faults.WithRetryPolicy(faults.NoRetry()))
		}
	}
	return nil
}
