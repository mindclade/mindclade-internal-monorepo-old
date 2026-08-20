// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package dispatcher

import (
	"context"
	"testing"

	_ "github.com/lib/pq"

	foundationconfig "go.mindclade.dev/libs/go/config"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/services/control_plane/internal/bootstrap"
)

func dispatcherSettings() foundationconfig.MapSource {
	return foundationconfig.MapSource{SourceName: "test", Values: map[string]string{
		"signing.hmac_key":   "01234567890123456789012345678901",
		"database.dsn":       "postgres://control:control@127.0.0.1:5432/control?sslmode=require",
		"messaging.provider": "memory",
		"messaging.topic":    "mindclade.control.events",
	}}
}

// Building through servicekit/production is the assertion that matters: Build
// fails unless the dispatcher role's capabilities are each backed by a
// concrete provider, and unless the composition puts a component in the
// coordination stage the role requires.
func TestEventDispatcherFactoryBuildsThroughProductionLifecycle(t *testing.T) {
	profile, err := bootstrap.ProfileFor(bootstrap.RoleEventDispatcher)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewEventDispatcherFactory(dispatcherSettings()).Create(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}
	service, err := bootstrap.Build(profile, runtime)
	if err != nil {
		t.Fatalf("dispatcher runtime does not satisfy its production role: %v", err)
	}
	if service == nil || service.Service() == nil {
		t.Fatal("production runtime was not assembled")
	}
}

// The dispatcher publishes; it does not serve, authenticate, project, or hold
// artifacts. Composing an aggregate it does not need would put those packages
// back into its import graph.
func TestEventDispatcherComposesOnlyWhatItsRoleNeeds(t *testing.T) {
	profile, err := bootstrap.ProfileFor(bootstrap.RoleEventDispatcher)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewEventDispatcherFactory(dispatcherSettings()).Create(context.Background(), profile)
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
		"projector", "inbox_processor", "cursor_store", "work_queue_store",
		"leadership", "lease_store", "migrations",
	} {
		if _, found := present[absent]; found {
			t.Fatalf("dispatcher composes %q, which its role does not require", absent)
		}
	}
}

// The in-memory broker is process-local. Allowing it outside development would
// turn a delivery outage into a silent success.
func TestMemoryBrokerIsRefusedOutsideDevelopment(t *testing.T) {
	for _, environment := range []string{"staging", "production"} {
		t.Run(environment, func(t *testing.T) {
			source := dispatcherSettings()
			source.Values["environment"] = environment
			profile, err := bootstrap.ProfileFor(bootstrap.RoleEventDispatcher)
			if err != nil {
				t.Fatal(err)
			}
			_, err = NewEventDispatcherFactory(source).Create(context.Background(), profile)
			if err == nil {
				t.Fatal("memory broker accepted outside development")
			}
			// config.Settings.Validate is the first gate and rejects the
			// provider before construction; newPublisher is the second.
			if reason := faults.ReasonOf(err); reason != "durable_messaging_required" && reason != "memory_messaging_outside_development" {
				t.Fatalf("reason=%s", reason)
			}
		})
	}
}

func TestPubSubProviderReportsThatItIsNotConfigured(t *testing.T) {
	source := dispatcherSettings()
	source.Values["messaging.provider"] = "pubsub"
	profile, err := bootstrap.ProfileFor(bootstrap.RoleEventDispatcher)
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewEventDispatcherFactory(source).Create(context.Background(), profile)
	if err == nil || faults.ReasonOf(err) != "pubsub_provider_not_configured" {
		t.Fatalf("err=%v", err)
	}
}
