// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package controller is the composition root for the control-plane controller
// role: the process that reconciles declared intent against cluster state.
//
// It is the first role to run a controller-runtime manager, and therefore the
// first to hold both an informer cache and a foundation lease at once. The
// reconcilers themselves are domain code and are not assembled here.
package controller

import (
	"context"
	"os"
	"time"

	crclient "sigs.k8s.io/controller-runtime/pkg/client"

	foundationconfig "go.mindclade.dev/libs/go/config"
	"go.mindclade.dev/libs/go/coordination/leadership"
	"go.mindclade.dev/libs/go/coordination/workqueue"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/kubernetes/controller"
	"go.mindclade.dev/libs/go/kubernetes/events"
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

// Leadership timings match the scheduler's. The renew interval is well inside
// the TTL so a single slow renewal does not surrender the lease, and the
// release timeout is bounded so shutdown cannot hang on an unreachable
// database. The key differs because the two roles are separate singletons.
const (
	controllerLeaseKey      = "control-plane/controller"
	operatorLeaseKey        = "control-plane/operator"
	leaseTTL                = 15 * time.Second
	leaseRenewInterval      = 5 * time.Second
	leaseAcquireInterval    = 2 * time.Second
	leaseReleaseTimeout     = 5 * time.Second
	leaderReadinessRequired = true
)

// The reporting component recorded on every object event a process publishes.
// It appears in `kubectl describe` output, so it names the process rather than
// the library that emitted the event, and the two roles must differ or an
// operator reading an object cannot tell which process acted on it.
const (
	controllerEventSource = "mindclade-control-plane-controller"
	operatorEventSource   = "mindclade-control-plane-operator"
)

// The stage queue each role drains. The two roles are separate singletons under
// separate leases, so they must not share a queue: one queue drained by both
// would let the operator claim an item the controller's reconciler was meant to
// handle, and a claim is exclusive -- the controller would never see it again.
// The queue name therefore joins the lease key and the event source in the
// short list of things these two otherwise identical compositions differ by.
const (
	controllerStageQueue = "control-plane/controller-stages"
	operatorStageQueue   = "control-plane/operator-stages"
)

// Stage worker tuning. Concurrency is bounded because each reconcile issues
// cluster writes, and the lease is renewed well inside its duration so a slow
// heartbeat does not surrender an in-flight item. The numbers match the
// scheduler's placement worker deliberately: both drain the same store and do a
// bounded amount of cluster work per item, and two unexplained sets of numbers
// for the same shape is how one of them silently stops being reviewed.
const (
	stageWorker            = "stage-reconciler"
	stagePollInterval      = 500 * time.Millisecond
	stageLeaseDuration     = 60 * time.Second
	stageHeartbeatInterval = 15 * time.Second
	stageBatchSize         = 16
	stageConcurrency       = 4
	stageFailureDelay      = 5 * time.Second
	// leaderWorkGroup names the task group the gated components run in. It
	// appears in task telemetry, so it names the role's leader work rather than
	// any one component inside it.
	leaderWorkGroup = "control-plane-leader"
)

// Factory assembles a reconciling process: the durable PostgreSQL mechanisms
// it shares with every other role, the singleton elector that makes it safe to
// run more than one replica, the controller-runtime manager whose cache and
// reconcilers it owns for the life of the process, and the leased stage worker
// that carries orchestration work items into that reconciler.
//
// The controller and operator roles have identical capability profiles, so
// they are the same composition rather than two copies of it. They differ only
// in the lease they claim, the source they report events under, and the stage
// queue they drain -- exactly what keeps them separate singletons that an
// operator can tell apart.
type Factory struct {
	sources     []foundationconfig.Source
	role        bootstrap.Role
	leaseKey    string
	eventSource string
	stageQueue  string
	stage       workqueue.Handler
}

// NewControllerFactory returns the controller provider factory. With no
// sources the process reads its configuration from the explicit environment
// mapping; tests pass a MapSource instead.
func NewControllerFactory(sources ...foundationconfig.Source) *Factory {
	return newFactory(bootstrap.RoleController, controllerLeaseKey, controllerEventSource, controllerStageQueue, sources)
}

// NewOperatorFactory returns the operator provider factory.
func NewOperatorFactory(sources ...foundationconfig.Source) *Factory {
	return newFactory(bootstrap.RoleOperator, operatorLeaseKey, operatorEventSource, operatorStageQueue, sources)
}

// WithStageReconciler injects the domain handler that turns a claimed stage
// work item into a reconcile. It is the seam between this composition root and
// orchestration policy: the root owns the queue, the lease, the fencing, and
// the worker lifecycle, and the domain owns what a work item means.
//
// The expected shape is orchestration.Handler(handler, observers...), which
// already adapts an orchestration.StageHandler to workqueue.HandlerFunc,
// decodes the orchestration.WorkItem payload, and expresses the retry
// disposition as the fault policy this worker reads. This is a work-queue seam,
// not a controller-runtime reconciler: reconcilers are registered on the
// manager, and an item claimed from a durable queue is a different arrival.
//
// Left unset, the worker fails every item closed rather than acknowledging work
// it cannot perform. A dropped stage that reports success is a run that never
// finishes and never fails.
func (factory *Factory) WithStageReconciler(handler workqueue.Handler) *Factory {
	if factory == nil {
		return nil
	}
	factory.stage = handler
	return factory
}

func newFactory(role bootstrap.Role, leaseKey, eventSource, stageQueue string, sources []foundationconfig.Source) *Factory {
	if len(sources) == 0 {
		sources = []foundationconfig.Source{config.EnvironmentSource()}
	}
	return &Factory{
		sources: sources, role: role, leaseKey: leaseKey,
		eventSource: eventSource, stageQueue: stageQueue,
	}
}

// Create resolves configuration and constructs every provider the controller
// role requires. Construction is ordered cheapest-first: configuration and
// pure mechanisms fail before any socket, connection, or cluster client is
// opened, and anything already opened is released if a later step fails.
func (factory *Factory) Create(ctx context.Context, profile bootstrap.Profile) (runtime bootstrap.Runtime, err error) {
	if factory == nil || ctx == nil {
		return bootstrap.Runtime{}, faults.New(
			faults.CodeInvalidArgument,
			"reconciling factory requires a context",
			faults.WithReason("invalid_factory_request"),
			faults.WithOperation("controlplane.controller.Factory.Create"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	if err := profile.Validate(); err != nil {
		return bootstrap.Runtime{}, err
	}
	// The controller and operator profiles are capability-identical, so
	// production.Builder cannot tell them apart: either composition satisfies
	// either profile. This factory is the only object that knows which variant
	// it is, and therefore the only place a command wired to the wrong one can
	// be caught. Left unchecked, a process would run under one role's name
	// while claiming the other's singleton lease -- two deployments contending
	// for one lease, silently.
	if profile.Role != factory.role {
		return bootstrap.Runtime{}, faults.New(
			faults.CodeInvalidArgument,
			"reconciling factory role does not match the process profile",
			faults.WithReason("factory_profile_role_mismatch"),
			faults.WithOperation("controlplane.controller.Factory.Create"),
			faults.WithField("factory_role", factory.role.String()),
			faults.WithField("profile_role", profile.Role.String()),
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
	key, err := lease.ParseKey(factory.leaseKey)
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

	publisher, brokerLifecycle, err := broker.NewPublisher(settings, shared.Clock)
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	release = append(release, func() { _ = brokerLifecycle.Stop(ctx) })

	kubernetes, err := cluster.New(ctx, settings, crclient.Options{})
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	manager, err := newManager(kubernetes.Config)
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	// Readiness comes from the discovery client rather than the manager,
	// because a manager whose cache has gone stale is still running: asking
	// the API server directly is what distinguishes the two.
	managerRuntime, err := controller.NewManagerRuntime(manager, kubernetes.Readiness)
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	stageHandler := factory.stage
	if stageHandler == nil {
		stageHandler = workqueue.HandlerFunc(refuseStageReconcile)
	}
	stageRunner, err := workqueue.NewWorker(
		queue,
		stageHandler,
		workqueue.WorkerConfig{
			Owner:             owner,
			Queues:            []string{factory.stageQueue},
			PollInterval:      stagePollInterval,
			LeaseDuration:     stageLeaseDuration,
			HeartbeatInterval: stageHeartbeatInterval,
			BatchSize:         stageBatchSize,
			Concurrency:       stageConcurrency,
			FailureDelay:      stageFailureDelay,
		},
		workqueue.WithClock(shared.Clock),
		workqueue.WithRetry(shared.Retry),
	)
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	// One elector, one handler, two things that may only run under the lease.
	// gateLeaderWork is what makes that expressible; see its doc for why the
	// two must fail as a unit.
	leaderHandler, gated, err := gateLeaderWork(leaderWorkGroup,
		managerRuntime.Component(orchestration.ManagerComponent),
		stageRunner.Component("worker/"+stageWorker),
	)
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	managerComponent, stageComponent := gated[0], gated[1]
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
	// The replacement GetEventRecorder API cannot attach the request-lineage
	// annotations that events.Recorder guarantees. Keep the compatibility API
	// until client-go exposes equivalent structured-event metadata support.
	//nolint:staticcheck // AnnotatedEventf is required to preserve request lineage.
	publications, err := events.New(manager.GetEventRecorderFor(factory.eventSource))
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
		// The aggregate list is the role's capability profile, written out.
		// Anything absent here is a package this binary does not link.
		Dependencies: []bootstrap.Aggregate{
			foundation.Core{
				Clock:         shared.Clock,
				Configuration: resolved.Current,
				IDs:           shared.IDs,
				// Lineage is carried, not decorative: the reconcile middleware
				// in kubernetes/controller stamps request metadata onto every
				// reconcile, so a cluster write stays traceable to the intent
				// that caused it.
				RequestMetadataConfigured: true,
				Observability:             shared.Observability,
				Retry:                     shared.Retry,
			},
			// No migration runner: the controller reads and writes tables that
			// the registry role owns and creates.
			persistence.SQL{
				Postgres:     stores.Pool,
				Transactions: stores.Transactions,
			},
			// No signer: unlike the scheduler, the controller issues no
			// execution tickets and encodes no cursors, so its role does not
			// require the signing capability and it does not claim one.
			governance.Controls{
				Audit:       recorder,
				Idempotency: records,
				// Reconciliation writes back to records other roles also
				// write, so every mutation carries a compare-and-set
				// precondition.
				ResourceVersionsConfigured: true,
			},
			leasing.Mechanisms{
				Leases: leases,
				Leader: elector,
			},
			tasks.Mechanisms{
				Queue:   queue,
				Workers: map[string]servicekit.Component{stageWorker: stageComponent},
			},
			// The manager's cached client is the one reconcilers read through.
			// The direct client the cluster provider builds stays unused here:
			// reading around the cache would defeat the informers the manager
			// exists to run.
			orchestration.Cluster{
				Client:  manager.GetClient(),
				Events:  publications,
				Manager: &managerComponent,
			},
			eventing.Mechanisms{
				Publisher: publisher,
				Outbox:    stores.Outbox,
			},
		},
		Components: bootstrap.Components{
			Auxiliary: []bootstrap.StagedComponent{
				{Stage: servicekit.StageInfrastructure, Component: brokerLifecycle},
			},
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
			"unable to determine the controller lease owner",
			faults.WithReason("hostname_unavailable"),
			faults.WithOperation("controlplane.controller.leaseOwner"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	return hostname, nil
}
