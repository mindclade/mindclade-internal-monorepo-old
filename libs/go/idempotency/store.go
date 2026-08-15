// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package idempotency

import (
	"context"
	"time"

	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/identifiers"
	"mindclade.internal/libs/go/requestmeta"
)

const (
	DefaultRecordTTL     = 24 * time.Hour
	MaximumRecordTTL     = 30 * 24 * time.Hour
	DefaultLeaseDuration = time.Minute
	MaximumLeaseDuration = time.Hour
)

type AcquireRequest struct {
	Identity      Identity
	Fingerprint   identifiers.Digest
	RequestID     requestmeta.RequestID
	TTL           time.Duration
	LeaseDuration time.Duration
}

func (request AcquireRequest) Normalized() AcquireRequest {
	if request.TTL == 0 {
		request.TTL = DefaultRecordTTL
	}
	if request.LeaseDuration == 0 {
		request.LeaseDuration = DefaultLeaseDuration
	}
	return request
}
func (request AcquireRequest) Validate() error {
	request = request.Normalized()
	if err := request.Identity.Validate(); err != nil {
		return invalid(ErrInvalidRequest, ReasonInvalidRequest, "invalid idempotency acquisition request", "idempotency.AcquireRequest.Validate", nil)
	}
	if !request.Fingerprint.Valid() {
		return invalid(ErrInvalidFingerprint, ReasonInvalidFingerprint, "invalid idempotency fingerprint", "idempotency.AcquireRequest.Validate", nil)
	}
	if !request.RequestID.IsZero() {
		if err := request.RequestID.Validate(); err != nil {
			return invalid(ErrInvalidRequest, ReasonInvalidRequest, "invalid idempotency request identifier", "idempotency.AcquireRequest.Validate", nil)
		}
	}
	if request.TTL <= 0 || request.TTL > MaximumRecordTTL || request.LeaseDuration <= 0 || request.LeaseDuration > MaximumLeaseDuration || request.LeaseDuration >= request.TTL {
		return invalid(ErrInvalidRequest, ReasonInvalidRequest, "invalid idempotency retention or lease duration", "idempotency.AcquireRequest.Validate", faults.Fields{"ttl": request.TTL.String(), "lease_duration": request.LeaseDuration.String()})
	}
	return nil
}

type Disposition string

const (
	DispositionAcquired   Disposition = "acquired"
	DispositionReplay     Disposition = "replay"
	DispositionInProgress Disposition = "in_progress"
	DispositionConflict   Disposition = "conflict"
)

func (disposition Disposition) Valid() bool {
	switch disposition {
	case DispositionAcquired, DispositionReplay, DispositionInProgress, DispositionConflict:
		return true
	default:
		return false
	}
}

type Acquisition struct {
	Disposition Disposition
	Record      Record
	Lease       Lease
}

func (acquisition Acquisition) Validate() error {
	if !acquisition.Disposition.Valid() || acquisition.Record.Validate() != nil {
		return invalid(ErrInvalidRequest, ReasonInvalidRequest, "invalid idempotency acquisition", "idempotency.Acquisition.Validate", nil)
	}
	switch acquisition.Disposition {
	case DispositionAcquired:
		lease := acquisition.Lease
		record := acquisition.Record
		if lease.Validate() != nil || record.State() != StateInProgress ||
			lease.RecordID != record.ID() || lease.Identity != record.Identity() ||
			!lease.Fingerprint.Equal(record.Fingerprint()) || !lease.ExpiresAt.Equal(record.LeaseExpiresAt()) ||
			lease.Version != record.Version() {
			return invalid(ErrInvalidRequest, ReasonInvalidRequest, "invalid acquired idempotency lease", "idempotency.Acquisition.Validate", nil)
		}
	case DispositionReplay:
		if acquisition.Record.State() != StateCompleted || !acquisition.Lease.IsZero() {
			return invalid(ErrInvalidRequest, ReasonInvalidRequest, "invalid replay acquisition", "idempotency.Acquisition.Validate", nil)
		}
	case DispositionInProgress:
		if acquisition.Record.State() != StateInProgress || !acquisition.Lease.IsZero() {
			return invalid(ErrInvalidRequest, ReasonInvalidRequest, "invalid in-progress acquisition", "idempotency.Acquisition.Validate", nil)
		}
	case DispositionConflict:
		if !acquisition.Lease.IsZero() {
			return invalid(ErrInvalidRequest, ReasonInvalidRequest, "unexpected lease in idempotency acquisition", "idempotency.Acquisition.Validate", nil)
		}
	}
	return nil
}

type CompleteRequest struct {
	Lease  Lease
	Result Result
}

func (request CompleteRequest) Validate() error {
	if err := request.Lease.Validate(); err != nil {
		return err
	}
	return request.Result.Validate()
}

type ReleaseRequest struct{ Lease Lease }

func (request ReleaseRequest) Validate() error { return request.Lease.Validate() }

type RenewRequest struct {
	Lease    Lease
	ExtendBy time.Duration
}

func (request RenewRequest) Validate() error {
	if err := request.Lease.Validate(); err != nil {
		return err
	}
	if request.ExtendBy <= 0 || request.ExtendBy > MaximumLeaseDuration {
		return invalid(ErrInvalidRequest, ReasonInvalidRequest, "invalid idempotency lease extension", "idempotency.RenewRequest.Validate", nil)
	}
	return nil
}

// Store must implement Acquire, Complete, Release, and Renew atomically with
// respect to one Identity. Compare-and-swap must include lease token and
// version so stale workers cannot commit or delete reclaimed work.
type Store interface {
	Acquire(context.Context, AcquireRequest) (Acquisition, error)
	Complete(context.Context, CompleteRequest) (Record, error)
	Release(context.Context, ReleaseRequest) error
	Renew(context.Context, RenewRequest) (Lease, error)
	Lookup(context.Context, Identity) (Record, error)
}
