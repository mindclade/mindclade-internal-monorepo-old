// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package ingestion

import (
	"context"
	"testing"

	_ "github.com/lib/pq"

	foundationconfig "go.mindclade.dev/libs/go/config"
	"go.mindclade.dev/libs/go/coordination/workqueue"
	"go.mindclade.dev/libs/go/faults"
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
