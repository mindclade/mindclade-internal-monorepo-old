// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package connectors defines bounded provider-neutral source discovery values.
// Bulk transfer and parsing remain owned by the Rust artifact plane.
package connectors

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"strings"
	"time"
)

const (
	MaxCursorBytes = 4096
	MaxObjects     = 1_000_000
)

type Cursor struct {
	Value    string `json:"value"`
	Sequence uint64 `json:"sequence"`
}

func (c Cursor) Validate() error {
	if c.Value == "" || len(c.Value) > MaxCursorBytes || c.Sequence == 0 {
		return errors.New("connector cursor is invalid")
	}
	return nil
}

func (c Cursor) CanAdvance(next Cursor) bool {
	return c.Validate() == nil && next.Validate() == nil && next.Sequence > c.Sequence
}

type Object struct {
	URI        string    `json:"uri"`
	Generation string    `json:"generation"`
	ETag       string    `json:"etag,omitempty"`
	SizeBytes  int64     `json:"size_bytes"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (o Object) Validate() error {
	parsed, err := url.Parse(o.URI)
	if err != nil || !slices.Contains([]string{"gs", "s3", "https"}, parsed.Scheme) ||
		parsed.Host == "" || strings.Trim(parsed.Path, "/") == "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("connector object URI must be absolute and credential-free")
	}
	if o.Generation == "" || len(o.Generation) > 256 || o.SizeBytes < 0 || o.UpdatedAt.IsZero() {
		return errors.New("connector object generation, size, or update time is invalid")
	}
	return nil
}

type Snapshot struct {
	Source     string    `json:"source"`
	Version    string    `json:"version"`
	Cursor     Cursor    `json:"cursor"`
	Objects    []Object  `json:"objects"`
	ObservedAt time.Time `json:"observed_at"`
	LicenseRef string    `json:"license_ref"`
}

func (s Snapshot) Validate() error {
	if s.Source == "" || len(s.Source) > 128 || s.Version == "" || len(s.Version) > 256 ||
		s.ObservedAt.IsZero() || s.LicenseRef == "" || len(s.LicenseRef) > 256 {
		return errors.New("connector snapshot metadata is invalid")
	}
	if err := s.Cursor.Validate(); err != nil {
		return err
	}
	if len(s.Objects) == 0 || len(s.Objects) > MaxObjects {
		return errors.New("connector snapshot object count is outside bounds")
	}
	previous := ""
	for _, object := range s.Objects {
		if err := object.Validate(); err != nil {
			return err
		}
		identity := object.URI + "\x00" + object.Generation
		if identity <= previous {
			return errors.New("connector snapshot objects must be sorted and unique")
		}
		previous = identity
	}
	return nil
}

func (s Snapshot) Digest() (string, error) {
	if err := s.Validate(); err != nil {
		return "", err
	}
	payload, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}
