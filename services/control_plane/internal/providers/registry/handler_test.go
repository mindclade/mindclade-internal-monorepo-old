// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

package registry

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.mindclade.dev/control/registry/models"
	"go.mindclade.dev/control/registry/releases"
	"go.mindclade.dev/libs/go/identifiers"
)

type modelEngineFunc struct {
	publish func(context.Context, models.Descriptor) (models.Descriptor, error)
	resolve func(context.Context, identifiers.Digest) (models.Descriptor, error)
}

func (engine modelEngineFunc) Publish(ctx context.Context, descriptor models.Descriptor) (models.Descriptor, error) {
	return engine.publish(ctx, descriptor)
}

func (engine modelEngineFunc) Resolve(ctx context.Context, digest identifiers.Digest) (models.Descriptor, error) {
	return engine.resolve(ctx, digest)
}

type releaseEngineFunc func(context.Context, releases.Release, releases.EvidenceGraph) error

func (engine releaseEngineFunc) Promote(ctx context.Context, release releases.Release, graph releases.EvidenceGraph) error {
	return engine(ctx, release, graph)
}

func TestRegistryMuxPublishesAndResolvesModels(t *testing.T) {
	digest := identifiers.SHA256String("descriptor")
	engine := modelEngineFunc{
		publish: func(_ context.Context, descriptor models.Descriptor) (models.Descriptor, error) {
			descriptor.DescriptorDigest = digest
			return descriptor, nil
		},
		resolve: func(_ context.Context, requested identifiers.Digest) (models.Descriptor, error) {
			if !requested.Equal(digest) {
				t.Fatalf("Resolve digest = %s, want %s", requested.String(), digest.String())
			}
			return models.Descriptor{DescriptorDigest: digest, ModelID: "model_0000000003e870008000000000000000"}, nil
		},
	}
	handler, err := newRegistryMux(domains{models: engine, releases: releaseEngineFunc(func(context.Context, releases.Release, releases.EvidenceGraph) error { return nil })})
	if err != nil {
		t.Fatal(err)
	}

	publish := httptest.NewRequest(http.MethodPost, modelsPath, strings.NewReader(`{"model_id":"model_0000000003e870008000000000000000"}`))
	publish.Header.Set("Content-Type", "application/json")
	published := httptest.NewRecorder()
	handler.ServeHTTP(published, publish)
	if published.Code != http.StatusCreated {
		t.Fatalf("publish status = %d, body=%s", published.Code, published.Body.String())
	}
	if location := published.Header().Get("Location"); location != modelPathPrefix+digest.String() {
		t.Fatalf("Location = %q", location)
	}
	var body map[string]any
	if err := json.Unmarshal(published.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["descriptor_digest"] != digest.String() {
		t.Fatalf("descriptor_digest = %v", body["descriptor_digest"])
	}

	resolved := httptest.NewRecorder()
	handler.ServeHTTP(resolved, httptest.NewRequest(http.MethodGet, modelPathPrefix+digest.String(), nil))
	if resolved.Code != http.StatusOK || resolved.Header().Get("ETag") != `"`+digest.String()+`"` {
		t.Fatalf("resolve status=%d etag=%q body=%s", resolved.Code, resolved.Header().Get("ETag"), resolved.Body.String())
	}
}

func TestRegistryMuxRejectsCallerSuppliedSealAndIdentityMismatch(t *testing.T) {
	digest := identifiers.SHA256String("caller-seal")
	called := false
	handler, err := newRegistryMux(domains{
		models: modelEngineFunc{
			publish: func(context.Context, models.Descriptor) (models.Descriptor, error) {
				called = true
				return models.Descriptor{}, nil
			},
			resolve: func(context.Context, identifiers.Digest) (models.Descriptor, error) { return models.Descriptor{}, nil },
		},
		releases: releaseEngineFunc(func(context.Context, releases.Release, releases.EvidenceGraph) error { called = true; return nil }),
	})
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, modelsPath, strings.NewReader(`{"descriptor_digest":"`+digest.String()+`"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || called {
		t.Fatalf("caller seal: status=%d called=%v body=%s", response.Code, called, response.Body.String())
	}

	releaseID := "release_0000000003e870008000000000000000"
	request = httptest.NewRequest(http.MethodPost, releasePathStart+releaseID+releasePathEnd,
		strings.NewReader(`{"release":{"release_id":"release_0000000003e870008000000000000001"},"evidence_graph":{"release_id":"`+releaseID+`"}}`))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || called {
		t.Fatalf("identity mismatch: status=%d called=%v body=%s", response.Code, called, response.Body.String())
	}
}

func TestRegistryAuthorizationIsExplicitAndFailClosed(t *testing.T) {
	tests := []struct {
		method, path, permission string
		mapped                   bool
	}{
		{http.MethodPost, modelsPath, "registry.models.publish", true},
		{http.MethodGet, modelPathPrefix + identifiers.SHA256String("model").String(), "registry.models.read", true},
		{http.MethodPost, releasePathStart + "release_0000000003e870008000000000000000" + releasePathEnd, "registry.releases.promote", true},
		{http.MethodDelete, modelsPath, "", false},
		{http.MethodPost, "/v1/registry/new-unmapped-route", "", false},
	}
	for _, testCase := range tests {
		request := httptest.NewRequest(testCase.method, testCase.path, nil)
		target, mapped, err := resolveAuthorization(request)
		if err != nil {
			t.Fatalf("%s %s: %v", testCase.method, testCase.path, err)
		}
		if mapped != testCase.mapped {
			t.Fatalf("%s %s: mapped=%v", testCase.method, testCase.path, mapped)
		}
		if mapped && target.Permission.String() != testCase.permission {
			t.Fatalf("%s %s: permission=%s", testCase.method, testCase.path, target.Permission.String())
		}
	}
}

func TestRegistryMuxRefusesMissingDomainEngines(t *testing.T) {
	if handler, err := newRegistryMux(domains{}); err == nil || handler != nil {
		t.Fatalf("newRegistryMux = %#v, %v; want fail-closed error", handler, err)
	}
}
