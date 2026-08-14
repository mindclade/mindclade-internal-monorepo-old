// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package production

import (
	"sync"

	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/servicekit"
)

// Builder is the only supported production composition path for Go processes.
//
// It records capabilities from successfully constructed dependencies, assigns
// lifecycle-owning mechanisms to their canonical servicekit stage, accepts
// domain work as explicit components, validates the final role profile, and
// freezes the resulting runtime. Builder owns no provider construction and no
// domain policy.
type Builder struct {
	mu           sync.Mutex
	service      string
	role         Role
	assembly     *servicekit.Assembly
	capabilities map[Capability]struct{}
	built        bool
}

// NewBuilder creates an empty production process assembly. Service names and
// roles are validated immediately so misconfigured binaries fail before they
// create listeners, database pools, or background goroutines.
func NewBuilder(service string, role Role, options ...servicekit.Option) (*Builder, error) {
	if !role.Valid() {
		return nil, invalid("invalid_production_role", service, role, []string{"valid_role"})
	}
	assembly, err := servicekit.NewAssembly(service, options...)
	if err != nil {
		return nil, err
	}
	return &Builder{
		service:      service,
		role:         role,
		assembly:     assembly,
		capabilities: map[Capability]struct{}{CapabilityServiceLifecycle: {}},
	}, nil
}

// Declare records a passive capability such as authentication, authorization,
// audit, idempotency, or retry. It may also be used for a capability whose
// lifecycle is owned by another registered component (for example Connect
// handlers served by the HTTP component).
func (builder *Builder) Declare(capability Capability) error {
	if builder == nil {
		return builderFault("nil_production_builder", "production builder must not be nil", "servicekit.production.Builder.Declare", "", capability, nil)
	}
	if !capability.Valid() {
		return builderFault("invalid_production_capability", "invalid production capability", "servicekit.production.Builder.Declare", builder.service, capability, nil)
	}
	if capability.Active() {
		return builderFault("active_capability_requires_component", "active production capability requires a lifecycle component", "servicekit.production.Builder.Declare", builder.service, capability, nil)
	}
	builder.mu.Lock()
	defer builder.mu.Unlock()
	if builder.built {
		return builderFault("production_builder_frozen", "production builder is frozen", "servicekit.production.Builder.Declare", builder.service, capability, nil)
	}
	builder.capabilities[capability] = struct{}{}
	return nil
}

// AddCapability registers a lifecycle-owning mechanism at its canonical stage
// and records its capability. Passive capabilities must use Declare.
func (builder *Builder) AddCapability(capability Capability, component servicekit.Component) error {
	if builder == nil {
		return builderFault("nil_production_builder", "production builder must not be nil", "servicekit.production.Builder.AddCapability", "", capability, nil)
	}
	stage, ok := StageFor(capability)
	if !ok {
		return builderFault("passive_capability_has_component", "passive production capability must be declared without a component", "servicekit.production.Builder.AddCapability", builder.service, capability, nil)
	}
	builder.mu.Lock()
	defer builder.mu.Unlock()
	if builder.built {
		return builderFault("production_builder_frozen", "production builder is frozen", "servicekit.production.Builder.AddCapability", builder.service, capability, nil)
	}
	if err := builder.assembly.Add(stage, component); err != nil {
		return err
	}
	builder.capabilities[capability] = struct{}{}
	return nil
}

// AddComponent registers an auxiliary or domain component at an explicit stage
// without claiming a production capability. Typical uses are migrations in the
// infrastructure stage and controllers, schedulers, projectors, or API engines
// in the work stage. Provider mechanisms should prefer AddCapability.
func (builder *Builder) AddComponent(stage servicekit.Stage, component servicekit.Component) error {
	if builder == nil {
		return builderFault("nil_production_builder", "production builder must not be nil", "servicekit.production.Builder.AddComponent", "", "", nil)
	}
	builder.mu.Lock()
	defer builder.mu.Unlock()
	if builder.built {
		return builderFault("production_builder_frozen", "production builder is frozen", "servicekit.production.Builder.AddComponent", builder.service, "", nil)
	}
	return builder.assembly.Add(stage, component)
}

// AddWork is the canonical shorthand for domain engines and long-running
// process loops. Reusable libraries remain mechanisms; consumers own this work.
func (builder *Builder) AddWork(component servicekit.Component) error {
	return builder.AddComponent(servicekit.StageWork, component)
}

// Build validates the actual wired capability inventory, freezes the builder,
// and returns the immutable runtime. A failed validation does not freeze the
// builder, allowing a composition root to report and correct missing wiring in
// tests before retrying Build.
func (builder *Builder) Build() (*Runtime, error) {
	if builder == nil {
		return nil, builderFault("nil_production_builder", "production builder must not be nil", "servicekit.production.Builder.Build", "", "", nil)
	}
	builder.mu.Lock()
	if builder.built {
		builder.mu.Unlock()
		return nil, builderFault("production_builder_frozen", "production builder has already been built", "servicekit.production.Builder.Build", builder.service, "", nil)
	}
	capabilities := sortedCapabilities(builder.capabilities)
	manifest, err := NewManifest(builder.service, builder.role, capabilities...)
	if err != nil {
		builder.mu.Unlock()
		return nil, err
	}
	entries := builder.assembly.Entries()
	if err := validateRuntimePlan(builder.service, builder.role, entries); err != nil {
		builder.mu.Unlock()
		return nil, err
	}
	service, err := builder.assembly.Build()
	if err != nil {
		builder.mu.Unlock()
		return nil, err
	}
	builder.built = true
	builder.mu.Unlock()
	return newRuntime(manifest, service, entries), nil
}

// StageFor returns the canonical lifecycle stage for a capability that owns a
// component. Passive capabilities return ok=false.
func StageFor(capability Capability) (stage servicekit.Stage, ok bool) {
	switch capability {
	case CapabilityObservability:
		return servicekit.StageFoundation, true
	case CapabilityDatabase, CapabilityMigrations:
		return servicekit.StageInfrastructure, true
	case CapabilityKubernetesManager, CapabilityProjector, CapabilityWorkQueueWorker:
		return servicekit.StageWork, true
	case CapabilityOutboxDispatcher, CapabilityLeadership:
		return servicekit.StageCoordination, true
	case CapabilityHTTP, CapabilityGRPC:
		return servicekit.StageServing, true
	default:
		return 0, false
	}
}

func builderFault(reason, message, operation, service string, capability any, fields faults.Fields) error {
	merged := faults.Fields{"service": service}
	if capability != nil && capability != "" {
		merged["capability"] = capability
	}
	for key, value := range fields {
		merged[key] = value
	}
	return faults.New(
		faults.CodeFailedPrecondition,
		message,
		faults.WithReason(reason),
		faults.WithOperation(operation),
		faults.WithFields(merged),
		faults.WithRetryPolicy(faults.NoRetry()),
	)
}
