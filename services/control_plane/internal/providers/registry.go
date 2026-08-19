// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package providers

import (
	"context"

	"go.mindclade.dev/libs/go/auth"
	foundationconfig "go.mindclade.dev/libs/go/config"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/servicekit"
	"go.mindclade.dev/services/control_plane/internal/bootstrap"
	"go.mindclade.dev/services/control_plane/internal/config"
	"go.mindclade.dev/services/control_plane/internal/foundation"
)

// RegistryFactory assembles the control-plane registry process: the durable
// PostgreSQL mechanisms, the artifact object store, the read cache, and the
// inbound HTTP transport.
//
// It is the first materialized control-plane factory. The remaining roles
// still bootstrap through bootstrap.UnconfiguredFactory and fail closed.
type RegistryFactory struct {
	sources []foundationconfig.Source
}

// NewRegistryFactory returns the registry provider factory. With no sources
// the process reads its configuration from the explicit environment mapping;
// tests pass a MapSource instead.
func NewRegistryFactory(sources ...foundationconfig.Source) *RegistryFactory {
	if len(sources) == 0 {
		sources = []foundationconfig.Source{config.EnvironmentSource()}
	}
	return &RegistryFactory{sources: sources}
}

// Create resolves configuration and constructs every provider the registry
// role requires. Construction is ordered cheapest-first: configuration and
// pure mechanisms fail before any socket, connection, or cloud client is
// opened, and anything already opened is released if a later step fails.
func (factory *RegistryFactory) Create(ctx context.Context, profile bootstrap.Profile) (runtime bootstrap.Runtime, err error) {
	if factory == nil || ctx == nil {
		return bootstrap.Runtime{}, faults.New(
			faults.CodeInvalidArgument,
			"registry factory requires a context",
			faults.WithReason("invalid_factory_request"),
			faults.WithOperation("controlplane.providers.RegistryFactory.Create"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	resolved, err := config.Load(ctx, profile.Name, factory.sources...)
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	settings := resolved.Settings

	shared, err := newMechanisms(settings)
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	authenticator, err := newAuthenticator(settings, shared.clock)
	if err != nil {
		return bootstrap.Runtime{}, err
	}

	release := make([]func(), 0, 3)
	defer func() {
		if err == nil {
			return
		}
		for index := len(release) - 1; index >= 0; index-- {
			release[index]()
		}
	}()

	stores, err := newDatabase(settings, shared.clock, shared.ids)
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	release = append(release, func() { _ = stores.db.Close() })

	blobs, blobLifecycle, err := newBlobStore(ctx, settings)
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	release = append(release, func() { _ = blobLifecycle.Stop(ctx) })

	caches, cacheLifecycle, err := newCacheStore(settings)
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	release = append(release, func() { _ = cacheLifecycle.Stop(ctx) })

	inbound, err := newServing(settings, shared.observability, authenticator)
	if err != nil {
		return bootstrap.Runtime{}, err
	}

	components := inbound.components
	components.Auxiliary = append(components.Auxiliary,
		bootstrap.StagedComponent{Stage: servicekit.StageInfrastructure, Component: blobLifecycle},
		bootstrap.StagedComponent{Stage: servicekit.StageInfrastructure, Component: cacheLifecycle},
	)

	return bootstrap.Runtime{
		Dependencies: foundation.Dependencies{
			Clock:         shared.clock,
			Configuration: resolved.Current,
			IDs:           shared.ids,
			// Both flags are claims about wiring, not intent: the canonical
			// middleware stack installs request-metadata extraction and the
			// transport decodes conditional-request preconditions.
			RequestMetadataConfigured:  true,
			ResourceVersionsConfigured: true,
			Authenticator:              authenticator,
			Authorizer:                 auth.PermissionAuthorizer{},
			Audit:                      stores.audit,
			Idempotency:                stores.idempotency,
			Retry:                      shared.retry,
			Observability:              shared.observability,
			Signer:                     shared.signer,
			Verifier:                   shared.verifier,
			Pagination:                 shared.pagination,
			Postgres:                   stores.pool,
			Migrations:                 stores.migrations,
			Transactions:               stores.transactions,
			Blobs:                      blobs,
			Cache:                      caches,
			Outbox:                     stores.outbox,
		},
		Components: components,
		Bind:       inbound.bind,
	}, nil
}
