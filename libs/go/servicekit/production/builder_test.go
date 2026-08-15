// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package production

import (
	"context"
	"reflect"
	"testing"

	"mindclade.internal/libs/go/servicekit"
)

func component(name string) servicekit.Component {
	return servicekit.Component{Name: name, Start: func(context.Context) error { return nil }}
}

func declareBase(t *testing.T, builder *Builder) {
	t.Helper()
	for _, capability := range []Capability{
		CapabilityClock,
		CapabilityConfiguration,
		CapabilityIdentifiers,
		CapabilityRequestMetadata,
		CapabilityRetry,
	} {
		if err := builder.Declare(capability); err != nil {
			t.Fatal(err)
		}
	}
	if err := builder.AddCapability(CapabilityObservability, component("telemetry")); err != nil {
		t.Fatal(err)
	}
}

func TestBuilderInfersStagesAndValidatesActualCapabilities(t *testing.T) {
	builder, err := NewBuilder("scheduler", RoleScheduler)
	if err != nil {
		t.Fatal(err)
	}
	declareBase(t, builder)
	for _, capability := range []Capability{
		CapabilityAudit,
		CapabilityIdempotency,
		CapabilityTransactions,
		CapabilityLeaseStore,
		CapabilityKubernetes,
		CapabilityWorkQueueStore,
		CapabilityMessaging,
		CapabilityResourceVersion,
		CapabilitySigning,
		CapabilityOutboxStore,
	} {
		if err := builder.Declare(capability); err != nil {
			t.Fatal(err)
		}
	}
	for _, registration := range []struct {
		capability Capability
		name       string
	}{
		{CapabilityDatabase, "postgres"},
		{CapabilityLeadership, "leadership"},
	} {
		if err := builder.AddCapability(registration.capability, component(registration.name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := builder.AddWork(component("scheduler-engine")); err != nil {
		t.Fatal(err)
	}
	runtime, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	want := []servicekit.AssemblyEntry{
		{Stage: servicekit.StageFoundation, Component: "telemetry"},
		{Stage: servicekit.StageInfrastructure, Component: "postgres"},
		{Stage: servicekit.StageCoordination, Component: "leadership"},
		{Stage: servicekit.StageWork, Component: "scheduler-engine"},
	}
	entries := runtime.Entries()
	if !reflect.DeepEqual(entries, want) {
		t.Fatalf("entries=%v want=%v", entries, want)
	}
	if runtime.Manifest().Role() != RoleScheduler {
		t.Fatalf("role=%v", runtime.Manifest().Role())
	}
}

func TestBuilderRejectsMissingRoleMechanismThenCanRecover(t *testing.T) {
	builder, err := NewBuilder("projector", RoleEventProjector)
	if err != nil {
		t.Fatal(err)
	}
	declareBase(t, builder)
	if _, err := builder.Build(); err == nil {
		t.Fatal("incomplete role accepted")
	}
	if err := builder.AddCapability(CapabilityDatabase, component("database")); err != nil {
		t.Fatal(err)
	}
	for _, capability := range []Capability{
		CapabilityTransactions,
		CapabilityIdempotency,
		CapabilityLeaseStore,
		CapabilityInboxProcessor,
		CapabilityCursorStore,
		CapabilityMessaging,
	} {
		if err := builder.Declare(capability); err != nil {
			t.Fatal(err)
		}
	}
	if err := builder.AddCapability(CapabilityLeadership, component("leadership")); err != nil {
		t.Fatal(err)
	}
	if err := builder.AddCapability(CapabilityProjector, component("projection")); err != nil {
		t.Fatal(err)
	}
	if _, err := builder.Build(); err != nil {
		t.Fatal(err)
	}
}

func TestBuilderRejectsPassiveCapabilityComponent(t *testing.T) {
	builder, err := NewBuilder("api", RoleAPI)
	if err != nil {
		t.Fatal(err)
	}
	if err := builder.AddCapability(CapabilityAuthentication, component("auth")); err == nil {
		t.Fatal("passive capability component accepted")
	}
	if err := builder.AddCapability(CapabilityOutboxStore, component("outbox-store")); err == nil {
		t.Fatal("passive outbox store component accepted")
	}
	if err := builder.Declare(CapabilityHTTP); err == nil {
		t.Fatal("active HTTP capability declared without component")
	}
}

func TestBuilderRejectsRoleWithoutRequiredLifecycleStage(t *testing.T) {
	builder, err := NewBuilder("api", RoleAPI)
	if err != nil {
		t.Fatal(err)
	}
	declareBase(t, builder)
	for _, capability := range []Capability{
		CapabilityTransactions,
		CapabilityAuthentication,
		CapabilityAuthorization,
		CapabilityAudit,
		CapabilityIdempotency,
		CapabilityOutboxStore,
		CapabilityConnect,
	} {
		if err := builder.Declare(capability); err != nil {
			t.Fatal(err)
		}
	}
	if err := builder.AddCapability(CapabilityDatabase, component("database")); err != nil {
		t.Fatal(err)
	}
	if _, err := builder.Build(); err == nil {
		t.Fatal("API without serving component accepted")
	}
}

func TestRuntimeEntriesAreIsolated(t *testing.T) {
	builder, err := NewBuilder("dispatcher", RoleDispatcher)
	if err != nil {
		t.Fatal(err)
	}
	declareBase(t, builder)
	if err := builder.AddCapability(CapabilityDatabase, component("database")); err != nil {
		t.Fatal(err)
	}
	if err := builder.Declare(CapabilityMessaging); err != nil {
		t.Fatal(err)
	}
	if err := builder.Declare(CapabilityOutboxStore); err != nil {
		t.Fatal(err)
	}
	if err := builder.AddCapability(CapabilityOutboxDispatcher, component("outbox-dispatcher")); err != nil {
		t.Fatal(err)
	}
	runtime, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	first := runtime.Entries()
	second := runtime.Entries()
	first[0].Component = "mutated"
	if reflect.DeepEqual(first, second) {
		t.Fatal("runtime entries share mutable storage")
	}
	if err := builder.Declare(CapabilityAudit); err == nil {
		t.Fatal("builder mutation after build accepted")
	}
}
