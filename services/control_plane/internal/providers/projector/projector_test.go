// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package projector

import (
	"context"
	"testing"

	_ "github.com/lib/pq"

	foundationconfig "go.mindclade.dev/libs/go/config"
	"go.mindclade.dev/libs/go/coordination/cursor"
	"go.mindclade.dev/libs/go/coordination/projector"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/idempotency"
	"go.mindclade.dev/services/control_plane/internal/bootstrap"
	foundationprojection "go.mindclade.dev/services/control_plane/internal/foundation/projection"
)

func projectorSettings() foundationconfig.MapSource {
	return foundationconfig.MapSource{SourceName: "test", Values: map[string]string{
		"signing.hmac_key":       "01234567890123456789012345678901",
		"database.dsn":           "postgres://control:control@127.0.0.1:5432/control?sslmode=require",
		"messaging.provider":     "memory",
		"messaging.topic":        "mindclade.control.events",
		"messaging.subscription": "mindclade.control.events",
	}}
}

// Building through servicekit/production is the assertion that matters: the
// event projector is the only role that requires CapabilityProjector, and the
// first to require the inbox and cursor capabilities at all. Build fails
// unless each is backed by a concrete provider and a component occupies the
// work stage the projector owns.
func TestProjectorFactoryBuildsThroughProductionLifecycle(t *testing.T) {
	profile, err := bootstrap.ProfileFor(bootstrap.RoleEventProjector)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewProjectorFactory(projectorSettings()).Create(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}
	service, err := bootstrap.Build(profile, runtime)
	if err != nil {
		t.Fatalf("event-projector runtime does not satisfy its production role: %v", err)
	}
	if service == nil || service.Service() == nil {
		t.Fatal("production runtime was not assembled")
	}
}

func TestProjectorHasNoStandbyRunLoop(t *testing.T) {
	profile, err := bootstrap.ProfileFor(bootstrap.RoleEventProjector)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewProjectorFactory(projectorSettings()).Create(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}
	for _, dependency := range runtime.Dependencies {
		mechanisms, ok := dependency.(foundationprojection.Mechanisms)
		if !ok {
			continue
		}
		component, found := mechanisms.Projectors[projectionName]
		if !found {
			t.Fatal("projector component was not composed")
		}
		if component.Run != nil {
			t.Fatal("projector can run independently of leadership")
		}
		return
	}
	t.Fatal("projection mechanisms were not composed")
}

// The projector consumes. It serves no transport, reaches no cluster, records
// no audit, holds no work queue, and writes no outbox. Composing an aggregate
// it does not need would put those packages back into its import graph.
func TestProjectorComposesOnlyWhatItsRoleNeeds(t *testing.T) {
	profile, err := bootstrap.ProfileFor(bootstrap.RoleEventProjector)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewProjectorFactory(projectorSettings()).Create(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}
	capabilities := bootstrap.Capabilities(runtime.Dependencies...)
	present := make(map[string]struct{}, len(capabilities))
	for _, capability := range capabilities {
		present[capability.String()] = struct{}{}
	}
	for _, absent := range []string{
		"authentication", "authorization", "blob_store", "cache", "audit",
		"kubernetes", "kubernetes_manager", "work_queue_store", "migrations",
		"http", "grpc", "outbox_store", "outbox_dispatcher", "signing",
	} {
		if _, found := present[absent]; found {
			t.Fatalf("event-projector composes %q, which its role does not require", absent)
		}
	}
}

// An unconfigured projection must announce itself. A source that returned no
// events would leave a misconfigured projector indistinguishable from an idle
// one, which is the failure that takes longest to notice.
func TestUnconfiguredProjectionFailsClosed(t *testing.T) {
	if _, err := refuseFetch(context.Background(), nil, 1); err == nil {
		t.Fatal("default event source returned no error")
	} else if reason := faults.ReasonOf(err); reason != "projection_source_not_configured" {
		t.Fatalf("reason=%s", reason)
	}
	if _, err := refuseApply(context.Background(), projector.Event{}); err == nil {
		t.Fatal("default projection handler returned no error")
	} else if reason := faults.ReasonOf(err); reason != "projection_handler_not_configured" {
		t.Fatalf("reason=%s", reason)
	}
}

// An injected projection replaces both halves of the seam.
func TestInjectedProjectionReplacesTheDefaults(t *testing.T) {
	profile, err := bootstrap.ProfileFor(bootstrap.RoleEventProjector)
	if err != nil {
		t.Fatal(err)
	}
	source := sourceFunc(func(context.Context, *cursor.Cursor, int) ([]projector.Event, error) {
		return nil, nil
	})
	handler := projector.HandlerFunc(func(context.Context, projector.Event) (idempotency.Result, error) {
		return idempotency.Result{}, nil
	})
	factory := NewProjectorFactory(projectorSettings()).WithProjection(source, handler)
	runtime, err := factory.Create(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bootstrap.Build(profile, runtime); err != nil {
		t.Fatalf("injected projection does not satisfy the role: %v", err)
	}
}

// The projector runs migrations for nothing. One database holds every
// adapter's tables and the registry role owns their ordering.
func TestProjectorRunsNoMigrations(t *testing.T) {
	profile, err := bootstrap.ProfileFor(bootstrap.RoleEventProjector)
	if err != nil {
		t.Fatal(err)
	}
	source := projectorSettings()
	source.Values["migrations.enabled"] = "true"
	runtime, err := NewProjectorFactory(source).Create(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}
	for _, capability := range bootstrap.Capabilities(runtime.Dependencies...) {
		if capability.String() == "migrations" {
			t.Fatal("event-projector composed a migration runner")
		}
	}
}

// The in-memory broker is process-local. A projector subscribed to one would
// see an empty topic rather than a broken one.
func TestProjectorRefusesMemoryBrokerOutsideDevelopment(t *testing.T) {
	for _, environment := range []string{"staging", "production"} {
		t.Run(environment, func(t *testing.T) {
			source := projectorSettings()
			source.Values["environment"] = environment
			profile, err := bootstrap.ProfileFor(bootstrap.RoleEventProjector)
			if err != nil {
				t.Fatal(err)
			}
			_, err = NewProjectorFactory(source).Create(context.Background(), profile)
			if err == nil {
				t.Fatal("memory broker accepted outside development")
			}
			if reason := faults.ReasonOf(err); reason != "durable_messaging_required" && reason != "memory_messaging_outside_development" {
				t.Fatalf("reason=%s", reason)
			}
		})
	}
}
