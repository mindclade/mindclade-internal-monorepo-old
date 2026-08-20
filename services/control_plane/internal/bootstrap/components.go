// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package bootstrap

import (
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/servicekit"
	"go.mindclade.dev/libs/go/servicekit/production"
)

// Mechanism binds one lifecycle-owning production capability to its canonical
// servicekit component. Examples include an HTTP server, gRPC server,
// Kubernetes manager, or provider component constructed by the service.
type Mechanism struct {
	Capability production.Capability
	Component  servicekit.Component
}

// StagedComponent is auxiliary lifecycle work that intentionally does not
// claim a reusable capability, such as a migration runner or cache warmer.
type StagedComponent struct {
	Stage     servicekit.Stage
	Component servicekit.Component
}

// Components contains adapters and domain engines owned by the concrete
// process rather than by the shared foundation dependency aggregate.
type Components struct {
	// Passive capabilities are declared only after construction. Typical
	// examples are Connect handlers mounted into HTTP, Kubernetes clients, or
	// provider stores whose lifecycle is owned by another component.
	Passive []production.Capability

	// Mechanisms own lifecycle components and therefore use the canonical stage
	// selected by servicekit/production.
	Mechanisms []Mechanism

	// Auxiliary contains lifecycle components that do not satisfy a reusable
	// role capability.
	Auxiliary []StagedComponent

	// Work contains service-specific controllers, schedulers, coordinators, API
	// engines, and administrative processors.
	Work []servicekit.Component
}

// Register adds process-owned mechanisms and work to builder.
func (components Components) Register(builder *production.Builder) error {
	if builder == nil {
		return faults.New(
			faults.CodeInvalidArgument,
			"production builder must not be nil",
			faults.WithReason("nil_production_builder"),
			faults.WithOperation("controlplane.bootstrap.Components.Register"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	for _, capability := range components.Passive {
		if err := builder.Declare(capability); err != nil {
			return err
		}
	}
	for _, mechanism := range components.Mechanisms {
		if err := builder.AddCapability(mechanism.Capability, mechanism.Component); err != nil {
			return err
		}
	}
	for _, auxiliary := range components.Auxiliary {
		if err := builder.AddComponent(auxiliary.Stage, auxiliary.Component); err != nil {
			return err
		}
	}
	for _, component := range components.Work {
		if err := builder.AddWork(component); err != nil {
			return err
		}
	}
	return nil
}
