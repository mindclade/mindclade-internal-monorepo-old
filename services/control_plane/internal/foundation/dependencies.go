// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package foundation defines the typed process-level dependency aggregate used
// by Go control-plane composition roots. It is intentionally outside libs/go:
// these fields select and compose reusable mechanisms for concrete processes,
// while libs/go remains free of service and domain policy.
package foundation

import (
	"reflect"
	"sort"

	"go.mindclade.dev/libs/go/audit"
	"go.mindclade.dev/libs/go/auth"
	mcclock "go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/config"
	"go.mindclade.dev/libs/go/coordination/cursor"
	"go.mindclade.dev/libs/go/coordination/inbox"
	"go.mindclade.dev/libs/go/coordination/leadership"
	"go.mindclade.dev/libs/go/coordination/outbox"
	"go.mindclade.dev/libs/go/coordination/projector"
	"go.mindclade.dev/libs/go/coordination/workqueue"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/httpx/outbound"
	"go.mindclade.dev/libs/go/idempotency"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/messaging"
	"go.mindclade.dev/libs/go/observability"
	"go.mindclade.dev/libs/go/pagination"
	"go.mindclade.dev/libs/go/retry"
	"go.mindclade.dev/libs/go/servicekit"
	"go.mindclade.dev/libs/go/servicekit/production"
	"go.mindclade.dev/libs/go/signing"
	"go.mindclade.dev/libs/go/storage/blob"
	"go.mindclade.dev/libs/go/storage/cache"
	"go.mindclade.dev/libs/go/storage/lease"
	"go.mindclade.dev/libs/go/storage/sql/migrate"
	sqlpostgres "go.mindclade.dev/libs/go/storage/sql/postgres"
	"go.mindclade.dev/libs/go/storage/sql/transaction"
)

// Dependencies is the shared typed substrate from which one Go process is
// assembled. It contains mechanisms and provider adapters, not domain
// repositories, generated handlers, route tables, or business services.
type Dependencies struct {
	Clock                      mcclock.Clock
	Configuration              *config.Atomic
	IDs                        *identifiers.Generator
	RequestMetadataConfigured  bool
	ResourceVersionsConfigured bool
	Authenticator              auth.Authenticator
	Authorizer                 auth.Authorizer
	Audit                      audit.Recorder
	Idempotency                idempotency.Store
	Retry                      *retry.Executor
	Observability              *observability.Runtime
	Signer                     signing.Signer
	Verifier                   signing.Verifier
	Pagination                 *pagination.Codec
	MessagingPublisher         messaging.Publisher
	MessagingSubscription      messaging.Subscription
	OutboundHTTP               *outbound.Client

	Postgres     *sqlpostgres.Pool
	Migrations   *migrate.Runner
	Transactions transaction.Beginner
	Blobs        blob.Store
	Cache        cache.Store
	Leases       lease.Store

	Cursors cursor.Store
	Inbox   *inbox.Processor
	Outbox  outbox.Store
	Work    workqueue.Store

	Leader     *leadership.Elector
	Dispatcher *outbox.Dispatcher
	Projectors map[string]*projector.Processor
	Workers    map[string]*workqueue.Worker
}

// Capabilities reports the deterministic production mechanisms represented by
// Dependencies. It is used by diagnostics and tests; Register is authoritative
// for lifecycle staging.
func (dependencies Dependencies) Capabilities() []production.Capability {
	values := make(map[production.Capability]struct{})
	add := func(capability production.Capability, present bool) {
		if present {
			values[capability] = struct{}{}
		}
	}
	add(production.CapabilityClock, !nilInterface(dependencies.Clock))
	add(production.CapabilityConfiguration, dependencies.Configuration != nil)
	add(production.CapabilityIdentifiers, dependencies.IDs != nil)
	add(production.CapabilityRequestMetadata, dependencies.RequestMetadataConfigured)
	add(production.CapabilityResourceVersion, dependencies.ResourceVersionsConfigured)
	add(production.CapabilitySigning, !nilInterface(dependencies.Signer) && !nilInterface(dependencies.Verifier))
	add(production.CapabilityPagination, dependencies.Pagination != nil)
	add(production.CapabilityMessaging, !nilInterface(dependencies.MessagingPublisher) || !nilInterface(dependencies.MessagingSubscription))
	add(production.CapabilityOutboundHTTP, dependencies.OutboundHTTP != nil)
	add(production.CapabilityAuthentication, !nilInterface(dependencies.Authenticator))
	add(production.CapabilityAuthorization, !nilInterface(dependencies.Authorizer))
	add(production.CapabilityAudit, !nilInterface(dependencies.Audit))
	add(production.CapabilityIdempotency, !nilInterface(dependencies.Idempotency))
	add(production.CapabilityRetry, dependencies.Retry != nil)
	add(production.CapabilityObservability, dependencies.Observability != nil)
	add(production.CapabilityDatabase, dependencies.Postgres != nil)
	add(production.CapabilityMigrations, dependencies.Migrations != nil && dependencies.Postgres != nil)
	add(production.CapabilityTransactions, !nilInterface(dependencies.Transactions) || dependencies.Postgres != nil)
	add(production.CapabilityBlobStore, !nilInterface(dependencies.Blobs))
	add(production.CapabilityCache, !nilInterface(dependencies.Cache))
	add(production.CapabilityLeaseStore, !nilInterface(dependencies.Leases))
	add(production.CapabilityCursorStore, !nilInterface(dependencies.Cursors))
	add(production.CapabilityInboxProcessor, dependencies.Inbox != nil)
	add(production.CapabilityOutboxStore, !nilInterface(dependencies.Outbox))
	add(production.CapabilityWorkQueueStore, !nilInterface(dependencies.Work))
	add(production.CapabilityLeadership, dependencies.Leader != nil)
	add(production.CapabilityOutboxDispatcher, dependencies.Dispatcher != nil)
	add(production.CapabilityProjector, nonNilProjectorCount(dependencies.Projectors) > 0)
	add(production.CapabilityWorkQueueWorker, nonNilWorkerCount(dependencies.Workers) > 0)

	result := make([]production.Capability, 0, len(values))
	for capability := range values {
		result = append(result, capability)
	}
	sort.Slice(result, func(left, right int) bool { return result[left] < result[right] })
	return result
}

// Validate checks that every requested capability is represented by a concrete
// dependency. The production Builder performs the final role validation.
func (dependencies Dependencies) Validate(required ...production.Capability) error {
	available := make(map[production.Capability]struct{})
	for _, capability := range dependencies.Capabilities() {
		available[capability] = struct{}{}
	}
	missing := make([]string, 0)
	for _, capability := range required {
		if capability == "" {
			continue
		}
		if _, ok := available[capability]; !ok {
			missing = append(missing, capability.String())
		}
	}
	if len(missing) == 0 {
		return nil
	}
	sort.Strings(missing)
	return faults.New(
		faults.CodeFailedPrecondition,
		"control-plane process dependencies are incomplete",
		faults.WithReason("missing_process_capabilities"),
		faults.WithOperation("controlplane.foundation.Dependencies.Validate"),
		faults.WithField("missing_capabilities", missing),
		faults.WithRetryPolicy(faults.NoRetry()),
	)
}

// ServiceOptions returns the canonical process-wide servicekit options.
func (dependencies Dependencies) ServiceOptions() []servicekit.Option {
	options := make([]servicekit.Option, 0, 2)
	if !nilInterface(dependencies.Clock) {
		options = append(options, servicekit.WithClock(dependencies.Clock))
	}
	if dependencies.Observability != nil {
		options = append(options, servicekit.WithObserver(observability.NewServiceObserver(dependencies.Observability)))
	}
	return options
}

// Register contributes every concrete shared mechanism to builder. Passive
// stores are declared only after construction; active loops and lifecycle-owned
// adapters are registered with their canonical servicekit stages.
func (dependencies Dependencies) Register(builder *production.Builder) error {
	if builder == nil {
		return faults.New(
			faults.CodeInvalidArgument,
			"production builder is required",
			faults.WithReason("nil_production_builder"),
			faults.WithOperation("controlplane.foundation.Dependencies.Register"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	declare := func(capability production.Capability, present bool) error {
		if !present {
			return nil
		}
		return builder.Declare(capability)
	}
	for _, declaration := range []struct {
		capability production.Capability
		present    bool
	}{
		{production.CapabilityClock, !nilInterface(dependencies.Clock)},
		{production.CapabilityConfiguration, dependencies.Configuration != nil},
		{production.CapabilityIdentifiers, dependencies.IDs != nil},
		{production.CapabilityRequestMetadata, dependencies.RequestMetadataConfigured},
		{production.CapabilityResourceVersion, dependencies.ResourceVersionsConfigured},
		{production.CapabilitySigning, !nilInterface(dependencies.Signer) && !nilInterface(dependencies.Verifier)},
		{production.CapabilityPagination, dependencies.Pagination != nil},
		{production.CapabilityMessaging, !nilInterface(dependencies.MessagingPublisher) || !nilInterface(dependencies.MessagingSubscription)},
		{production.CapabilityOutboundHTTP, dependencies.OutboundHTTP != nil},
		{production.CapabilityAuthentication, !nilInterface(dependencies.Authenticator)},
		{production.CapabilityAuthorization, !nilInterface(dependencies.Authorizer)},
		{production.CapabilityAudit, !nilInterface(dependencies.Audit)},
		{production.CapabilityIdempotency, !nilInterface(dependencies.Idempotency)},
		{production.CapabilityRetry, dependencies.Retry != nil},
		{production.CapabilityTransactions, !nilInterface(dependencies.Transactions) || dependencies.Postgres != nil},
		{production.CapabilityBlobStore, !nilInterface(dependencies.Blobs)},
		{production.CapabilityCache, !nilInterface(dependencies.Cache)},
		{production.CapabilityLeaseStore, !nilInterface(dependencies.Leases)},
		{production.CapabilityCursorStore, !nilInterface(dependencies.Cursors)},
		{production.CapabilityInboxProcessor, dependencies.Inbox != nil},
		{production.CapabilityOutboxStore, !nilInterface(dependencies.Outbox)},
		{production.CapabilityWorkQueueStore, !nilInterface(dependencies.Work)},
	} {
		if err := declare(declaration.capability, declaration.present); err != nil {
			return err
		}
	}

	if dependencies.Observability != nil {
		if err := builder.AddCapability(production.CapabilityObservability, dependencies.Observability.ServiceComponent("observability")); err != nil {
			return err
		}
	}
	if dependencies.Postgres != nil {
		if err := builder.AddCapability(production.CapabilityDatabase, dependencies.Postgres.Component("postgres")); err != nil {
			return err
		}
	}
	if dependencies.Migrations != nil {
		if dependencies.Postgres == nil || dependencies.Postgres.DB() == nil {
			return faults.New(
				faults.CodeFailedPrecondition,
				"migration runner requires a configured PostgreSQL pool",
				faults.WithReason("migrations_without_database"),
				faults.WithOperation("controlplane.foundation.Dependencies.Register"),
				faults.WithRetryPolicy(faults.NoRetry()),
			)
		}
		if err := builder.AddCapability(production.CapabilityMigrations, dependencies.Migrations.Component("postgres-migrations", dependencies.Postgres.DB())); err != nil {
			return err
		}
	}
	if dependencies.Leader != nil {
		if err := builder.AddCapability(production.CapabilityLeadership, dependencies.Leader.Component("leadership")); err != nil {
			return err
		}
	}
	if dependencies.Dispatcher != nil {
		if err := builder.AddCapability(production.CapabilityOutboxDispatcher, dependencies.Dispatcher.Component("outbox-dispatcher")); err != nil {
			return err
		}
	}
	for _, name := range sortedKeys(dependencies.Projectors) {
		if value := dependencies.Projectors[name]; value != nil {
			if err := builder.AddCapability(production.CapabilityProjector, value.Component("projector/"+name)); err != nil {
				return err
			}
		}
	}
	for _, name := range sortedKeys(dependencies.Workers) {
		if value := dependencies.Workers[name]; value != nil {
			if err := builder.AddCapability(production.CapabilityWorkQueueWorker, value.Component("worker/"+name)); err != nil {
				return err
			}
		}
	}
	return nil
}

func nonNilProjectorCount(values map[string]*projector.Processor) int {
	count := 0
	for _, value := range values {
		if value != nil {
			count++
		}
	}
	return count
}

func nonNilWorkerCount(values map[string]*workqueue.Worker) int {
	count := 0
	for _, value := range values {
		if value != nil {
			count++
		}
	}
	return count
}

func sortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func nilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
