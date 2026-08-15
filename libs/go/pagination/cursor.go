// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package pagination

import (
	"strings"
	"time"

	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/identifiers"
)

const (
	CursorSchemaVersion     = 1
	MaximumTokenBytes       = 8192
	MaximumBindingBytes     = 256
	MaximumOrderFields      = 8
	MaximumCursorValues     = 16
	MaximumCursorValueBytes = 1024
)

type Direction string

const (
	Ascending  Direction = "asc"
	Descending Direction = "desc"
)

type Order struct {
	Field     string    `json:"field"`
	Direction Direction `json:"direction"`
}

func (order Order) Validate() error {
	if !canonical(order.Field) || (order.Direction != Ascending && order.Direction != Descending) {
		return invalid(ErrInvalidCursor, "invalid_cursor_order", "pagination.Order.Validate")
	}
	return nil
}

type Binding struct {
	Scope        string
	Resource     string
	FilterDigest identifiers.Digest
	Order        []Order
}

func (binding Binding) Validate() error {
	if !canonical(binding.Scope) || !canonical(binding.Resource) || binding.FilterDigest.IsZero() || len(binding.Order) == 0 || len(binding.Order) > MaximumOrderFields {
		return invalid(ErrInvalidCursor, "invalid_cursor_binding", "pagination.Binding.Validate")
	}
	for _, order := range binding.Order {
		if err := order.Validate(); err != nil {
			return err
		}
	}
	return nil
}
func (binding Binding) clone() Binding {
	binding.Order = append([]Order(nil), binding.Order...)
	return binding
}

type Cursor struct {
	Version      int       `json:"version"`
	Scope        string    `json:"scope"`
	Resource     string    `json:"resource"`
	FilterDigest string    `json:"filter_digest"`
	Order        []Order   `json:"order"`
	Values       []string  `json:"values"`
	IssuedAt     time.Time `json:"issued_at"`
	ExpiresAt    time.Time `json:"expires_at"`
}

func NewCursor(binding Binding, values []string, issuedAt, expiresAt time.Time) (Cursor, error) {
	if err := binding.Validate(); err != nil {
		return Cursor{}, err
	}
	cursor := Cursor{Version: CursorSchemaVersion, Scope: binding.Scope, Resource: binding.Resource, FilterDigest: binding.FilterDigest.String(), Order: append([]Order(nil), binding.Order...), Values: append([]string(nil), values...), IssuedAt: issuedAt.Round(0).UTC(), ExpiresAt: expiresAt.Round(0).UTC()}
	if err := cursor.Validate(); err != nil {
		return Cursor{}, err
	}
	return cursor, nil
}
func (cursor Cursor) Validate() error {
	if cursor.Version != CursorSchemaVersion || !canonical(cursor.Scope) || !canonical(cursor.Resource) || len(cursor.Order) == 0 || len(cursor.Order) > MaximumOrderFields || len(cursor.Values) == 0 || len(cursor.Values) > MaximumCursorValues || cursor.IssuedAt.IsZero() || cursor.ExpiresAt.IsZero() || !cursor.ExpiresAt.After(cursor.IssuedAt) {
		return invalid(ErrInvalidCursor, "invalid_cursor", "pagination.Cursor.Validate")
	}
	if _, err := identifiers.ParseDigest(cursor.FilterDigest); err != nil {
		return invalid(ErrInvalidCursor, "invalid_cursor_filter_digest", "pagination.Cursor.Validate")
	}
	for _, order := range cursor.Order {
		if err := order.Validate(); err != nil {
			return err
		}
	}
	for _, value := range cursor.Values {
		if len(value) == 0 || len(value) > MaximumCursorValueBytes {
			return invalid(ErrInvalidCursor, "invalid_cursor_value", "pagination.Cursor.Validate")
		}
	}
	return nil
}
func (cursor Cursor) Binding() (Binding, error) {
	digest, err := identifiers.ParseDigest(cursor.FilterDigest)
	if err != nil {
		return Binding{}, err
	}
	binding := Binding{Scope: cursor.Scope, Resource: cursor.Resource, FilterDigest: digest, Order: append([]Order(nil), cursor.Order...)}
	return binding, binding.Validate()
}
func (cursor Cursor) Matches(binding Binding) bool {
	if err := binding.Validate(); err != nil {
		return false
	}
	if cursor.Scope != binding.Scope || cursor.Resource != binding.Resource || cursor.FilterDigest != binding.FilterDigest.String() || len(cursor.Order) != len(binding.Order) {
		return false
	}
	for index := range cursor.Order {
		if cursor.Order[index] != binding.Order[index] {
			return false
		}
	}
	return true
}
func canonical(value string) bool {
	if value == "" || len(value) > MaximumBindingBytes || strings.TrimSpace(value) != value {
		return false
	}
	for index, character := range value {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' {
			continue
		}
		if index > 0 && (character == '-' || character == '_' || character == '.' || character == '/' || character == ':') {
			continue
		}
		return false
	}
	return true
}
func invalid(cause error, reason, operation string) error {
	return faults.Wrap(cause, faults.CodeInvalidArgument, "invalid pagination cursor", faults.WithReason(reason), faults.WithOperation(operation), faults.WithRetryPolicy(faults.NoRetry()))
}
