// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package main

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"mindclade.internal/libs/go/config"
	cursormemory "mindclade.internal/libs/go/coordination/cursor/memory"
	"mindclade.internal/libs/go/coordination/leadership"
	"mindclade.internal/libs/go/coordination/outbox"
	outboxmemory "mindclade.internal/libs/go/coordination/outbox/memory"
	"mindclade.internal/libs/go/coordination/workqueue"
	workmemory "mindclade.internal/libs/go/coordination/workqueue/memory"
	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/identifiers"
	"mindclade.internal/libs/go/messaging"
	messagingmemory "mindclade.internal/libs/go/messaging/memory"
	"mindclade.internal/libs/go/observability"
	"mindclade.internal/libs/go/requestmeta"
	"mindclade.internal/libs/go/servicekit"
	"mindclade.internal/libs/go/servicekit/production"
	blobmemory "mindclade.internal/libs/go/storage/blob/memory"
	cachememory "mindclade.internal/libs/go/storage/cache/memory"
	"mindclade.internal/libs/go/storage/lease"
	leasememory "mindclade.internal/libs/go/storage/lease/memory"
)

const (
	ingestionServiceName = "example-ingestion-coordinator"
	ingestionQueue       = "reference-ingestion"
	snapshotTopic        = "data.snapshot.discovered"
)

// Application is a runnable, local-only ingestion coordinator integration. It
// demonstrates how Go owns durable stage coordination while leaving scientific
// record interpretation and curation outside the shared foundation.
type Application struct {
	Runtime      *production.Runtime
	Config       config.Snapshot
	WorkStore    *workmemory.Store
	OutboxStore  *outboxmemory.Store
	CursorStore  *cursormemory.Store
	BlobStore    *blobmemory.Store
	CacheStore   *cachememory.Store
	LeaseStore   *leasememory.Store
	Broker       *messagingmemory.Broker
	ItemID       identifiers.ID
	Received     <-chan messaging.Message
	OutboxIDs    <-chan identifiers.ID
	Kubernetes   localKubernetesAdapter
	result       chan error
	startOnce    sync.Once
	shutdownOnce sync.Once
	cancel       context.CancelFunc
}

// Start runs the validated coordinator in an owned goroutine. Start is
// single-use; callers receive completion through Wait.
func (application *Application) Start(parent context.Context) error {
	if application == nil || application.Runtime == nil {
		return faults.New(
			faults.CodeFailedPrecondition,
			"ingestion coordinator example is not configured",
			faults.WithReason("example_ingestion_not_configured"),
			faults.WithOperation("examples.ingestion_coordinator.Application.Start"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	if parent == nil {
		return faults.New(
			faults.CodeInvalidArgument,
			"context is required",
			faults.WithReason("nil_context"),
			faults.WithOperation("examples.ingestion_coordinator.Application.Start"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	started := false
	application.startOnce.Do(func() {
		ctx, cancel := context.WithCancel(parent)
		application.cancel = cancel
		started = true
		go func() { application.result <- application.Runtime.Run(ctx) }()
	})
	if !started {
		return faults.New(
			faults.CodeFailedPrecondition,
			"ingestion coordinator example already started",
			faults.WithReason("example_ingestion_already_started"),
			faults.WithOperation("examples.ingestion_coordinator.Application.Start"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	return nil
}

// Shutdown requests graceful drain. It is idempotent.
func (application *Application) Shutdown() {
	if application == nil {
		return
	}
	application.shutdownOnce.Do(func() {
		if application.cancel != nil {
			application.cancel()
		}
	})
}

// Wait waits for the service lifecycle to finish.
func (application *Application) Wait(ctx context.Context) error {
	if application == nil || application.result == nil {
		return nil
	}
	select {
	case err := <-application.result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// BuildApplication creates the complete local pipeline and enqueues one
// immutable source-snapshot discovery item before the service starts.
func BuildApplication(ctx context.Context) (*Application, error) {
	if ctx == nil {
		return nil, faults.New(
			faults.CodeInvalidArgument,
			"context is required",
			faults.WithReason("nil_context"),
			faults.WithOperation("examples.ingestion_coordinator.BuildApplication"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	snapshot, err := loadIngestionConfiguration(ctx)
	if err != nil {
		return nil, err
	}
	resource, err := observability.NewResource(
		ingestionServiceName,
		observability.WithServiceNamespace("mindclade.examples"),
		observability.WithDeploymentEnvironment("local"),
	)
	if err != nil {
		return nil, err
	}
	telemetry, err := observability.NewRuntime(resource)
	if err != nil {
		return nil, err
	}

	workStore := workmemory.New()
	outboxStore, err := outboxmemory.New()
	if err != nil {
		return nil, err
	}
	cursorStore := cursormemory.New()
	blobStore, err := blobmemory.New()
	if err != nil {
		return nil, err
	}
	cacheStore, err := cachememory.New()
	if err != nil {
		return nil, err
	}
	leaseStore, err := leasememory.New()
	if err != nil {
		return nil, err
	}
	broker, err := messagingmemory.NewBroker(messagingmemory.Config{
		Capacity:       64,
		MaxAttempts:    3,
		AckDeadline:    time.Second,
		HandlerWorkers: 1,
	})
	if err != nil {
		return nil, err
	}
	subscription, err := broker.Subscribe(snapshotTopic)
	if err != nil {
		return nil, err
	}

	received := make(chan messaging.Message, 1)
	outboxIDs := make(chan identifiers.ID, 1)
	subscriber := newSubscriberComponent(subscription, broker, received)

	outboxFactory, err := outbox.NewFactory()
	if err != nil {
		return nil, err
	}
	worker, err := workqueue.NewWorker(
		workStore,
		workqueue.HandlerFunc(func(handlerCtx context.Context, item workqueue.Item) (workqueue.Result, error) {
			message, createErr := outboxFactory.Create(
				snapshotTopic,
				"pdb",
				"application/json",
				item.Payload,
				map[string]string{"work_id": item.ID.String()},
				item.Request,
				time.Time{},
			)
			if createErr != nil {
				return workqueue.Result{}, createErr
			}
			if appendErr := outboxStore.Append(handlerCtx, message); appendErr != nil {
				return workqueue.Result{}, appendErr
			}
			select {
			case outboxIDs <- message.ID():
			default:
			}
			return workqueue.Result{ContentType: "application/json", Payload: []byte(`{"status":"published-to-outbox"}`)}, nil
		}),
		workqueue.WorkerConfig{
			Owner:             "example-ingestion-worker",
			Queues:            []string{ingestionQueue},
			PollInterval:      20 * time.Millisecond,
			LeaseDuration:     time.Second,
			HeartbeatInterval: 200 * time.Millisecond,
			BatchSize:         1,
			Concurrency:       1,
			FailureDelay:      50 * time.Millisecond,
		},
	)
	if err != nil {
		return nil, err
	}

	publisher := outbox.PublisherFunc(func(publishCtx context.Context, value outbox.Message) error {
		identifier, idErr := identifiers.NewID(messaging.MessageIDKind)
		if idErr != nil {
			return idErr
		}
		message, messageErr := messaging.NewMessage(
			identifier,
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
		_, publishErr := broker.Publish(publishCtx, message)
		return publishErr
	})
	dispatcher, err := outbox.NewDispatcher(
		outboxStore,
		publisher,
		outbox.DispatcherConfig{
			Owner:         "example-outbox-dispatcher",
			Topics:        []string{snapshotTopic},
			PollInterval:  20 * time.Millisecond,
			ClaimDuration: time.Second,
			BatchSize:     8,
			MaxDeliveries: 3,
			IdleReady:     true,
		},
	)
	if err != nil {
		return nil, err
	}

	elector, err := leadership.New(
		leaseStore,
		leadership.Config{
			Key:                    lease.MustParseKey("examples/ingestion-coordinator"),
			Owner:                  "example-ingestion-coordinator",
			TTL:                    2 * time.Second,
			RenewInterval:          500 * time.Millisecond,
			AcquireInterval:        50 * time.Millisecond,
			ReleaseTimeout:         time.Second,
			RequireLeaderReadiness: false,
		},
		func(leaderCtx context.Context, _ leadership.Session) error {
			<-leaderCtx.Done()
			return nil
		},
	)
	if err != nil {
		return nil, err
	}

	builder, err := production.NewBuilder(
		ingestionServiceName,
		production.RoleIngestionCoordinator,
		servicekit.WithObserver(observability.NewServiceObserver(telemetry)),
		servicekit.WithStartupTimeout(10*time.Second),
		servicekit.WithShutdownTimeout(10*time.Second),
		servicekit.WithComponentDrainTimeout(5*time.Second),
		servicekit.WithComponentStopTimeout(5*time.Second),
	)
	if err != nil {
		return nil, err
	}
	if err := builder.AddCapability(production.CapabilityObservability, telemetry.ServiceComponent("observability")); err != nil {
		return nil, err
	}
	if err := builder.AddCapability(production.CapabilityDatabase, localIngestionDatabaseComponent()); err != nil {
		return nil, err
	}
	if err := declareIngestionCapabilities(builder); err != nil {
		return nil, err
	}
	if err := builder.AddCapability(production.CapabilityLeadership, elector.Component("leadership")); err != nil {
		return nil, err
	}
	if err := builder.AddCapability(production.CapabilityOutboxDispatcher, dispatcher.Component("outbox-dispatcher")); err != nil {
		return nil, err
	}
	if err := builder.AddCapability(production.CapabilityWorkQueueWorker, worker.Component("ingestion-worker")); err != nil {
		return nil, err
	}
	if err := builder.AddWork(subscriber); err != nil {
		return nil, err
	}
	runtime, err := builder.Build()
	if err != nil {
		return nil, err
	}

	requestID, err := requestmeta.NewRequestID()
	if err != nil {
		return nil, err
	}
	metadata, err := requestmeta.New(requestID)
	if err != nil {
		return nil, err
	}
	metadata.Operation = requestmeta.MustParseOperation("ingestion.discover_snapshot")
	payload, err := json.Marshal(struct {
		Source       string `json:"source"`
		SnapshotID   string `json:"snapshot_id"`
		DatabaseType string `json:"database_type"`
	}{
		Source:       "pdb",
		SnapshotID:   "pdb-2026-08-13",
		DatabaseType: "structure",
	})
	if err != nil {
		return nil, err
	}
	item, err := workqueue.NewItem(ingestionQueue, payload, 100, time.Time{}, 3, metadata)
	if err != nil {
		return nil, err
	}
	if err := workStore.Enqueue(ctx, item); err != nil {
		return nil, err
	}

	return &Application{
		Runtime:     runtime,
		Config:      snapshot,
		WorkStore:   workStore,
		OutboxStore: outboxStore,
		CursorStore: cursorStore,
		BlobStore:   blobStore,
		CacheStore:  cacheStore,
		LeaseStore:  leaseStore,
		Broker:      broker,
		ItemID:      item.ID,
		Received:    received,
		OutboxIDs:   outboxIDs,
		Kubernetes:  localKubernetesAdapter{name: "local-reference"},
		result:      make(chan error, 1),
	}, nil
}

func loadIngestionConfiguration(ctx context.Context) (config.Snapshot, error) {
	loader, err := config.New(
		[]config.Field{
			{Key: "service.name", Required: true},
			{Key: "worker.concurrency", Default: config.String("1")},
			{Key: "reference.snapshot", Required: true},
		},
		config.MapSource{
			SourceName: "local-defaults",
			Values: map[string]string{
				"service.name":       ingestionServiceName,
				"reference.snapshot": "pdb-2026-08-13",
			},
		},
	)
	if err != nil {
		return config.Snapshot{}, err
	}
	return loader.Load(ctx)
}

func declareIngestionCapabilities(builder *production.Builder) error {
	capabilities := []production.Capability{
		production.CapabilityClock,
		production.CapabilityConfiguration,
		production.CapabilityIdentifiers,
		production.CapabilityRequestMetadata,
		production.CapabilityRetry,
		production.CapabilityTransactions,
		production.CapabilityAudit,
		production.CapabilityIdempotency,
		production.CapabilityBlobStore,
		production.CapabilityCache,
		production.CapabilityLeaseStore,
		production.CapabilityKubernetes,
		production.CapabilityWorkQueueStore,
		production.CapabilityCursorStore,
		production.CapabilityMessaging,
		production.CapabilityResourceVersion,
		production.CapabilityOutboxStore,
	}
	for _, capability := range capabilities {
		if err := builder.Declare(capability); err != nil {
			return err
		}
	}
	return nil
}

func newSubscriberComponent(
	subscription messaging.Subscription,
	broker *messagingmemory.Broker,
	received chan<- messaging.Message,
) servicekit.Component {
	var ready atomic.Bool
	return servicekit.Component{
		Name: "snapshot-event-subscriber",
		Start: func(context.Context) error {
			if subscription == nil || broker == nil {
				return faults.New(
					faults.CodeFailedPrecondition,
					"local messaging subscription is not configured",
					faults.WithReason("example_subscription_not_configured"),
					faults.WithOperation("examples.ingestion_coordinator.subscriber.Start"),
					faults.WithRetryPolicy(faults.NoRetry()),
				)
			}
			ready.Store(true)
			return nil
		},
		Run: func(ctx context.Context) error {
			return subscription.Receive(ctx, func(handlerCtx context.Context, delivery messaging.Delivery) error {
				select {
				case received <- delivery.Message():
				default:
				}
				return nil
			})
		},
		Drain: func(context.Context) error {
			ready.Store(false)
			return nil
		},
		Stop: func(ctx context.Context) error {
			ready.Store(false)
			return errors.Join(subscription.Close(ctx), broker.Close(ctx))
		},
		Liveness: func(context.Context) error {
			if subscription == nil || broker == nil {
				return faults.New(
					faults.CodeUnavailable,
					"local messaging subscriber is unavailable",
					faults.WithReason("example_subscriber_unavailable"),
					faults.WithOperation("examples.ingestion_coordinator.subscriber.Liveness"),
					faults.WithRetryPolicy(faults.ImmediateRetry(0)),
				)
			}
			return nil
		},
		Readiness: func(context.Context) error {
			if ready.Load() {
				return nil
			}
			return faults.New(
				faults.CodeUnavailable,
				"local messaging subscriber is not ready",
				faults.WithReason("example_subscriber_not_ready"),
				faults.WithOperation("examples.ingestion_coordinator.subscriber.Readiness"),
				faults.WithRetryPolicy(faults.ImmediateRetry(0)),
			)
		},
	}
}

func localIngestionDatabaseComponent() servicekit.Component {
	var ready atomic.Bool
	return servicekit.Component{
		Name:  "local-database",
		Start: func(context.Context) error { ready.Store(true); return nil },
		Drain: func(context.Context) error { ready.Store(false); return nil },
		Stop:  func(context.Context) error { ready.Store(false); return nil },
		Liveness: func(context.Context) error {
			if ready.Load() {
				return nil
			}
			return faults.New(
				faults.CodeUnavailable,
				"local database is unavailable",
				faults.WithReason("example_database_unavailable"),
				faults.WithOperation("examples.ingestion_coordinator.database.Liveness"),
				faults.WithRetryPolicy(faults.ImmediateRetry(0)),
			)
		},
		Readiness: func(context.Context) error {
			if ready.Load() {
				return nil
			}
			return faults.New(
				faults.CodeUnavailable,
				"local database is not ready",
				faults.WithReason("example_database_not_ready"),
				faults.WithOperation("examples.ingestion_coordinator.database.Readiness"),
				faults.WithRetryPolicy(faults.ImmediateRetry(0)),
			)
		},
	}
}

// localKubernetesAdapter represents the process-owned local launcher used by
// this example. The production ingestion coordinator replaces it with the
// qualified Kubernetes/JobSet/Kueue adapters from libs/go/kubernetes.
type localKubernetesAdapter struct{ name string }

func (adapter localKubernetesAdapter) Valid() bool { return adapter.name != "" }
