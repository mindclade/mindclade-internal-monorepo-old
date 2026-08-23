// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Route publication, which crosses control/routing and control/runtime_authority.
//
// Blueprint law 5: the Go control plane is never a per-request online inference dependency.
// It publishes immutable route snapshots instead, which means the canonicalization those
// snapshots are built from is what the Rust gateway ends up trusting -- a duplicate
// deployment or an out-of-region route that survives canonicalization is one the gateway
// will route to without a second opinion.
package tests

import (
	"reflect"
	"testing"
	"time"

	"go.mindclade.dev/libs/go/identifiers"

	"go.mindclade.dev/control/routing"
	"go.mindclade.dev/control/runtime_authority"
)

const (
	alphaDeploymentID = "deployment_0000000003e870008000000000000000"
	betaDeploymentID  = "deployment_0000000003e870008000000000000001"
)

// A fixed, valid digest. The value is irrelevant here; only its validity is, because
// Deployment.Validate and Policy.Validate both refuse a zero one.
func testDigest() identifiers.Digest {
	return identifiers.MustParseDigest(
		"sha256:0000000000000000000000000000000000000000000000000000000000000000")
}

func deployment(t *testing.T, id, region string, capabilities ...string) routing.Deployment {
	t.Helper()
	digest := testDigest()
	return routing.Deployment{
		DeploymentID:       id,
		ModelBundleDigest:  digest,
		EngineBundleDigest: digest,
		Endpoint:           "https://" + id + ".invalid",
		Region:             region,
		Weight:             1,
		Capabilities:       capabilities,
		LeaseExpires:       time.Unix(1770000000, 0).UTC().Add(time.Hour),
		SafetyPolicyDigest: digest,
	}
}

func TestCanonicalDeploymentsIsOrderIndependent(t *testing.T) {
	// Two publishers that disagree only about ordering must produce the same snapshot input,
	// or the same fleet state yields two different snapshots and the gateway sees churn that
	// corresponds to no actual change.
	forward := []routing.Deployment{
		deployment(t, alphaDeploymentID, "us-central1", "chat", "biology"),
		deployment(t, betaDeploymentID, "us-central1", "chat"),
	}
	reverse := []routing.Deployment{
		deployment(t, betaDeploymentID, "us-central1", "chat"),
		deployment(t, alphaDeploymentID, "us-central1", "biology", "chat"),
	}
	a, err := routing.CanonicalDeployments(forward)
	if err != nil {
		t.Fatal(err)
	}
	b, err := routing.CanonicalDeployments(reverse)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("canonical forms differ:\n a=%+v\n b=%+v", a, b)
	}
}

func TestCanonicalDeploymentsRejectsADuplicateDeployment(t *testing.T) {
	// Two rows claiming the same DeploymentID cannot both be honoured, and picking one
	// silently would make the choice depend on input order.
	_, err := routing.CanonicalDeployments([]routing.Deployment{
		deployment(t, alphaDeploymentID, "us-central1", "chat"),
		deployment(t, alphaDeploymentID, "us-east1", "chat"),
	})
	if err == nil {
		t.Fatal("a duplicate DeploymentID was accepted")
	}
}

func TestCanonicalDeploymentsDoesNotMutateTheCallersCapabilities(t *testing.T) {
	// CanonicalDeployments owns a deep copy of the nested capability slice. Sorting the
	// canonical result must not mutate caller-owned policy or make behavior depend on reuse.
	input := []routing.Deployment{
		deployment(t, alphaDeploymentID, "us-central1", "chat", "biology"),
	}
	canonical, err := routing.CanonicalDeployments(input)
	if err != nil {
		t.Fatal(err)
	}
	if got := input[0].Capabilities; !reflect.DeepEqual(got, []string{"chat", "biology"}) {
		t.Fatalf("caller capabilities mutated: got %v", got)
	}
	if got := canonical[0].Capabilities; !reflect.DeepEqual(got, []string{"biology", "chat"}) {
		t.Fatalf("canonical capabilities = %v; want sorted independent copy", got)
	}
}

func TestRoutePolicyRequiresEveryFreshnessInput(t *testing.T) {
	// Each field is what lets the Rust gateway decide a snapshot is too old to admit new work
	// without calling back into the control plane. A zero epoch would read as "no policy" and
	// pass a naive comparison, so Validate has to refuse each one individually.
	valid := routing.Policy{
		PolicyEpoch:           1,
		RevocationEpoch:       1,
		MinimumRuntimeVersion: "1.0.0",
		PolicyDigest:          testDigest(),
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid policy rejected: %v", err)
	}
	for name, mutate := range map[string]func(routing.Policy) routing.Policy{
		"no policy epoch":     func(p routing.Policy) routing.Policy { p.PolicyEpoch = 0; return p },
		"no revocation epoch": func(p routing.Policy) routing.Policy { p.RevocationEpoch = 0; return p },
		"no runtime version":  func(p routing.Policy) routing.Policy { p.MinimumRuntimeVersion = ""; return p },
		"no policy digest":    func(p routing.Policy) routing.Policy { p.PolicyDigest = identifiers.Digest{}; return p },
	} {
		if err := mutate(valid).Validate(); err == nil {
			t.Fatalf("policy with %s was accepted", name)
		}
	}
}

func TestDeploymentIsTheRuntimeAuthorityType(t *testing.T) {
	// routing.Deployment is an alias, not a conversion. If it ever becomes a distinct type,
	// every route crossing this boundary needs an explicit mapping, and the compiler will not
	// say so at the alias site -- it will say so wherever a snapshot is built.
	var route routing.Deployment
	requireRuntimeAuthorityRoute := func(runtime_authority.DeploymentRoute) {}
	requireRuntimeAuthorityRoute(route)
}
