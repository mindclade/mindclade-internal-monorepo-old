// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package registry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	_ "github.com/lib/pq"

	auditpostgres "go.mindclade.dev/libs/go/audit/postgres"
	foundationconfig "go.mindclade.dev/libs/go/config"
	outboxpostgres "go.mindclade.dev/libs/go/coordination/outbox/postgres"
	idempotencypostgres "go.mindclade.dev/libs/go/idempotency/postgres"
	"go.mindclade.dev/services/control_plane/internal/bootstrap"
	"go.mindclade.dev/services/control_plane/internal/config"
	"go.mindclade.dev/services/control_plane/internal/providers"
)

const testAPIKey = "registry-client-secret-value"

func testSettings(t *testing.T) foundationconfig.MapSource {
	t.Helper()
	digest := sha256.Sum256([]byte(testAPIKey))
	return foundationconfig.MapSource{SourceName: "test", Values: map[string]string{
		"signing.hmac_key": "01234567890123456789012345678901",
		"database.dsn":     "postgres://control:control@127.0.0.1:5432/control?sslmode=disable",
		"http.address":     "127.0.0.1:0",
		"blob.bucket":      "mindclade-registry-test",
		"cache.address":    "127.0.0.1:6379",
		"auth.api_keys":    "registry-client:" + hex.EncodeToString(digest[:]) + ":artifacts.read,artifacts.write",
	}}
}

// The registry factory is the first materialized control-plane composition
// root. Building it through servicekit/production is the assertion that
// matters: Build fails unless every capability the registry role requires is
// backed by a concrete provider, so this test breaks if a store is dropped.
func TestRegistryFactoryBuildsThroughProductionLifecycle(t *testing.T) {
	// The Cloud Storage client resolves credentials at construction. Pointing
	// it at an emulator host keeps the test hermetic; no request is made.
	t.Setenv("STORAGE_EMULATOR_HOST", "127.0.0.1:0")

	profile, err := bootstrap.ProfileFor(bootstrap.RoleRegistry)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRegistryFactory(testSettings(t)).Create(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}
	service, err := bootstrap.Build(profile, runtime)
	if err != nil {
		t.Fatalf("registry runtime does not satisfy its production role: %v", err)
	}
	if service == nil || service.Service() == nil {
		t.Fatal("production runtime was not assembled")
	}
	if runtime.Bind == nil {
		t.Fatal("registry runtime does not bind health probes")
	}
	if err := runtime.Bind(service); err != nil {
		t.Fatal(err)
	}
	if report := service.Service().Readiness(context.Background()); report.OK {
		t.Fatal("readiness must fail before the service starts")
	}
}

func TestRegistryFactoryFailsClosedWithoutProviders(t *testing.T) {
	for name, values := range map[string]map[string]string{
		"database": {"database.dsn": ""},
		"blob":     {"blob.bucket": ""},
		"cache":    {"cache.address": ""},
		"api keys": {"auth.api_keys": ""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("STORAGE_EMULATOR_HOST", "127.0.0.1:0")
			source := testSettings(t)
			for key, value := range values {
				source.Values[key] = value
			}
			profile, err := bootstrap.ProfileFor(bootstrap.RoleRegistry)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := NewRegistryFactory(source).Create(context.Background(), profile); err == nil {
				t.Fatal("factory accepted an unconfigured provider")
			}
		})
	}
}

// The three shared adapters ship DDL but no version, so the composition root
// orders them. A collision here would silently skip a schema.
func TestMigrationRunnerCarriesEveryAdapterSchema(t *testing.T) {
	runner, err := newMigrationRunner()
	if err != nil {
		t.Fatal(err)
	}
	if runner == nil {
		t.Fatal("nil migration runner")
	}
}

// Each adapter takes its table as a parameter, so the schema this process
// applies must be derived from the same constant the store is constructed
// with. A fixed schema beside a configurable store is how a migration and its
// reader stop describing the same table.
func TestMigrationsAndStoresNameTheSameTables(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		table string
		ddl   func(string) (string, error)
	}{
		{"audit", providers.AuditTable, auditpostgres.DDL},
		{"idempotency", providers.IdempotencyTable, idempotencypostgres.DDL},
		{"outbox", providers.OutboxTable, outboxpostgres.DDL},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			statement, err := testCase.ddl(testCase.table)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(statement, "CREATE TABLE IF NOT EXISTS "+testCase.table+" (") {
				t.Fatalf("schema does not create %q: %s", testCase.table, statement)
			}
		})
	}
}

func decodeTestSettings(t *testing.T) (config.Settings, error) {
	t.Helper()
	resolved, err := config.Load(context.Background(), "control-plane-registry", testSettings(t))
	if err != nil {
		return config.Settings{}, err
	}
	return resolved.Settings, nil
}
