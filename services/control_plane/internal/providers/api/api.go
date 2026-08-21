// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package api is the composition root for the control-plane API role: the
// request-serving process that fronts the fleet.
//
// It is the first role to mount Connect and gRPC. The registry serves HTTP
// only, so every Connect and gRPC submodule in the foundation -- interceptors,
// health, reflection, telemetry, and transport credentials -- reaches a
// production consumer here for the first time.
//
// Domain procedures are registered by the generated API surface that owns
// them; this package assembles the transports, the identity stack, and the
// durable mechanisms they run on.
package api

import (
	"context"

	"go.mindclade.dev/control/admission"
	"go.mindclade.dev/libs/go/auth"
	foundationconfig "go.mindclade.dev/libs/go/config"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/services/control_plane/internal/bootstrap"
	"go.mindclade.dev/services/control_plane/internal/config"
	"go.mindclade.dev/services/control_plane/internal/foundation"
	"go.mindclade.dev/services/control_plane/internal/foundation/eventing"
	"go.mindclade.dev/services/control_plane/internal/foundation/governance"
	"go.mindclade.dev/services/control_plane/internal/foundation/identity"
	"go.mindclade.dev/services/control_plane/internal/foundation/persistence"
	"go.mindclade.dev/services/control_plane/internal/providers"
	"go.mindclade.dev/services/control_plane/internal/providers/apikeys"
	"go.mindclade.dev/services/control_plane/internal/providers/durable"
	admissionstore "go.mindclade.dev/services/control_plane/internal/store/postgres/admission"
)

// APIFactory assembles a request-serving control-plane process: the durable
// PostgreSQL mechanisms, the service-identity stack, and the inbound HTTP,
// Connect, and gRPC transports.
//
// The api and admin roles have identical capability profiles, so they are the
// same composition rather than two copies of it. They are separate processes
// because they are separately deployed and separately addressed -- an
// administrative surface reachable on the same endpoint as the public API is a
// surface that cannot be firewalled off from it -- and each reads its own
// listener addresses from its own environment.
type APIFactory struct {
	sources []foundationconfig.Source
}

// NewAPIFactory returns the API provider factory. With no sources the process
// reads its configuration from the explicit environment mapping; tests pass a
// MapSource instead.
func NewAPIFactory(sources ...foundationconfig.Source) *APIFactory {
	if len(sources) == 0 {
		sources = []foundationconfig.Source{config.EnvironmentSource()}
	}
	return &APIFactory{sources: sources}
}

// NewAdminFactory returns the administrative provider factory. It is the API
// composition: the admin role requires exactly the same capabilities, and
// giving it a second implementation would mean two places to keep the identity
// and transport stacks correct.
func NewAdminFactory(sources ...foundationconfig.Source) *APIFactory {
	return NewAPIFactory(sources...)
}

// Create resolves configuration and constructs every provider the API role
// requires. Construction is ordered cheapest-first: configuration, pure
// mechanisms, and the credential registry all fail before a socket or a
// database connection is opened, and anything already opened is released if a
// later step fails.
func (factory *APIFactory) Create(ctx context.Context, profile bootstrap.Profile) (runtime bootstrap.Runtime, err error) {
	if factory == nil || ctx == nil {
		return bootstrap.Runtime{}, faults.New(
			faults.CodeInvalidArgument,
			"request-serving factory requires a context",
			faults.WithReason("invalid_factory_request"),
			faults.WithOperation("controlplane.api.APIFactory.Create"),
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
	authenticator, err := apikeys.NewAuthenticator(settings, shared.Clock)
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
	records, err := durable.NewIdempotencyStore(stores.DB, shared.Clock, shared.IDs)
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	admissions, err := admissionstore.New(stores.DB, recorder, stores.Outbox,
		admissionstore.WithClock(shared.Clock), admissionstore.WithGenerator(shared.IDs),
		admissionstore.WithRetry(shared.Retry))
	if err != nil {
		return bootstrap.Runtime{}, err
	}

	inbound, err := newServing(settings, shared.Observability, authenticator, admission.Service{
		Repository: admissions,
		Clock:      shared.Clock,
	})
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	inbound.components.Work = append(inbound.components.Work, admissions.Component("admission-schema"))

	return bootstrap.Runtime{
		// The aggregate list is the role's capability profile, written out.
		// Anything absent here is a package this binary does not link.
		Dependencies: []bootstrap.Aggregate{
			foundation.Core{
				Clock:         shared.Clock,
				Configuration: resolved.Current,
				IDs:           shared.IDs,
				// A claim about wiring, not intent: the HTTP middleware stack
				// and the Connect and gRPC interceptor chains all extract
				// request metadata, so lineage is established on every surface
				// this process serves.
				RequestMetadataConfigured: true,
				Observability:             shared.Observability,
				Retry:                     shared.Retry,
			},
			// No migration runner: the API reads and writes tables the
			// registry role owns and creates. Two runners against one database
			// would race for the same version ordering.
			persistence.SQL{
				Postgres:     stores.Pool,
				Transactions: stores.Transactions,
			},
			governance.Controls{
				Audit:       recorder,
				Idempotency: records,
				Signer:      shared.Signer,
				Verifier:    shared.Verifier,
				Pagination:  shared.Pagination,
				// Every transport decodes conditional-request preconditions:
				// HTTP through the precondition middleware, Connect and gRPC
				// through request metadata.
				ResourceVersionsConfigured: true,
			},
			identity.Controls{
				Authenticator: authenticator,
				Authorizer:    auth.PermissionAuthorizer{},
			},
			// The outbox only. The API writes events inside request
			// transactions and never publishes directly; the dispatcher drains
			// what it wrote.
			eventing.Mechanisms{
				Outbox: stores.Outbox,
			},
		},
		Components: inbound.components,
		Bind:       inbound.bind,
	}, nil
}
