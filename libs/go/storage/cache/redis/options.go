// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package redis

import (
	"errors"
	"strings"
)

const (
	DefaultPrefix            = "mindclade:cache:"
	DefaultMaximumEntryBytes = 4 << 20
)

type Option func(*Store) error

func WithPrefix(value string) Option {
	return func(store *Store) error {
		if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\r\n\x00") {
			return errors.New("redis cache: invalid prefix")
		}
		store.prefix = value
		return nil
	}
}

func WithMaximumEntryBytes(value int) Option {
	return func(store *Store) error {
		if value <= 0 {
			return errors.New("redis cache: maximum entry bytes must be positive")
		}
		store.maximumEntryBytes = value
		return nil
	}
}
