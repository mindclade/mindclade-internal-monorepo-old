// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package postgres

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/lib/pq"

	"go.mindclade.dev/control/evidence"
	"go.mindclade.dev/control/registry/models"
	"go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/signing"
	"go.mindclade.dev/libs/go/storage/lease"
	leasepostgres "go.mindclade.dev/libs/go/storage/lease/postgres"
	"go.mindclade.dev/libs/go/storage/sql/transaction"
)

const livePostgresEnvironment = "MINDCLADE_TEST_POSTGRES_DSN"

var liveSchemaSequence atomic.Uint64

func livePostgresStore(t *testing.T) (*Store, *sql.DB) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv(livePostgresEnvironment))
	if dsn == "" {
		t.Skipf("%s is not set; live PostgreSQL qualification is opt-in", livePostgresEnvironment)
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		t.Fatalf("connect to live PostgreSQL: %v", err)
	}

	schema := fmt.Sprintf("mc_registry_qual_%d_%d", os.Getpid(), liveSchemaSequence.Add(1))
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	// Cleanup uses a fresh handle because the database-loss scenario closes the
	// pool under test deliberately.
	t.Cleanup(func() {
		cleanup, openErr := sql.Open("postgres", dsn)
		if openErr == nil {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
			_, _ = cleanup.ExecContext(cleanupCtx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
			cleanupCancel()
			_ = cleanup.Close()
		}
		_ = db.Close()
	})

	descriptorTable := schema + ".descriptors"
	releaseTable := schema + ".releases"
	graphTable := schema + ".graphs"
	claimTable := schema + ".claims"
	verificationTable := schema + ".verifications"
	decisionTable := schema + ".decisions"
	revocationTable := schema + ".revocations"
	artifactIdentityTable := schema + ".artifact_identities"
	artifactLocationTable := schema + ".artifact_locations"
	lineageGraphTable := schema + ".lineage_graphs"
	statements, err := DDL(descriptorTable, releaseTable, graphTable)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("apply registry DDL: %v", err)
		}
	}
	evidenceStatements, err := EvidenceLedgerDDL(claimTable, verificationTable, decisionTable, revocationTable)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range evidenceStatements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("apply evidence ledger DDL: %v", err)
		}
	}
	artifactStatements, err := ArtifactCatalogDDL(artifactIdentityTable, artifactLocationTable)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range artifactStatements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("apply artifact catalog DDL: %v", err)
		}
	}
	lineageStatement, err := LineageGraphDDL(lineageGraphTable)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, lineageStatement); err != nil {
		t.Fatalf("apply lineage graph DDL: %v", err)
	}
	store, err := New(db,
		WithClock(clock.RealClock{}),
		WithLineageGraphTable(lineageGraphTable),
		WithArtifactIdentityTable(artifactIdentityTable),
		WithArtifactLocationTable(artifactLocationTable),
		WithDescriptorTable(descriptorTable),
		WithReleaseTable(releaseTable),
		WithEvidenceGraphTable(graphTable),
		WithEvidenceClaimTable(claimTable),
		WithEvidenceVerificationTable(verificationTable),
		WithEligibilityDecisionTable(decisionTable),
		WithEligibilityRevocationTable(revocationTable),
	)
	if err != nil {
		t.Fatal(err)
	}
	return store, db
}

func TestLivePostgresEvidenceLedgerRoundTrip(t *testing.T) {
	store, _ := livePostgresStore(t)
	now := time.Now().Round(0).UTC()
	subject := identifiers.SHA256String("deployment-bundle")
	claim := evidence.Claim{
		SchemaVersion: evidence.SchemaClaimV1, SubjectKind: "deployment_bundle", SubjectDigest: subject,
		ControlID: "source_ci", Owner: "platform", Scope: "production",
		Artifact:        evidence.Artifact{URI: "gs://evidence/source-ci.json", Digest: identifiers.SHA256String("source-ci"), MediaType: "application/json"},
		SourceAuthority: "gitops", IssuedAt: now, ValidUntil: now.Add(time.Hour),
	}
	if err := claim.Seal(); err != nil {
		t.Fatal(err)
	}
	verification := evidence.Verification{
		SchemaVersion: evidence.SchemaVerificationV1, ClaimDigest: claim.ClaimDigest,
		PolicyDigest: identifiers.SHA256String("policy"), PolicyEpoch: 1, Result: evidence.ResultPass,
		Reasons: []string{"verified"}, Artifact: claim.Artifact, VerifiedAt: now, ValidUntil: now.Add(time.Hour),
	}
	if err := verification.Seal(); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendClaim(context.Background(), claim, verification); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendClaim(context.Background(), claim, verification); err != nil {
		t.Fatalf("idempotent evidence append: %v", err)
	}
	resolvedClaim, resolvedVerification, err := store.Current(context.Background(), subject, claim.ControlID)
	if err != nil {
		t.Fatal(err)
	}
	if !resolvedClaim.ClaimDigest.Equal(claim.ClaimDigest) || !resolvedVerification.VerificationDigest.Equal(verification.VerificationDigest) {
		t.Fatal("evidence ledger returned different sealed records")
	}

	decision := sealedSignedDecision(t, subject, verification.PolicyDigest, now)
	if err := store.PutDecision(context.Background(), decision); err != nil {
		t.Fatal(err)
	}
	if err := store.Revoke(context.Background(), decision.Decision.DecisionDigest, "artifact_compromised", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	resolvedDecision, revoked, err := store.Decision(context.Background(), decision.Decision.DecisionDigest)
	if err != nil {
		t.Fatal(err)
	}
	if !revoked || !resolvedDecision.Decision.DecisionDigest.Equal(decision.Decision.DecisionDigest) {
		t.Fatal("signed decision or revocation did not round trip")
	}
}

func sealedSignedDecision(t *testing.T, bundleDigest, policyDigest identifiers.Digest, now time.Time) evidence.SignedDecision {
	t.Helper()
	decision := evidence.Decision{
		SchemaVersion: evidence.SchemaDecisionV1, BundleDigest: bundleDigest, PolicyDigest: policyDigest, PolicyEpoch: 1,
		Result: evidence.ResultEligible, EvaluatedAt: now, ExpiresAt: now.Add(time.Hour),
	}
	if err := decision.Seal(); err != nil {
		t.Fatal(err)
	}
	_, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	keyID, err := signing.ParseKeyID("production-eligibility-v1")
	if err != nil {
		t.Fatal(err)
	}
	signer, err := signing.NewEd25519Signer(keyID, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := decision.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	signature, err := signer.Sign(context.Background(), payload)
	if err != nil {
		t.Fatal(err)
	}
	return evidence.SignedDecision{Decision: decision, Signature: signature}
}

func TestLivePostgresRegistryRoundTrip(t *testing.T) {
	store, db := livePostgresStore(t)
	descriptor := sealedDescriptor(t)
	if err := store.PutDescriptor(context.Background(), descriptor); err != nil {
		t.Fatal(err)
	}
	// Insert-if-absent must be idempotent against the server's real conflict
	// and RowsAffected behavior, not only the sqltest driver.
	if err := store.PutDescriptor(context.Background(), descriptor); err != nil {
		t.Fatalf("idempotent republish: %v", err)
	}
	resolved, err := store.GetDescriptor(context.Background(), descriptor.DescriptorDigest)
	if err != nil {
		t.Fatal(err)
	}
	if !resolved.DescriptorDigest.Equal(descriptor.DescriptorDigest) || resolved.ModelID != descriptor.ModelID {
		t.Fatalf("resolved descriptor = %+v", resolved)
	}

	graph := evidenceGraph(t)
	release := qualifiedRelease(t)
	release.Channel = "candidate"
	if err := store.PutRelease(context.Background(), release); err != nil {
		t.Fatal(err)
	}
	release.ResourceVersion = 1
	err = transaction.RunVoid(context.Background(), db, transaction.Options{Isolation: sql.LevelSerializable},
		func(ctx context.Context, _ *sql.Tx) error {
			if err := store.PutGraph(ctx, graph); err != nil {
				return err
			}
			return store.PromoteRelease(ctx, release)
		})
	if err != nil {
		t.Fatal(err)
	}
	for table, want := range map[string]int64{store.EvidenceGraphTable(): 1, store.ReleaseTable(): 1} {
		var count int64
		if err := db.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != want {
			t.Fatalf("%s count=%d, want %d", table, count, want)
		}
	}
	var status string
	var version int64
	if err := db.QueryRow(
		"SELECT status, resource_version FROM "+store.ReleaseTable()+" WHERE release_id=$1",
		release.ReleaseID,
	).Scan(&status, &version); err != nil {
		t.Fatal(err)
	}
	if status != "promoted" || version != 2 {
		t.Fatalf("promoted release status=%q version=%d", status, version)
	}
}

func TestLivePostgresRollbackLeavesNoPartialEvidence(t *testing.T) {
	store, db := livePostgresStore(t)
	sentinel := errors.New("injected after graph write")
	err := transaction.RunVoid(context.Background(), db, transaction.Options{Isolation: sql.LevelSerializable},
		func(ctx context.Context, _ *sql.Tx) error {
			if err := store.PutGraph(ctx, evidenceGraph(t)); err != nil {
				return err
			}
			return sentinel
		})
	if !errors.Is(err, sentinel) {
		t.Fatalf("transaction error = %v, want injected failure", err)
	}
	var count int64
	if err := db.QueryRow("SELECT count(*) FROM " + store.EvidenceGraphTable()).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("partial evidence rows after rollback = %d", count)
	}
}

func TestLivePostgresDatabaseLossIsQualified(t *testing.T) {
	store, db := livePostgresStore(t)
	descriptor := sealedDescriptor(t)
	if err := store.PutDescriptor(context.Background(), descriptor); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	_, err := store.GetDescriptor(context.Background(), descriptor.DescriptorDigest)
	if !faults.IsCode(err, faults.CodeUnavailable) || !faults.RetryPolicyOf(err).Retryable() {
		t.Fatalf("database-loss error = %v; code=%s retry=%+v", err, faults.CodeOf(err), faults.RetryPolicyOf(err))
	}
}

func TestLivePostgresLeaseLossRejectsStaleOwner(t *testing.T) {
	_, db := livePostgresStore(t)
	table := fmt.Sprintf("mc_lease_qual_%d_%d", os.Getpid(), liveSchemaSequence.Add(1))
	ddl, err := leasepostgres.DDL(table)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ddl); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Exec("DROP TABLE IF EXISTS " + table) })
	store, err := leasepostgres.New(db, table)
	if err != nil {
		t.Fatal(err)
	}
	key := lease.MustParseKey("qualification/lease-loss")
	stale, err := store.Acquire(context.Background(), lease.AcquireRequest{Key: key, Owner: "first", TTL: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(40 * time.Millisecond)
	current, err := store.Acquire(context.Background(), lease.AcquireRequest{Key: key, Owner: "second", TTL: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if current.Version <= stale.Version {
		t.Fatalf("replacement version=%d, stale=%d", current.Version, stale.Version)
	}
	if _, err := store.Renew(context.Background(), stale, time.Second); !errors.Is(err, lease.ErrStale) {
		t.Fatalf("stale owner renewed after lease loss: %v", err)
	}
}

var _ models.Repository = (*Store)(nil)
