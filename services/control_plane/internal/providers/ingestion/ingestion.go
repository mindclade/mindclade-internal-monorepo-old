// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package ingestion is the composition root for the control-plane ingestion
// coordinator: the process that stages incoming data, tracks its position in
// each source, and launches the cluster work that processes it.
//
// It is the widest role in the fleet. It is the only one that holds artifacts,
// a read cache, a cursor, a work queue, and a cluster client at once, which is
// what makes it the second consumer of almost every provider adapter the
// foundation ships.
package ingestion

import (
	"context"
	"os"
	"time"

	crclient "sigs.k8s.io/controller-runtime/pkg/client"

	foundationconfig "go.mindclade.dev/libs/go/config"
	"go.mindclade.dev/libs/go/coordination/leadership"
	"go.mindclade.dev/libs/go/coordination/workqueue"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/servicekit"
	"go.mindclade.dev/libs/go/storage/lease"
	"go.mindclade.dev/services/control_plane/internal/bootstrap"
	"go.mindclade.dev/services/control_plane/internal/config"
	"go.mindclade.dev/services/control_plane/internal/foundation"
	"go.mindclade.dev/services/control_plane/internal/foundation/eventing"
	"go.mindclade.dev/services/control_plane/internal/foundation/governance"
	"go.mindclade.dev/services/control_plane/internal/foundation/leasing"
	"go.mindclade.dev/services/control_plane/internal/foundation/objects"
	"go.mindclade.dev/services/control_plane/internal/foundation/orchestration"
	"go.mindclade.dev/services/control_plane/internal/foundation/persistence"
	"go.mindclade.dev/services/control_plane/internal/foundation/projection"
	"go.mindclade.dev/services/control_plane/internal/foundation/tasks"
	"go.mindclade.dev/services/control_plane/internal/providers"
	"go.mindclade.dev/services/control_plane/internal/providers/broker"
	"go.mindclade.dev/services/control_plane/internal/providers/cluster"
	"go.mindclade.dev/services/control_plane/internal/providers/durable"
	objectstores "go.mindclade.dev/services/control_plane/internal/providers/objects"
)

// Leadership timings match the other singleton roles. The key differs because
// each singleton is a separate claim.
const (
	ingestionLeaseKey       = "control-plane/ingestion-coordinator"
	leaseTTL                = 15 * time.Second
	leaseRenewInterval      = 5 * time.Second
	leaseAcquireInterval    = 2 * time.Second
	leaseReleaseTimeout     = 5 * time.Second
	leaderReadinessRequired = true
)

// Staging worker tuning. Each item stages bytes into the artifact store and
// launches cluster work, so concurrency is bounded by what the object store
// and the API server will absorb rather than by CPU.
const (
	stagingQueue             = "control-plane/ingestion-staging"
	stagingWorker            = "staging"
	stagingPollInterval      = 500 * time.Millisecond
	stagingLeaseDuration     = 300 * time.Second
	stagingHeartbeatInterval = 60 * time.Second
	stagingBatchSize         = 8
	stagingConcurrency       = 4
	stagingFailureDelay      = 30 * time.Second
)

// IngestionFactory assembles the ingestion coordinator: the durable PostgreSQL
// mechanisms, the artifact store and read cache, the per-source cursor, the
// singleton elector, the leased staging queue, and the cluster client it
// launches processing work through.
type IngestionFactory struct {
	sources []foundationconfig.Source
	staging workqueue.Handler
}

// NewIngestionFactory returns the ingestion-coordinator provider factory. With
// no sources the process reads its configuration from the explicit environment
// mapping; tests pass a MapSource instead.
func NewIngestionFactory(sources ...foundationconfig.Source) *IngestionFactory {
	if len(sources) == 0 {
		sources = []foundationconfig.Source{config.EnvironmentSource()}
	}
	return &IngestionFactory{sources: sources}
}

// WithStagingHandler injects the domain handler that stages one unit of
// incoming data. It is the seam between this composition root and ingestion
// policy: the root owns the queue, the cursor, the lease, the artifact store,
// and the cluster client, and the domain owns what a source is and what
// staging one item means.
//
// Left unset, the worker fails every item closed. Staging that reports success
// without writing the artifact advances a cursor past data that was never
// stored, which is the one failure in this role that cannot be repaired by
// retrying.
func (factory *IngestionFactory) WithStagingHandler(handler workqueue.Handler) *IngestionFactory {
	if factory == nil {
		return nil
	}
	factory.staging = handler
	return factory
}

// Create resolves configuration and constructs every provider the ingestion
// coordinator requires. Construction is ordered cheapest-first: configuration
// and pure mechanisms fail before any socket, cloud client, or cluster
// connection is opened, and anything already opened is released if a later
// step fails.
func (factory *IngestionFactory) Create(ctx context.Context, profile bootstrap.Profile) (runtime bootstrap.Runtime, err error) {
	if factory == nil || ctx == nil {
		return bootstrap.Runtime{}, faults.New(
			faults.CodeInvalidArgument,
			"ingestion factory requires a context",
			faults.WithReason("invalid_factory_request"),
			faults.WithOperation("controlplane.ingestion.IngestionFactory.Create"),
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
	owner, err := leaseOwner()
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	key, err := lease.ParseKey(ingestionLeaseKey)
	if err != nil {
		return bootstrap.Runtime{}, err
	}

	release := make([]func(), 0, 4)
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

	recorder, err := durable.NewAuditRecorder(stores.DB)
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	records, err := durable.NewIdempotencyStore(stores.DB, shared.Clock, shared.IDs)
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	leases, err := durable.NewLeaseStore(stores.DB)
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	queue, err := durable.NewWorkQueueStore(stores.DB)
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	cursors, err := durable.NewCursorStore(stores.DB)
	if err != nil {
		return bootstrap.Runtime{}, err
	}

	handler := factory.staging
	if handler == nil {
		handler = workqueue.HandlerFunc(refuseStaging)
	}
	worker, err := workqueue.NewWorker(
		queue,
		handler,
		workqueue.WorkerConfig{
			Owner:             owner,
			Queues:            []string{stagingQueue},
			PollInterval:      stagingPollInterval,
			LeaseDuration:     stagingLeaseDuration,
			HeartbeatInterval: stagingHeartbeatInterval,
			BatchSize:         stagingBatchSize,
			Concurrency:       stagingConcurrency,
			FailureDelay:      stagingFailureDelay,
		},
		workqueue.WithClock(shared.Clock),
		workqueue.WithRetry(shared.Retry),
	)
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	leaderHandler, workerComponent, err := leadership.GateComponent(
		worker.Component("worker/" + stagingWorker),
	)
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	elector, err := leadership.New(
		leases,
		leadership.Config{
			Key:                    key,
			Owner:                  owner,
			TTL:                    leaseTTL,
			RenewInterval:          leaseRenewInterval,
			AcquireInterval:        leaseAcquireInterval,
			ReleaseTimeout:         leaseReleaseTimeout,
			RequireLeaderReadiness: leaderReadinessRequired,
			ExitOnLeadershipLoss:   true,
		},
		leaderHandler,
		leadership.WithClock(shared.Clock),
		leadership.WithRetry(shared.Retry),
	)
	if err != nil {
		return bootstrap.Runtime{}, err
	}

	blobs, blobLifecycle, err := objectstores.NewBlobStore(ctx, settings)
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	release = append(release, func() { _ = blobLifecycle.Stop(ctx) })

	caches, cacheLifecycle, err := objectstores.NewCacheStore(settings)
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	release = append(release, func() { _ = cacheLifecycle.Stop(ctx) })

	publisher, brokerLifecycle, err := broker.NewPublisher(settings, shared.Clock)
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	release = append(release, func() { _ = brokerLifecycle.Stop(ctx) })

	// A client and no manager: the coordinator launches and observes cluster
	// work but reconciles nothing, so an informer cache would be a second copy
	// of state it never reads.
	kubernetes, err := cluster.New(ctx, settings, crclient.Options{})
	if err != nil {
		return bootstrap.Runtime{}, err
	}

	return bootstrap.Runtime{
		// The aggregate list is the role's capability profile, written out.
		// Anything absent here is a package this binary does not link.
		Dependencies: []bootstrap.Aggregate{
			foundation.Core{
				Clock:         shared.Clock,
				Configuration: resolved.Current,
				IDs:           shared.IDs,
				// Lineage is carried, not decorative: a staging item stores the
				// request metadata of the submission that produced it, so a
				// staged artifact stays traceable to the data that caused it.
				RequestMetadataConfigured: true,
				Observability:             shared.Observability,
				Retry:                     shared.Retry,
			},
			// No migration runner: the coordinator reads and writes tables the
			// registry role owns and creates.
			persistence.SQL{
				Postgres:     stores.Pool,
				Transactions: stores.Transactions,
			},
			governance.Controls{
				Audit:       recorder,
				Idempotency: records,
				// Staging mutates records the registry also writes, so every
				// mutation carries a compare-and-set precondition.
				ResourceVersionsConfigured: true,
			},
			leasing.Mechanisms{
				Leases: leases,
				Leader: elector,
			},
			tasks.Mechanisms{
				Queue:   queue,
				Workers: map[string]servicekit.Component{stagingWorker: workerComponent},
			},
			// The cursor without the projector: this role tracks its position
			// in each source it reads, but it does not project an ordered event
			// stream, so it holds no inbox and runs no projector loop.
			projection.Mechanisms{
				Cursors: cursors,
			},
			objects.Stores{
				Blobs: blobs,
				Cache: caches,
			},
			orchestration.Cluster{
				Client: kubernetes.Client,
			},
			eventing.Mechanisms{
				Publisher: publisher,
				Outbox:    stores.Outbox,
			},
		},
		Components: bootstrap.Components{
			Auxiliary: []bootstrap.StagedComponent{
				{Stage: servicekit.StageInfrastructure, Component: blobLifecycle},
				{Stage: servicekit.StageInfrastructure, Component: cacheLifecycle},
				{Stage: servicekit.StageInfrastructure, Component: brokerLifecycle},
			},
		},
	}, nil
}

// refuseStaging is the default staging handler. What a source is and what
// staging one item means are domain decisions, so until a handler is injected
// the worker fails items closed rather than advancing past data it never
// stored.
func refuseStaging(context.Context, workqueue.Item) (workqueue.Result, error) {
	return workqueue.Result{}, faults.New(
		faults.CodeNotImplemented,
		"ingestion staging policy is not configured",
		faults.WithReason("staging_handler_not_configured"),
		faults.WithOperation("controlplane.ingestion.refuseStaging"),
		faults.WithRetryPolicy(faults.NoRetry()),
	)
}

// leaseOwner identifies this process instance to the lease store. The hostname
// is the right identity under Kubernetes because a pod name is unique for the
// lifetime of the pod, which is exactly the lifetime of the claim.
func leaseOwner() (string, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return "", faults.Wrap(err, faults.CodeInternal,
			"unable to determine the ingestion lease owner",
			faults.WithReason("hostname_unavailable"),
			faults.WithOperation("controlplane.ingestion.leaseOwner"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	return hostname, nil
}
