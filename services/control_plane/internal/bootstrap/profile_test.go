// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package bootstrap

import (
	"context"
	"reflect"
	"slices"
	"strings"
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

// The consumption document is generated from the import graph, so this test
// asserts build facts rather than the contents of a hand-written table. The
// table it replaced listed libs/go/storage/sql for every role — a directory
// with no Go files, which no binary could ever have linked.
func TestConsumptionMatrixIsDerivedFromTheBuild(t *testing.T) {
	matrix := ConsumptionMatrix()
	if len(matrix) != len(Profiles()) {
		t.Fatalf("matrix=%d profiles=%d", len(matrix), len(Profiles()))
	}
	for _, entry := range matrix {
		if entry.Role == RoleMaintenance {
			// No command is built for this role, so nothing links on its
			// behalf. An empty inventory is the correct answer, not a gap in
			// the document.
			if len(entry.Packages) != 0 {
				t.Fatalf("role %q has no command but declares %v", entry.Role, entry.Packages)
			}
			continue
		}
		if !slices.IsSorted(entry.Packages) {
			t.Fatalf("role %q inventory is not sorted", entry.Role)
		}
		// The floor is what bootstrap itself pulls in. It is deliberately
		// small: this package names no concrete mechanism, so a command that
		// has no provider factory yet links almost nothing. Roles acquire the
		// rest by composing aggregates, not by importing this package.
		for _, required := range []string{
			"libs/go/clock",
			"libs/go/faults",
			"libs/go/servicekit",
			"libs/go/servicekit/production",
		} {
			if !slices.Contains(entry.Packages, required) {
				t.Fatalf("role %q does not link %q", entry.Role, required)
			}
		}
	}
}

// The registry role is the first with a materialized provider factory. If its
// factory stops constructing an adapter, the adapter leaves the import graph
// and this test fails — which is the property the old table could not have.
func TestRegistryRoleLinksItsProductionAdapters(t *testing.T) {
	consumption, err := ConsumptionFor(RoleRegistry)
	if err != nil {
		t.Fatal(err)
	}
	for _, adapter := range []string{
		"libs/go/audit/postgres",
		"libs/go/coordination/outbox/postgres",
		"libs/go/idempotency/postgres",
		"libs/go/storage/blob/gcs",
		"libs/go/storage/cache/redis",
		"libs/go/storage/sql/migrate",
		"libs/go/storage/sql/postgres",
	} {
		if !slices.Contains(consumption.Packages, adapter) {
			t.Fatalf("registry role does not link %q", adapter)
		}
	}
}

// A role without a materialized factory must not be linking provider
// adapters. If one appears here, something is reaching past the aggregate list.
func TestUnmaterializedRolesLinkNoProviderAdapter(t *testing.T) {
	consumption, err := ConsumptionFor(RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	for _, packagePath := range consumption.Packages {
		for _, adapter := range []string{"/postgres", "/gcs", "/redis", "/pubsub"} {
			if strings.HasSuffix(packagePath, adapter) {
				t.Fatalf("unwired role links provider adapter %q", packagePath)
			}
		}
	}
}

func TestConsumptionRejectsUnknownRole(t *testing.T) {
	if _, err := ConsumptionFor(Role("not-a-role")); err == nil {
		t.Fatal("unknown role accepted")
	}
}
