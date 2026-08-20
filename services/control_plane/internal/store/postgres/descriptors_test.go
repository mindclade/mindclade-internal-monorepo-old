// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"go.mindclade.dev/control/registry/models"
	"go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/storage/sql/sqltest"
)

var writeTime = time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)

// sealedDescriptor returns a valid descriptor with its digest sealed, which is
// the only state the repository accepts.
func sealedDescriptor(t *testing.T) models.Descriptor {
	t.Helper()
	descriptor := models.Descriptor{
		ModelID:               "model_0000000003e870008000000000000000",
		Family:                "text",
		Version:               "1.4.0",
		Lifecycle:             models.LifecycleServing,
		ModelBundleDigest:     identifiers.SHA256String("model-bundle"),
		EngineBundleDigest:    identifiers.SHA256String("engine-bundle"),
		ResolvedConfigDigest:  identifiers.SHA256String("resolved-config"),
		KernelManifestDigest:  identifiers.SHA256String("kernel-manifest"),
		SafetyPolicyDigest:    identifiers.SHA256String("safety-policy"),
		Capabilities:          []string{"chat", "completion"},
		AcceleratorCapability: "sm_90",
		MinimumRuntimeVersion: "2026.8.0",
		SchemaVersion:         1,
		PolicyEpoch:           7,
		Created:               writeTime,
		Expires:               writeTime.Add(720 * time.Hour),
		CompatibilityClasses: []models.CompatibilityClass{{
			ClassID:              "forward-bf16-short",
			ExecutionKind:        models.ExecutionForward,
			Precision:            models.PrecisionBF16,
			ShapeBucket:          "0-2048",
			MaximumBatchRequests: 32,
			MaximumBatchGPUBytes: 8 << 30,
			MaximumInputUnits:    2048,
			MaximumOutputUnits:   1024,
		}},
		Envelope: models.ResourceEnvelope{
			WeightsResidentBytes:      12 << 30,
			HostMemoryBytes:           8 << 30,
			GPUMemoryFloorBytes:       16 << 30,
			GPUMemoryPerRequestBytes:  256 << 20,
			MaximumConcurrentRequests: 32,
			LoadDeadline:              90 * time.Second,
			DrainDeadline:             30 * time.Second,
		},
	}
	if err := descriptor.SealDigest(); err != nil {
		t.Fatalf("descriptor fixture is not sealable: %v", err)
	}
	return descriptor
}

func newStore(t *testing.T, state *sqltest.State, options ...Option) (*Store, *sql.DB) {
	t.Helper()
	db, err := sqltest.Open(state)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := New(db, append([]Option{WithClock(clock.NewFake(writeTime))}, options...)...)
	if err != nil {
		t.Fatal(err)
	}
	return store, db
}

func TestPutDescriptorInsertsWhenAbsent(t *testing.T) {
	t.Parallel()
	descriptor := sealedDescriptor(t)
	state := &sqltest.State{Exec: func(_ context.Context, query string, arguments []driver.NamedValue) (driver.Result, error) {
		if !strings.Contains(query, "ON CONFLICT (descriptor_digest) DO NOTHING") {
			t.Fatalf("query=%q", query)
		}
		if len(arguments) != 13 {
			t.Fatalf("args=%d", len(arguments))
		}
		if got, want := arguments[0].Value, descriptor.DescriptorDigest.String(); got != want {
			t.Fatalf("digest=%v want=%v", got, want)
		}
		return driver.RowsAffected(1), nil
	}}
	store, _ := newStore(t, state)
	if err := store.PutDescriptor(context.Background(), descriptor); err != nil {
		t.Fatal(err)
	}
	if state.Queries.Load() != 0 {
		t.Fatalf("a successful insert read the table back %d times", state.Queries.Load())
	}
}

// Republishing byte-identical content is what makes models.Service.Publish
// safe to retry. It must not surface as a conflict.
func TestPutDescriptorIsIdempotentForIdenticalContent(t *testing.T) {
	t.Parallel()
	descriptor := sealedDescriptor(t)
	document, err := json.Marshal(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	state := &sqltest.State{
		Exec: func(context.Context, string, []driver.NamedValue) (driver.Result, error) {
			return driver.RowsAffected(0), nil
		},
		Query: func(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
			if !strings.Contains(query, "SELECT content_digest") {
				t.Fatalf("query=%q", query)
			}
			return sqltest.NewRows([]string{"content_digest"},
				[]driver.Value{identifiers.SHA256(document).String()}), nil
		},
	}
	store, _ := newStore(t, state)
	if err := store.PutDescriptor(context.Background(), descriptor); err != nil {
		t.Fatalf("identical republish was rejected: %v", err)
	}
}

// A digest that is already stored against different content means the seal no
// longer identifies the descriptor. That is never a retryable race.
func TestPutDescriptorRefusesADigestCollision(t *testing.T) {
	t.Parallel()
	state := &sqltest.State{
		Exec: func(context.Context, string, []driver.NamedValue) (driver.Result, error) {
			return driver.RowsAffected(0), nil
		},
		Query: func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
			return sqltest.NewRows([]string{"content_digest"},
				[]driver.Value{identifiers.SHA256String("something-else").String()}), nil
		},
	}
	store, _ := newStore(t, state)
	err := store.PutDescriptor(context.Background(), sealedDescriptor(t))
	if err == nil {
		t.Fatal("a digest collision was accepted")
	}
	if reason := faults.ReasonOf(err); reason != "model_descriptor_digest_collision" {
		t.Fatalf("reason=%s", reason)
	}
	if faults.CodeOf(err) != faults.CodeFailedPrecondition {
		t.Fatalf("code=%v", faults.CodeOf(err))
	}
	if policy := faults.RetryPolicyOf(err); policy.Retryable() {
		t.Fatal("a digest collision was marked retryable")
	}
}

// An unsealed descriptor must not reach storage: the digest is the primary key,
// so storing one that does not describe its content would make the row
// unresolvable.
func TestPutDescriptorRefusesAnUnsealedDescriptor(t *testing.T) {
	t.Parallel()
	descriptor := sealedDescriptor(t)
	descriptor.Lifecycle = models.LifecycleRevoked // digest now describes the old content
	state := &sqltest.State{Exec: func(context.Context, string, []driver.NamedValue) (driver.Result, error) {
		t.Fatal("an unsealed descriptor reached the database")
		return nil, nil
	}}
	store, _ := newStore(t, state)
	err := store.PutDescriptor(context.Background(), descriptor)
	if err == nil {
		t.Fatal("an unsealed descriptor was accepted")
	}
	if reason := faults.ReasonOf(err); reason != "model_descriptor_unsealed" {
		t.Fatalf("reason=%s", reason)
	}
}

// A lifecycle change reseals to a different digest, so it is a different row.
// This is the fact that makes the repository insert-only, and it is worth
// pinning: if SealDigest ever stopped covering Lifecycle, an in-place update
// would become necessary and this repository would silently start losing
// transitions.
func TestLifecycleChangeProducesADistinctIdentity(t *testing.T) {
	t.Parallel()
	serving := sealedDescriptor(t)
	deprecated := sealedDescriptor(t)
	deprecated.Lifecycle = models.LifecycleDeprecated
	if err := deprecated.SealDigest(); err != nil {
		t.Fatal(err)
	}
	if serving.DescriptorDigest.Equal(deprecated.DescriptorDigest) {
		t.Fatal("lifecycle is not covered by the sealed digest; the store must handle updates")
	}
}

func TestGetDescriptorRoundTripsEveryField(t *testing.T) {
	t.Parallel()
	descriptor := sealedDescriptor(t)
	document, err := json.Marshal(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	state := &sqltest.State{Query: func(_ context.Context, query string, arguments []driver.NamedValue) (driver.Rows, error) {
		if !strings.Contains(query, "SELECT document") || len(arguments) != 1 {
			t.Fatalf("query=%q args=%d", query, len(arguments))
		}
		return sqltest.NewRows([]string{"document"}, []driver.Value{document}), nil
	}}
	store, _ := newStore(t, state)
	loaded, err := store.GetDescriptor(context.Background(), descriptor.DescriptorDigest)
	if err != nil {
		t.Fatal(err)
	}
	// VerifyDigest is the real assertion: it recomputes the canonical encoding
	// from every field, so a round trip that dropped or altered one fails here
	// rather than needing a field-by-field comparison.
	if err := loaded.VerifyDigest(); err != nil {
		t.Fatalf("stored descriptor did not survive the round trip: %v", err)
	}
	if !loaded.DescriptorDigest.Equal(descriptor.DescriptorDigest) {
		t.Fatal("round trip changed the descriptor identity")
	}
}

func TestGetDescriptorReportsAbsenceAsNotFound(t *testing.T) {
	t.Parallel()
	state := &sqltest.State{Query: func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
		return sqltest.NewRows([]string{"document"}), nil
	}}
	store, _ := newStore(t, state)
	_, err := store.GetDescriptor(context.Background(), identifiers.SHA256String("absent"))
	if err == nil {
		t.Fatal("a missing descriptor returned a zero value")
	}
	if faults.CodeOf(err) != faults.CodeNotFound {
		t.Fatalf("code=%v", faults.CodeOf(err))
	}
	if reason := faults.ReasonOf(err); reason != "model_descriptor_not_found" {
		t.Fatalf("reason=%s", reason)
	}
}

func TestGetDescriptorRefusesAnInvalidDigest(t *testing.T) {
	t.Parallel()
	state := &sqltest.State{Query: func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
		t.Fatal("an invalid digest reached the database")
		return nil, nil
	}}
	store, _ := newStore(t, state)
	if _, err := store.GetDescriptor(context.Background(), identifiers.Digest{}); err == nil {
		t.Fatal("an invalid digest was accepted")
	}
}

// The adapters must never open their own transaction: a repository that did
// could not be composed into the mutation-and-publication commit.
func TestDescriptorWritesJoinTheCallersTransaction(t *testing.T) {
	t.Parallel()
	state := &sqltest.State{Exec: func(context.Context, string, []driver.NamedValue) (driver.Result, error) {
		return driver.RowsAffected(1), nil
	}}
	store, _ := newStore(t, state)
	if err := store.PutDescriptor(context.Background(), sealedDescriptor(t)); err != nil {
		t.Fatal(err)
	}
	if state.Begins.Load() != 0 || state.Commits.Load() != 0 {
		t.Fatalf("the store opened its own transaction: begins=%d commits=%d",
			state.Begins.Load(), state.Commits.Load())
	}
}
