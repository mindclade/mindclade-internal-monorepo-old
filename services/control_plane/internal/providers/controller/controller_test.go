// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package controller

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/lib/pq"

	foundationconfig "go.mindclade.dev/libs/go/config"
	"go.mindclade.dev/libs/go/coordination/workqueue"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/services/control_plane/internal/bootstrap"
	"go.mindclade.dev/services/control_plane/internal/foundation/orchestration"
	"go.mindclade.dev/services/control_plane/internal/foundation/tasks"
)

// A kubeconfig pointing at an address nothing listens on. Construction must
// not need a reachable API server: the controller builds its manager at
// startup and discovers reachability later through the readiness probe. A test
// that required a live cluster would be testing the cluster, not the
// composition.
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

func controllerSettings(t *testing.T) foundationconfig.MapSource {
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

// Building through servicekit/production is the assertion that matters: the
// controller is the first role to require CapabilityKubernetesManager, and
// Build fails unless a component actually occupies the work stage that
// capability owns.
func TestControllerFactoryBuildsThroughProductionLifecycle(t *testing.T) {
	profile, err := bootstrap.ProfileFor(bootstrap.RoleController)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewControllerFactory(controllerSettings(t)).Create(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}
	service, err := bootstrap.Build(profile, runtime)
	if err != nil {
		t.Fatalf("controller runtime does not satisfy its production role: %v", err)
	}
	if service == nil || service.Service() == nil {
		t.Fatal("production runtime was not assembled")
	}
}

// The controller reconciles. It serves no inbound transport, authenticates
// nobody, holds no artifacts, and issues no signed tickets. Composing an
// aggregate it does not need would put those packages back into its import
// graph.
func TestControllerComposesOnlyWhatItsRoleNeeds(t *testing.T) {
	profile, err := bootstrap.ProfileFor(bootstrap.RoleController)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewControllerFactory(controllerSettings(t)).Create(context.Background(), profile)
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
		"http", "grpc", "outbox_dispatcher", "signing", "pagination",
	} {
		if _, found := present[absent]; found {
			t.Fatalf("controller composes %q, which its role does not require", absent)
		}
	}
}

// The manager capability is the whole point of this role, and it is what the
// scheduler deliberately does not hold.
func TestControllerHoldsTheKubernetesManager(t *testing.T) {
	profile, err := bootstrap.ProfileFor(bootstrap.RoleController)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewControllerFactory(controllerSettings(t)).Create(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, capability := range bootstrap.Capabilities(runtime.Dependencies...) {
		if capability.String() == "kubernetes_manager" {
			found = true
		}
	}
	if !found {
		t.Fatal("controller composed no Kubernetes manager")
	}
}

func TestControllerManagerHasNoStandbyRunLoop(t *testing.T) {
	profile, err := bootstrap.ProfileFor(bootstrap.RoleController)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewControllerFactory(controllerSettings(t)).Create(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}
	for _, dependency := range runtime.Dependencies {
		cluster, ok := dependency.(orchestration.Cluster)
		if !ok || cluster.Manager == nil {
			continue
		}
		if cluster.Manager.Run != nil {
			t.Fatal("controller manager can run independently of leadership")
		}
		return
	}
	t.Fatal("controller manager component was not composed")
}

// One database holds every adapter's tables and the registry role owns their
// ordering. A second runner would race it.
func TestControllerRunsNoMigrations(t *testing.T) {
	profile, err := bootstrap.ProfileFor(bootstrap.RoleController)
	if err != nil {
		t.Fatal(err)
	}
	source := controllerSettings(t)
	source.Values["migrations.enabled"] = "true"
	runtime, err := NewControllerFactory(source).Create(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}
	for _, capability := range bootstrap.Capabilities(runtime.Dependencies...) {
		if capability.String() == "migrations" {
			t.Fatal("controller composed a migration runner")
		}
	}
}

// The in-memory broker is process-local. Allowing it outside development would
// turn a delivery outage into a silent success.
func TestControllerRefusesMemoryBrokerOutsideDevelopment(t *testing.T) {
	for _, environment := range []string{"staging", "production"} {
		t.Run(environment, func(t *testing.T) {
			source := controllerSettings(t)
			source.Values["environment"] = environment
			profile, err := bootstrap.ProfileFor(bootstrap.RoleController)
			if err != nil {
				t.Fatal(err)
			}
			_, err = NewControllerFactory(source).Create(context.Background(), profile)
			if err == nil {
				t.Fatal("memory broker accepted outside development")
			}
			if reason := faults.ReasonOf(err); reason != "durable_messaging_required" && reason != "memory_messaging_outside_development" {
				t.Fatalf("reason=%s", reason)
			}
		})
	}
}

// The operator is the same composition as the controller and must satisfy its
// own role profile through it.
func TestOperatorFactoryBuildsThroughProductionLifecycle(t *testing.T) {
	profile, err := bootstrap.ProfileFor(bootstrap.RoleOperator)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewOperatorFactory(controllerSettings(t)).Create(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}
	service, err := bootstrap.Build(profile, runtime)
	if err != nil {
		t.Fatalf("operator runtime does not satisfy its production role: %v", err)
	}
	if service == nil || service.Service() == nil {
		t.Fatal("production runtime was not assembled")
	}
}

// The two roles are separate singletons. Sharing a lease key would let one
// role's process block the other's, and sharing an event source would make an
// object's event history unattributable.
func TestControllerAndOperatorAreDistinctSingletons(t *testing.T) {
	controller := NewControllerFactory()
	operator := NewOperatorFactory()
	if controller.leaseKey == operator.leaseKey {
		t.Fatalf("controller and operator share lease key %q", controller.leaseKey)
	}
	if controller.eventSource == operator.eventSource {
		t.Fatalf("controller and operator share event source %q", controller.eventSource)
	}
}

// The controller and operator have identical capability profiles, so nothing
// downstream can tell them apart: production.Builder validates capabilities,
// and both compositions satisfy both profiles. A command wired to the wrong
// variant would run under one role's process name while claiming the other's
// singleton lease and reporting events under the other's source -- two
// deployments contending for one lease, with no error anywhere. The factory is
// the only place that knows which variant it is, so it is the only place that
// can refuse.
func TestControllerFactoryRefusesTheOperatorProfile(t *testing.T) {
	for name, factory := range map[string]*Factory{
		"controller factory, operator profile": NewControllerFactory(controllerSettings(t)),
		"operator factory, controller profile": NewOperatorFactory(controllerSettings(t)),
	} {
		t.Run(name, func(t *testing.T) {
			role := bootstrap.RoleOperator
			if factory.leaseKey == operatorLeaseKey {
				role = bootstrap.RoleController
			}
			profile, err := bootstrap.ProfileFor(role)
			if err != nil {
				t.Fatal(err)
			}
			_, err = factory.Create(context.Background(), profile)
			if err == nil {
				t.Fatal("factory accepted a profile for the role it does not implement")
			}
			if reason := faults.ReasonOf(err); reason != "factory_profile_role_mismatch" {
				t.Fatalf("reason=%s", reason)
			}
		})
	}
}

// The stage seam's default. It is fail-closed on purpose: stage reconciliation
// is domain code and a composition root does not author it, so an unwired role
// must fail its items rather than acknowledge work it cannot do.
func TestStageReconcilerRefusesWorkUntilItIsConfigured(t *testing.T) {
	_, err := refuseStageReconcile(context.Background(), workqueue.Item{})
	if !faults.IsCode(err, faults.CodeNotImplemented) || !faults.IsReason(err, "stage_reconciler_not_configured") {
		t.Fatalf("default stage handler = %s/%q, want not_implemented/stage_reconciler_not_configured",
			faults.CodeOf(err), faults.ReasonOf(err))
	}
	if faults.IsRetryable(err) {
		t.Fatal("an unconfigured stage reconciler asked the queue to retry a permanent refusal")
	}
}

// The stage worker is leader-gated exactly like the manager. A standby that
// could start it would claim durable items the leader is reconciling, which is
// the split brain the singleton lease exists to prevent.
func TestControllerStageWorkerHasNoStandbyRunLoop(t *testing.T) {
	profile, err := bootstrap.ProfileFor(bootstrap.RoleController)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewControllerFactory(controllerSettings(t)).Create(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}
	for _, dependency := range runtime.Dependencies {
		mechanisms, ok := dependency.(tasks.Mechanisms)
		if !ok {
			continue
		}
		worker, found := mechanisms.Workers[stageWorker]
		if !found {
			t.Fatal("stage worker component was not composed")
		}
		if worker.Run != nil {
			t.Fatal("stage worker can run independently of leadership")
		}
		return
	}
	t.Fatal("workqueue mechanisms were not composed")
}

// The controller and the operator are separate singletons under separate
// leases. A shared stage queue would let either claim the other's work, and a
// claim is exclusive: the intended reconciler would never see the item again.
func TestControllerAndOperatorDrainSeparateStageQueues(t *testing.T) {
	controller := NewControllerFactory(controllerSettings(t))
	operator := NewOperatorFactory(controllerSettings(t))
	if controller.stageQueue == "" || operator.stageQueue == "" {
		t.Fatal("a reconciling role composed no stage queue")
	}
	if controller.stageQueue == operator.stageQueue {
		t.Fatalf("both roles drain %q", controller.stageQueue)
	}
}

// The manager and the stage worker must reach their aggregates under their own
// names. Both are servicekit.Component, so before leadership.GateComponents
// keyed its result by name, swapping its two arguments compiled and registered
// the manager as the stage worker -- and every other test here still passed,
// because both components are stripped of Run either way.
func TestControllerRegistersEachGatedComponentUnderItsOwnName(t *testing.T) {
	profile, err := bootstrap.ProfileFor(bootstrap.RoleController)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewControllerFactory(controllerSettings(t)).Create(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}
	var sawManager, sawWorker bool
	for _, dependency := range runtime.Dependencies {
		switch aggregate := dependency.(type) {
		case orchestration.Cluster:
			if aggregate.Manager == nil {
				t.Fatal("the cluster aggregate carries no manager component")
			}
			if aggregate.Manager.Name != orchestration.ManagerComponent {
				t.Fatalf("manager registered as %q, want %q",
					aggregate.Manager.Name, orchestration.ManagerComponent)
			}
			sawManager = true
		case tasks.Mechanisms:
			worker, found := aggregate.Workers[stageWorker]
			if !found {
				t.Fatal("stage worker component was not composed")
			}
			if worker.Name != "worker/"+stageWorker {
				t.Fatalf("stage worker registered as %q, want %q", worker.Name, "worker/"+stageWorker)
			}
			sawWorker = true
		}
	}
	if !sawManager || !sawWorker {
		t.Fatalf("manager aggregate seen=%v, worker aggregate seen=%v", sawManager, sawWorker)
	}
}
