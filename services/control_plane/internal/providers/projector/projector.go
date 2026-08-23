// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package projector is the composition root for the control-plane event
// projector: the process that consumes the ordered event stream exactly once
// and advances a durable cursor as it goes.
//
// It is the only role that holds the projector mechanism, and the first to
// hold the inbox and cursor mechanisms at all. What the events mean, and what
// log they are read from, is domain policy and is injected rather than decided
// here.
package projector

import (
	"context"
	"os"
	"time"

	foundationconfig "go.mindclade.dev/libs/go/config"
	"go.mindclade.dev/libs/go/coordination/cursor"
	"go.mindclade.dev/libs/go/coordination/inbox"
	"go.mindclade.dev/libs/go/coordination/leadership"
	"go.mindclade.dev/libs/go/coordination/projector"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/idempotency"
	"go.mindclade.dev/libs/go/servicekit"
	"go.mindclade.dev/libs/go/storage/lease"
	"go.mindclade.dev/libs/go/storage/sql/transaction"
	"go.mindclade.dev/services/control_plane/internal/bootstrap"
	"go.mindclade.dev/services/control_plane/internal/config"
	"go.mindclade.dev/services/control_plane/internal/foundation"
	"go.mindclade.dev/services/control_plane/internal/foundation/eventing"
	"go.mindclade.dev/services/control_plane/internal/foundation/governance"
	"go.mindclade.dev/services/control_plane/internal/foundation/leasing"
	"go.mindclade.dev/services/control_plane/internal/foundation/persistence"
	"go.mindclade.dev/services/control_plane/internal/foundation/projection"
	"go.mindclade.dev/services/control_plane/internal/providers"
	"go.mindclade.dev/services/control_plane/internal/providers/broker"
	"go.mindclade.dev/services/control_plane/internal/providers/durable"
)

// Leadership timings match the other singleton roles. The key differs because
// each singleton is a separate claim.
const (
	projectorLeaseKey       = "control-plane/event-projector"
	leaseTTL                = 15 * time.Second
	leaseRenewInterval      = 5 * time.Second
	leaseAcquireInterval    = 2 * time.Second
	leaseReleaseTimeout     = 5 * time.Second
	leaderReadinessRequired = true
)

// Projection tuning. The message TTL bounds how long an event may sit
// unprojected before it is treated as expired, and the lease duration bounds
// how long this process may hold the projection without renewing.
const (
	projectionName          = "control-plane-events"
	cursorNamespace         = "control-plane"
	cursorName              = "events"
	projectionPollInterval  = 250 * time.Millisecond
	projectionBatchSize     = 64
	projectionMessageTTL    = 24 * time.Hour
	projectionLeaseDuration = 60 * time.Second
)

// ProjectorFactory assembles the event projector: the idempotent inbox, the
// compare-and-advance cursor store, the singleton elector that fences the
// projection, and the projector loop that ties them together.
type ProjectorFactory struct {
	sources []foundationconfig.Source
	source  projector.Source
	handler projector.Handler
}

// NewProjectorFactory returns the event-projector provider factory. With no
// sources the process reads its configuration from the explicit environment
// mapping; tests pass a MapSource instead.
func NewProjectorFactory(sources ...foundationconfig.Source) *ProjectorFactory {
	if len(sources) == 0 {
		sources = []foundationconfig.Source{config.EnvironmentSource()}
	}
	return &ProjectorFactory{sources: sources}
}

// WithProjection injects the domain's event log and the handler that applies
// its events. These are the seam between this composition root and projection
// policy: the root owns the inbox transaction, the cursor, the fence, and the
// loop, and the domain owns what an event is and what applying one does.
//
// Left unset, the source fails closed. A projector that silently reads an
// empty log looks healthy while projecting nothing, which is the failure that
// takes longest to notice.
func (factory *ProjectorFactory) WithProjection(source projector.Source, handler projector.Handler) *ProjectorFactory {
	if factory == nil {
		return nil
	}
	factory.source = source
	factory.handler = handler
	return factory
}

// Create resolves configuration and constructs every provider the event
// projector requires. Construction is ordered cheapest-first: configuration
// and pure mechanisms fail before any socket or connection is opened, and
// anything already opened is released if a later step fails.
func (factory *ProjectorFactory) Create(ctx context.Context, profile bootstrap.Profile) (runtime bootstrap.Runtime, err error) {
	if factory == nil || ctx == nil {
		return bootstrap.Runtime{}, faults.New(
			faults.CodeInvalidArgument,
			"event-projector factory requires a context",
			faults.WithReason("invalid_factory_request"),
			faults.WithOperation("controlplane.projector.ProjectorFactory.Create"),
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
	leaseKey, err := lease.ParseKey(projectorLeaseKey)
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	cursorKey, err := cursor.NewKey(cursorNamespace, cursorName)
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

	records, err := durable.NewIdempotencyStore(stores.DB, shared.Clock, shared.IDs)
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	leases, err := durable.NewLeaseStore(stores.DB)
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	cursors, err := durable.NewCursorStore(stores.DB)
	if err != nil {
		return bootstrap.Runtime{}, err
	}

	// The inbox and the cursor advance in the same transaction. That is the
	// whole correctness argument for this role: an effect that commits without
	// its cursor is replayed, and a cursor that commits without its effect is
	// lost.
	processor, err := inbox.New(
		inbox.SQLRunner{Beginner: stores.Transactions, Options: transaction.Options{}},
		records,
	)
	if err != nil {
		return bootstrap.Runtime{}, err
	}

	// The processor needs the elector's current fencing token, while the
	// elector must own the processor's Run loop. The handler is bound below,
	// after both mechanisms exist and before the runtime can start.
	var leaderHandler leadership.Handler
	elector, err := leadership.New(
		leases,
		leadership.Config{
			Key:                    leaseKey,
			Owner:                  owner,
			TTL:                    leaseTTL,
			RenewInterval:          leaseRenewInterval,
			AcquireInterval:        leaseAcquireInterval,
			ReleaseTimeout:         leaseReleaseTimeout,
			RequireLeaderReadiness: leaderReadinessRequired,
			ExitOnLeadershipLoss:   true,
		},
		func(ctx context.Context, session leadership.Session) error {
			if leaderHandler == nil {
				return faults.New(
					faults.CodeFailedPrecondition,
					"projector leadership handler is not configured",
					faults.WithReason("projector_leadership_handler_not_configured"),
					faults.WithOperation("controlplane.projector.ProjectorFactory.Create"),
					faults.WithRetryPolicy(faults.NoRetry()),
				)
			}
			return leaderHandler(ctx, session)
		},
		leadership.WithClock(shared.Clock),
		leadership.WithRetry(shared.Retry),
	)
	if err != nil {
		return bootstrap.Runtime{}, err
	}

	source := factory.source
	if source == nil {
		source = sourceFunc(refuseFetch)
	}
	handler := factory.handler
	if handler == nil {
		handler = projector.HandlerFunc(refuseApply)
	}
	// The fence comes from the elector rather than from a counter of this
	// process's own: a projection that advanced a cursor after losing
	// leadership would overwrite the work of the process that took over.
	loop, err := projector.New(
		source,
		handler,
		processor,
		cursors,
		projector.FenceProviderFunc(electorFence(elector)),
		projector.Config{
			Cursor:        cursorKey,
			PollInterval:  projectionPollInterval,
			BatchSize:     projectionBatchSize,
			MessageTTL:    projectionMessageTTL,
			LeaseDuration: projectionLeaseDuration,
		},
		projector.WithClock(shared.Clock),
	)
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	leaderHandler, projectorComponent, err := leadership.GateComponent(
		loop.Component("projector/" + projectionName),
	)
	if err != nil {
		return bootstrap.Runtime{}, err
	}

	subscription, brokerLifecycle, err := broker.NewSubscription(settings, shared.Clock)
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	release = append(release, func() { _ = brokerLifecycle.Stop(ctx) })

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
				// Lineage is carried, not decorative: a projector event holds
				// the request identifier of the request that produced it, so a
				// projected effect stays traceable to its origin.
				RequestMetadataConfigured: true,
				Observability:             shared.Observability,
				Retry:                     shared.Retry,
			},
			// No migration runner: the projector reads and writes tables that
			// the registry role owns and creates.
			persistence.SQL{
				Postgres:     stores.Pool,
				Transactions: stores.Transactions,
			},
			// Idempotency only. The projector records no audit events, issues
			// no signed tickets, and paginates nothing, so it claims none of
			// those capabilities.
			governance.Controls{
				Idempotency: records,
			},
			leasing.Mechanisms{
				Leases: leases,
				Leader: elector,
			},
			projection.Mechanisms{
				Cursors:    cursors,
				Inbox:      processor,
				Projectors: map[string]servicekit.Component{projectionName: projectorComponent},
			},
			// A subscription rather than a publisher: this role consumes the
			// stream other roles produce, and writes no outbox of its own.
			eventing.Mechanisms{
				Subscription: subscription,
			},
		},
		Components: bootstrap.Components{
			Auxiliary: []bootstrap.StagedComponent{
				{Stage: servicekit.StageInfrastructure, Component: brokerLifecycle},
			},
		},
	}, nil
}

// sourceFunc adapts a function to projector.Source. The library ships a
// HandlerFunc but no SourceFunc, and a composition root should not need a
// named type to express a default.
type sourceFunc func(context.Context, *cursor.Cursor, int) ([]projector.Event, error)

func (function sourceFunc) Fetch(ctx context.Context, position *cursor.Cursor, limit int) ([]projector.Event, error) {
	return function(ctx, position, limit)
}

// refuseFetch is the default event source. What the event log is -- an outbox
// table, a broker topic, a change feed -- is a domain decision, so until one
// is injected the projector reports that it cannot read rather than reporting
// an empty log. An idle projector and a misconfigured one look identical
// otherwise.
func refuseFetch(context.Context, *cursor.Cursor, int) ([]projector.Event, error) {
	return nil, faults.New(
		faults.CodeNotImplemented,
		"projection event source is not configured",
		faults.WithReason("projection_source_not_configured"),
		faults.WithOperation("controlplane.projector.refuseFetch"),
		faults.WithRetryPolicy(faults.NoRetry()),
	)
}

// refuseApply is the default projection handler, for the same reason.
func refuseApply(context.Context, projector.Event) (idempotency.Result, error) {
	return idempotency.Result{}, faults.New(
		faults.CodeNotImplemented,
		"projection handler is not configured",
		faults.WithReason("projection_handler_not_configured"),
		faults.WithOperation("controlplane.projector.refuseApply"),
		faults.WithRetryPolicy(faults.NoRetry()),
	)
}

// electorFence exposes the leadership fence to the projector. A cursor advance
// carries the fencing token of the lease that authorized it, so a stale leader
// cannot move a cursor its successor has already moved.
func electorFence(elector *leadership.Elector) func() (uint64, bool) {
	return func() (uint64, bool) {
		session, ok := elector.Current()
		if !ok {
			return 0, false
		}
		return session.Fence(), true
	}
}

// leaseOwner identifies this process instance to the lease store. The hostname
// is the right identity under Kubernetes because a pod name is unique for the
// lifetime of the pod, which is exactly the lifetime of the claim.
func leaseOwner() (string, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return "", faults.Wrap(err, faults.CodeInternal,
			"unable to determine the event-projector lease owner",
			faults.WithReason("hostname_unavailable"),
			faults.WithOperation("controlplane.projector.leaseOwner"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	return hostname, nil
}
