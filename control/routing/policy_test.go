// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package routing

import (
	"context"
	"errors"
	"go.mindclade.dev/control/runtime_authority"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/signing"
	"testing"
	"time"
)

type recordingPublisher struct {
	err       error
	snapshots []runtime_authority.RouteSnapshot
}

func (p *recordingPublisher) PublishRouteSnapshot(_ context.Context, _ string, snapshot runtime_authority.RouteSnapshot) error {
	p.snapshots = append(p.snapshots, cloneSnapshot(snapshot))
	return p.err
}

func testRoutingService(t *testing.T, repository SnapshotRepository, publisher Publisher) Service {
	t.Helper()
	key := []byte("0123456789abcdef0123456789abcdef")
	sg, err := signing.NewHMACSigner(signing.MustParseKeyID("route"), key)
	if err != nil {
		t.Fatal(err)
	}
	return Service{
		Repository: repository,
		Publisher:  publisher,
		Builder: SnapshotBuilder{
			Issuer: runtime_authority.Issuer{Name: "control", Signer: sg},
			TTL:    time.Minute,
		},
	}
}

func testDeployment(t *testing.T) Deployment {
	t.Helper()
	depID, err := identifiers.NewID(identifiers.MustParseKind("deployment"))
	if err != nil {
		t.Fatal(err)
	}
	return Deployment{
		DeploymentID:       depID.String(),
		ModelBundleDigest:  identifiers.SHA256([]byte("m")),
		EngineBundleDigest: identifiers.SHA256([]byte("e")),
		Endpoint:           "unix:///runtime",
		Region:             "us",
		Weight:             1,
		LeaseExpires:       time.Now().Add(time.Hour),
		Capabilities:       []string{"fold", "embed"},
	}
}

func testPolicy() Policy {
	return Policy{
		PolicyEpoch:           1,
		RevocationEpoch:       1,
		MinimumRuntimeVersion: "1",
		PolicyDigest:          identifiers.SHA256([]byte("p")),
	}
}

func TestSnapshotPublicationMonotonic(t *testing.T) {
	svc := testRoutingService(t, NewMemoryRepository(), nil)
	r := testDeployment(t)
	p := testPolicy()
	if _, err := svc.PublishAt(context.Background(), "us", 1, p, []Deployment{r}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PublishAt(context.Background(), "us", 1, p, []Deployment{r}, time.Now()); err == nil {
		t.Fatal("expected non-monotonic version rejection")
	}
}

func TestCanonicalDeploymentsDoesNotMutateCaller(t *testing.T) {
	route := testDeployment(t)
	original := append([]string(nil), route.Capabilities...)
	canonical, err := CanonicalDeployments([]Deployment{route})
	if err != nil {
		t.Fatal(err)
	}
	if got := route.Capabilities; got[0] != original[0] || got[1] != original[1] {
		t.Fatalf("caller capabilities mutated: got %v want %v", got, original)
	}
	if got := canonical[0].Capabilities; got[0] != "embed" || got[1] != "fold" {
		t.Fatalf("capabilities were not canonicalized: %v", got)
	}
}

func TestMemoryRepositoryOwnsImmutableCopies(t *testing.T) {
	repository := NewMemoryRepository()
	snapshot, err := testRoutingService(t, repository, nil).PublishAt(
		context.Background(), "us", 1, testPolicy(), []Deployment{testDeployment(t)}, time.Now(),
	)
	if err != nil {
		t.Fatal(err)
	}

	snapshot.Claims.Routes[0].Capabilities[0] = "mutated-outside"
	snapshot.Signature.Value[0] ^= 0xff
	first, err := repository.Current(context.Background(), "us")
	if err != nil {
		t.Fatal(err)
	}
	if first.Claims.Routes[0].Capabilities[0] == "mutated-outside" {
		t.Fatal("repository retained a caller-owned capabilities slice")
	}
	first.Claims.Routes[0].Capabilities[0] = "mutated-read"
	second, err := repository.Current(context.Background(), "us")
	if err != nil {
		t.Fatal(err)
	}
	if second.Claims.Routes[0].Capabilities[0] == "mutated-read" {
		t.Fatal("repository returned a mutable internal capabilities slice")
	}
	if err := second.Claims.VerifyDigest(); err != nil {
		t.Fatalf("stored snapshot digest changed through an external alias: %v", err)
	}
}

func TestPublisherFailureRetainsExactSnapshotForRepublish(t *testing.T) {
	deliveryError := errors.New("publisher unavailable")
	publisher := &recordingPublisher{err: deliveryError}
	repository := NewMemoryRepository()
	service := testRoutingService(t, repository, publisher)

	snapshot, err := service.PublishAt(
		context.Background(), "us", 1, testPolicy(), []Deployment{testDeployment(t)}, time.Now(),
	)
	if !errors.Is(err, deliveryError) {
		t.Fatalf("expected delivery error, got %v", err)
	}
	if snapshot.Claims.Version != 1 || len(publisher.snapshots) != 1 {
		t.Fatalf("failed publication did not return its exact snapshot: %+v", snapshot.Claims)
	}

	publisher.err = nil
	republished, err := service.Republish(context.Background(), "us", snapshot.Claims.SnapshotDigest)
	if err != nil {
		t.Fatal(err)
	}
	if len(publisher.snapshots) != 2 {
		t.Fatalf("expected exactly one retry, got %d deliveries", len(publisher.snapshots))
	}
	if !republished.Claims.SnapshotDigest.Equal(snapshot.Claims.SnapshotDigest) ||
		publisher.snapshots[1].Signature.KeyID != snapshot.Signature.KeyID {
		t.Fatal("republish manufactured a different snapshot identity")
	}

	publisher.err = deliveryError
	newer, err := service.PublishAt(
		context.Background(), "us", 2, testPolicy(), []Deployment{testDeployment(t)}, time.Now(),
	)
	if !errors.Is(err, deliveryError) || newer.Claims.Version != 2 {
		t.Fatalf("expected stored newer snapshot and delivery error, got version=%d err=%v", newer.Claims.Version, err)
	}
	deliveries := len(publisher.snapshots)
	if _, err := service.Republish(context.Background(), "us", snapshot.Claims.SnapshotDigest); err == nil {
		t.Fatal("expected stale exact-snapshot retry to fail closed")
	}
	if len(publisher.snapshots) != deliveries {
		t.Fatal("stale retry delivered the wrong current snapshot")
	}
}
