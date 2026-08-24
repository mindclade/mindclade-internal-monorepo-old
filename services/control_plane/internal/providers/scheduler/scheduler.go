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

	"go.mindclade.dev/control/scheduling"
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
	orchestrationstore "go.mindclade.dev/services/control_plane/internal/store/postgres/orchestration"
	schedulingstore "go.mindclade.dev/services/control_plane/internal/store/postgres/scheduling"
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
	// placementQueue is the domain's own constant rather than a second copy of
	// the string. The two were duplicated and nothing compared them, so had
	// either side changed, this role would have drained a queue nobody filled --
	// silently, with no failing test and no error to read.
	placementQueue             = scheduling.PlacementQueue
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
	facts     PlacementFacts
}

// WithPlacementHandler replaces the standard scheduling.Service placement
// handler. The domain contract is fixed: the item is drained from
// scheduling.PlacementQueue and its payload is a scheduling.PlacementCommand,
// so a replacement must implement that exact contract. This seam exists for
// qualification and extensions; the production default is fully configured and
// never reports synthetic success.
func (factory *SchedulerFactory) WithPlacementHandler(handler workqueue.Handler) *SchedulerFactory {
	if factory == nil {
		return nil
	}
	factory.placement = handler
	return factory
}

// WithPlacementFacts binds the source of the fleet admission facts a promoted
// stage does not carry, and with it the placement producer.
//
// There is no default, and deliberately no fail-closed one either. A producer
// that refused every promotion would make a promoted stage un-promotable rather
// than unplaced, and a producer bound to a facts source that invented a tenant
// would charge the fleet ledger against a workspace nobody named. Both are
// worse than composing no producer: without one, the orchestration repository
// still records stage state, which is exactly what its own WithEnqueuer doc
// calls a legitimate composition. The role therefore builds the promotion path
// only when it has been given the facts to translate with, and says so here
// rather than hiding it behind a stub. See PlacementFacts for why the four
// identity fields cannot supply them.
func (factory *SchedulerFactory) WithPlacementFacts(facts PlacementFacts) *SchedulerFactory {
	if factory == nil {
		return nil
	}
	factory.facts = facts
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

	// The durable scheduling repository. Its audit recorder and outbox store
	// are this role's PostgreSQL adapters, so a reservation, its audit record
	// and its outbox message share one SERIALIZABLE transaction.
	//
	// shared.Retry is deliberately NOT passed. The store sizes its own
	// serialization budget from this role's contention shape -- every mutation
	// takes the singleton ledger row first, so under SERIALIZABLE a concurrent
	// writer is aborted rather than delayed -- and that argument is written out
	// in schedulingstore's config.go against placementConcurrency below.
	// Handing it the shared executor would silently replace a re-argued budget
	// with the generic one.
	placements, err := schedulingstore.New(stores.DB, recorder, stores.Outbox,
		schedulingstore.WithClock(shared.Clock),
		schedulingstore.WithGenerator(shared.IDs),
	)
	if err != nil {
		return bootstrap.Runtime{}, err
	}

	// Constructed before the handler that reads it and handed to the gate
	// below, so the worker's view of the epoch is the elector's own rather than
	// a number copied at wiring time. See fencedPlacement.
	view := &leadership.SessionView{}
	handler := factory.placement
	if handler == nil {
		handler = fencedPlacement(scheduling.Service{
			Repository: placements,
			Clock:      shared.Clock,
			// Fence is supplied per call. Setting it here would pin the epoch
			// this process was constructed in.
		}, view)
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
	leaderHandler, workerComponent, err := leadership.GateComponentWithSession(
		worker.Component("worker/"+placementWorker), view,
	)
	if err != nil {
		return bootstrap.Runtime{}, err
	}

	// The promotion path. It exists only when the role has been given a facts
	// source, because a producer with nothing to translate from is worse than
	// no producer at all -- see WithPlacementFacts.
	schemas := []bootstrap.StagedComponent{
		{Stage: servicekit.StageInfrastructure, Component: placements.Component("scheduling-schema")},
	}
	if !foundation.IsNil(factory.facts) {
		producer, producerErr := newPlacementProducer(queue, factory.facts)
		if producerErr != nil {
			return bootstrap.Runtime{}, producerErr
		}
		promotions, promotionErr := orchestrationstore.New(stores.DB, recorder, stores.Outbox,
			orchestrationstore.WithClock(shared.Clock),
			orchestrationstore.WithGenerator(shared.IDs),
			orchestrationstore.WithRetry(shared.Retry),
			orchestrationstore.WithEnqueuer(producer),
		)
		if promotionErr != nil {
			return bootstrap.Runtime{}, promotionErr
		}
		schemas = append(schemas, bootstrap.StagedComponent{
			Stage: servicekit.StageInfrastructure, Component: promotions.Component("orchestration-schema"),
		})
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
		// The shutdown budgets belong to the deployment, not to a constant in
		// this package. A role that does not pass them runs on the servicekit
		// package defaults, and drain.timeout stops meaning anything.
		Lifecycle: bootstrap.Lifecycle{
			ShutdownTimeout: settings.ShutdownTimeout,
			DrainTimeout:    settings.DrainTimeout,
		},
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
			// The schema probes come first: a scheduler whose scheduling tables
			// are missing cannot place anything, and without a probe it would
			// report ready and dead-letter every item it claimed.
			Auxiliary: append(schemas,
				bootstrap.StagedComponent{Stage: servicekit.StageInfrastructure, Component: brokerLifecycle},
				// CapabilityKubernetes owns no component of its own and this
				// role runs no manager, so this probe is the only thing that
				// answers for API-server reachability. Without it a scheduler
				// whose cluster is unreachable reports ready and places
				// nothing.
				bootstrap.StagedComponent{Stage: servicekit.StageInfrastructure, Component: kubernetes.Component("kubernetes")},
				bootstrap.StagedComponent{Stage: servicekit.StageInfrastructure, Component: eventStream},
			),
		},
	}, nil
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
