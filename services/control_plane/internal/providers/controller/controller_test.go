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
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/services/control_plane/internal/bootstrap"
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
		"database.dsn":          "postgres://control:control@127.0.0.1:5432/control?sslmode=disable",
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
