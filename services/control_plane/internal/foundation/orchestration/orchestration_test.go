// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package orchestration

import (
	"context"
	"testing"

	crclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"go.mindclade.dev/libs/go/kubernetes/controller"
	"go.mindclade.dev/libs/go/servicekit/production"
)

type stubManager struct{}

func (stubManager) Start(ctx context.Context) error { <-ctx.Done(); return nil }

func TestEmptyClusterProvidesNothing(t *testing.T) {
	if capabilities := (Cluster{}).Capabilities(); len(capabilities) != 0 {
		t.Fatalf("capabilities=%v", capabilities)
	}
}

// A manager is the one provider here that owns a lifecycle component. If it
// registered passively the process would report the capability and never
// start a reconcile loop.
func TestManagerRegistersAWorkStageComponent(t *testing.T) {
	runtime, err := controller.NewManagerRuntime(stubManager{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	component := runtime.Component(ManagerComponent)
	cluster := Cluster{Manager: &component}

	capabilities := cluster.Capabilities()
	if len(capabilities) != 1 || capabilities[0] != production.CapabilityKubernetesManager {
		t.Fatalf("capabilities=%v", capabilities)
	}
	if _, owned := production.StageFor(production.CapabilityKubernetesManager); !owned {
		t.Fatal("StageFor() reports the manager capability as passive")
	}

	builder, err := production.NewBuilder("test", production.RoleController)
	if err != nil {
		t.Fatal(err)
	}
	if err := cluster.Register(builder); err != nil {
		t.Fatalf("Register() = %v", err)
	}
}

// The recorder is carried, not declared. Holding one must not make a process
// claim cluster access it does not have.
func TestRecorderAloneDeclaresNoCapability(t *testing.T) {
	if capabilities := (Cluster{Events: nil}).Capabilities(); len(capabilities) != 0 {
		t.Fatalf("capabilities=%v", capabilities)
	}
}

// A client is what CapabilityKubernetes means. Without this the aggregate
// could stop declaring it and only the manager path would notice.
func TestClientDeclaresClusterAccess(t *testing.T) {
	cluster := Cluster{Client: fake.NewClientBuilder().Build()}
	capabilities := cluster.Capabilities()
	if len(capabilities) != 1 || capabilities[0] != production.CapabilityKubernetes {
		t.Fatalf("capabilities=%v", capabilities)
	}
	// Passive: nothing here owns the client's lifetime, so the role must
	// register the provider's readiness component itself.
	if _, owned := production.StageFor(production.CapabilityKubernetes); owned {
		t.Fatal("StageFor() reports cluster access as owning a component")
	}
}

// A typed nil stored in the interface is not a client. Declaring the
// capability for one would let a process pass assembly and fail on first use.
func TestTypedNilClientIsAbsent(t *testing.T) {
	var absent *fakeClient
	if capabilities := (Cluster{Client: absent}).Capabilities(); len(capabilities) != 0 {
		t.Fatalf("capabilities=%v", capabilities)
	}
}

// fakeClient exists only to be a nil pointer of a type implementing the
// client interface; no method on it is ever called.
type fakeClient struct{ crclient.Client }
