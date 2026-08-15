// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package audit

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"mindclade.internal/libs/go/auth"
	"mindclade.internal/libs/go/clock"
	"mindclade.internal/libs/go/identifiers"
	"mindclade.internal/libs/go/requestmeta"
)

type zeroReader struct{}

func (zeroReader) Read(value []byte) (int, error) { clear(value); return len(value), nil }

func auditFixture(t *testing.T) (*Factory, Actor, Target, requestmeta.Metadata) {
	t.Helper()
	start := time.Date(2026, 8, 12, 21, 0, 0, 0, time.UTC)
	generator, err := identifiers.NewGenerator(
		identifiers.WithTimeSource(func() time.Time { return start }),
		identifiers.WithEntropySource(zeroReader{}),
	)
	if err != nil {
		t.Fatal(err)
	}
	factory, err := NewFactory(WithClock(clock.NewFake(start)), WithGenerator(generator))
	if err != nil {
		t.Fatal(err)
	}
	principal, err := auth.NewPrincipal(auth.PrincipalKindUser, "user-42", auth.WithIssuer("mindclade"))
	if err != nil {
		t.Fatal(err)
	}
	actor, err := ActorFromPrincipal(principal)
	if err != nil {
		t.Fatal(err)
	}
	runID, err := generator.ID(identifiers.MustParseKind("run"))
	if err != nil {
		t.Fatal(err)
	}
	target, err := NewTarget("run", WithTargetID(runID), WithTargetName("training-run"))
	if err != nil {
		t.Fatal(err)
	}
	requestID, err := requestmeta.NewRequestIDAt(start)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := requestmeta.New(requestID)
	if err != nil {
		t.Fatal(err)
	}
	metadata.Operation = requestmeta.MustParseOperation("runs.cancel")
	return factory, actor, target, metadata
}

func TestEventCreationAndJSONRoundTrip(t *testing.T) {
	factory, actor, target, metadata := auditFixture(t)
	change, err := NewChange(identifiers.SHA256String("before"), identifiers.SHA256String("after"), "status", "updated_at")
	if err != nil {
		t.Fatal(err)
	}
	event, err := factory.Create(
		MustParseAction("runs.cancel"), actor, target, OutcomeSucceeded,
		WithRequestMetadata(metadata), WithChange(change), WithFields(Fields{"region": "us-central1"}),
	)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Event
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ID() != event.ID() || decoded.Action() != event.Action() || decoded.Change().AfterDigest() != change.AfterDigest() {
		t.Fatalf("round trip = %#v", decoded)
	}
	request, ok := decoded.RequestMetadata()
	if !ok || request.RequestID != metadata.RequestID {
		t.Fatalf("request = %#v, %v", request, ok)
	}
}

func TestFailedEventRequiresReason(t *testing.T) {
	factory, actor, target, _ := auditFixture(t)
	_, err := factory.Create(MustParseAction("runs.cancel"), actor, target, OutcomeFailed)
	if !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := factory.Create(MustParseAction("runs.cancel"), actor, target, OutcomeFailed, WithReason("backend_unavailable")); err != nil {
		t.Fatal(err)
	}
}

func TestFieldsRejectSensitiveKeys(t *testing.T) {
	err := (Fields{"api_token": "secret"}).Validate()
	if !errors.Is(err, ErrInvalidFields) {
		t.Fatalf("Fields.Validate() = %v", err)
	}
}
