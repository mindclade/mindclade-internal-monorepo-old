// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package scheduler

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/lib/pq"

	"go.mindclade.dev/control/orchestration"
	"go.mindclade.dev/control/scheduling"
	foundationconfig "go.mindclade.dev/libs/go/config"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/services/control_plane/internal/bootstrap"
	"go.mindclade.dev/services/control_plane/internal/foundation/tasks"
)

// A kubeconfig pointing at an address nothing listens on. Construction must
// not need a reachable API server: the scheduler resolves its configuration
// and builds clients at startup, and discovers reachability later through the
// readiness probe. A test that required a live cluster would be testing the
// cluster, not the composition.
const unreachableKubeconfig = `apiVersion: v1
kind: Config
clusters:
- name: test
  cluster:
    server: https://127.0.0.1:1
contexts:
- name: test
  context:
    cluster: test
    user: test
current-context: test
users:
- name: test
  user:
    token: test-token
`

func schedulerSettings(t *testing.T) foundationconfig.MapSource {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kubeconfig")
	if err := os.WriteFile(path, []byte(unreachableKubeconfig), 0o600); err != nil {
		t.Fatal(err)
	}
	return foundationconfig.MapSource{SourceName: "test", Values: map[string]string{
		"signing.hmac_key":      "01234567890123456789012345678901",
		"database.dsn":          "postgres://control:control@127.0.0.1:5432/control?sslmode=require",
		"messaging.provider":    "memory",
		"messaging.topic":       "mindclade.control.events",
		"kubernetes.source":     "kubeconfig",
		"kubernetes.kubeconfig": path,
	}}
}

// Building through servicekit/production is the assertion that matters: Build
// fails unless every capability the scheduler role requires -- including the
// Kubernetes, lease, leadership, and work-queue capabilities no other
// materialized role has ever provided -- is backed by a concrete provider.
func TestSchedulerFactoryBuildsThroughProductionLifecycle(t *testing.T) {
	profile, err := bootstrap.ProfileFor(bootstrap.RoleScheduler)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewSchedulerFactory(schedulerSettings(t)).Create(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}
	service, err := bootstrap.Build(profile, runtime)
	if err != nil {
		t.Fatalf("scheduler runtime does not satisfy its production role: %v", err)
	}
	if service == nil || service.Service() == nil {
		t.Fatal("production runtime was not assembled")
	}
}

func TestSchedulerWorkerHasNoStandbyRunLoop(t *testing.T) {
	profile, err := bootstrap.ProfileFor(bootstrap.RoleScheduler)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewSchedulerFactory(schedulerSettings(t)).Create(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}
	for _, dependency := range runtime.Dependencies {
		mechanisms, ok := dependency.(tasks.Mechanisms)
		if !ok {
			continue
		}
		worker, found := mechanisms.Workers[placementWorker]
		if !found {
			t.Fatal("placement worker component was not composed")
		}
		if worker.Run != nil {
			t.Fatal("placement worker can run independently of leadership")
		}
		return
	}
	t.Fatal("workqueue mechanisms were not composed")
}

// The scheduler places work and holds a singleton lease. It serves no inbound
// transport, authenticates nobody, holds no artifacts, and projects no events.
// Composing an aggregate it does not need would put those packages back into
// its import graph.
func TestSchedulerComposesOnlyWhatItsRoleNeeds(t *testing.T) {
	profile, err := bootstrap.ProfileFor(bootstrap.RoleScheduler)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewSchedulerFactory(schedulerSettings(t)).Create(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}
	capabilities := bootstrap.Capabilities(runtime.Dependencies...)
	present := make(map[string]struct{}, len(capabilities))
	for _, capability := range capabilities {
		present[capability.String()] = struct{}{}
	}
	for _, absent := range []string{
		"authentication", "authorization", "blob_store", "cache",
		"projector", "inbox_processor", "cursor_store", "migrations",
		"http", "grpc", "kubernetes_manager", "outbox_dispatcher",
	} {
		if _, found := present[absent]; found {
			t.Fatalf("scheduler composes %q, which its role does not require", absent)
		}
	}
}

// The scheduler runs migrations for nothing. One database holds every
// adapter's tables and the registry role owns their ordering; a second runner
// would race it.
func TestSchedulerRunsNoMigrations(t *testing.T) {
	profile, err := bootstrap.ProfileFor(bootstrap.RoleScheduler)
	if err != nil {
		t.Fatal(err)
	}
	source := schedulerSettings(t)
	source.Values["migrations.enabled"] = "true"
	runtime, err := NewSchedulerFactory(source).Create(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}
	for _, capability := range bootstrap.Capabilities(runtime.Dependencies...) {
		if capability.String() == "migrations" {
			t.Fatal("scheduler composed a migration runner")
		}
	}
}

// The in-memory broker is process-local, and the scheduler publishes execution
// events through it. Allowing it outside development would turn a delivery
// outage into a silent success.
func TestSchedulerRefusesMemoryBrokerOutsideDevelopment(t *testing.T) {
	for _, environment := range []string{"staging", "production"} {
		t.Run(environment, func(t *testing.T) {
			source := schedulerSettings(t)
			source.Values["environment"] = environment
			profile, err := bootstrap.ProfileFor(bootstrap.RoleScheduler)
			if err != nil {
				t.Fatal(err)
			}
			_, err = NewSchedulerFactory(source).Create(context.Background(), profile)
			if err == nil {
				t.Fatal("memory broker accepted outside development")
			}
			if reason := faults.ReasonOf(err); reason != "durable_messaging_required" && reason != "memory_messaging_outside_development" {
				t.Fatalf("reason=%s", reason)
			}
		})
	}
}

// auxiliaryNames lists the auxiliary components a runtime composed, in order.
func auxiliaryNames(runtime bootstrap.Runtime) []string {
	names := make([]string, 0, len(runtime.Components.Auxiliary))
	for _, component := range runtime.Components.Auxiliary {
		names = append(names, component.Component.Name)
	}
	return names
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// The scheduling schema probe is not optional. This role's only job is to drain
// the placement queue into the scheduling store, and a store whose tables are
// absent fails every item it claims. Without the probe the process would report
// ready and dead-letter the queue.
func TestSchedulerProbesTheSchedulingSchema(t *testing.T) {
	profile, err := bootstrap.ProfileFor(bootstrap.RoleScheduler)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewSchedulerFactory(schedulerSettings(t)).Create(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}
	if names := auxiliaryNames(runtime); !contains(names, "scheduling-schema") {
		t.Fatalf("auxiliary components %v carry no scheduling schema probe", names)
	}
}

// The promotion path is composed only when the role has been given the facts to
// translate a promoted stage with. Composing it unconditionally would mean
// either a producer that refuses every promotion -- turning a promotable stage
// into an un-promotable one -- or one that invents the tenant it charges the
// fleet ledger against. See WithPlacementFacts.
func TestSchedulerComposesThePromotionPathOnlyWithPlacementFacts(t *testing.T) {
	profile, err := bootstrap.ProfileFor(bootstrap.RoleScheduler)
	if err != nil {
		t.Fatal(err)
	}
	unbound, err := NewSchedulerFactory(schedulerSettings(t)).Create(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}
	if names := auxiliaryNames(unbound); contains(names, "orchestration-schema") {
		t.Fatalf("auxiliary components %v composed a promotion path with no facts source", names)
	}

	bound, err := NewSchedulerFactory(schedulerSettings(t)).
		WithPlacementFacts(PlacementFactsFunc(
			func(context.Context, orchestration.WorkItem) (scheduling.AdmissionRequest, error) {
				return scheduling.AdmissionRequest{}, nil
			})).
		Create(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}
	if names := auxiliaryNames(bound); !contains(names, "orchestration-schema") {
		t.Fatalf("auxiliary components %v composed no promotion path despite a facts source", names)
	}
}
