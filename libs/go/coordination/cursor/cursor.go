// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package cursor

import (
	"bytes"
	"time"

	"mindclade.internal/libs/go/faults"
)

const MaximumOpaqueBytes = 64 * 1024

// Cursor is the latest committed position for one durable stream. Sequence is
// monotonic. Version is the compare-and-swap revision. Fence is the ownership
// epoch of the coordinator that committed the position.
type Cursor struct {
	Key       Key       `json:"key"`
	Sequence  uint64    `json:"sequence"`
	Opaque    []byte    `json:"opaque,omitempty"`
	Fence     uint64    `json:"fence"`
	Version   uint64    `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
}

func New(key Key, sequence uint64, opaque []byte, fence, version uint64, updatedAt time.Time) (Cursor, error) {
	value := Cursor{Key: key, Sequence: sequence, Opaque: append([]byte(nil), opaque...), Fence: fence, Version: version, UpdatedAt: updatedAt.Round(0).UTC()}
	if err := value.Validate(); err != nil {
		return Cursor{}, err
	}
	return value, nil
}
func (value Cursor) Validate() error {
	if err := value.Key.Validate(); err != nil || value.Version == 0 || value.Fence == 0 || value.UpdatedAt.IsZero() || len(value.Opaque) > MaximumOpaqueBytes {
		return faults.Wrap(ErrInvalidRequest, faults.CodeInvalidArgument, "invalid cursor", faults.WithReason("invalid_cursor"), faults.WithOperation("cursor.Cursor.Validate"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return nil
}
func (value Cursor) Clone() Cursor { value.Opaque = append([]byte(nil), value.Opaque...); return value }
func (value Cursor) SamePosition(other Cursor) bool {
	return value.Sequence == other.Sequence && bytes.Equal(value.Opaque, other.Opaque)
}

type AdvanceRequest struct {
	Key             Key
	ExpectedVersion uint64
	Sequence        uint64
	Opaque          []byte
	Fence           uint64
	UpdatedAt       time.Time
}

func (request AdvanceRequest) Validate() error {
	if err := request.Key.Validate(); err != nil || request.Fence == 0 || request.UpdatedAt.IsZero() || len(request.Opaque) > MaximumOpaqueBytes {
		return faults.Wrap(ErrInvalidRequest, faults.CodeInvalidArgument, "invalid cursor advance request", faults.WithReason("invalid_cursor_advance"), faults.WithOperation("cursor.AdvanceRequest.Validate"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return nil
}
