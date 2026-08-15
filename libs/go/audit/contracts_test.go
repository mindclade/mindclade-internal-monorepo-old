// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package audit

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"mindclade.internal/libs/go/auth"
	"mindclade.internal/libs/go/clock"
	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/identifiers"
)

func TestActionAndOutcomeContracts(t *testing.T) {
	t.Parallel()

	action := MustParseAction("models.release.promote")
	text, err := action.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	var decoded Action
	if err := decoded.UnmarshalText(text); err != nil || decoded != action {
		t.Fatalf("action round trip = %q, %v", decoded, err)
	}
	for _, invalid := range []string{"Models.release", ".models.release", "models..release", "models.release-"} {
		if _, err := ParseAction(invalid); !errors.Is(err, ErrInvalidAction) {
			t.Fatalf("ParseAction(%q) error = %v", invalid, err)
		}
	}
	var nilAction *Action
	if err := nilAction.UnmarshalText([]byte("runs.read")); !errors.Is(err, ErrInvalidAction) {
		t.Fatalf("nil action receiver error = %v", err)
	}
	for _, outcome := range []Outcome{OutcomeSucceeded, OutcomeFailed, OutcomeDenied} {
		if !outcome.Valid() || outcome.String() == "" {
			t.Fatalf("invalid outcome %q", outcome)
		}
	}
	if Outcome("unknown").Valid() {
		t.Fatal("unknown outcome validated")
	}
}

func TestActorJSONPreservesEqualIdentifierValues(t *testing.T) {
	t.Parallel()

	identifier := identifiers.MustParseID("user_018f3f4a5b6c7d8e8f900123456789ab")
	principal, err := auth.NewPrincipal(
		auth.PrincipalKindUser,
		"user-42",
		auth.WithIssuer("https://identity.example"),
		auth.WithPrincipalID(identifier),
		auth.WithOrganizationID(identifier),
	)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := ActorFromPrincipal(principal)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(actor)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Actor
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.PrincipalID() != identifier || decoded.OrganizationID() != identifier || decoded.Kind() != auth.PrincipalKindUser || decoded.Subject() != "user-42" || decoded.Issuer() == "" {
		t.Fatalf("decoded actor = %#v", decoded)
	}
	var nilActor *Actor
	if err := nilActor.UnmarshalJSON(encoded); !errors.Is(err, ErrInvalidActor) {
		t.Fatalf("nil actor receiver error = %v", err)
	}

	system, err := NewSystemActor("scheduler")
	if err != nil || system.Kind() != auth.PrincipalKindSystem || system.TenantID().String() != "" {
		t.Fatalf("system actor = %#v, %v", system, err)
	}
}

func TestTargetAndChangeContracts(t *testing.T) {
	t.Parallel()

	parent := identifiers.MustParseID("org_018f3f4a5b6c7d8e8f900123456789ac")
	resource := identifiers.MustParseID("run_018f3f4a5b6c7d8e8f900123456789ad")
	target, err := NewTarget("training.run", WithTargetID(resource), WithParentTargetID(parent), WithTargetName("Run 42"))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(target)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Target
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Type() != "training.run" || decoded.ID() != resource || decoded.ParentID() != parent || decoded.Name() != "Run 42" {
		t.Fatalf("decoded target = %#v", decoded)
	}
	var nilTarget *Target
	if err := nilTarget.UnmarshalJSON(encoded); !errors.Is(err, ErrInvalidTarget) {
		t.Fatalf("nil target receiver error = %v", err)
	}
	if _, err := NewTarget("run", WithTargetID(resource), WithParentTargetID(resource)); !errors.Is(err, ErrInvalidTarget) {
		t.Fatalf("target cycle error = %v", err)
	}

	before := identifiers.SHA256String("before")
	after := identifiers.SHA256String("after")
	change, err := NewChange(before, after, "status", "status", "updated_at")
	if err != nil {
		t.Fatal(err)
	}
	if change.IsZero() || change.BeforeDigest() != before || change.AfterDigest() != after || len(change.Fields()) != 2 {
		t.Fatalf("change = %#v", change)
	}
}

func TestFieldSanitization(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"api-key", "authorization.header", "session_cookie"} {
		if err := (Fields{key: "value"}).Validate(); !errors.Is(err, ErrInvalidFields) {
			t.Fatalf("sensitive field %q error = %v", key, err)
		}
	}
	fields := Fields{"region": "us-central1"}
	clone := fields.Clone()
	clone["region"] = "mutated"
	if fields["region"] != "us-central1" {
		t.Fatal("Fields.Clone returned an alias")
	}
}

func TestEventAccessorsOptionsAndNilReceiver(t *testing.T) {
	t.Parallel()

	factory, actor, target, metadata := auditFixture(t)
	occurredAt := time.Date(2026, 8, 12, 22, 0, 0, 0, time.UTC)
	identifier, err := identifiers.NewIDAt(EventIDKind, occurredAt)
	if err != nil {
		t.Fatal(err)
	}
	fields := Fields{"region": "us-central1"}
	event, err := factory.Create(
		MustParseAction("runs.cancel"),
		actor,
		target,
		OutcomeDenied,
		WithReason("policy_denied"),
		WithRequestMetadata(metadata),
		WithFields(fields),
		WithOccurredAt(occurredAt),
		WithEventID(identifier),
	)
	if err != nil {
		t.Fatal(err)
	}
	fields["region"] = "mutated"
	if event.ID() != identifier || event.SchemaVersion() != CurrentSchemaVersion || !event.OccurredAt().Equal(occurredAt) || event.Outcome() != OutcomeDenied || event.Actor().Subject() != actor.Subject() || event.Target().ID() != target.ID() || event.Reason() != "policy_denied" || event.Fields()["region"] != "us-central1" {
		t.Fatalf("event accessors = %#v", event)
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	var nilEvent *Event
	if err := nilEvent.UnmarshalJSON(encoded); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("nil event receiver error = %v", err)
	}
}

func TestFactoryAndRecorderTypedNilAndClassification(t *testing.T) {
	t.Parallel()

	var nilClock *clock.FakeClock
	if _, err := NewFactory(WithClock(nilClock)); !errors.Is(err, ErrNilFactory) {
		t.Fatalf("typed nil clock error = %v", err)
	}

	factory, actor, target, _ := auditFixture(t)
	event, err := factory.Create(MustParseAction("runs.read"), actor, target, OutcomeSucceeded)
	if err != nil {
		t.Fatal(err)
	}
	var nilRecorder RecorderFunc
	if err := Record(context.Background(), nilRecorder, event); !errors.Is(err, ErrNilRecorder) {
		t.Fatalf("typed nil recorder error = %v", err)
	}

	providerFailure := faults.New(
		faults.CodeResourceExhausted,
		"audit queue is full",
		faults.WithReason("audit_queue_full"),
		faults.WithField("queue", "audit"),
		faults.WithRetryPolicy(faults.BackoffRetry(3)),
	)
	err = Record(context.Background(), RecorderFunc(func(context.Context, Event) error { return providerFailure }), event)
	if !errors.Is(err, ErrRecorderFailure) || !faults.IsCode(err, faults.CodeResourceExhausted) || !faults.IsReason(err, "audit_queue_full") || faults.FieldsOf(err)["audit_event_id"] != event.ID().String() || !faults.IsRetryable(err) {
		t.Fatalf("classified recorder error = %v", err)
	}
	if err := Record(context.Background(), NopRecorder{}, event); err != nil {
		t.Fatal(err)
	}
}

func TestFactoryDefaultGeneratorTracksInjectedClock(t *testing.T) {
	t.Parallel()

	now := time.Date(2040, 1, 2, 3, 4, 5, 678000000, time.UTC)
	factory, err := NewFactory(WithClock(clock.NewFake(now)))
	if err != nil {
		t.Fatal(err)
	}
	actor, err := NewSystemActor("scheduler")
	if err != nil {
		t.Fatal(err)
	}
	target, err := NewTarget("training.run")
	if err != nil {
		t.Fatal(err)
	}
	event, err := factory.Create(MustParseAction("runs.schedule"), actor, target, OutcomeSucceeded)
	if err != nil {
		t.Fatal(err)
	}
	identifierTime, ok := event.ID().Time()
	if !ok || !identifierTime.Equal(now.Truncate(time.Millisecond)) || !event.OccurredAt().Equal(now) {
		t.Fatalf("event ID time=%v (%v), occurred_at=%v", identifierTime, ok, event.OccurredAt())
	}
	if _, err := NewFactory(WithGenerator(nil)); !errors.Is(err, ErrNilFactory) {
		t.Fatalf("nil generator error = %v", err)
	}
}

func TestAuditValidationPreservesSpecificAndAggregateSentinels(t *testing.T) {
	t.Parallel()

	var actor Actor
	if err := json.Unmarshal([]byte(`{"kind":"user","principal_id":"not-an-id","subject":"user-42","issuer":"mindclade"}`), &actor); !errors.Is(err, ErrInvalidActor) {
		t.Fatalf("malformed actor error = %v", err)
	}
	var target Target
	if err := json.Unmarshal([]byte(`{"type":"training.run","id":"not-an-id"}`), &target); !errors.Is(err, ErrInvalidTarget) {
		t.Fatalf("malformed target error = %v", err)
	}

	factory, _, validTarget, _ := auditFixture(t)
	_, err := factory.Create(MustParseAction("runs.cancel"), Actor{}, validTarget, OutcomeSucceeded)
	if !errors.Is(err, ErrInvalidEvent) || !errors.Is(err, ErrInvalidActor) {
		t.Fatalf("invalid actor event error = %v", err)
	}
}
