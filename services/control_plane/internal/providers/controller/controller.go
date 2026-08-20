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

// Factory assembles a reconciling process: the durable PostgreSQL mechanisms
// it shares with every other role, the singleton elector that makes it safe to
// run more than one replica, and the controller-runtime manager whose cache
// and reconcilers it owns for the life of the process.
//
// The controller and operator roles have identical capability profiles, so
// they are the same composition rather than two copies of it. They differ only
// in the lease they claim and the source they report events under, which is
// exactly what keeps them separate singletons that an operator can tell apart.
type Factory struct {
	sources     []foundationconfig.Source
	leaseKey    string
	eventSource string
}

// NewControllerFactory returns the controller provider factory. With no
// sources the process reads its configuration from the explicit environment
// mapping; tests pass a MapSource instead.
func NewControllerFactory(sources ...foundationconfig.Source) *Factory {
	return newFactory(controllerLeaseKey, controllerEventSource, sources)
}

// NewOperatorFactory returns the operator provider factory.
func NewOperatorFactory(sources ...foundationconfig.Source) *Factory {
	return newFactory(operatorLeaseKey, operatorEventSource, sources)
}

func newFactory(leaseKey, eventSource string, sources []foundationconfig.Source) *Factory {
	if len(sources) == 0 {
		sources = []foundationconfig.Source{config.EnvironmentSource()}
	}
	return &Factory{sources: sources, leaseKey: leaseKey, eventSource: eventSource}
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
	leaderHandler, managerComponent, err := leadership.GateComponent(
		managerRuntime.Component(orchestration.ManagerComponent),
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
	// The replacement GetEventRecorder API cannot attach the request-lineage
	// annotations that events.Recorder guarantees. Keep the compatibility API
	// until client-go exposes equivalent structured-event metadata support.
	//nolint:staticcheck // AnnotatedEventf is required to preserve request lineage.
	publications, err := events.New(manager.GetEventRecorderFor(factory.eventSource))
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
				Queue: queue,
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
