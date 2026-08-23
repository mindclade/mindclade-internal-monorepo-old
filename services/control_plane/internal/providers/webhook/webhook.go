// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package webhook is the composition root for the control-plane webhook
// dispatcher: the one process that calls out to systems this control plane
// does not own.
//
// It is the only role whose profile requires CapabilityOutboundHTTP, and
// therefore the only consumer of the policy-bound outbound client. What a
// webhook payload contains and where it is sent are domain decisions; this
// package owns the queue, the worker, the signer, and the egress policy that
// bounds them.
package webhook

import (
	"context"
	"os"
	"time"

	foundationconfig "go.mindclade.dev/libs/go/config"
	"go.mindclade.dev/libs/go/coordination/workqueue"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/httpx/outbound"
	"go.mindclade.dev/libs/go/servicekit"
	"go.mindclade.dev/services/control_plane/internal/bootstrap"
	"go.mindclade.dev/services/control_plane/internal/config"
	"go.mindclade.dev/services/control_plane/internal/foundation"
	"go.mindclade.dev/services/control_plane/internal/foundation/egress"
	"go.mindclade.dev/services/control_plane/internal/foundation/eventing"
	"go.mindclade.dev/services/control_plane/internal/foundation/governance"
	"go.mindclade.dev/services/control_plane/internal/foundation/persistence"
	"go.mindclade.dev/services/control_plane/internal/foundation/tasks"
	"go.mindclade.dev/services/control_plane/internal/providers"
	"go.mindclade.dev/services/control_plane/internal/providers/broker"
	"go.mindclade.dev/services/control_plane/internal/providers/durable"
)

// Delivery worker tuning. Concurrency is bounded because each delivery is an
// outbound request to a system with its own capacity, and the failure delay is
// longer than the scheduler's because a failing endpoint is usually failing for
// longer than a failing placement.
const (
	deliveryQueue             = "control-plane/webhook-delivery"
	deliveryWorker            = "webhook-delivery"
	deliveryPollInterval      = 500 * time.Millisecond
	deliveryLeaseDuration     = 120 * time.Second
	deliveryHeartbeatInterval = 30 * time.Second
	deliveryBatchSize         = 16
	deliveryConcurrency       = 8
	deliveryFailureDelay      = 30 * time.Second
)

// Egress policy. These are deliberately strict defaults rather than settings:
// a webhook target is a third-party endpoint, and every relaxation here is a
// way for a caller to reach something inside the fleet's own network.
const (
	egressTimeout          = 20 * time.Second
	egressDialTimeout      = 5 * time.Second
	egressResponseTimeout  = 10 * time.Second
	egressMaxResponseBytes = 1 << 20
	egressMaxRedirects     = 3
	egressMaxConnsPerHost  = 8
)

// WebhookFactory assembles the webhook dispatcher: the durable PostgreSQL
// mechanisms, the leased delivery queue, the signer that proves a payload came
// from this control plane, and the policy-bound client that carries it out.
type WebhookFactory struct {
	sources  []foundationconfig.Source
	delivery workqueue.Handler
}

// NewWebhookFactory returns the webhook-dispatcher provider factory. With no
// sources the process reads its configuration from the explicit environment
// mapping; tests pass a MapSource instead.
func NewWebhookFactory(sources ...foundationconfig.Source) *WebhookFactory {
	if len(sources) == 0 {
		sources = []foundationconfig.Source{config.EnvironmentSource()}
	}
	return &WebhookFactory{sources: sources}
}

// WithDeliveryHandler injects the domain handler that turns a claimed work
// item into a signed outbound request. It is the seam between this composition
// root and delivery policy: the root owns the queue, the fencing, the signer,
// and the egress bounds, and the domain owns what a webhook says.
//
// Left unset, the worker fails every item closed rather than acknowledging a
// delivery it cannot make. A webhook that reports success without being sent
// is indistinguishable, to the receiver, from one that was never queued.
func (factory *WebhookFactory) WithDeliveryHandler(handler workqueue.Handler) *WebhookFactory {
	if factory == nil {
		return nil
	}
	factory.delivery = handler
	return factory
}

// Create resolves configuration and constructs every provider the webhook
// dispatcher requires. Construction is ordered cheapest-first: configuration,
// pure mechanisms, and the egress policy all fail before a socket or a
// database connection is opened, and anything already opened is released if a
// later step fails.
func (factory *WebhookFactory) Create(ctx context.Context, profile bootstrap.Profile) (runtime bootstrap.Runtime, err error) {
	if factory == nil || ctx == nil {
		return bootstrap.Runtime{}, faults.New(
			faults.CodeInvalidArgument,
			"webhook-dispatcher factory requires a context",
			faults.WithReason("invalid_factory_request"),
			faults.WithOperation("controlplane.webhook.WebhookFactory.Create"),
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
	owner, err := deliveryOwner()
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	// The egress client is built before the database on purpose: an empty or
	// malformed allow-list is a deployment error, and finding it out before a
	// connection is opened keeps the failure cheap and unambiguous.
	client, err := newEgressClient(settings)
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
	queue, err := durable.NewWorkQueueStore(stores.DB)
	if err != nil {
		return bootstrap.Runtime{}, err
	}

	handler := factory.delivery
	if handler == nil {
		handler = workqueue.HandlerFunc(refuseDelivery)
	}
	worker, err := workqueue.NewWorker(
		queue,
		handler,
		workqueue.WorkerConfig{
			Owner:             owner,
			Queues:            []string{deliveryQueue},
			PollInterval:      deliveryPollInterval,
			LeaseDuration:     deliveryLeaseDuration,
			HeartbeatInterval: deliveryHeartbeatInterval,
			BatchSize:         deliveryBatchSize,
			Concurrency:       deliveryConcurrency,
			FailureDelay:      deliveryFailureDelay,
		},
		workqueue.WithClock(shared.Clock),
		workqueue.WithRetry(shared.Retry),
	)
	if err != nil {
		return bootstrap.Runtime{}, err
	}

	publisher, brokerLifecycle, err := broker.NewPublisher(settings, shared.Clock)
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
				// Lineage is carried, not decorative: a delivery work item
				// stores the request metadata of the request that caused the
				// event, so an outbound call stays traceable to its origin.
				RequestMetadataConfigured: true,
				Observability:             shared.Observability,
				Retry:                     shared.Retry,
			},
			// No migration runner: the dispatcher reads and writes tables the
			// registry role owns and creates.
			persistence.SQL{
				Postgres:     stores.Pool,
				Transactions: stores.Transactions,
			},
			// The signer is not optional here: it is what lets a receiver
			// distinguish a webhook from this control plane from anything else
			// that can reach its endpoint.
			governance.Controls{
				Audit:       recorder,
				Idempotency: records,
				Signer:      shared.Signer,
				Verifier:    shared.Verifier,
			},
			tasks.Mechanisms{
				Queue: queue,
				Workers: map[string]servicekit.Component{
					deliveryWorker: worker.Component("worker/" + deliveryWorker),
				},
			},
			egress.Client{
				Outbound: client,
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

// newEgressClient builds the only outbound HTTP client in the control plane.
//
// The allow-list is required rather than defaulted to open: a dispatcher that
// will call any host it is handed is a server-side request forgery primitive
// with a queue in front of it. Private address ranges stay refused for the
// same reason, and HTTPS is mandatory because a signed payload sent in the
// clear proves origin to anyone who can read it.
func newEgressClient(settings config.Settings) (*outbound.Client, error) {
	if len(settings.OutboundAllowedHosts) == 0 {
		return nil, faults.New(
			faults.CodeFailedPrecondition,
			"webhook egress allow-list is not configured",
			faults.WithReason("outbound_allowed_hosts_not_configured"),
			faults.WithOperation("controlplane.webhook.newEgressClient"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	return outbound.NewClient(outbound.Policy{
		AllowedHosts:          settings.OutboundAllowedHosts,
		HTTPSOnly:             true,
		AllowPrivateAddresses: false,
		Timeout:               egressTimeout,
		DialTimeout:           egressDialTimeout,
		ResponseHeaderTimeout: egressResponseTimeout,
		MaxResponseBytes:      egressMaxResponseBytes,
		MaxRedirects:          egressMaxRedirects,
		MaxConnsPerHost:       egressMaxConnsPerHost,
		UserAgent:             settings.ServiceName,
	})
}

// refuseDelivery is the default delivery handler. Webhook payload and target
// selection are domain policy, so until a handler is injected the worker fails
// items closed, which leaves the undelivered webhook visible.
//
// The fault carries NoRetry deliberately, and that is not the same thing as
// letting the item exhaust its attempts. The worker treats a non-retryable
// handler error as terminal on the spot, so the item is dead-lettered on its
// first attempt rather than re-leased until its budget is spent: retrying a
// delivery policy that is absent cannot make it present, and every retry
// would be another outbound attempt charged against a third-party endpoint.
func refuseDelivery(context.Context, workqueue.Item) (workqueue.Result, error) {
	return workqueue.Result{}, faults.New(
		faults.CodeNotImplemented,
		"webhook delivery policy is not configured",
		faults.WithReason("delivery_handler_not_configured"),
		faults.WithOperation("controlplane.webhook.refuseDelivery"),
		faults.WithRetryPolicy(faults.NoRetry()),
	)
}

// deliveryOwner identifies this process instance to the work queue. The
// hostname is the right identity under Kubernetes because a pod name is unique
// for the lifetime of the pod, which is exactly the lifetime of a claim.
func deliveryOwner() (string, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return "", faults.Wrap(err, faults.CodeInternal,
			"unable to determine the webhook delivery owner",
			faults.WithReason("hostname_unavailable"),
			faults.WithOperation("controlplane.webhook.deliveryOwner"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	return hostname, nil
}
