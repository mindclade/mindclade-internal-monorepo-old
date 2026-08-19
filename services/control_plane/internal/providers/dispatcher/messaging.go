// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package dispatcher

import (
	"time"

	mcclock "go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/messaging"
	messagingmemory "go.mindclade.dev/libs/go/messaging/memory"
	"go.mindclade.dev/libs/go/servicekit"
	"go.mindclade.dev/services/control_plane/internal/config"
)

const (
	brokerCapacity    = 1024
	brokerMaxAttempts = 5
	brokerAckDeadline = 30 * time.Second
)

// newPublisher resolves the configured messaging provider.
//
// The in-memory broker is reachable only from a development or test
// environment. It is a real adapter, not a stub, but it is process-local: a
// dispatcher using it publishes to nobody outside this process, so allowing it
// under staging or production would turn a delivery outage into a silent
// success. config.Settings.Validate already rejects the memory provider in
// those environments; this is the second gate, at the point of construction,
// because a single check that far from the adapter is easy to route around.
func newPublisher(settings config.Settings, value mcclock.Clock) (messaging.Publisher, servicekit.Component, error) {
	switch settings.MessagingProvider {
	case "memory":
		if settings.Environment != config.EnvironmentDevelopment && settings.Environment != config.EnvironmentTest {
			return nil, servicekit.Component{}, faults.New(
				faults.CodeFailedPrecondition,
				"the in-memory broker is not a durable messaging provider",
				faults.WithReason("memory_messaging_outside_development"),
				faults.WithOperation("controlplane.dispatcher.newPublisher"),
				faults.WithField("environment", string(settings.Environment)),
				faults.WithRetryPolicy(faults.NoRetry()),
			)
		}
		broker, err := messagingmemory.NewBroker(messagingmemory.Config{
			Capacity:    brokerCapacity,
			MaxAttempts: brokerMaxAttempts,
			AckDeadline: brokerAckDeadline,
			Clock:       value,
		})
		if err != nil {
			return nil, servicekit.Component{}, err
		}
		component := servicekit.Component{
			Name: "memory-broker",
			Stop: broker.Close,
		}
		return broker, component, nil
	case "pubsub":
		// libs/go/messaging/pubsub is provider-neutral and needs a TopicResolver
		// backed by a Pub/Sub SDK. No such module is in go.mod, so there is
		// nothing to construct and saying so is better than degrading to a
		// process-local broker.
		return nil, servicekit.Component{}, faults.New(
			faults.CodeFailedPrecondition,
			"the Pub/Sub messaging provider is not configured",
			faults.WithReason("pubsub_provider_not_configured"),
			faults.WithOperation("controlplane.dispatcher.newPublisher"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	default:
		return nil, servicekit.Component{}, faults.New(
			faults.CodeInvalidArgument,
			"unsupported messaging provider",
			faults.WithReason("unsupported_messaging_provider"),
			faults.WithOperation("controlplane.dispatcher.newPublisher"),
			faults.WithField("provider", settings.MessagingProvider),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
}
