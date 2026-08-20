// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/lib/pq"

	"go.mindclade.dev/control/registry/models"
	"go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/faults"
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
	statements, err := DDL(descriptorTable, releaseTable, graphTable)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("apply registry DDL: %v", err)
		}
	}
	store, err := New(db,
		WithClock(clock.RealClock{}),
		WithDescriptorTable(descriptorTable),
		WithReleaseTable(releaseTable),
		WithEvidenceGraphTable(graphTable),
	)
	if err != nil {
		t.Fatal(err)
	}
	return store, db
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
	err = transaction.RunVoid(context.Background(), db, transaction.Options{Isolation: sql.LevelSerializable},
		func(ctx context.Context, _ *sql.Tx) error {
			if err := store.PutGraph(ctx, graph); err != nil {
				return err
			}
			return store.PutRelease(ctx, release)
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
