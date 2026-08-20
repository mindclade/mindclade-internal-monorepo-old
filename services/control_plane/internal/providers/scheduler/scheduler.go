// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package scheduler is the composition root for the control-plane scheduler
// role: the process that decides what runs, claims that decision under a
// singleton lease, and places the resulting work on the cluster.
//
// It is the first role to materialize the Kubernetes and durable-coordination
// halves of the foundation. Everything it assembles is a mechanism; the
// scheduling policy itself is domain code and does not live here.
package scheduler

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
	"go.mindclade.dev/services/control_plane/internal/foundation/orchestration"
	"go.mindclade.dev/services/control_plane/internal/foundation/persistence"
	"go.mindclade.dev/services/control_plane/internal/foundation/tasks"
	"go.mindclade.dev/services/control_plane/internal/providers"
	"go.mindclade.dev/services/control_plane/internal/providers/broker"
	"go.mindclade.dev/services/control_plane/internal/providers/cluster"
	"go.mindclade.dev/services/control_plane/internal/providers/durable"
)

// Leadership timings. The renew interval is well inside the TTL so a single
// slow renewal does not surrender the lease, and the release timeout is
// bounded so shutdown cannot hang on an unreachable database.
const (
	schedulerLeaseKey       = "control-plane/scheduler"
	leaseTTL                = 15 * time.Second
	leaseRenewInterval      = 5 * time.Second
	leaseAcquireInterval    = 2 * time.Second
	leaseReleaseTimeout     = 5 * time.Second
	leaderReadinessRequired = true
)

// Placement worker tuning. The lease is renewed well inside its duration so a
// slow heartbeat does not surrender an in-flight item, and concurrency is
// bounded because each placement issues cluster writes.
const (
	placementQueue             = "control-plane/placement"
	placementWorker            = "placement"
	placementPollInterval      = 500 * time.Millisecond
	placementLeaseDuration     = 60 * time.Second
	placementHeartbeatInterval = 15 * time.Second
	placementBatchSize         = 16
	placementConcurrency       = 4
	placementFailureDelay      = 5 * time.Second
)

// SchedulerFactory assembles the scheduler process: the durable PostgreSQL
// mechanisms it shares with every other role, the leased work queue it drains,
// the singleton elector that makes it safe to run more than one replica, and
// the cluster client it places work through.
type SchedulerFactory struct {
	sources   []foundationconfig.Source
	placement workqueue.Handler
}

// WithPlacementHandler injects the domain handler that turns a claimed work
// item into a placement. It is the seam between this composition root and
// scheduling policy: the root owns the queue, the lease, the fencing, and the
// worker lifecycle, and the domain owns what a work item means.
//
// Left unset, the worker fails every item closed rather than acknowledging
// work it cannot perform. A dropped placement that reports success is worse
// than one that retries.
func (factory *SchedulerFactory) WithPlacementHandler(handler workqueue.Handler) *SchedulerFactory {
	if factory == nil {
		return nil
	}
	factory.placement = handler
	return factory
}

// NewSchedulerFactory returns the scheduler provider factory. With no sources
// the process reads its configuration from the explicit environment mapping;
// tests pass a MapSource instead.
func NewSchedulerFactory(sources ...foundationconfig.Source) *SchedulerFactory {
	if len(sources) == 0 {
		sources = []foundationconfig.Source{config.EnvironmentSource()}
	}
	return &SchedulerFactory{sources: sources}
}

// Create resolves configuration and constructs every provider the scheduler
// role requires. Construction is ordered cheapest-first: configuration and
// pure mechanisms fail before any socket, connection, or cluster client is
// opened, and anything already opened is released if a later step fails.
func (factory *SchedulerFactory) Create(ctx context.Context, profile bootstrap.Profile) (runtime bootstrap.Runtime, err error) {
	if factory == nil || ctx == nil {
		return bootstrap.Runtime{}, faults.New(
			faults.CodeInvalidArgument,
			"scheduler factory requires a context",
			faults.WithReason("invalid_factory_request"),
			faults.WithOperation("controlplane.scheduler.SchedulerFactory.Create"),
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
	key, err := lease.ParseKey(schedulerLeaseKey)
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

	handler := factory.placement
	if handler == nil {
		handler = workqueue.HandlerFunc(refusePlacement)
	}
	worker, err := workqueue.NewWorker(
		queue,
		handler,
		workqueue.WorkerConfig{
			Owner:             owner,
			Queues:            []string{placementQueue},
			PollInterval:      placementPollInterval,
			LeaseDuration:     placementLeaseDuration,
			HeartbeatInterval: placementHeartbeatInterval,
			BatchSize:         placementBatchSize,
			Concurrency:       placementConcurrency,
			FailureDelay:      placementFailureDelay,
		},
		workqueue.WithClock(shared.Clock),
		workqueue.WithRetry(shared.Retry),
	)
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	leaderHandler, workerComponent, err := leadership.GateComponent(
		worker.Component("worker/" + placementWorker),
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

	publisher, brokerLifecycle, err := broker.NewPublisher(settings, shared.Clock)
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	release = append(release, func() { _ = brokerLifecycle.Stop(ctx) })

	// The scheduler reads and writes cluster objects but runs no reconciler,
	// so it holds a client and no controller-runtime manager: a manager would
	// start an informer cache and contend for a leader lease this role already
	// holds through its own elector.
	kubernetes, err := cluster.New(ctx, settings, crclient.Options{})
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	// The scheduler runs no manager, so it owns its own event stream. The
	// broadcaster runs a goroutine from construction and is released like any
	// other opened resource if a later step fails.
	publications, eventStream, err := kubernetes.Recorder(ctx, settings.ServiceName)
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	release = append(release, func() { _ = eventStream.Stop(ctx) })

	return bootstrap.Runtime{
		// The aggregate list is the role's capability profile, written out.
		// Anything absent here is a package this binary does not link.
		Dependencies: []bootstrap.Aggregate{
			foundation.Core{
				Clock:         shared.Clock,
				Configuration: resolved.Current,
				IDs:           shared.IDs,
				// Lineage is carried, not decorative: every work item the
				// scheduler enqueues stores the request metadata of the
				// request that caused it, so a placed workload stays
				// traceable to its origin.
				RequestMetadataConfigured: true,
				Observability:             shared.Observability,
				Retry:                     shared.Retry,
			},
			// No migration runner: the scheduler reads and writes tables that
			// the registry role owns and creates.
			persistence.SQL{
				Postgres:     stores.Pool,
				Transactions: stores.Transactions,
			},
			governance.Controls{
				Audit:       recorder,
				Idempotency: records,
				Signer:      shared.Signer,
				Verifier:    shared.Verifier,
				// Scheduling decisions mutate records other roles also write,
				// so every mutation carries a compare-and-set precondition.
				ResourceVersionsConfigured: true,
			},
			leasing.Mechanisms{
				Leases: leases,
				Leader: elector,
			},
			tasks.Mechanisms{
				Queue:   queue,
				Workers: map[string]servicekit.Component{placementWorker: workerComponent},
			},
			orchestration.Cluster{
				Client: kubernetes.Client,
				Events: publications,
			},
			eventing.Mechanisms{
				Publisher: publisher,
				Outbox:    stores.Outbox,
			},
		},
		Components: bootstrap.Components{
			Auxiliary: []bootstrap.StagedComponent{
				{Stage: servicekit.StageInfrastructure, Component: brokerLifecycle},
				// CapabilityKubernetes owns no component of its own and this
				// role runs no manager, so this probe is the only thing that
				// answers for API-server reachability. Without it a scheduler
				// whose cluster is unreachable reports ready and places
				// nothing.
				{Stage: servicekit.StageInfrastructure, Component: kubernetes.Component("kubernetes")},
				{Stage: servicekit.StageInfrastructure, Component: eventStream},
			},
		},
	}, nil
}

// refusePlacement is the default placement handler. Scheduling policy is
// domain code and is not assembled in a composition root, so until a handler
// is injected the worker fails items closed: the queue retries them and
// eventually dead-letters them, which leaves the work visible. Acknowledging
// an item this process cannot place would lose it silently.
func refusePlacement(context.Context, workqueue.Item) (workqueue.Result, error) {
	return workqueue.Result{}, faults.New(
		faults.CodeNotImplemented,
		"scheduler placement policy is not configured",
		faults.WithReason("placement_handler_not_configured"),
		faults.WithOperation("controlplane.scheduler.refusePlacement"),
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
			"unable to determine the scheduler lease owner",
			faults.WithReason("hostname_unavailable"),
			faults.WithOperation("controlplane.scheduler.leaseOwner"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	return hostname, nil
}
