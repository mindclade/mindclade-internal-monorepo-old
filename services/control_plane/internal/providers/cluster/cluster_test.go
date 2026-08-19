// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package cluster

import (
	"context"
	"testing"
	"time"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"

	"go.mindclade.dev/libs/go/faults"
	kubeclient "go.mindclade.dev/libs/go/kubernetes/client"
	"go.mindclade.dev/services/control_plane/internal/config"
)

// The library default is the safe one: discover in-cluster credentials, fall
// back to a kubeconfig. A settings value must not be able to silently widen it.
func TestSettingsMapOntoTheLibraryContract(t *testing.T) {
	settings := config.Settings{
		ServiceName:          "control-plane-controller",
		KubernetesSource:     "in_cluster",
		KubernetesKubeconfig: "  ",
		KubernetesContext:    " staging ",
		KubernetesTimeout:    45 * time.Second,
	}
	resolved := kubernetesConfig(settings)

	if resolved.Source != kubeclient.SourceInCluster {
		t.Fatalf("source=%q", resolved.Source)
	}
	// A blank path must not survive: in-cluster plus a kubeconfig path is a
	// contradiction the library rejects outright.
	if resolved.KubeconfigPath != "" {
		t.Fatalf("kubeconfig=%q", resolved.KubeconfigPath)
	}
	if resolved.Context != "staging" {
		t.Fatalf("context=%q", resolved.Context)
	}
	if resolved.UserAgent != settings.ServiceName {
		t.Fatalf("user agent=%q", resolved.UserAgent)
	}
	if resolved.Timeout != 45*time.Second {
		t.Fatalf("timeout=%s", resolved.Timeout)
	}
	if err := resolved.Validate(); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
}

// QPS and burst are library-owned. Mapping settings must leave the qualified
// defaults in place rather than zeroing them.
func TestClientBudgetKeepsTheLibraryDefault(t *testing.T) {
	defaults := kubeclient.DefaultConfig()
	resolved := kubernetesConfig(config.Settings{ServiceName: "control-plane-scheduler"})
	if resolved.QPS != defaults.QPS || resolved.Burst != defaults.Burst {
		t.Fatalf("qps=%v burst=%d", resolved.QPS, resolved.Burst)
	}
}

// A Cluster with no discovery client is not a healthy one that happens to be
// idle; reporting ready would let a role start with no API server.
func TestReadinessFailsClosedWithoutDiscovery(t *testing.T) {
	err := (&Cluster{}).Readiness(context.Background())
	if err == nil || faults.ReasonOf(err) != "kubernetes_discovery_not_configured" {
		t.Fatalf("Readiness() = %v", err)
	}
}

// The component carries the readiness answer. A role that registers it must
// get the same failure the probe itself gives, not a nil default.
func TestComponentCarriesReadiness(t *testing.T) {
	component := (&Cluster{}).Component("kubernetes")
	if component.Name != "kubernetes" {
		t.Fatalf("name=%q", component.Name)
	}
	if component.Readiness == nil {
		t.Fatal("component has no readiness probe")
	}
	if err := component.Readiness(context.Background()); err == nil {
		t.Fatal("Readiness() on an unconfigured cluster returned nil")
	}
}

// The readiness component owns no lifecycle. A role that runs a manager takes
// Readiness directly, so a Start here would open something twice.
func TestReadinessComponentHasNoLifecycle(t *testing.T) {
	component := (&Cluster{}).Component("kubernetes")
	if component.Start != nil || component.Stop != nil || component.Run != nil {
		t.Fatal("readiness component owns a lifecycle it should not")
	}
}

// A recorder needs a resolved REST configuration and a named source. Both
// failures are factory bugs and must surface as refusals, not as a recorder
// that silently writes events attributed to nothing.
func TestRecorderFailsClosed(t *testing.T) {
	if _, _, err := (&Cluster{}).Recorder(context.Background(), "scheduler"); err == nil {
		t.Fatal("Recorder() without a configuration returned nil")
	}
	if _, _, err := (&Cluster{Config: &rest.Config{}}).Recorder(context.Background(), "  "); err == nil {
		t.Fatal("Recorder() with a blank source returned nil")
	}
}

// Stop must be safe before Start and safe twice. Servicekit unwinds partially
// started processes, so a stream that only tolerates the happy order turns one
// startup failure into a panic during shutdown.
func TestEventStreamStopIsSafeBeforeStartAndTwice(t *testing.T) {
	stream := &eventStream{broadcaster: record.NewBroadcaster()}
	if err := stream.stop(context.Background()); err != nil {
		t.Fatalf("first stop() = %v", err)
	}
	if err := stream.stop(context.Background()); err != nil {
		t.Fatalf("second stop() = %v", err)
	}
}

// An unconfigured stream must refuse to start rather than dereference a nil
// broadcaster.
func TestEventStreamStartFailsClosed(t *testing.T) {
	if err := (&eventStream{}).start(context.Background()); err == nil {
		t.Fatal("start() without a broadcaster returned nil")
	}
}

func TestNewRequiresAContext(t *testing.T) {
	//nolint:staticcheck // the nil context is the condition under test.
	if _, err := New(nil, config.Settings{}, crclient.Options{}); err == nil {
		t.Fatal("New() with a nil context returned nil")
	}
}
