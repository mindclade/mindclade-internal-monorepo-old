// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package maintenance

import (
	"context"
	"testing"

	_ "github.com/lib/pq"

	foundationconfig "go.mindclade.dev/libs/go/config"
	"go.mindclade.dev/libs/go/coordination/workqueue"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/services/control_plane/internal/bootstrap"
)

func maintenanceSettings() foundationconfig.MapSource {
	return foundationconfig.MapSource{SourceName: "test", Values: map[string]string{
		"signing.hmac_key": "01234567890123456789012345678901",
		"database.dsn":     "postgres://control:control@127.0.0.1:5432/control?sslmode=require",
	}}
}

// Building through servicekit/production is the assertion that matters: the
// maintenance role needs a lease, an elector, and a worker in the work stage.
func TestMaintenanceFactoryBuildsThroughProductionLifecycle(t *testing.T) {
	profile, err := bootstrap.ProfileFor(bootstrap.RoleMaintenance)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewMaintenanceFactory(maintenanceSettings()).Create(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}
	service, err := bootstrap.Build(profile, runtime)
	if err != nil {
		t.Fatalf("maintenance runtime does not satisfy its production role: %v", err)
	}
	if service == nil || service.Service() == nil {
		t.Fatal("production runtime was not assembled")
	}
}

// Maintenance is the narrowest role that still holds a lease. It publishes
// nothing, serves nothing, and reaches no cluster.
func TestMaintenanceComposesOnlyWhatItsRoleNeeds(t *testing.T) {
	profile, err := bootstrap.ProfileFor(bootstrap.RoleMaintenance)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewMaintenanceFactory(maintenanceSettings()).Create(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}
	capabilities := bootstrap.Capabilities(runtime.Dependencies...)
	present := make(map[string]struct{}, len(capabilities))
	for _, capability := range capabilities {
		present[capability.String()] = struct{}{}
	}
	for _, absent := range []string{
		"http", "grpc", "connect", "authentication", "authorization",
		"kubernetes", "kubernetes_manager", "messaging", "outbox_store",
		"blob_store", "cache", "projector", "cursor_store", "migrations",
		"signing", "pagination",
	} {
		if _, found := present[absent]; found {
			t.Fatalf("maintenance composes %q, which its role does not require", absent)
		}
	}
}

// Housekeeping that reports success without running leaves the state it was
// meant to reclaim in place, and nothing else notices.
func TestUnconfiguredHousekeepingFailsClosed(t *testing.T) {
	if _, err := refuseHousekeeping(context.Background(), workqueue.Item{}); err == nil {
		t.Fatal("default housekeeping handler returned no error")
	} else if reason := faults.ReasonOf(err); reason != "housekeeping_handler_not_configured" {
		t.Fatalf("reason=%s", reason)
	}
}
