// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package idempotency

import (
	"encoding/json"
	"errors"
	"time"

	"mindclade.internal/libs/go/identifiers"
	"mindclade.internal/libs/go/requestmeta"
)

const RecordIDKind identifiers.Kind = "idempotency"

type State string

const (
	StateInProgress State = "in_progress"
	StateCompleted  State = "completed"
)

func (state State) String() string { return string(state) }
func (state State) Valid() bool    { return state == StateInProgress || state == StateCompleted }

// RecordData is the durable representation used by Store adapters.
type RecordData struct {
	ID             identifiers.ID        `json:"id"`
	Identity       Identity              `json:"identity"`
	Fingerprint    identifiers.Digest    `json:"fingerprint"`
	State          State                 `json:"state"`
	Result         Result                `json:"result,omitempty"`
	RequestID      requestmeta.RequestID `json:"request_id,omitempty"`
	CreatedAt      time.Time             `json:"created_at"`
	UpdatedAt      time.Time             `json:"updated_at"`
	ExpiresAt      time.Time             `json:"expires_at"`
	LeaseExpiresAt time.Time             `json:"lease_expires_at,omitempty"`
	Version        uint64                `json:"version"`
}

// Record is immutable; Data returns a defensive result copy.
type Record struct{ data RecordData }

func NewRecord(data RecordData) (Record, error) {
	data.CreatedAt = data.CreatedAt.Round(0).UTC()
	data.UpdatedAt = data.UpdatedAt.Round(0).UTC()
	data.ExpiresAt = data.ExpiresAt.Round(0).UTC()
	data.LeaseExpiresAt = data.LeaseExpiresAt.Round(0).UTC()
	if !data.Result.IsZero() {
		result, err := NewResult(data.Result.Payload(), data.Result.ContentType(), data.Result.Metadata())
		if err != nil {
			return Record{}, err
		}
		data.Result = result
	}
	record := Record{data: data}
	if err := record.Validate(); err != nil {
		return Record{}, err
	}
	return record, nil
}
func (record Record) Data() RecordData {
	data := record.data
	if !data.Result.IsZero() {
		data.Result, _ = NewResult(data.Result.Payload(), data.Result.ContentType(), data.Result.Metadata())
	}
	return data
}
func (record Record) ID() identifiers.ID              { return record.data.ID }
func (record Record) Identity() Identity              { return record.data.Identity }
func (record Record) Fingerprint() identifiers.Digest { return record.data.Fingerprint }
func (record Record) State() State                    { return record.data.State }
func (record Record) Result() Result {
	if record.data.Result.IsZero() {
		return Result{}
	}
	result, _ := NewResult(record.data.Result.Payload(), record.data.Result.ContentType(), record.data.Result.Metadata())
	return result
}
func (record Record) RequestID() requestmeta.RequestID { return record.data.RequestID }
func (record Record) CreatedAt() time.Time             { return record.data.CreatedAt }
func (record Record) UpdatedAt() time.Time             { return record.data.UpdatedAt }
func (record Record) ExpiresAt() time.Time             { return record.data.ExpiresAt }
func (record Record) LeaseExpiresAt() time.Time        { return record.data.LeaseExpiresAt }
func (record Record) Version() uint64                  { return record.data.Version }
func (record Record) IsZero() bool                     { return record.data.ID.IsZero() }
func (record Record) Expired(now time.Time) bool {
	return !record.data.ExpiresAt.IsZero() && !now.Before(record.data.ExpiresAt)
}
func (record Record) LeaseExpired(now time.Time) bool {
	return record.data.State == StateInProgress && !record.data.LeaseExpiresAt.IsZero() && !now.Before(record.data.LeaseExpiresAt)
}
func (record Record) Validate() error {
	data := record.data
	if data.ID.IsZero() || data.ID.Kind() != RecordIDKind || data.ID.Validate() != nil {
		return invalid(ErrInvalidRecord, ReasonInvalidRecord, "invalid idempotency record identifier", "idempotency.Record.Validate", nil)
	}
	if err := data.Identity.Validate(); err != nil {
		return invalid(errors.Join(ErrInvalidRecord, err), ReasonInvalidRecord, "invalid idempotency record", "idempotency.Record.Validate", nil)
	}
	if !data.Fingerprint.Valid() {
		return invalid(ErrInvalidFingerprint, ReasonInvalidFingerprint, "invalid idempotency fingerprint", "idempotency.Record.Validate", nil)
	}
	if !data.State.Valid() || data.Version == 0 || data.CreatedAt.IsZero() || data.UpdatedAt.IsZero() || data.UpdatedAt.Before(data.CreatedAt) || !data.ExpiresAt.After(data.UpdatedAt) {
		return invalid(ErrInvalidRecord, ReasonInvalidRecord, "invalid idempotency record state", "idempotency.Record.Validate", nil)
	}
	if !data.RequestID.IsZero() {
		if err := data.RequestID.Validate(); err != nil {
			return invalid(errors.Join(ErrInvalidRecord, err), ReasonInvalidRecord, "invalid idempotency request identifier", "idempotency.Record.Validate", nil)
		}
	}
	switch data.State {
	case StateInProgress:
		if data.LeaseExpiresAt.IsZero() || !data.LeaseExpiresAt.After(data.UpdatedAt) || data.LeaseExpiresAt.After(data.ExpiresAt) || !data.Result.IsZero() {
			return invalid(ErrInvalidRecord, ReasonInvalidRecord, "invalid in-progress idempotency record", "idempotency.Record.Validate", nil)
		}
	case StateCompleted:
		if !data.LeaseExpiresAt.IsZero() || data.Result.IsZero() {
			return invalid(ErrInvalidRecord, ReasonInvalidRecord, "invalid completed idempotency record", "idempotency.Record.Validate", nil)
		}
		if err := data.Result.Validate(); err != nil {
			return err
		}
	}
	return nil
}

type recordJSON struct {
	ID             identifiers.ID        `json:"id"`
	Identity       Identity              `json:"identity"`
	Fingerprint    identifiers.Digest    `json:"fingerprint"`
	State          State                 `json:"state"`
	Result         *Result               `json:"result,omitempty"`
	RequestID      requestmeta.RequestID `json:"request_id,omitempty"`
	CreatedAt      time.Time             `json:"created_at"`
	UpdatedAt      time.Time             `json:"updated_at"`
	ExpiresAt      time.Time             `json:"expires_at"`
	LeaseExpiresAt time.Time             `json:"lease_expires_at,omitempty"`
	Version        uint64                `json:"version"`
}

func (record Record) MarshalJSON() ([]byte, error) {
	if err := record.Validate(); err != nil {
		return nil, err
	}
	data := record.Data()
	wire := recordJSON{
		ID:             data.ID,
		Identity:       data.Identity,
		Fingerprint:    data.Fingerprint,
		State:          data.State,
		RequestID:      data.RequestID,
		CreatedAt:      data.CreatedAt,
		UpdatedAt:      data.UpdatedAt,
		ExpiresAt:      data.ExpiresAt,
		LeaseExpiresAt: data.LeaseExpiresAt,
		Version:        data.Version,
	}
	if !data.Result.IsZero() {
		result := data.Result
		wire.Result = &result
	}
	return json.Marshal(wire)
}

func (record *Record) UnmarshalJSON(value []byte) error {
	if record == nil {
		return ErrInvalidRecord
	}
	var wire recordJSON
	if err := json.Unmarshal(value, &wire); err != nil {
		return err
	}
	data := RecordData{
		ID:             wire.ID,
		Identity:       wire.Identity,
		Fingerprint:    wire.Fingerprint,
		State:          wire.State,
		RequestID:      wire.RequestID,
		CreatedAt:      wire.CreatedAt,
		UpdatedAt:      wire.UpdatedAt,
		ExpiresAt:      wire.ExpiresAt,
		LeaseExpiresAt: wire.LeaseExpiresAt,
		Version:        wire.Version,
	}
	if wire.Result != nil {
		data.Result = *wire.Result
	}
	parsed, err := NewRecord(data)
	if err != nil {
		return err
	}
	*record = parsed
	return nil
}

// Lease is an unforgeable ownership token for an in-progress record.
type Lease struct {
	RecordID    identifiers.ID     `json:"record_id"`
	Identity    Identity           `json:"identity"`
	Fingerprint identifiers.Digest `json:"fingerprint"`
	Token       identifiers.UUID   `json:"token"`
	ExpiresAt   time.Time          `json:"expires_at"`
	Version     uint64             `json:"version"`
}

func (lease Lease) IsZero() bool {
	return lease.RecordID.IsZero() && lease.Identity.IsZero() && lease.Fingerprint.IsZero() && lease.Token.IsZero() && lease.ExpiresAt.IsZero() && lease.Version == 0
}

func (lease Lease) Validate() error {
	if lease.RecordID.IsZero() || lease.RecordID.Kind() != RecordIDKind || lease.RecordID.Validate() != nil || lease.Token.IsZero() || lease.Identity.Validate() != nil || !lease.Fingerprint.Valid() || lease.ExpiresAt.IsZero() || lease.Version == 0 {
		return invalid(ErrInvalidLease, ReasonInvalidLease, "invalid idempotency lease", "idempotency.Lease.Validate", nil)
	}
	return nil
}
