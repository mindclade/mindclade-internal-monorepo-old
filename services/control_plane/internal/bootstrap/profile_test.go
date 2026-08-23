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
	"time"

	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/servicekit"
	"go.mindclade.dev/libs/go/servicekit/production"
)

func testComponent(name string) servicekit.Component {
	return servicekit.Component{Name: name, Start: func(context.Context) error { return nil }}
}

// dispatcherComposition is the smallest composition that satisfies the
// dispatcher role. It exists so lifecycle tests can build a real production
// runtime without naming a concrete provider.
func dispatcherComposition(mechanism servicekit.Component) Components {
	return Components{
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
			{Capability: production.CapabilityOutboxDispatcher, Component: mechanism},
		},
	}
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
	runtime, err := Build(profile, Runtime{Components: dispatcherComposition(testComponent("dispatcher"))})
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
		// Every role now has a command and a materialized factory, so every
		// inventory is non-empty. This previously excepted maintenance, which
		// had no command at all.
		if len(entry.Packages) == 0 {
			t.Fatalf("role %q declares no foundation packages", entry.Role)
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
// Every role is materialized now, so the old guard -- that an unwired role
// drags in no provider adapter -- has nothing left to catch. The invariant that
// still has teeth is the narrower one it was really protecting: a role links
// only the adapters its own profile justifies, and a shared composition-root
// package must not quietly pull a provider into a binary that never opens it.
func TestRolesLinkOnlyTheProviderAdaptersTheyNeed(t *testing.T) {
	forbidden := map[Role][]string{
		// Publishes and drains the outbox. Touches no artifact store, no read
		// cache, and no cluster.
		RoleEventDispatcher: {"/gcs", "/redis", "libs/go/kubernetes"},
		// Housekeeping over the control plane's own tables only.
		RoleMaintenance: {"/gcs", "/redis", "libs/go/kubernetes"},
		// Consumes an ordered stream. Holds no artifacts and no cluster.
		RoleEventProjector: {"/gcs", "/redis", "libs/go/kubernetes"},
		// Serves requests. Reaches no cluster.
		RoleAPI:   {"libs/go/kubernetes"},
		RoleAdmin: {"libs/go/kubernetes"},
	}
	for role, adapters := range forbidden {
		consumption, err := ConsumptionFor(role)
		if err != nil {
			t.Fatal(err)
		}
		for _, packagePath := range consumption.Packages {
			for _, adapter := range adapters {
				if strings.HasSuffix(packagePath, adapter) || strings.HasPrefix(packagePath, adapter) {
					t.Fatalf("role %q links %q, which its profile does not justify", role, packagePath)
				}
			}
		}
	}
}

func TestConsumptionRejectsUnknownRole(t *testing.T) {
	if _, err := ConsumptionFor(Role("not-a-role")); err == nil {
		t.Fatal("unknown role accepted")
	}
}

// The configured shutdown budget must reach the lifecycle that enforces it.
//
// This is a behavioural assertion rather than a wiring one: the component's
// Stop hook reads the deadline servicekit hands it, which is the only place a
// process can observe the budget it will actually be stopped under. Before the
// budgets were wired, every role ran on the servicekit package defaults -- a
// 10s component stop budget -- no matter what drain.timeout the deployment
// set, and raising shutdown.timeout changed nothing at all.
func TestBuildAppliesTheConfiguredShutdownBudget(t *testing.T) {
	const (
		shutdownBudget = 90 * time.Second
		drainBudget    = 45 * time.Second
		// Halfway between the servicekit package default (10s) and the
		// configured budget, so the assertion cannot pass on the default.
		floor = 30 * time.Second
	)
	profile, err := ProfileFor(RoleEventDispatcher)
	if err != nil {
		t.Fatal(err)
	}
	var observed time.Duration
	mechanism := servicekit.Component{
		Name:  "dispatcher",
		Start: func(context.Context) error { return nil },
		Stop: func(ctx context.Context) error {
			if deadline, ok := ctx.Deadline(); ok {
				observed = time.Until(deadline)
			}
			return nil
		},
	}
	runtime, err := Build(profile, Runtime{
		Lifecycle:  Lifecycle{ShutdownTimeout: shutdownBudget, DrainTimeout: drainBudget},
		Components: dispatcherComposition(mechanism),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := runtime.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if observed <= floor {
		t.Fatalf("component stop budget=%s; the configured drain timeout of %s never reached servicekit", observed, drainBudget)
	}
}

// An unconfigured Lifecycle must inherit the bounded servicekit default rather
// than disabling the timeout. Passing a zero duration through to servicekit
// would remove the bound entirely, which is the failure mode the option list
// is written to avoid.
func TestBuildLeavesUnconfiguredBudgetsBounded(t *testing.T) {
	profile, err := ProfileFor(RoleEventDispatcher)
	if err != nil {
		t.Fatal(err)
	}
	bounded := false
	mechanism := servicekit.Component{
		Name:  "dispatcher",
		Start: func(context.Context) error { return nil },
		Stop: func(ctx context.Context) error {
			_, bounded = ctx.Deadline()
			return nil
		},
	}
	runtime, err := Build(profile, Runtime{Components: dispatcherComposition(mechanism)})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := runtime.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if !bounded {
		t.Fatal("component stop ran with no deadline; process shutdown is unbounded")
	}
}

// A drain budget larger than the whole shutdown budget cannot hold: the total
// silently truncates it. Refusing the pair at Build is what keeps the two
// numbers a contract rather than a suggestion.
func TestBuildRejectsADrainBudgetLargerThanShutdown(t *testing.T) {
	profile, err := ProfileFor(RoleEventDispatcher)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Build(profile, Runtime{
		Lifecycle:  Lifecycle{ShutdownTimeout: 10 * time.Second, DrainTimeout: 30 * time.Second},
		Components: dispatcherComposition(testComponent("dispatcher")),
	})
	if err == nil {
		t.Fatal("a drain budget longer than the shutdown budget was accepted")
	}
	if reason := faults.ReasonOf(err); reason != "drain_budget_exceeds_shutdown_budget" {
		t.Fatalf("reason=%s", reason)
	}
}
