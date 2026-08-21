// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package maintenance is the composition root for the control-plane
// maintenance role: the process that runs periodic housekeeping over the
// control plane's own state.
//
// It is the narrowest role that still holds a singleton lease. It publishes
// nothing, serves nothing, and reaches no cluster; it claims work, does it
// once, and records that it happened.
package maintenance

import (
	"context"
	"os"
	"time"

	foundationconfig "go.mindclade.dev/libs/go/config"
	"go.mindclade.dev/libs/go/coordination/leadership"
	"go.mindclade.dev/libs/go/coordination/workqueue"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/servicekit"
	"go.mindclade.dev/libs/go/storage/lease"
	"go.mindclade.dev/services/control_plane/internal/bootstrap"
	"go.mindclade.dev/services/control_plane/internal/config"
	"go.mindclade.dev/services/control_plane/internal/foundation"
	"go.mindclade.dev/services/control_plane/internal/foundation/governance"
	"go.mindclade.dev/services/control_plane/internal/foundation/leasing"
	"go.mindclade.dev/services/control_plane/internal/foundation/persistence"
	"go.mindclade.dev/services/control_plane/internal/foundation/tasks"
	"go.mindclade.dev/services/control_plane/internal/providers"
	"go.mindclade.dev/services/control_plane/internal/providers/durable"
)

// Leadership timings match the other singleton roles. The key differs because
// each singleton is a separate claim.
const (
	maintenanceLeaseKey     = "control-plane/maintenance"
	leaseTTL                = 15 * time.Second
	leaseRenewInterval      = 5 * time.Second
	leaseAcquireInterval    = 2 * time.Second
	leaseReleaseTimeout     = 5 * time.Second
	leaderReadinessRequired = true
)

// Housekeeping worker tuning. Concurrency is deliberately low and the lease
// long: maintenance work is usually a single large sweep over a table rather
// than many small independent items.
const (
	housekeepingQueue             = "control-plane/maintenance"
	housekeepingWorker            = "housekeeping"
	housekeepingPollInterval      = 2 * time.Second
	housekeepingLeaseDuration     = 300 * time.Second
	housekeepingHeartbeatInterval = 60 * time.Second
	housekeepingBatchSize         = 4
	housekeepingConcurrency       = 2
	housekeepingFailureDelay      = 60 * time.Second
)

// MaintenanceFactory assembles the maintenance process: the durable PostgreSQL
// mechanisms, the singleton elector that keeps two replicas from sweeping the
// same table at once, and the leased queue its housekeeping runs through.
type MaintenanceFactory struct {
	sources     []foundationconfig.Source
	housekeeper workqueue.Handler
}

// NewMaintenanceFactory returns the maintenance provider factory. With no
// sources the process reads its configuration from the explicit environment
// mapping; tests pass a MapSource instead.
func NewMaintenanceFactory(sources ...foundationconfig.Source) *MaintenanceFactory {
	if len(sources) == 0 {
		sources = []foundationconfig.Source{config.EnvironmentSource()}
	}
	return &MaintenanceFactory{sources: sources}
}

// WithHousekeepingHandler injects the domain handler that performs one unit of
// housekeeping. It is the seam between this composition root and maintenance
// policy: the root owns the lease, the queue, and the fencing, and the domain
// owns what a sweep does.
//
// Left unset, the worker fails every item closed. Housekeeping that reports
// success without running leaves the state it was meant to reclaim in place,
// and nothing else notices.
func (factory *MaintenanceFactory) WithHousekeepingHandler(handler workqueue.Handler) *MaintenanceFactory {
	if factory == nil {
		return nil
	}
	factory.housekeeper = handler
	return factory
}

// Create resolves configuration and constructs every provider the maintenance
// role requires. Construction is ordered cheapest-first: configuration and
// pure mechanisms fail before a database connection is opened, and anything
// already opened is released if a later step fails.
func (factory *MaintenanceFactory) Create(ctx context.Context, profile bootstrap.Profile) (runtime bootstrap.Runtime, err error) {
	if factory == nil || ctx == nil {
		return bootstrap.Runtime{}, faults.New(
			faults.CodeInvalidArgument,
			"maintenance factory requires a context",
			faults.WithReason("invalid_factory_request"),
			faults.WithOperation("controlplane.maintenance.MaintenanceFactory.Create"),
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
	key, err := lease.ParseKey(maintenanceLeaseKey)
	if err != nil {
		return bootstrap.Runtime{}, err
	}

	release := make([]func(), 0, 1)
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
	leases, err := durable.NewLeaseStore(stores.DB)
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	queue, err := durable.NewWorkQueueStore(stores.DB)
	if err != nil {
		return bootstrap.Runtime{}, err
	}

	handler := factory.housekeeper
	if handler == nil {
		handler = workqueue.HandlerFunc(refuseHousekeeping)
	}
	worker, err := workqueue.NewWorker(
		queue,
		handler,
		workqueue.WorkerConfig{
			Owner:             owner,
			Queues:            []string{housekeepingQueue},
			PollInterval:      housekeepingPollInterval,
			LeaseDuration:     housekeepingLeaseDuration,
			HeartbeatInterval: housekeepingHeartbeatInterval,
			BatchSize:         housekeepingBatchSize,
			Concurrency:       housekeepingConcurrency,
			FailureDelay:      housekeepingFailureDelay,
		},
		workqueue.WithClock(shared.Clock),
		workqueue.WithRetry(shared.Retry),
	)
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	leaderHandler, workerComponent, err := leadership.GateComponent(
		worker.Component("worker/" + housekeepingWorker),
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

	return bootstrap.Runtime{
		// The aggregate list is the role's capability profile, written out.
		// Anything absent here is a package this binary does not link.
		Dependencies: []bootstrap.Aggregate{
			foundation.Core{
				Clock:         shared.Clock,
				Configuration: resolved.Current,
				IDs:           shared.IDs,
				// Lineage is carried, not decorative: a housekeeping item
				// stores the request metadata of whatever scheduled it, so a
				// reclaimed row stays traceable to the decision to reclaim it.
				RequestMetadataConfigured: true,
				Observability:             shared.Observability,
				Retry:                     shared.Retry,
			},
			// No migration runner. CONSUMPTION.md names maintenance as the
			// intended migration process, but the registry role owns the
			// manifest today and two runners against one database would race
			// for the same version ordering. Moving ownership is a deployment
			// change, not a composition change.
			persistence.SQL{
				Postgres:     stores.Pool,
				Transactions: stores.Transactions,
			},
			// Audit only. Maintenance mutates control-plane state without a
			// caller, so what it did must be recorded; it issues no tickets,
			// paginates nothing, and deduplicates nothing.
			governance.Controls{
				Audit: recorder,
			},
			leasing.Mechanisms{
				Leases: leases,
				Leader: elector,
			},
			tasks.Mechanisms{
				Queue:   queue,
				Workers: map[string]servicekit.Component{housekeepingWorker: workerComponent},
			},
		},
	}, nil
}

// refuseHousekeeping is the default handler. What a sweep does is domain
// policy, so until a handler is injected the worker fails items closed rather
// than reporting that housekeeping ran.
func refuseHousekeeping(context.Context, workqueue.Item) (workqueue.Result, error) {
	return workqueue.Result{}, faults.New(
		faults.CodeNotImplemented,
		"maintenance housekeeping policy is not configured",
		faults.WithReason("housekeeping_handler_not_configured"),
		faults.WithOperation("controlplane.maintenance.refuseHousekeeping"),
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
			"unable to determine the maintenance lease owner",
			faults.WithReason("hostname_unavailable"),
			faults.WithOperation("controlplane.maintenance.leaseOwner"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	return hostname, nil
}
