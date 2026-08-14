// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package postgres

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/storage/lease"
	"mindclade.internal/libs/go/storage/sql/sqltest"
)

var leaseColumns = []string{"lease_key", "token", "owner", "version", "acquired_at", "expires_at"}

func testToken(t *testing.T) lease.Token {
	t.Helper()
	token, err := lease.ParseToken("550e8400-e29b-41d4-a716-446655440000")
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func testLeaseRow(t *testing.T, version int64) []driver.Value {
	t.Helper()
	acquired := time.Date(2026, 8, 12, 12, 0, 0, 0, time.FixedZone("test", -4*60*60))
	return []driver.Value{"runs/example", testToken(t).String(), "worker-a", version, acquired, acquired.Add(time.Minute)}
}

func openStore(t *testing.T, state *sqltest.State, options ...Option) *Store {
	t.Helper()
	database, err := sqltest.Open(state)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	store, err := New(database, "mindclade.leases", options...)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestDDLValidation(t *testing.T) {
	ddl, err := DDL("mindclade.leases")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ddl, "mindclade.leases") || !strings.Contains(ddl, "leases_expires_at_idx") {
		t.Fatalf("DDL = %s", ddl)
	}
	for _, table := range []string{"leases; DROP TABLE runs", "one.two.three", strings.Repeat("x", 64)} {
		if _, err := DDL(table); err == nil {
			t.Fatalf("DDL(%q) accepted unsafe table", table)
		}
	}
	longTable := "t" + strings.Repeat("x", 62)
	name := indexName(longTable)
	if len(name) > maximumPostgresIdentifierBytes || !strings.HasSuffix(name, "_expires_at_idx") {
		t.Fatalf("indexName() = %q", name)
	}
}

func TestAcquireUsesDatabaseClock(t *testing.T) {
	state := &sqltest.State{}
	state.Query = func(_ context.Context, query string, arguments []driver.NamedValue) (driver.Rows, error) {
		if !strings.Contains(query, "clock_timestamp()") || strings.Contains(query, "$5") {
			t.Fatalf("query does not use authoritative database time: %s", query)
		}
		if len(arguments) != 4 || arguments[0].Value != "runs/example" || arguments[2].Value != "worker-a" || arguments[3].Value != int64(60000000) {
			t.Fatalf("arguments = %#v", arguments)
		}
		return sqltest.NewRows(leaseColumns, testLeaseRow(t, 1)), nil
	}
	store := openStore(t, state, WithTokenGenerator(func() (lease.Token, error) { return testToken(t), nil }))
	value, err := store.Acquire(context.Background(), lease.AcquireRequest{Key: lease.MustParseKey("runs/example"), Owner: "worker-a", TTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if value.Version != 1 || value.AcquiredAt.Location() != time.UTC || value.ExpiresAt.Location() != time.UTC {
		t.Fatalf("lease = %#v", value)
	}
}

func TestAcquireHeldUsesDatabaseRemainingTime(t *testing.T) {
	var calls atomic.Int64
	state := &sqltest.State{}
	state.Query = func(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
		switch calls.Add(1) {
		case 1:
			return sqltest.NewRows(leaseColumns), nil
		case 2:
			if !strings.Contains(query, "expires_at - clock_timestamp()") {
				t.Fatalf("lookup query = %s", query)
			}
			columns := append(append([]string(nil), leaseColumns...), "remaining_ms")
			row := append(testLeaseRow(t, 2), int64(1500))
			return sqltest.NewRows(columns, row), nil
		default:
			t.Fatalf("unexpected query call")
			return nil, nil
		}
	}
	store := openStore(t, state, WithTokenGenerator(func() (lease.Token, error) { return testToken(t), nil }))
	_, err := store.Acquire(context.Background(), lease.AcquireRequest{Key: lease.MustParseKey("runs/example"), Owner: "worker-b", TTL: time.Minute})
	if !faults.IsCode(err, faults.CodeConflict) || faults.RetryPolicyOf(err).After != 1500*time.Millisecond {
		t.Fatalf("error = %v, retry=%#v", err, faults.RetryPolicyOf(err))
	}
}

func TestAcquireRaceIsRetryable(t *testing.T) {
	state := &sqltest.State{Query: func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
		return sqltest.NewRows(leaseColumns), nil
	}}
	store := openStore(t, state, WithTokenGenerator(func() (lease.Token, error) { return testToken(t), nil }))
	_, err := store.Acquire(context.Background(), lease.AcquireRequest{Key: lease.MustParseKey("runs/example"), Owner: "worker", TTL: time.Second})
	if !faults.IsCode(err, faults.CodeAborted) || !faults.IsRetryable(err) {
		t.Fatalf("error = %v", err)
	}
}

func TestRenew(t *testing.T) {
	current := lease.Lease{
		Key:        lease.MustParseKey("runs/example"),
		Token:      testToken(t),
		Owner:      "worker-a",
		Version:    1,
		AcquiredAt: time.Unix(100, 0),
		ExpiresAt:  time.Unix(200, 0),
	}
	t.Run("success", func(t *testing.T) {
		state := &sqltest.State{Query: func(_ context.Context, query string, arguments []driver.NamedValue) (driver.Rows, error) {
			if !strings.Contains(query, "clock_timestamp()") || len(arguments) != 4 || arguments[3].Value != int64(1) {
				t.Fatalf("query=%s arguments=%#v", query, arguments)
			}
			return sqltest.NewRows(leaseColumns, testLeaseRow(t, 2)), nil
		}}
		store := openStore(t, state)
		value, err := store.Renew(context.Background(), current, time.Microsecond)
		if err != nil || value.Version != 2 {
			t.Fatalf("Renew() = %#v, %v", value, err)
		}
	})
	t.Run("stale", func(t *testing.T) {
		state := &sqltest.State{Query: func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
			return sqltest.NewRows(leaseColumns), nil
		}}
		store := openStore(t, state)
		_, err := store.Renew(context.Background(), current, time.Second)
		if !faults.IsCode(err, faults.CodeConflict) {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestLookup(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		state := &sqltest.State{Query: func(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
			if !strings.Contains(query, "expires_at > clock_timestamp()") {
				t.Fatalf("query = %s", query)
			}
			return sqltest.NewRows(leaseColumns, testLeaseRow(t, 1)), nil
		}}
		store := openStore(t, state)
		value, err := store.Lookup(context.Background(), lease.MustParseKey("runs/example"))
		if err != nil || value.Version != 1 {
			t.Fatalf("Lookup() = %#v, %v", value, err)
		}
	})
	t.Run("missing", func(t *testing.T) {
		state := &sqltest.State{Query: func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
			return sqltest.NewRows(leaseColumns), nil
		}}
		store := openStore(t, state)
		_, err := store.Lookup(context.Background(), lease.MustParseKey("runs/example"))
		if !faults.IsCode(err, faults.CodeNotFound) {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestRelease(t *testing.T) {
	current := lease.Lease{Key: lease.MustParseKey("runs/example"), Token: testToken(t), Owner: "worker-a", Version: 1, AcquiredAt: time.Unix(100, 0), ExpiresAt: time.Unix(200, 0)}
	for _, affected := range []int64{1, 0} {
		t.Run(string(rune('0'+affected)), func(t *testing.T) {
			state := &sqltest.State{Exec: func(_ context.Context, query string, arguments []driver.NamedValue) (driver.Result, error) {
				if !strings.Contains(query, "token = $2 AND version = $3") || len(arguments) != 3 {
					t.Fatalf("query=%s arguments=%#v", query, arguments)
				}
				return driver.RowsAffected(affected), nil
			}}
			store := openStore(t, state)
			err := store.Release(context.Background(), current)
			if affected == 1 && err != nil {
				t.Fatal(err)
			}
			if affected == 0 && !faults.IsCode(err, faults.CodeConflict) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestTokenGeneratorFailure(t *testing.T) {
	sentinel := errors.New("entropy unavailable")
	state := &sqltest.State{}
	store := openStore(t, state, WithTokenGenerator(func() (lease.Token, error) { return lease.Token{}, sentinel }))
	_, err := store.Acquire(context.Background(), lease.AcquireRequest{Key: lease.MustParseKey("runs/example"), Owner: "worker", TTL: time.Second})
	if !errors.Is(err, sentinel) || !faults.IsCode(err, faults.CodeInternal) {
		t.Fatalf("error = %v", err)
	}
}

func TestNewValidation(t *testing.T) {
	if _, err := New(nil, "leases"); err == nil {
		t.Fatal("New(nil) returned nil")
	}
	state := &sqltest.State{}
	database, err := sqltest.Open(state)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := New(database, "bad-table"); err == nil {
		t.Fatal("unsafe table accepted")
	}
	if _, err := New(database, "leases", WithTokenGenerator(nil)); err == nil {
		t.Fatal("nil generator accepted")
	}
}
