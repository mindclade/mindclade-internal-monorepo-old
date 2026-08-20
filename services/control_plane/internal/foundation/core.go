// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package foundation

import (
	"reflect"
	"sort"

	mcclock "go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/config"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/observability"
	"go.mindclade.dev/libs/go/retry"
	"go.mindclade.dev/libs/go/servicekit"
	"go.mindclade.dev/libs/go/servicekit/production"
)

// Core is the substrate every control-plane role needs regardless of what it
// does: time, configuration, identity generation, request lineage, telemetry,
// and the retry executor. No role can opt out, so nothing here is optional.
type Core struct {
	Clock                     mcclock.Clock
	Configuration             *config.Atomic
	IDs                       *identifiers.Generator
	RequestMetadataConfigured bool
	Observability             *observability.Runtime
	Retry                     *retry.Executor
}

func (core Core) declarations() []Declaration {
	return []Declaration{
		{Capability: production.CapabilityClock, Present: !IsNil(core.Clock)},
		{Capability: production.CapabilityConfiguration, Present: core.Configuration != nil},
		{Capability: production.CapabilityIdentifiers, Present: core.IDs != nil},
		{Capability: production.CapabilityRequestMetadata, Present: core.RequestMetadataConfigured},
		{Capability: production.CapabilityRetry, Present: core.Retry != nil},
		{
			Capability: production.CapabilityObservability,
			Present:    core.Observability != nil,
			Component:  componentOf(core.Observability, "observability"),
		},
	}
}

func (core Core) Capabilities() []production.Capability { return Present(core.declarations()) }

// ServiceOptions returns the process-wide servicekit options. Only Core carries
// any: the clock and the telemetry observer are properties of the process, not
// of a subsystem.
func (core Core) ServiceOptions() []servicekit.Option {
	options := make([]servicekit.Option, 0, 2)
	if !IsNil(core.Clock) {
		options = append(options, servicekit.WithClock(core.Clock))
	}
	if core.Observability != nil {
		options = append(options, servicekit.WithObserver(observability.NewServiceObserver(core.Observability)))
	}
	return options
}

func (core Core) Register(builder *production.Builder) error {
	return Register(builder, core.declarations())
}

func componentOf(runtime *observability.Runtime, name string) *servicekit.Component {
	if runtime == nil {
		return nil
	}
	component := runtime.ServiceComponent(name)
	return &component
}

// Declaration binds one capability to whether this process actually provides
// it, and to the lifecycle component that owns it when one exists. A nil
// Component means the capability is passive: something else owns the lifetime.
type Declaration struct {
	Capability production.Capability
	Present    bool
	Component  *servicekit.Component
}

// Present returns the deduplicated capabilities the declarations provide.
func Present(declarations []Declaration) []production.Capability {
	seen := make(map[production.Capability]struct{})
	for _, declaration := range declarations {
		if declaration.Present && declaration.Capability != "" {
			seen[declaration.Capability] = struct{}{}
		}
	}
	result := make([]production.Capability, 0, len(seen))
	for capability := range seen {
		result = append(result, capability)
	}
	sort.Slice(result, func(left, right int) bool { return result[left] < result[right] })
	return result
}

// Register contributes the present declarations to builder. One list drives
// both Capabilities and Register so the two can never disagree about what a
// process provides.
func Register(builder *production.Builder, declarations []Declaration) error {
	if builder == nil {
		return faults.New(
			faults.CodeInvalidArgument,
			"production builder is required",
			faults.WithReason("nil_production_builder"),
			faults.WithOperation("controlplane.foundation.Register"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	for _, declaration := range declarations {
		if !declaration.Present {
			continue
		}
		if declaration.Component == nil {
			if err := builder.Declare(declaration.Capability); err != nil {
				return err
			}
			continue
		}
		if err := builder.AddCapability(declaration.Capability, *declaration.Component); err != nil {
			return err
		}
	}
	return nil
}

// IsNil reports whether value is nil, including a typed nil held in an
// interface. Aggregates use it because a nil provider stored in a contract
// field is not the same as an absent capability.
func IsNil(value any) bool {
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

// SortedKeys returns map keys in stable order so component registration is
// deterministic across processes.
func SortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
