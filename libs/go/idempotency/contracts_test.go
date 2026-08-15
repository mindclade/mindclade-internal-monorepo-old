// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package idempotency

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/identifiers"
	"mindclade.internal/libs/go/requestmeta"
)

func testIdentity(t *testing.T) Identity {
	t.Helper()
	identity, err := NewIdentity(MustParseScope("control-plane/runs.create"), MustParseKey("request-123456"))
	if err != nil {
		t.Fatal(err)
	}
	return identity
}

func testRecordData(t *testing.T, state State) RecordData {
	t.Helper()
	now := time.Date(2026, 8, 12, 18, 0, 0, 0, time.UTC)
	identifier, err := identifiers.NewIDAt(RecordIDKind, now)
	if err != nil {
		t.Fatal(err)
	}
	data := RecordData{
		ID:          identifier,
		Identity:    testIdentity(t),
		Fingerprint: identifiers.SHA256String("canonical-request"),
		State:       state,
		CreatedAt:   now,
		UpdatedAt:   now,
		ExpiresAt:   now.Add(time.Hour),
		Version:     1,
	}
	switch state {
	case StateInProgress:
		data.LeaseExpiresAt = now.Add(time.Minute)
	case StateCompleted:
		data.Result, err = NewResult([]byte("created"), "application/json", map[string]string{"model": "clade-1"})
		if err != nil {
			t.Fatal(err)
		}
	}
	return data
}

func TestKeyAndScopeSerializationAndSQL(t *testing.T) {
	t.Parallel()

	key := MustParseKey("request-123456")
	text, err := key.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	var decodedKey Key
	if err := decodedKey.UnmarshalText(text); err != nil || decodedKey != key || !decodedKey.Valid() {
		t.Fatalf("key text round trip = %#v, %v", decodedKey, err)
	}
	if value, err := key.Value(); err != nil || value != key.String() {
		t.Fatalf("key Value() = %#v, %v", value, err)
	}
	if err := decodedKey.Scan([]byte(key.String())); err != nil {
		t.Fatal(err)
	}
	if err := decodedKey.Scan(nil); err != nil || !decodedKey.IsZero() {
		t.Fatalf("key Scan(nil) = %#v, %v", decodedKey, err)
	}
	if err := decodedKey.Scan(42); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("key Scan(int) error = %v", err)
	}
	var nilKey *Key
	if err := nilKey.UnmarshalJSON([]byte(`null`)); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("nil key receiver error = %v", err)
	}

	scope := MustParseScope("control-plane/runs.create")
	text, err = scope.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	var decodedScope Scope
	if err := decodedScope.UnmarshalText(text); err != nil || decodedScope != scope || !decodedScope.Valid() {
		t.Fatalf("scope text round trip = %#v, %v", decodedScope, err)
	}
	encoded, err := json.Marshal(scope)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, &decodedScope); err != nil || decodedScope != scope {
		t.Fatalf("scope JSON round trip = %#v, %v", decodedScope, err)
	}
	var nilScope *Scope
	if err := nilScope.UnmarshalJSON([]byte(`null`)); !errors.Is(err, ErrInvalidScope) {
		t.Fatalf("nil scope receiver error = %v", err)
	}
}

func TestIdentityAndResultCanonicalization(t *testing.T) {
	t.Parallel()

	identity := testIdentity(t)
	if identity.IsZero() || !identity.Valid() || identity.String() == "" || !identity.Digest().Valid() {
		t.Fatalf("identity = %#v", identity)
	}

	metadata := map[string]string{" model ": "clade-1"}
	result, err := NewResult([]byte("ok"), " application/json ", metadata)
	if err != nil {
		t.Fatal(err)
	}
	metadata[" model "] = "mutated"
	if result.ContentType() != "application/json" || result.Metadata()["model"] != "clade-1" || result.IsZero() || !result.Digest().Equal(identifiers.SHA256String("ok")) {
		t.Fatalf("result = %#v", result)
	}
	if _, err := NewResult(nil, "", map[string]string{"api-key": "secret"}); !errors.Is(err, ErrInvalidResult) {
		t.Fatalf("sensitive metadata error = %v", err)
	}
	if _, err := NewResult(nil, "", map[string]string{" model ": "one", "model": "two"}); !errors.Is(err, ErrInvalidResult) {
		t.Fatalf("duplicate canonical metadata error = %v", err)
	}
	empty, err := EmptyResult()
	if err != nil || empty.IsZero() || len(empty.Payload()) != 0 {
		t.Fatalf("EmptyResult() = %#v, %v", empty, err)
	}
}

func TestRecordJSONAndTemporalInvariants(t *testing.T) {
	t.Parallel()

	data := testRecordData(t, StateInProgress)
	requestID := requestmeta.MustParseRequestID("request_018f3f4a5b6c7d8e8f900123456789ab")
	data.RequestID = requestID
	record, err := NewRecord(data)
	if err != nil {
		t.Fatal(err)
	}
	if record.IsZero() || record.State().String() != "in_progress" || record.RequestID() != requestID || record.CreatedAt().IsZero() || record.UpdatedAt().IsZero() || record.ExpiresAt().IsZero() || record.Version() != 1 {
		t.Fatalf("record accessors = %#v", record)
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Record
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ID() != record.ID() || decoded.Identity() != record.Identity() || !decoded.Fingerprint().Equal(record.Fingerprint()) {
		t.Fatalf("record round trip = %#v", decoded)
	}
	var nilRecord *Record
	if err := nilRecord.UnmarshalJSON(encoded); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("nil record receiver error = %v", err)
	}

	invalid := data
	invalid.LeaseExpiresAt = invalid.ExpiresAt.Add(time.Second)
	if _, err := NewRecord(invalid); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("lease beyond retention error = %v", err)
	}
	invalid = data
	invalid.UpdatedAt = invalid.ExpiresAt
	if _, err := NewRecord(invalid); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("update at expiration error = %v", err)
	}
}

func TestLeaseAndAcquisitionValidation(t *testing.T) {
	t.Parallel()

	record, err := NewRecord(testRecordData(t, StateInProgress))
	if err != nil {
		t.Fatal(err)
	}
	token := identifiers.MustParseUUID("018f3f4a-5b6c-4d8e-8f90-0123456789ab")
	lease := Lease{
		RecordID:    record.ID(),
		Identity:    record.Identity(),
		Fingerprint: record.Fingerprint(),
		Token:       token,
		ExpiresAt:   record.LeaseExpiresAt(),
		Version:     record.Version(),
	}
	if lease.IsZero() || lease.Validate() != nil {
		t.Fatalf("lease = %#v", lease)
	}
	acquisition := Acquisition{Disposition: DispositionAcquired, Record: record, Lease: lease}
	if err := acquisition.Validate(); err != nil {
		t.Fatal(err)
	}
	mismatched := acquisition
	mismatched.Lease.Version++
	if err := mismatched.Validate(); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("mismatched acquisition error = %v", err)
	}

	completed, err := NewRecord(testRecordData(t, StateCompleted))
	if err != nil {
		t.Fatal(err)
	}
	if err := (Acquisition{Disposition: DispositionReplay, Record: completed}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (Acquisition{Disposition: DispositionInProgress, Record: record}).Validate(); err != nil {
		t.Fatal(err)
	}
	if !(Lease{}).IsZero() {
		t.Fatal("zero lease was not zero")
	}
}

func TestStoreRequestValidation(t *testing.T) {
	t.Parallel()

	request := AcquireRequest{Identity: testIdentity(t), Fingerprint: identifiers.SHA256String("body")}
	normalized := request.Normalized()
	if normalized.TTL != DefaultRecordTTL || normalized.LeaseDuration != DefaultLeaseDuration || normalized.Validate() != nil {
		t.Fatalf("normalized request = %#v", normalized)
	}
	request.TTL = time.Minute
	request.LeaseDuration = time.Minute
	if err := request.Validate(); !errors.Is(err, ErrInvalidRequest) || !faults.IsReason(err, ReasonInvalidRequest) {
		t.Fatalf("invalid duration error = %v", err)
	}

	record, err := NewRecord(testRecordData(t, StateInProgress))
	if err != nil {
		t.Fatal(err)
	}
	lease := Lease{
		RecordID: record.ID(), Identity: record.Identity(), Fingerprint: record.Fingerprint(),
		Token:     identifiers.MustParseUUID("018f3f4a-5b6c-4d8e-8f90-0123456789ab"),
		ExpiresAt: record.LeaseExpiresAt(), Version: record.Version(),
	}
	result, _ := NewResult([]byte("done"), "", nil)
	if err := (CompleteRequest{Lease: lease, Result: result}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (ReleaseRequest{Lease: lease}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (RenewRequest{Lease: lease, ExtendBy: time.Minute}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (RenewRequest{Lease: lease}).Validate(); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("invalid renewal error = %v", err)
	}
}
