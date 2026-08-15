// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package audit

import (
	"strings"
	"time"

	"mindclade.internal/libs/go/clock"
	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/identifiers"
	"mindclade.internal/libs/go/requestmeta"
)

// Factory creates canonical audit events with injected time and ID sources.
type Factory struct {
	clock     clock.Clock
	generator *identifiers.Generator
}

type FactoryOption func(*Factory) error

func WithClock(value clock.Clock) FactoryOption {
	return func(factory *Factory) error {
		if nilInterface(value) {
			return faults.Wrap(ErrNilFactory, faults.CodeInvalidArgument, "audit clock is not configured", faults.WithReason("nil_audit_clock"), faults.WithOperation("audit.WithClock"))
		}
		factory.clock = value
		return nil
	}
}
func WithGenerator(value *identifiers.Generator) FactoryOption {
	return func(factory *Factory) error {
		if value == nil {
			return faults.Wrap(ErrNilFactory, faults.CodeInvalidArgument, "audit identifier generator is not configured", faults.WithReason("nil_audit_generator"), faults.WithOperation("audit.WithGenerator"), faults.WithRetryPolicy(faults.NoRetry()))
		}
		factory.generator = value
		return nil
	}
}

func NewFactory(options ...FactoryOption) (*Factory, error) {
	factory := &Factory{clock: clock.RealClock{}}
	for _, option := range options {
		if option != nil {
			if err := option(factory); err != nil {
				return nil, err
			}
		}
	}
	if nilInterface(factory.clock) {
		return nil, faults.Wrap(ErrNilFactory, faults.CodeInvalidArgument, "audit clock is not configured", faults.WithReason("nil_audit_clock"), faults.WithOperation("audit.NewFactory"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	if factory.generator == nil {
		generator, err := identifiers.NewGenerator(identifiers.WithTimeSource(factory.clock.Now))
		if err != nil {
			return nil, faults.Wrap(err, faults.CodeInternal, "unable to configure audit identifier generation", faults.WithReason("audit_generator_configuration_failed"), faults.WithOperation("audit.NewFactory"), faults.WithRetryPolicy(faults.NoRetry()))
		}
		factory.generator = generator
	}
	return factory, nil
}

// EventOption configures a newly created event.
type EventOption func(*Event) error

func WithReason(reason string) EventOption {
	return func(event *Event) error { event.reason = reason; return nil }
}
func WithRequestMetadata(metadata requestmeta.Metadata) EventOption {
	return func(event *Event) error { event.request = metadata; return nil }
}
func WithChange(change Change) EventOption {
	return func(event *Event) error { event.change = change; return nil }
}
func WithFields(fields Fields) EventOption {
	captured := fields.Clone()
	return func(event *Event) error { event.fields = captured; return nil }
}
func WithOccurredAt(timestamp time.Time) EventOption {
	return func(event *Event) error { event.occurredAt = timestamp.Round(0).UTC(); return nil }
}
func WithEventID(identifier identifiers.ID) EventOption {
	return func(event *Event) error { event.id = identifier; return nil }
}

func (factory *Factory) Create(action Action, actor Actor, target Target, outcome Outcome, options ...EventOption) (Event, error) {
	if factory == nil || nilInterface(factory.clock) {
		return Event{}, faults.Wrap(ErrNilFactory, faults.CodeFailedPrecondition, "audit factory is not configured", faults.WithReason("nil_audit_factory"), faults.WithOperation("audit.Factory.Create"))
	}
	identifier, err := factory.generator.ID(EventIDKind)
	if err != nil {
		return Event{}, faults.Wrap(err, faults.CodeInternal, "unable to generate audit event id", faults.WithReason("audit_id_generation_failed"), faults.WithOperation("audit.Factory.Create"))
	}
	event := Event{
		id: identifier, schemaVersion: CurrentSchemaVersion, occurredAt: factory.clock.Now().Round(0).UTC(),
		action: action, outcome: outcome, actor: actor, target: target,
	}
	for _, option := range options {
		if option != nil {
			if err := option(&event); err != nil {
				return Event{}, err
			}
		}
	}
	event.reason = strings.TrimSpace(event.reason)
	event.fields = event.fields.Clone()
	if err := event.Validate(); err != nil {
		return Event{}, err
	}
	return event, nil
}
