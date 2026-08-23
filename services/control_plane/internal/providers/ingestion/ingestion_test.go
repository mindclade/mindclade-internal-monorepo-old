// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package ingestion

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/lib/pq"

	foundationconfig "go.mindclade.dev/libs/go/config"
	"go.mindclade.dev/libs/go/coordination/workqueue"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/servicekit"
	"go.mindclade.dev/services/control_plane/internal/bootstrap"
)

// Staging that reports success without writing the artifact advances a cursor
// past data that was never stored, which is the one failure in this role that
// retrying cannot repair.
func TestUnconfiguredStagingFailsClosed(t *testing.T) {
	if _, err := refuseStaging(context.Background(), workqueue.Item{}); err == nil {
		t.Fatal("default staging handler returned no error")
	} else if reason := faults.ReasonOf(err); reason != "staging_handler_not_configured" {
		t.Fatalf("reason=%s", reason)
	}
}

// The blob and cache stores are provider-required rather than process-required:
// the coordinator's profile demands both, so an unconfigured deployment must
// fail at startup instead of degrading to a weaker adapter.
func TestUnconfiguredObjectStoresFailClosed(t *testing.T) {
	profile, err := bootstrap.ProfileFor(bootstrap.RoleIngestionController)
	if err != nil {
		t.Fatal(err)
	}
	source := foundationconfig.MapSource{SourceName: "test", Values: map[string]string{
		"signing.hmac_key":   "01234567890123456789012345678901",
		"database.dsn":       "postgres://control:control@127.0.0.1:5432/control?sslmode=require",
		"messaging.provider": "memory",
		"messaging.topic":    "mindclade.control.events",
	}}
	_, err = NewIngestionFactory(source).Create(context.Background(), profile)
	if err == nil {
		t.Fatal("ingestion coordinator started with no artifact store configured")
	}
	if reason := faults.ReasonOf(err); reason != "blob_bucket_not_configured" && reason != "cache_address_not_configured" {
		t.Fatalf("reason=%s", reason)
	}
}

// An injected handler replaces the fail-closed default.
func TestInjectedStagingHandlerIsRetained(t *testing.T) {
	handler := workqueue.HandlerFunc(func(context.Context, workqueue.Item) (workqueue.Result, error) {
		return workqueue.Result{}, nil
	})
	factory := NewIngestionFactory(foundationconfig.MapSource{SourceName: "test"}).WithStagingHandler(handler)
	if factory.staging == nil {
		t.Fatal("injected staging handler was not retained")
	}
}

// A kubeconfig pointing at an address nothing listens on. Construction must not
// need a reachable API server: the coordinator builds its cluster client at
// startup and discovers reachability later through the readiness probe. A test
// that required a live cluster would be testing the cluster, not the
// composition.
const unreachableKubeconfig = "apiVersion: v1\n" +
	"kind: Config\n" +
	"clusters:\n" +
	"- name: test\n" +
	"  cluster:\n" +
	"    server: https://127.0.0.1:1\n" +
	"contexts:\n" +
	"- name: test\n" +
	"  context:\n" +
	"    cluster: test\n" +
	"    user: test\n" +
	"current-context: test\n" +
	"users:\n" +
	"- name: test\n" +
	"  user:\n" +
	"    token: test-token\n"

func ingestionSettings(t *testing.T) foundationconfig.MapSource {
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
		"blob.bucket":           "mindclade-ingestion-test",
		"cache.address":         "127.0.0.1:6379",
		"kubernetes.source":     "kubeconfig",
		"kubernetes.kubeconfig": path,
	}}
}

func ingestionRuntime(t *testing.T) bootstrap.Runtime {
	t.Helper()
	// The Cloud Storage client resolves credentials at construction. Pointing
	// it at an emulator host keeps the test hermetic; no request is made.
	t.Setenv("STORAGE_EMULATOR_HOST", "127.0.0.1:0")
	profile, err := bootstrap.ProfileFor(bootstrap.RoleIngestionController)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewIngestionFactory(ingestionSettings(t)).Create(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}

// The coordinator holds a cluster client and runs no controller-runtime
// manager, so nothing else in its composition ever asks the API server whether
// it is reachable. CapabilityKubernetes is passive in the production
// vocabulary: it is satisfied by holding a client, not by that client working.
// Without this probe the process reports ready against an unreachable API
// server and stages nothing, which is the failure that takes longest to notice.
func TestIngestionReportsClusterUnreachability(t *testing.T) {
	runtime := ingestionRuntime(t)
	var probe servicekit.Probe
	for _, staged := range runtime.Components.Auxiliary {
		if staged.Component.Name == clusterComponent {
			probe = staged.Component.Readiness
		}
	}
	if probe == nil {
		t.Fatal("ingestion coordinator registers no Kubernetes readiness probe; an unreachable API server would report ready")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := probe(ctx); err == nil {
		t.Fatal("Kubernetes readiness reported ready against an unreachable API server")
	}
}

// Every capability the coordinator claims must be backed by a real provider,
// and the probe above must not displace the staging worker that occupies the
// role's required runtime stage.
func TestIngestionFactoryBuildsThroughProductionLifecycle(t *testing.T) {
	profile, err := bootstrap.ProfileFor(bootstrap.RoleIngestionController)
	if err != nil {
		t.Fatal(err)
	}
	service, err := bootstrap.Build(profile, ingestionRuntime(t))
	if err != nil {
		t.Fatalf("ingestion runtime does not satisfy its production role: %v", err)
	}
	if service == nil || service.Service() == nil {
		t.Fatal("production runtime was not assembled")
	}
}
