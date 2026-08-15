// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// event_dispatcher is a runnable local integration of the production Go paved
// road. It uses in-memory providers but the same outbox, messaging, retry,
// capability validation, drain, and shutdown contracts used by production.
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"time"

	"mindclade.internal/libs/go/coordination/outbox"
	outboxmemory "mindclade.internal/libs/go/coordination/outbox/memory"
	"mindclade.internal/libs/go/identifiers"
	"mindclade.internal/libs/go/messaging"
	messagingmemory "mindclade.internal/libs/go/messaging/memory"
	"mindclade.internal/libs/go/requestmeta"
	"mindclade.internal/libs/go/servicekit"
	"mindclade.internal/libs/go/servicekit/production"
)

const topic = "runs.created"

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := run(ctx, os.Stdout); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, output io.Writer) error {
	store, err := outboxmemory.New()
	if err != nil {
		return err
	}
	broker, err := messagingmemory.NewBroker(messagingmemory.Config{Capacity: 32, HandlerWorkers: 1})
	if err != nil {
		return err
	}
	subscription, err := broker.Subscribe(topic)
	if err != nil {
		return err
	}

	factory, err := outbox.NewFactory()
	if err != nil {
		return err
	}
	message, err := factory.Create(
		topic,
		"run-demo",
		"application/json",
		[]byte(`{"run_id":"run-demo","state":"created"}`),
		map[string]string{"schema": "mindclade.run.created.v1"},
		requestMetadata(),
		time.Time{},
	)
	if err != nil {
		return err
	}
	if err := store.Append(ctx, message); err != nil {
		return err
	}

	delivered := make(chan messaging.Message, 1)
	published := make(chan struct{}, 1)
	publisher := outbox.PublisherFunc(func(publishCtx context.Context, value outbox.Message) error {
		id, idErr := identifiers.NewIDAt(messaging.MessageIDKind, value.CreatedAt())
		if idErr != nil {
			return idErr
		}
		brokerMessage, messageErr := messaging.NewMessage(
			id,
			value.Topic(),
			value.PartitionKey(),
			value.ContentType(),
			value.Payload(),
			value.Headers(),
			value.Request(),
			value.CreatedAt(),
		)
		if messageErr != nil {
			return messageErr
		}
		_, publishErr := broker.Publish(publishCtx, brokerMessage)
		return publishErr
	})
	dispatcher, err := outbox.NewDispatcher(
		store,
		publisher,
		outbox.DispatcherConfig{
			Owner:         "example-event-dispatcher",
			Topics:        []string{topic},
			PollInterval:  5 * time.Millisecond,
			ClaimDuration: time.Second,
			BatchSize:     8,
			MaxDeliveries: 3,
		},
		outbox.WithDispatchObserver(outbox.DispatchObserverFunc(func(event outbox.DispatchEvent) {
			if event.Kind == outbox.DispatchPublished {
				select {
				case published <- struct{}{}:
				default:
				}
			}
		})),
	)
	if err != nil {
		return err
	}

	builder, err := production.NewBuilder("example-event-dispatcher", production.RoleDispatcher)
	if err != nil {
		return err
	}
	for _, capability := range []production.Capability{
		production.CapabilityClock,
		production.CapabilityConfiguration,
		production.CapabilityIdentifiers,
		production.CapabilityRequestMetadata,
		production.CapabilityRetry,
		production.CapabilityMessaging,
		production.CapabilityOutboxStore,
	} {
		if err := builder.Declare(capability); err != nil {
			return err
		}
	}
	if err := builder.AddCapability(production.CapabilityObservability, passive("observability")); err != nil {
		return err
	}
	if err := builder.AddCapability(production.CapabilityDatabase, passive("memory-database")); err != nil {
		return err
	}
	if err := builder.AddComponent(servicekit.StageInfrastructure, servicekit.Component{
		Name:  "memory-broker",
		Start: func(context.Context) error { return nil },
		Stop:  broker.Close,
	}); err != nil {
		return err
	}
	if err := builder.AddCapability(production.CapabilityOutboxDispatcher, dispatcher.Component("outbox-dispatcher")); err != nil {
		return err
	}
	if err := builder.AddWork(servicekit.Component{
		Name: "event-observer",
		Run: func(receiveCtx context.Context) error {
			return subscription.Receive(receiveCtx, func(_ context.Context, delivery messaging.Delivery) error {
				select {
				case delivered <- delivery.Message():
				default:
				}
				return nil
			})
		},
		Stop: subscription.Close,
	}); err != nil {
		return err
	}

	var once sync.Once
	if err := builder.AddWork(servicekit.Component{
		Name: "example-completion",
		Run: func(completionCtx context.Context) error {
			var got messaging.Message
			for deliveredOK, publishedOK := false, false; !deliveredOK || !publishedOK; {
				select {
				case <-completionCtx.Done():
					return completionCtx.Err()
				case got = <-delivered:
					deliveredOK = true
				case <-published:
					publishedOK = true
				}
			}
			once.Do(func() {
				fmt.Fprintf(output, "delivered topic=%s payload=%s\n", got.Topic(), string(got.Payload()))
			})
			return nil
		},
	}); err != nil {
		return err
	}

	runtime, err := builder.Build()
	if err != nil {
		return err
	}
	if err := runtime.Run(ctx); err != nil && ctx.Err() == nil {
		return err
	}
	record, err := store.Lookup(context.Background(), message.ID().String())
	if err != nil {
		return err
	}
	if record.State != outbox.StatePublished {
		return fmt.Errorf("outbox record ended in %q", record.State)
	}
	fmt.Fprintf(output, "outbox state=%s attempts=%d\n", record.State, record.Attempts)
	return nil
}

func passive(name string) servicekit.Component {
	return servicekit.Component{
		Name:  name,
		Start: func(context.Context) error { return nil },
		Stop:  func(context.Context) error { return nil },
	}
}

func requestMetadata() requestmeta.Metadata {
	// A zero request-metadata envelope is valid for internally generated work.
	// The example deliberately does not invent fake user lineage.
	return requestmeta.Metadata{}
}
