// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package bootstrap

import (
	"context"
	"time"

	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/servicekit"
	"go.mindclade.dev/libs/go/servicekit/production"
)

// Aggregate is one group of shared mechanisms a process composes. Splitting the
// substrate into aggregates is what keeps a command's import graph honest: this
// package names no concrete mechanism, so a binary links only the aggregates
// its own factory constructs rather than every subsystem some other role uses.
type Aggregate interface {
	Capabilities() []production.Capability
	ServiceOptions() []servicekit.Option
	Register(*production.Builder) error
}

// Lifecycle carries the process shutdown budgets a role resolved from its
// deployment configuration into the servicekit lifecycle that enforces them.
//
// It exists because the two budgets the control-plane configuration schema
// declares -- shutdown.timeout and drain.timeout -- previously reached nothing.
// Roles passed drain.timeout to their HTTP and gRPC servers, but servicekit
// bounds every component Stop hook with its own budget, and httpx.Server.Stop
// honours an inherited deadline ahead of its own; with servicekit left at its
// 10s package default, a deployment that configured a 20s drain got 10s and no
// error said so. shutdown.timeout reached nothing at all: it was parsed,
// cross-validated against drain.timeout, and then discarded, so raising it
// could not raise the budget it names.
//
// Both fields are optional. Zero keeps the servicekit package default, which is
// bounded in either case; it never means "no limit" here.
type Lifecycle struct {
	// ShutdownTimeout is the total graceful shutdown budget for the process. It
	// bounds drain, stop, and the wait for run loops together.
	ShutdownTimeout time.Duration

	// DrainTimeout is the budget for one component's Stop hook, which is where
	// a control-plane transport actually finishes in-flight requests: httpx and
	// grpcx expose graceful shutdown through Stop, not through Drain.
	DrainTimeout time.Duration
}

// options renders the configured budgets as servicekit options.
//
// ShutdownTimeout is omitted rather than passed as zero because servicekit
// reads a zero total budget as "disable the package timeout", which would make
// process shutdown unbounded. The per-component options treat zero as "bounded
// only by the total budget" instead, so omitting them is a defaulting choice
// rather than a safety one -- but it is still the right choice, because a
// component that inherited the whole budget could consume it and starve every
// component stopped after it.
//
// Only the Stop budget is set. Nothing in this service registers a Drain hook;
// the only one that runs is servicekit's own task-group drain, and leaving it
// on the 10s package default keeps a slow drain from spending the window the
// deployment allocated to finishing requests.
func (lifecycle Lifecycle) options() []servicekit.Option {
	options := make([]servicekit.Option, 0, 2)
	if lifecycle.ShutdownTimeout > 0 {
		options = append(options, servicekit.WithShutdownTimeout(lifecycle.ShutdownTimeout))
	}
	if lifecycle.DrainTimeout > 0 {
		options = append(options, servicekit.WithComponentStopTimeout(lifecycle.DrainTimeout))
	}
	return options
}

// validate refuses a pair of budgets that cannot both hold. A per-component
// budget longer than the whole shutdown budget is the exact shape of the defect
// this type exists to close: the process would advertise the longer window and
// silently enforce the shorter one.
//
// The bound is the same one config.Settings.Validate applies, deliberately: the
// configuration schema is the authority on what a deployment may ask for, and a
// second, stricter rule here would reject deployments that are valid by the
// contract they were written against. It does leave headroom as the operator's
// problem -- servicekit stops components in reverse order under one budget, so
// a drain budget close to the shutdown budget can starve the stops that follow
// the first slow one.
func (lifecycle Lifecycle) validate() error {
	if lifecycle.ShutdownTimeout < 0 || lifecycle.DrainTimeout < 0 {
		return invalidLifecycle("negative_lifecycle_budget")
	}
	if lifecycle.ShutdownTimeout > 0 && lifecycle.DrainTimeout > lifecycle.ShutdownTimeout {
		return invalidLifecycle("drain_budget_exceeds_shutdown_budget")
	}
	return nil
}

func invalidLifecycle(reason string) error {
	return faults.New(
		faults.CodeInvalidArgument,
		"invalid control-plane process shutdown budget",
		faults.WithReason(reason),
		faults.WithOperation("controlplane.bootstrap.Lifecycle.Validate"),
		faults.WithRetryPolicy(faults.NoRetry()),
	)
}

// Runtime is a complete process composition returned by a provider factory.
type Runtime struct {
	Dependencies []Aggregate
	Components   Components

	// Lifecycle carries the deployment-configured shutdown and drain budgets.
	// A factory that leaves it zero inherits the servicekit package defaults.
	Lifecycle Lifecycle

	// Bind, when set, receives the assembled production runtime after Build
	// and before Run. It exists for components that must observe process
	// health but are constructed before the service exists, such as the
	// liveness and readiness handler mounted into the HTTP transport. It must
	// not start work, register components, or retain the runtime beyond the
	// process lifetime.
	Bind func(*production.Runtime) error
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
	if err := runtime.Lifecycle.validate(); err != nil {
		return nil, err
	}
	options := make([]servicekit.Option, 0)
	for _, aggregate := range runtime.Dependencies {
		if aggregate != nil {
			options = append(options, aggregate.ServiceOptions()...)
		}
	}
	// Last, so the deployment-configured budgets win over anything an aggregate
	// set. Aggregates carry process-wide concerns such as the clock and the
	// telemetry observer; the shutdown budget belongs to the deployment.
	options = append(options, runtime.Lifecycle.options()...)
	builder, err := production.NewBuilder(profile.Name, profile.ProductionRole, options...)
	if err != nil {
		return nil, err
	}
	for _, aggregate := range runtime.Dependencies {
		if aggregate == nil {
			continue
		}
		if err := aggregate.Register(builder); err != nil {
			return nil, err
		}
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
	if runtime.Bind != nil {
		if err := runtime.Bind(service); err != nil {
			return err
		}
	}
	return service.RunWithSignals(ctx)
}
