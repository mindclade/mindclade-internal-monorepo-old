// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package outbox

import (
	"time"

	mcclock "go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/requestmeta"
)

type Factory struct {
	clock     mcclock.Clock
	generator *identifiers.Generator
}

type FactoryOption func(*Factory) error

func WithFactoryClock(value mcclock.Clock) FactoryOption {
	return func(factory *Factory) error {
		if value == nil {
			return faults.Wrap(ErrInvalidRequest, faults.CodeInvalidArgument, "outbox clock is required", faults.WithReason("nil_outbox_clock"), faults.WithOperation("outbox.WithFactoryClock"), faults.WithRetryPolicy(faults.NoRetry()))
		}
		factory.clock = value
		return nil
	}
}

func WithFactoryGenerator(value *identifiers.Generator) FactoryOption {
	return func(factory *Factory) error {
		if value == nil {
			return faults.Wrap(ErrInvalidRequest, faults.CodeInvalidArgument, "outbox identifier generator is required", faults.WithReason("nil_outbox_generator"), faults.WithOperation("outbox.WithFactoryGenerator"), faults.WithRetryPolicy(faults.NoRetry()))
		}
		factory.generator = value
		return nil
	}
}

func NewFactory(options ...FactoryOption) (*Factory, error) {
	factory := &Factory{clock: mcclock.RealClock{}}
	for _, option := range options {
		if option != nil {
			if err := option(factory); err != nil {
				return nil, err
			}
		}
	}
	if factory.generator == nil {
		generator, err := identifiers.NewGenerator(identifiers.WithTimeSource(factory.clock.Now))
		if err != nil {
			return nil, faults.Wrap(err, faults.CodeInternal, "unable to initialize outbox identifier generator", faults.WithReason("outbox_generator_failed"), faults.WithOperation("outbox.NewFactory"), faults.WithRetryPolicy(faults.NoRetry()))
		}
		factory.generator = generator
	}
	return factory, nil
}

func (factory *Factory) Create(topic, partitionKey, contentType string, payload []byte, headers map[string]string, request requestmeta.Metadata, availableAt time.Time) (Message, error) {
	if factory == nil || factory.clock == nil || factory.generator == nil {
		return Message{}, faults.Wrap(ErrInvalidRequest, faults.CodeFailedPrecondition, "outbox factory is not configured", faults.WithReason("outbox_factory_missing"), faults.WithOperation("outbox.Factory.Create"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	now := factory.clock.Now().Round(0).UTC()
	if availableAt.IsZero() {
		availableAt = now
	}
	id, err := factory.generator.ID(MessageIDKind)
	if err != nil {
		return Message{}, faults.Wrap(err, faults.CodeInternal, "unable to generate outbox message id", faults.WithReason("outbox_id_generation_failed"), faults.WithOperation("outbox.Factory.Create"), faults.WithRetryPolicy(faults.BackoffRetry(3)))
	}
	return NewMessage(id, topic, partitionKey, contentType, payload, headers, request, now, availableAt)
}
