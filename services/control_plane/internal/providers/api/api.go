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
	"go.mindclade.dev/libs/go/servicekit"
	"go.mindclade.dev/services/control_plane/internal/bootstrap"
	"go.mindclade.dev/services/control_plane/internal/config"
	"go.mindclade.dev/services/control_plane/internal/foundation"
	"go.mindclade.dev/services/control_plane/internal/foundation/eventing"
	"go.mindclade.dev/services/control_plane/internal/foundation/governance"
	"go.mindclade.dev/services/control_plane/internal/foundation/identity"
	"go.mindclade.dev/services/control_plane/internal/foundation/persistence"
	"go.mindclade.dev/services/control_plane/internal/providers"
	"go.mindclade.dev/services/control_plane/internal/providers/admissionmetrics"
	"go.mindclade.dev/services/control_plane/internal/providers/apikeys"
	"go.mindclade.dev/services/control_plane/internal/providers/durable"
	"go.mindclade.dev/services/control_plane/internal/providers/iapauth"
	admissionstore "go.mindclade.dev/services/control_plane/internal/store/postgres/admission"
)

// APIFactory assembles a request-serving control-plane process: the durable
// PostgreSQL mechanisms, the service-identity stack, and the inbound HTTP,
// Connect, and gRPC transports.
//
// The api and admin roles share one domain and transport composition rather
// than duplicating it. They remain separate processes because they are
// separately deployed and addressed -- an administrative surface reachable on
// the same endpoint as the public API cannot be firewalled off from it. The API
// profile alone adds the bounded admission-metrics slice consumed by its
// deployment monitoring; it does not broaden the admin process by accident.
type APIFactory struct {
	sources []foundationconfig.Source
	role    bootstrap.Role
}

// NewAPIFactory returns the API provider factory. With no sources the process
// reads its configuration from the explicit environment mapping; tests pass a
// MapSource instead.
func NewAPIFactory(sources ...foundationconfig.Source) *APIFactory {
	if len(sources) == 0 {
		sources = []foundationconfig.Source{config.EnvironmentSource()}
	}
	return &APIFactory{sources: sources, role: bootstrap.RoleAPI}
}

// NewAdminFactory returns the administrative provider factory. It shares the
// API domain, identity, and transport construction; profile-specific auxiliary
// components are still selected inside Create.
func NewAdminFactory(sources ...foundationconfig.Source) *APIFactory {
	if len(sources) == 0 {
		sources = []foundationconfig.Source{config.EnvironmentSource()}
	}
	return &APIFactory{sources: sources, role: bootstrap.RoleAdmin}
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
	if err := profile.Validate(); err != nil {
		return bootstrap.Runtime{}, err
	}
	if profile.Role != factory.role {
		return bootstrap.Runtime{}, faults.New(
			faults.CodeInvalidArgument,
			"request-serving factory role does not match the process profile",
			faults.WithReason("factory_profile_role_mismatch"),
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
	var authenticator auth.Authenticator
	bearerTokenHeader := ""
	if profile.Role == bootstrap.RoleAPI {
		authenticator, err = apikeys.NewAuthenticator(settings, shared.Clock)
	} else {
		authenticator, err = iapauth.NewAuthenticator(ctx, settings, shared.Clock)
		bearerTokenHeader = iapauth.HeaderName
	}
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
	admissions, err := admissionstore.New(stores.DB, recorder, stores.Outbox,
		admissionstore.WithClock(shared.Clock), admissionstore.WithGenerator(shared.IDs),
		admissionstore.WithRetry(shared.Retry))
	if err != nil {
		return bootstrap.Runtime{}, err
	}

	var admissionSurface admissionEngine
	var policySurface policyEngine
	if profile.Role == bootstrap.RoleAPI {
		admissionSurface = admission.Service{Repository: admissions, Clock: shared.Clock}
	} else {
		policySurface = admission.GovernanceService{
			Repository: admissions,
			IDs:        shared.IDs,
			Clock:      shared.Clock,
			Signer:     shared.Signer,
		}
	}
	var metrics *admissionmetrics.Runtime
	if profile.Role == bootstrap.RoleAPI {
		metrics, err = admissionmetrics.New(settings.MetricsAddress, settings.DrainTimeout)
		if err != nil {
			return bootstrap.Runtime{}, err
		}
		release = append(release, func() { _ = metrics.Close() })
	}

	inbound, err := newServing(settings, shared.Observability, authenticator, bearerTokenHeader, admissionSurface, policySurface, metrics)
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	inbound.components.Work = append(inbound.components.Work, admissions.Component("admission-schema"))
	if metrics != nil {
		inbound.components.Auxiliary = append(inbound.components.Auxiliary,
			bootstrap.StagedComponent{Stage: servicekit.StageServing, Component: metrics.Component()})
	}

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
