// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package audit

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/identifiers"
	"mindclade.internal/libs/go/requestmeta"
)

const CurrentSchemaVersion = 1

const EventIDKind identifiers.Kind = "audit"

// Event is an immutable audit record.
type Event struct {
	id            identifiers.ID
	schemaVersion int
	occurredAt    time.Time
	action        Action
	outcome       Outcome
	actor         Actor
	target        Target
	request       requestmeta.Metadata
	reason        string
	change        Change
	fields        Fields
}

func (event Event) ID() identifiers.ID    { return event.id }
func (event Event) SchemaVersion() int    { return event.schemaVersion }
func (event Event) OccurredAt() time.Time { return event.occurredAt }
func (event Event) Action() Action        { return event.action }
func (event Event) Outcome() Outcome      { return event.outcome }
func (event Event) Actor() Actor          { return event.actor }
func (event Event) Target() Target        { return event.target }
func (event Event) Reason() string        { return event.reason }
func (event Event) Change() Change        { return event.change }
func (event Event) Fields() Fields        { return event.fields.Clone() }
func (event Event) RequestMetadata() (requestmeta.Metadata, bool) {
	return event.request, !event.request.IsZero()
}

func (event Event) Validate() error {
	if event.id.IsZero() || event.id.Kind() != EventIDKind || event.id.Validate() != nil {
		return invalidEvent("invalid_audit_event_id", nil)
	}
	if event.schemaVersion != CurrentSchemaVersion {
		return invalidEvent("unsupported_audit_schema", nil)
	}
	if event.occurredAt.IsZero() {
		return invalidEvent("missing_audit_timestamp", nil)
	}
	if !event.action.Valid() || !event.outcome.Valid() {
		return invalidEvent("invalid_audit_classification", nil)
	}
	if err := event.actor.Validate(); err != nil {
		return invalidEvent("invalid_audit_actor", err)
	}
	if err := event.target.Validate(); err != nil {
		return invalidEvent("invalid_audit_target", err)
	}
	if !event.request.IsZero() {
		if err := event.request.Validate(); err != nil {
			return invalidEvent("invalid_audit_request_metadata", err)
		}
	}
	if event.reason != "" && !validCanonicalName(event.reason, MaximumReasonLength, false) {
		return invalidEvent("invalid_audit_reason", nil)
	}
	if event.outcome != OutcomeSucceeded && event.reason == "" {
		return invalidEvent("missing_audit_reason", nil)
	}
	if err := event.change.Validate(); err != nil {
		return invalidEvent("invalid_audit_change", err)
	}
	if err := event.fields.Validate(); err != nil {
		return invalidEvent("invalid_audit_fields", err)
	}
	return nil
}

func invalidEvent(reason string, cause error) error {
	if cause == nil {
		cause = ErrInvalidEvent
	} else {
		cause = errors.Join(ErrInvalidEvent, cause)
	}
	return faults.Wrap(cause, faults.CodeInvalidArgument, "invalid audit event", faults.WithReason(reason), faults.WithOperation("audit.Event.Validate"), faults.WithRetryPolicy(faults.NoRetry()))
}

type eventJSON struct {
	ID            string                `json:"id"`
	SchemaVersion int                   `json:"schema_version"`
	OccurredAt    time.Time             `json:"occurred_at"`
	Action        Action                `json:"action"`
	Outcome       Outcome               `json:"outcome"`
	Actor         Actor                 `json:"actor"`
	Target        Target                `json:"target"`
	Request       *requestmeta.Metadata `json:"request,omitempty"`
	Reason        string                `json:"reason,omitempty"`
	BeforeDigest  string                `json:"before_digest,omitempty"`
	AfterDigest   string                `json:"after_digest,omitempty"`
	ChangedFields []string              `json:"changed_fields,omitempty"`
	Fields        Fields                `json:"fields,omitempty"`
}

func (event Event) MarshalJSON() ([]byte, error) {
	if err := event.Validate(); err != nil {
		return nil, err
	}
	wire := eventJSON{
		ID: event.id.String(), SchemaVersion: event.schemaVersion, OccurredAt: event.occurredAt.UTC(),
		Action: event.action, Outcome: event.outcome, Actor: event.actor, Target: event.target,
		Reason: event.reason, BeforeDigest: event.change.before.String(), AfterDigest: event.change.after.String(),
		ChangedFields: event.change.Fields(), Fields: event.fields.Clone(),
	}
	if !event.request.IsZero() {
		request := event.request
		wire.Request = &request
	}
	return json.Marshal(wire)
}

func (event *Event) UnmarshalJSON(value []byte) error {
	if event == nil {
		return invalidEvent("nil_audit_event", ErrInvalidEvent)
	}
	var wire eventJSON
	if err := json.Unmarshal(value, &wire); err != nil {
		return invalidEvent("malformed_audit_event", err)
	}
	identifier, err := identifiers.ParseIDKind(wire.ID, EventIDKind)
	if err != nil {
		return invalidEvent("invalid_audit_event_id", err)
	}
	before, after := identifiers.Digest{}, identifiers.Digest{}
	if wire.BeforeDigest != "" {
		before, err = identifiers.ParseDigest(wire.BeforeDigest)
		if err != nil {
			return invalidEvent("invalid_before_digest", err)
		}
	}
	if wire.AfterDigest != "" {
		after, err = identifiers.ParseDigest(wire.AfterDigest)
		if err != nil {
			return invalidEvent("invalid_after_digest", err)
		}
	}
	change, err := NewChange(before, after, wire.ChangedFields...)
	if err != nil {
		return err
	}
	parsed := Event{
		id: identifier, schemaVersion: wire.SchemaVersion, occurredAt: wire.OccurredAt.Round(0).UTC(),
		action: wire.Action, outcome: wire.Outcome, actor: wire.Actor, target: wire.Target,
		reason: strings.TrimSpace(wire.Reason), change: change, fields: wire.Fields.Clone(),
	}
	if wire.Request != nil {
		parsed.request = *wire.Request
	}
	if err := parsed.Validate(); err != nil {
		return err
	}
	*event = parsed
	return nil
}
