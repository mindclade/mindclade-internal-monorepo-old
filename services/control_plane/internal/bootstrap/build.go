// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package bootstrap

import (
	"context"

	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/servicekit/production"
	"go.mindclade.dev/services/control_plane/internal/foundation"
)

// Runtime is a complete process composition returned by a provider factory.
type Runtime struct {
	Dependencies foundation.Dependencies
	Components   Components
}

// Factory creates concrete providers, repositories, domain engines, and
// adapters for one process profile.
type Factory interface {
	Create(context.Context, Profile) (Runtime, error)
}

// FactoryFunc adapts a function to Factory.
type FactoryFunc func(context.Context, Profile) (Runtime, error)

func (function FactoryFunc) Create(ctx context.Context, profile Profile) (Runtime, error) {
	if function == nil {
		return Runtime{}, faults.New(
			faults.CodeFailedPrecondition,
			"control-plane factory is not configured",
			faults.WithReason("nil_control_plane_factory"),
			faults.WithOperation("controlplane.bootstrap.FactoryFunc.Create"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	return function(ctx, profile)
}

// Build validates capabilities and assembles one process through the sole
// production lifecycle path: servicekit/production.Builder.
func Build(profile Profile, runtime Runtime) (*production.Runtime, error) {
	if err := profile.Validate(); err != nil {
		return nil, err
	}
	builder, err := production.NewBuilder(profile.Name, profile.ProductionRole, runtime.Dependencies.ServiceOptions()...)
	if err != nil {
		return nil, err
	}
	if err := runtime.Dependencies.Register(builder); err != nil {
		return nil, err
	}
	if err := runtime.Components.Register(builder); err != nil {
		return nil, err
	}
	return builder.Build()
}

// Execute creates, builds, and runs role through repository-standard signal
// handling, bounded drain, reverse-order shutdown, and immutable capability
// evidence.
func Execute(ctx context.Context, role Role, factory Factory) error {
	if ctx == nil || factory == nil {
		return faults.New(
			faults.CodeInvalidArgument,
			"control-plane bootstrap requires context and factory",
			faults.WithReason("invalid_bootstrap_request"),
			faults.WithOperation("controlplane.bootstrap.Execute"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	profile, err := ProfileFor(role)
	if err != nil {
		return err
	}
	runtime, err := factory.Create(ctx, profile)
	if err != nil {
		return err
	}
	service, err := Build(profile, runtime)
	if err != nil {
		return err
	}
	return service.RunWithSignals(ctx)
}
