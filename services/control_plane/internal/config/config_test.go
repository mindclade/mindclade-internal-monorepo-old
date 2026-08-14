// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package config

import (
	"context"
	"testing"

	foundationconfig "mindclade.internal/libs/go/config"
	"mindclade.internal/libs/go/faults"
)

func TestDevelopmentConfigurationResolvesAndRedacts(t *testing.T) {
	resolved, err := Load(context.Background(), "control-plane-api", foundationconfig.MapSource{SourceName: "test", Values: map[string]string{
		"signing.hmac_key":       "01234567890123456789012345678901",
		"outbound.allowed_hosts": "Example.COM, api.example.com,example.com",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Settings.ServiceName != "control-plane-api" || resolved.Settings.Environment != EnvironmentDevelopment {
		t.Fatalf("settings=%+v", resolved.Settings)
	}
	if got := resolved.Snapshot.Redacted()["signing.hmac_key"]; got != "[REDACTED]" {
		t.Fatalf("redacted=%q", got)
	}
	if len(resolved.Settings.OutboundAllowedHosts) != 2 {
		t.Fatalf("hosts=%v", resolved.Settings.OutboundAllowedHosts)
	}
	if resolved.Current == nil || resolved.Current.Snapshot().Digest().IsZero() {
		t.Fatal("missing atomic configuration snapshot")
	}
}

func TestProductionFailsClosedWithoutDurableProviders(t *testing.T) {
	_, err := Load(context.Background(), "control-plane-api", foundationconfig.MapSource{Values: map[string]string{
		"environment": "production",
	}})
	if err == nil || faults.ReasonOf(err) != "durable_database_required" {
		t.Fatalf("err=%v", err)
	}
}

func TestCrossFieldValidationRejectsDrainBeyondShutdown(t *testing.T) {
	_, err := Load(context.Background(), "control-plane-api", foundationconfig.MapSource{Values: map[string]string{
		"drain.timeout":    "40s",
		"shutdown.timeout": "30s",
	}})
	if err == nil || faults.ReasonOf(err) != "drain_exceeds_shutdown" {
		t.Fatalf("err=%v", err)
	}
}
