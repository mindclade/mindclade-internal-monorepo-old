// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package dispatcher

import (
	"context"
	"time"

	foundationconfig "go.mindclade.dev/libs/go/config"
	"go.mindclade.dev/libs/go/coordination/outbox"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/messaging"
	"go.mindclade.dev/libs/go/servicekit"
	"go.mindclade.dev/services/control_plane/internal/bootstrap"
	"go.mindclade.dev/services/control_plane/internal/config"
	"go.mindclade.dev/services/control_plane/internal/foundation"
	"go.mindclade.dev/services/control_plane/internal/foundation/eventing"
	"go.mindclade.dev/services/control_plane/internal/foundation/persistence"
	"go.mindclade.dev/services/control_plane/internal/providers"
)

const (
	dispatcherOwner         = "control-plane-event-dispatcher"
	dispatcherPollInterval  = 250 * time.Millisecond
	dispatcherClaimDuration = 30 * time.Second
	dispatcherBatchSize     = 64
	dispatcherMaxDeliveries = 8
)

// EventDispatcherFactory assembles the process that drains the transactional
// outbox onto the broker. It is the narrowest control-plane role: it owns no
// transport, no domain policy, and no schema -- it reads records another role
// wrote and publishes them.
type EventDispatcherFactory struct {
	sources []foundationconfig.Source
}

// NewEventDispatcherFactory returns the dispatcher provider factory.
func NewEventDispatcherFactory(sources ...foundationconfig.Source) *EventDispatcherFactory {
	if len(sources) == 0 {
		sources = []foundationconfig.Source{config.EnvironmentSource()}
	}
	return &EventDispatcherFactory{sources: sources}
}

func (factory *EventDispatcherFactory) Create(ctx context.Context, profile bootstrap.Profile) (runtime bootstrap.Runtime, err error) {
	if factory == nil || ctx == nil {
		return bootstrap.Runtime{}, faults.New(
			faults.CodeInvalidArgument,
			"event-dispatcher factory requires a context",
			faults.WithReason("invalid_factory_request"),
			faults.WithOperation("controlplane.dispatcher.EventDispatcherFactory.Create"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	resolved, err := config.Load(ctx, profile.Name, factory.sources...)
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	settings := resolved.Settings

	shared, err := providers.NewMechanisms(settings)
	if err != nil {
		return bootstrap.Runtime{}, err
	}

	release := make([]func(), 0, 2)
	defer func() {
		if err == nil {
			return
		}
		for index := len(release) - 1; index >= 0; index-- {
			release[index]()
		}
	}()

	stores, err := providers.NewDatabase(settings, shared.Clock, shared.IDs)
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	release = append(release, func() { _ = stores.DB.Close() })

	publisher, brokerLifecycle, err := newPublisher(settings, shared.Clock)
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	release = append(release, func() { _ = brokerLifecycle.Stop(ctx) })

	dispatcher, err := outbox.NewDispatcher(
		stores.Outbox,
		outbox.PublisherFunc(publishThroughBroker(publisher, shared.IDs)),
		outbox.DispatcherConfig{
			Owner:         dispatcherOwner,
			Topics:        []string{settings.MessagingTopic},
			PollInterval:  dispatcherPollInterval,
			ClaimDuration: dispatcherClaimDuration,
			BatchSize:     dispatcherBatchSize,
			MaxDeliveries: dispatcherMaxDeliveries,
			IdleReady:     true,
		},
	)
	if err != nil {
		return bootstrap.Runtime{}, err
	}

	return bootstrap.Runtime{
		// The shutdown budgets belong to the deployment, not to a constant in
		// this package. A role that does not pass them runs on the servicekit
		// package defaults, and drain.timeout stops meaning anything.
		Lifecycle: bootstrap.Lifecycle{
			ShutdownTimeout: settings.ShutdownTimeout,
			DrainTimeout:    settings.DrainTimeout,
		},
		Dependencies: []bootstrap.Aggregate{
			foundation.Core{
				Clock:         shared.Clock,
				Configuration: resolved.Current,
				IDs:           shared.IDs,
				// Lineage is not decorative here: publishThroughBroker copies
				// the request metadata off the stored record onto the message,
				// so a published event stays traceable to the request that
				// caused it.
				RequestMetadataConfigured: true,
				Observability:             shared.Observability,
				Retry:                     shared.Retry,
			},
			// No migration runner: the dispatcher reads a table that the roles
			// writing to it own.
			persistence.SQL{
				Postgres:     stores.Pool,
				Transactions: stores.Transactions,
			},
			eventing.Mechanisms{
				Publisher:  publisher,
				Outbox:     stores.Outbox,
				Dispatcher: dispatcher,
			},
		},
		Components: bootstrap.Components{
			Auxiliary: []bootstrap.StagedComponent{
				{Stage: servicekit.StageInfrastructure, Component: brokerLifecycle},
			},
		},
	}, nil
}

// publishThroughBroker converts one durable outbox record into a broker
// message. Only the composition root knows both shapes, so the translation
// lives here rather than in either mechanism.
func publishThroughBroker(publisher messaging.Publisher, ids *identifiers.Generator) func(context.Context, outbox.Message) error {
	return func(ctx context.Context, record outbox.Message) error {
		identifier, err := identifiers.NewID(messaging.MessageIDKind)
		if err != nil {
			return err
		}
		headers := record.Headers()
		if headers == nil {
			headers = make(map[string]string)
		}
		// The outbox identifier travels with the message so a duplicate
		// delivery is recognisable by the consumer's inbox.
		headers["outbox_id"] = record.ID().String()
		message, err := messaging.NewMessage(
			identifier,
			record.Topic(),
			record.PartitionKey(),
			record.ContentType(),
			record.Payload(),
			headers,
			record.Request(),
			record.CreatedAt(),
		)
		if err != nil {
			return err
		}
		_, err = publisher.Publish(ctx, message)
		return err
	}
}
