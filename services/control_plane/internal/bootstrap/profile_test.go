// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package bootstrap

import (
	"context"
	"reflect"
	"testing"

	"go.mindclade.dev/libs/go/servicekit"
	"go.mindclade.dev/libs/go/servicekit/production"
)

func testComponent(name string) servicekit.Component {
	return servicekit.Component{Name: name, Start: func(context.Context) error { return nil }}
}

func TestEveryRoleHasAValidProfile(t *testing.T) {
	profiles := Profiles()
	if len(profiles) != 11 {
		t.Fatalf("profiles=%d", len(profiles))
	}
	seen := make(map[Role]struct{})
	for _, profile := range profiles {
		if err := profile.Validate(); err != nil {
			t.Fatal(err)
		}
		if len(profile.Requirements()) == 0 {
			t.Fatalf("role %q has no production requirements", profile.Role)
		}
		if _, duplicate := seen[profile.Role]; duplicate {
			t.Fatalf("duplicate role %q", profile.Role)
		}
		seen[profile.Role] = struct{}{}
	}
}

func TestBuildUsesProductionBuilderAndCanonicalStages(t *testing.T) {
	profile, err := ProfileFor(RoleEventDispatcher)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := Build(profile, Runtime{Components: Components{
		Passive: []production.Capability{
			production.CapabilityClock,
			production.CapabilityConfiguration,
			production.CapabilityIdentifiers,
			production.CapabilityRequestMetadata,
			production.CapabilityRetry,
			production.CapabilityMessaging,
			production.CapabilityOutboxStore,
		},
		Mechanisms: []Mechanism{
			{Capability: production.CapabilityObservability, Component: testComponent("telemetry")},
			{Capability: production.CapabilityDatabase, Component: testComponent("database")},
			{Capability: production.CapabilityOutboxDispatcher, Component: testComponent("dispatcher")},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := []servicekit.AssemblyEntry{
		{Stage: servicekit.StageFoundation, Component: "telemetry"},
		{Stage: servicekit.StageInfrastructure, Component: "database"},
		{Stage: servicekit.StageCoordination, Component: "dispatcher"},
	}
	if got := runtime.Entries(); !reflect.DeepEqual(got, want) {
		t.Fatalf("entries=%v want=%v", got, want)
	}
	if runtime.Manifest().Role() != production.RoleDispatcher {
		t.Fatalf("role=%s", runtime.Manifest().Role())
	}
}

func TestBuildRejectsMissingCapabilities(t *testing.T) {
	profile, err := ProfileFor(RoleScheduler)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Build(profile, Runtime{}); err == nil {
		t.Fatal("empty scheduler composition accepted")
	}
}

func TestConsumptionMatrixCoversEveryRoleAndCoreFoundation(t *testing.T) {
	matrix := ConsumptionMatrix()
	if len(matrix) != len(Profiles()) {
		t.Fatalf("matrix=%d profiles=%d", len(matrix), len(Profiles()))
	}
	coverage := make(map[string]struct{})
	for _, entry := range matrix {
		if len(entry.Packages) == 0 {
			t.Fatalf("role %q has no package consumption", entry.Role)
		}
		for _, packagePath := range entry.Packages {
			coverage[packagePath] = struct{}{}
		}
	}
	for _, packagePath := range []string{
		"libs/go/auth",
		"libs/go/coordination/cursor",
		"libs/go/coordination/inbox",
		"libs/go/coordination/leadership",
		"libs/go/coordination/outbox",
		"libs/go/coordination/projector",
		"libs/go/coordination/workqueue",
		"libs/go/kubernetes",
		"libs/go/servicekit/production",
		"libs/go/storage/blob",
		"libs/go/storage/cache",
		"libs/go/storage/lease",
		"libs/go/storage/sql",
	} {
		if _, ok := coverage[packagePath]; !ok {
			t.Fatalf("foundation package %q is not consumed by any role", packagePath)
		}
	}
}
