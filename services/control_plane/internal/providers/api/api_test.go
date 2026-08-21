// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	_ "github.com/lib/pq"

	foundationconfig "go.mindclade.dev/libs/go/config"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/servicekit"
	"go.mindclade.dev/services/control_plane/internal/bootstrap"
	"go.mindclade.dev/services/control_plane/internal/config"
)

const testAPIKey = "api-client-secret-value"

func apiSettings() foundationconfig.MapSource {
	digest := sha256.Sum256([]byte(testAPIKey))
	return foundationconfig.MapSource{SourceName: "test", Values: map[string]string{
		"signing.hmac_key": "01234567890123456789012345678901",
		"database.dsn":     "postgres://control:control@127.0.0.1:5432/control?sslmode=require",
		"auth.api_keys":    "api-client:" + hex.EncodeToString(digest[:]) + ":artifacts.read",
		// Port 0 lets the kernel choose, so the suite never collides with a
		// developer's running process or with a parallel package.
		"http.address":    "127.0.0.1:0",
		"grpc.address":    "127.0.0.1:0",
		"metrics.address": "127.0.0.1:0",
	}}
}

// Building through servicekit/production is the assertion that matters: the
// API role requires a network transport, and Build fails unless a component
// occupies the serving stage.
func TestAPIFactoryBuildsThroughProductionLifecycle(t *testing.T) {
	profile, err := bootstrap.ProfileFor(bootstrap.RoleAPI)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewAPIFactory(apiSettings()).Create(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}
	service, err := bootstrap.Build(profile, runtime)
	if err != nil {
		t.Fatalf("api runtime does not satisfy its production role: %v", err)
	}
	if service == nil || service.Service() == nil {
		t.Fatal("production runtime was not assembled")
	}
}

// The whole point of this role is that it mounts all three inbound surfaces.
// The registry serves HTTP only, so these three capabilities together are what
// bring the Connect and gRPC submodules into a production binary.
func TestAPIMountsEveryInboundTransport(t *testing.T) {
	profile, err := bootstrap.ProfileFor(bootstrap.RoleAPI)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewAPIFactory(apiSettings()).Create(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}
	present := make(map[string]struct{})
	for _, capability := range runtime.Components.Passive {
		present[capability.String()] = struct{}{}
	}
	for _, mechanism := range runtime.Components.Mechanisms {
		present[mechanism.Capability.String()] = struct{}{}
	}
	for _, required := range []string{"http", "grpc", "connect"} {
		if _, found := present[required]; !found {
			t.Fatalf("api does not mount the %q transport", required)
		}
	}
}

func TestAPIWiresDedicatedAdmissionMetricsLifecycle(t *testing.T) {
	profile, err := bootstrap.ProfileFor(bootstrap.RoleAPI)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewAPIFactory(apiSettings()).Create(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}
	for _, auxiliary := range runtime.Components.Auxiliary {
		if auxiliary.Stage == servicekit.StageServing && auxiliary.Component.Name == "admission-metrics-server" {
			return
		}
	}
	t.Fatal("API has no dedicated admission metrics serving component")
}

func TestAdminDoesNotWireAdmissionMetricsSlice(t *testing.T) {
	profile, err := bootstrap.ProfileFor(bootstrap.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewAdminFactory(apiSettings()).Create(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}
	for _, auxiliary := range runtime.Components.Auxiliary {
		if auxiliary.Component.Name == "admission-metrics-server" {
			t.Fatal("admin role unexpectedly owns the API admission metrics listener")
		}
	}
}

// The API serves. It reconciles nothing, leases nothing, and drains no queue.
// Composing an aggregate it does not need would put those packages back into
// its import graph.
func TestAPIComposesOnlyWhatItsRoleNeeds(t *testing.T) {
	profile, err := bootstrap.ProfileFor(bootstrap.RoleAPI)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewAPIFactory(apiSettings()).Create(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}
	capabilities := bootstrap.Capabilities(runtime.Dependencies...)
	present := make(map[string]struct{}, len(capabilities))
	for _, capability := range capabilities {
		present[capability.String()] = struct{}{}
	}
	for _, absent := range []string{
		"kubernetes", "kubernetes_manager", "lease_store", "leadership",
		"work_queue_store", "work_queue_worker", "projector", "cursor_store",
		"inbox_processor", "migrations", "outbox_dispatcher", "blob_store", "cache",
	} {
		if _, found := present[absent]; found {
			t.Fatalf("api composes %q, which its role does not require", absent)
		}
	}
}

// Half-configured transport security is a deployment mistake. Serving
// plaintext because one value was missing is the failure that would not be
// noticed until traffic had already flowed.
func TestIncompleteTLSMaterialIsRefused(t *testing.T) {
	cases := map[string]map[string]string{
		"certificate_without_key": {"grpc.tls.certificate": "/tmp/cert.pem"},
		"key_without_certificate": {"grpc.tls.key": "/tmp/key.pem"},
	}
	for name, overrides := range cases {
		t.Run(name, func(t *testing.T) {
			source := apiSettings()
			for key, value := range overrides {
				source.Values[key] = value
			}
			settings, err := decodeSettings(t, source)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := transportCredentials(settings); err == nil {
				t.Fatal("incomplete TLS material was accepted")
			} else if reason := faults.ReasonOf(err); reason != "incomplete_grpc_tls_pair" {
				t.Fatalf("reason=%s", reason)
			}
		})
	}
}

// A client CA without a server certificate cannot establish mutual TLS, so it
// is a misconfiguration rather than a stricter setting.
func TestClientCAWithoutCertificateIsRefused(t *testing.T) {
	source := apiSettings()
	source.Values["grpc.tls.client_ca"] = "/tmp/ca.pem"
	settings, err := decodeSettings(t, source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transportCredentials(settings); err == nil {
		t.Fatal("client CA without a certificate was accepted")
	} else if reason := faults.ReasonOf(err); reason != "client_ca_without_server_certificate" {
		t.Fatalf("reason=%s", reason)
	}
}

// With no TLS material the process serves plaintext and says so, for a
// deployment that terminates TLS ahead of it.
func TestAbsentTLSMaterialServesPlaintext(t *testing.T) {
	settings, err := decodeSettings(t, apiSettings())
	if err != nil {
		t.Fatal(err)
	}
	transport, err := transportCredentials(settings)
	if err != nil {
		t.Fatal(err)
	}
	if transport != nil {
		t.Fatalf("expected plaintext, got credentials=%v", transport)
	}
}

// An empty credential registry is a deployment error, not an open door.
func TestUnconfiguredAPIKeysFailClosed(t *testing.T) {
	source := apiSettings()
	source.Values["auth.api_keys"] = ""
	profile, err := bootstrap.ProfileFor(bootstrap.RoleAPI)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewAPIFactory(source).Create(context.Background(), profile); err == nil {
		t.Fatal("api started with no credential registry")
	} else if reason := faults.ReasonOf(err); reason != "api_keys_not_configured" {
		t.Fatalf("reason=%s", reason)
	}
}

func decodeSettings(t *testing.T, source foundationconfig.MapSource) (config.Settings, error) {
	t.Helper()
	resolved, err := config.Load(context.Background(), "control-plane-api", source)
	if err != nil {
		return config.Settings{}, err
	}
	return resolved.Settings, nil
}
