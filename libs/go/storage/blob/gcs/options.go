// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package gcs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	DefaultMaximumObjectBytes int64 = 5 << 40 // Cloud Storage's 5 TiB object limit.
	DefaultWriterChunkSize          = 16 << 20
	DigestMetadataKey               = "mindclade-sha256"
)

type Option func(*Store) error

// WithPrefix confines all objects to prefix. The stored blob key remains
// relative to this prefix.
func WithPrefix(prefix string) Option {
	return func(store *Store) error {
		normalized, err := normalizePrefix(prefix)
		if err != nil {
			return err
		}
		store.prefix = normalized
		return nil
	}
}

// WithMaximumObjectBytes sets the largest object accepted by Put.
func WithMaximumObjectBytes(value int64) Option {
	return func(store *Store) error {
		if value <= 0 || value > DefaultMaximumObjectBytes {
			return errors.New("gcs blob: maximum object bytes must be in (0, 5 TiB]")
		}
		store.maximumObjectBytes = value
		return nil
	}
}

// WithTemporaryDirectory sets the directory used to spool uploads. Spooling
// keeps digest verification atomic with respect to the destination object.
func WithTemporaryDirectory(directory string) Option {
	return func(store *Store) error {
		if strings.TrimSpace(directory) == "" {
			return errors.New("gcs blob: temporary directory must not be empty")
		}
		absolute, err := filepath.Abs(directory)
		if err != nil {
			return errors.New("gcs blob: resolve temporary directory: " + err.Error())
		}
		info, err := os.Stat(absolute)
		if err != nil {
			return errors.New("gcs blob: inspect temporary directory: " + err.Error())
		}
		if !info.IsDir() {
			return errors.New("gcs blob: temporary path is not a directory")
		}
		store.temporaryDirectory = absolute
		return nil
	}
}

// WithWriterChunkSize controls the Cloud Storage writer buffer. Zero disables
// resumable upload buffering and provider retries and is therefore discouraged
// for production use.
func WithWriterChunkSize(value int) Option {
	return func(store *Store) error {
		if value < 0 {
			return errors.New("gcs blob: writer chunk size must not be negative")
		}
		store.writerChunkSize = value
		return nil
	}
}

// WithChunkRetryDeadline controls the provider's per-chunk retry budget.
func WithChunkRetryDeadline(value time.Duration) Option {
	return func(store *Store) error {
		if value < 0 {
			return errors.New("gcs blob: chunk retry deadline must not be negative")
		}
		store.chunkRetryDeadline = value
		return nil
	}
}

func normalizePrefix(prefix string) (string, error) {
	if prefix == "" {
		return "", nil
	}
	if strings.TrimSpace(prefix) != prefix || strings.HasPrefix(prefix, "/") || strings.Contains(prefix, "\\") || strings.Contains(prefix, "//") {
		return "", errors.New("gcs blob: invalid object prefix")
	}
	prefix = strings.TrimSuffix(prefix, "/")
	if prefix == "" || strings.Contains(prefix, "/../") || strings.HasSuffix(prefix, "/..") || strings.HasPrefix(prefix, "../") {
		return "", errors.New("gcs blob: invalid object prefix")
	}
	return prefix + "/", nil
}
