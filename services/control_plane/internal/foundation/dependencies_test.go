// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package foundation

import (
	"context"
	"testing"
	"time"

	"mindclade.internal/libs/go/audit"
	"mindclade.internal/libs/go/auth"
	mcclock "mindclade.internal/libs/go/clock"
	foundationconfig "mindclade.internal/libs/go/config"
	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/identifiers"
	"mindclade.internal/libs/go/messaging"
	"mindclade.internal/libs/go/pagination"
	"mindclade.internal/libs/go/retry"
	"mindclade.internal/libs/go/servicekit/production"
	"mindclade.internal/libs/go/signing"
	"mindclade.internal/libs/go/storage/sql/migrate"
)

func TestCapabilitiesAreDeterministic(t *testing.T) {
	retryExecutor, err := retry.NewExecutor(retry.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	generator, err := identifiers.NewGenerator()
	if err != nil {
		t.Fatal(err)
	}
	configuration := testConfiguration(t)
	signer, verifier, codec := testSigning(t)
	dependencies := Dependencies{
		Clock:                      mcclock.RealClock{},
		Configuration:              configuration,
		IDs:                        generator,
		RequestMetadataConfigured:  true,
		ResourceVersionsConfigured: true,
		Signer:                     signer,
		Verifier:                   verifier,
		Pagination:                 codec,
		MessagingPublisher: messaging.PublisherFunc(func(context.Context, messaging.Message) (messaging.Publication, error) {
			return messaging.Publication{}, nil
		}),
		Authenticator: auth.AuthenticatorFunc(func(context.Context, auth.Credential) (auth.Principal, error) {
			return auth.Principal{}, auth.ErrUnauthenticated
		}),
		Authorizer: auth.PermissionAuthorizer{},
		Audit:      audit.NopRecorder{},
		Retry:      retryExecutor,
	}
	first := dependencies.Capabilities()
	second := dependencies.Capabilities()
	if len(first) != len(second) {
		t.Fatalf("capabilities changed: %v vs %v", first, second)
	}
	for index := range first {
		if first[index] != second[index] {
			t.Fatalf("capability order changed: %v vs %v", first, second)
		}
	}
	if err := dependencies.Validate(
		production.CapabilityClock,
		production.CapabilityConfiguration,
		production.CapabilityIdentifiers,
		production.CapabilityRequestMetadata,
		production.CapabilityResourceVersion,
		production.CapabilitySigning,
		production.CapabilityPagination,
		production.CapabilityMessaging,
		production.CapabilityAuthentication,
		production.CapabilityAuthorization,
		production.CapabilityAudit,
		production.CapabilityRetry,
	); err != nil {
		t.Fatal(err)
	}
}

func TestMissingCapabilityFailsClosed(t *testing.T) {
	if err := (Dependencies{}).Validate(production.CapabilityDatabase); err == nil {
		t.Fatal("expected missing database capability")
	}
}

func TestMigrationRunnerWithoutDatabaseFailsClosed(t *testing.T) {
	manifest, err := migrate.NewManifest(migrate.Migration{Version: 1, Name: "initial", Up: "SELECT 1"})
	if err != nil {
		t.Fatal(err)
	}
	runner, err := migrate.NewRunner(manifest, migrate.Options{})
	if err != nil {
		t.Fatal(err)
	}
	builder, err := production.NewBuilder("test", production.RoleMaintenance)
	if err != nil {
		t.Fatal(err)
	}
	if err := (Dependencies{Migrations: runner}).Register(builder); err == nil || faults.ReasonOf(err) != "migrations_without_database" {
		t.Fatalf("err=%v", err)
	}
}

func testConfiguration(t *testing.T) *foundationconfig.Atomic {
	t.Helper()
	loader, err := foundationconfig.New([]foundationconfig.Field{{Key: "service.name", Required: true}}, foundationconfig.MapSource{Values: map[string]string{"service.name": "test"}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := loader.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	atomic, err := foundationconfig.NewAtomic(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return atomic
}

func testSigning(t *testing.T) (signing.Signer, signing.Verifier, *pagination.Codec) {
	t.Helper()
	keyID := signing.MustParseKeyID("test/control-plane")
	key := []byte("01234567890123456789012345678901")
	signer, err := signing.NewHMACSigner(keyID, key)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := signing.NewKeySet(mcclock.RealClock{}, signing.VerificationKey{ID: keyID, Algorithm: signing.AlgorithmHMACSHA256, HMACKey: key})
	if err != nil {
		t.Fatal(err)
	}
	codec, err := pagination.NewCodec(signer, verifier, mcclock.RealClock{}, 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return signer, verifier, codec
}
