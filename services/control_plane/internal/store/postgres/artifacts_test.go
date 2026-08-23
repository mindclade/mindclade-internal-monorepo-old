// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package postgres

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"go.mindclade.dev/control/artifacts"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/storage/sql/sqltest"
)

func artifactRef() artifacts.Ref {
	return artifacts.Ref{
		Digest:        identifiers.SHA256String("artifact"),
		SizeBytes:     4096,
		MediaType:     "application/octet-stream",
		LogicalKind:   "dataset_shard",
		SchemaVersion: 1,
	}
}

func artifactLocation(ref artifacts.Ref, index int) artifacts.Location {
	return artifacts.Location{
		Artifact:   ref,
		Provider:   "gcs",
		URI:        fmt.Sprintf("gs://bucket/object-%d", index),
		Generation: "1",
		Region:     "us-central1",
	}
}

// identityRow is what artifactIdentityState scans: the projected immutable
// columns, which are exactly the domain's EqualIdentity predicate.
func identityRow(ref artifacts.Ref) driver.Rows {
	return sqltest.NewRows(
		[]string{"size_bytes", "media_type", "logical_kind", "schema_version"},
		[]driver.Value{int64(ref.SizeBytes), ref.MediaType, ref.LogicalKind, int64(ref.SchemaVersion)},
	)
}

func countRow(column string, value int64) driver.Rows {
	return sqltest.NewRows([]string{column}, []driver.Value{value})
}

// Register must reach the database as one statement. Put followed by N
// PutLocation calls had no commit boundary between them, so a crash in the
// middle left a registered identity whose bytes nothing could find -- and
// because the digest binding is permanent, that half-state was durable.
func TestRegisterArtifactIsOneStatement(t *testing.T) {
	t.Parallel()
	ref := artifactRef()
	var executions atomic.Int64
	state := &sqltest.State{Exec: func(_ context.Context, query string, arguments []driver.NamedValue) (driver.Result, error) {
		executions.Add(1)
		for _, required := range []string{
			"WITH identity_write AS (",
			"ON CONFLICT (digest) DO NOTHING",
			"RETURNING digest",
			"UNION ALL",
			"incoming (provider, uri, generation, region) AS (",
			"CROSS JOIN incoming",
			"ON CONFLICT (digest, provider, uri, generation) DO NOTHING",
		} {
			if !strings.Contains(query, required) {
				t.Fatalf("the register statement does not contain %q: %q", required, query)
			}
		}
		// Eight fixed placeholders plus four per placement.
		if len(arguments) != artifactRegisterFixedArguments+2*4 {
			t.Fatalf("args=%d", len(arguments))
		}
		return driver.RowsAffected(2), nil
	}}
	store, _ := newStore(t, state)
	if err := store.Register(context.Background(), ref, []artifacts.Location{artifactLocation(ref, 0), artifactLocation(ref, 1)}); err != nil {
		t.Fatal(err)
	}
	if got := executions.Load(); got != 1 {
		t.Fatalf("Register issued %d statements, want 1", got)
	}
	if state.Begins.Load() != 0 || state.Commits.Load() != 0 {
		t.Fatalf("the store opened its own transaction: begins=%d commits=%d", state.Begins.Load(), state.Commits.Load())
	}
}

// The identity CTE is guarded on the immutable columns, so a digest already
// bound to different metadata yields no identity row and therefore no location
// rows: the conflict blocks the placements rather than attaching them to
// somebody else's artifact.
func TestRegisterArtifactReportsAnIdentityConflictAndWritesNoLocation(t *testing.T) {
	t.Parallel()
	ref := artifactRef()
	stored := ref
	stored.SizeBytes = 8192

	state := &sqltest.State{
		Exec: func(context.Context, string, []driver.NamedValue) (driver.Result, error) {
			// The guarded CTE matched nothing, so no placement was written.
			return driver.RowsAffected(0), nil
		},
		Query: func(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
			if !strings.Contains(query, "SELECT size_bytes, media_type, logical_kind, schema_version") {
				t.Fatalf("unexpected disambiguation query: %q", query)
			}
			return identityRow(stored), nil
		},
	}
	store, _ := newStore(t, state)
	err := store.Register(context.Background(), ref, []artifacts.Location{artifactLocation(ref, 0)})
	if err == nil {
		t.Fatal("a digest was rebound to different immutable metadata")
	}
	if !errors.Is(err, artifacts.ErrIdentityConflict) {
		t.Fatalf("err=%v does not carry artifacts.ErrIdentityConflict", err)
	}
	if reason := faults.ReasonOf(err); reason != artifacts.ReasonIdentityConflict {
		t.Fatalf("reason=%s want %s", reason, artifacts.ReasonIdentityConflict)
	}
	if faults.RetryPolicyOf(err).Retryable() {
		t.Fatal("a permanent identity binding conflict was marked retryable")
	}
}

// A replay of an identical registration inserts nothing, which is
// indistinguishable at the driver from a conflict until the store asks.
func TestRegisterArtifactAcceptsAnIdenticalReplay(t *testing.T) {
	t.Parallel()
	ref := artifactRef()
	state := &sqltest.State{
		Exec: func(context.Context, string, []driver.NamedValue) (driver.Result, error) {
			return driver.RowsAffected(0), nil
		},
		Query: func(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
			switch {
			case strings.Contains(query, "SELECT size_bytes, media_type, logical_kind, schema_version"):
				return identityRow(ref), nil
			case strings.Contains(query, "WITH wanted (provider, uri, generation)"):
				return countRow("count", 1), nil
			default:
				t.Fatalf("unexpected query: %q", query)
				return nil, nil
			}
		},
	}
	store, _ := newStore(t, state)
	if err := store.Register(context.Background(), ref, []artifacts.Location{artifactLocation(ref, 0)}); err != nil {
		t.Fatalf("replaying an identical registration was rejected: %v", err)
	}
}

// The catalog has no Delete and no eviction, so a placement set that grew
// without limit would be a durable leak. The statement refuses the whole batch
// rather than landing part of it.
func TestRegisterArtifactRefusesAnOverfullPlacementSet(t *testing.T) {
	t.Parallel()
	ref := artifactRef()

	t.Run("batch_larger_than_the_bound_never_reaches_sql", func(t *testing.T) {
		t.Parallel()
		state := &sqltest.State{Exec: func(context.Context, string, []driver.NamedValue) (driver.Result, error) {
			t.Fatal("an oversized placement batch reached the database")
			return nil, nil
		}}
		store, _ := newStore(t, state)
		oversized := make([]artifacts.Location, 0, artifacts.MaximumLocationsPerArtifact+1)
		for index := range artifacts.MaximumLocationsPerArtifact + 1 {
			oversized = append(oversized, artifactLocation(ref, index))
		}
		err := store.Register(context.Background(), ref, oversized)
		if !errors.Is(err, artifacts.ErrLocationBudget) {
			t.Fatalf("err=%v", err)
		}
		if code := faults.CodeOf(err); code != faults.CodeResourceExhausted {
			t.Fatalf("code=%v", code)
		}
	})

	t.Run("budget_guard_blocked_the_write", func(t *testing.T) {
		t.Parallel()
		state := &sqltest.State{
			Exec: func(context.Context, string, []driver.NamedValue) (driver.Result, error) {
				return driver.RowsAffected(0), nil
			},
			Query: func(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
				switch {
				case strings.Contains(query, "SELECT size_bytes, media_type, logical_kind, schema_version"):
					return identityRow(ref), nil
				case strings.Contains(query, "WITH wanted (provider, uri, generation)"):
					// The identity matched but the placement never landed.
					return countRow("count", 0), nil
				default:
					t.Fatalf("unexpected query: %q", query)
					return nil, nil
				}
			},
		}
		store, _ := newStore(t, state)
		err := store.Register(context.Background(), artifactRef(), []artifacts.Location{artifactLocation(ref, 0)})
		if !errors.Is(err, artifacts.ErrLocationBudget) {
			t.Fatalf("a silently dropped placement was reported as success: %v", err)
		}
	})
}

// A location may not exist without a matching identity. The guard is in the
// statement rather than a preceding read, because the window between a check
// and an insert is the whole rule.
func TestPutArtifactLocationRefusesAnUnknownIdentity(t *testing.T) {
	t.Parallel()
	ref := artifactRef()
	state := &sqltest.State{
		Exec: func(_ context.Context, query string, arguments []driver.NamedValue) (driver.Result, error) {
			for _, required := range []string{
				"identity.size_bytes=$7", "identity.media_type=$8",
				"identity.logical_kind=$9", "identity.schema_version=$10",
				"ON CONFLICT (digest, provider, uri, generation) DO NOTHING",
			} {
				if !strings.Contains(query, required) {
					t.Fatalf("the location insert is not guarded on %q: %q", required, query)
				}
			}
			if strings.Contains(query, "INSERT INTO") && !strings.Contains(query, "SELECT $1,$2,$3,$4,$5,$6 FROM") {
				t.Fatalf("the location insert is not guarded by a select: %q", query)
			}
			if len(arguments) != 11 {
				t.Fatalf("args=%d", len(arguments))
			}
			return driver.RowsAffected(0), nil
		},
		Query: func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
			// No matching identity, no stored placement, no locations at all.
			return sqltest.NewRows([]string{"matching", "stored", "total"},
				[]driver.Value{int64(0), int64(0), int64(0)}), nil
		},
	}
	store, _ := newStore(t, state)
	err := store.PutLocation(context.Background(), artifactLocation(ref, 0))
	if !errors.Is(err, artifacts.ErrLocationUnknownIdentity) {
		t.Fatalf("a location was written for an unregistered identity: %v", err)
	}
	if reason := faults.ReasonOf(err); reason != artifacts.ReasonLocationUnknownIdentity {
		t.Fatalf("reason=%s", reason)
	}
}

func TestPutArtifactLocationDistinguishesReplayFromExhaustedBudget(t *testing.T) {
	t.Parallel()
	ref := artifactRef()
	cases := map[string]struct {
		row     []driver.Value
		wantErr error
	}{
		"replay":  {[]driver.Value{int64(1), int64(1), int64(3)}, nil},
		"budget":  {[]driver.Value{int64(1), int64(0), int64(artifacts.MaximumLocationsPerArtifact)}, artifacts.ErrLocationBudget},
		"unknown": {[]driver.Value{int64(0), int64(0), int64(0)}, artifacts.ErrLocationUnknownIdentity},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			state := &sqltest.State{
				Exec: func(context.Context, string, []driver.NamedValue) (driver.Result, error) {
					return driver.RowsAffected(0), nil
				},
				Query: func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
					return sqltest.NewRows([]string{"matching", "stored", "total"}, testCase.row), nil
				},
			}
			store, _ := newStore(t, state)
			err := store.PutLocation(context.Background(), artifactLocation(ref, 0))
			if testCase.wantErr == nil {
				if err != nil {
					t.Fatalf("err=%v", err)
				}
				return
			}
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("err=%v want %v", err, testCase.wantErr)
			}
		})
	}
}

// The write path caps the placement set, so a page that came back over the cap
// means the cap was bypassed. Truncating would tell a garbage collector an
// object had fewer copies than it does.
func TestArtifactLocationsReportsAnOverfullPageRatherThanTruncating(t *testing.T) {
	t.Parallel()
	ref := artifactRef()
	document := fmt.Sprintf(
		`{"digest":%q,"size_bytes":%d,"media_type":%q,"logical_kind":%q,"schema_version":%d}`,
		ref.Digest.String(), ref.SizeBytes, ref.MediaType, ref.LogicalKind, ref.SchemaVersion)
	rows := make([][]driver.Value, 0, artifacts.MaximumLocationsPerArtifact+1)
	for index := range artifacts.MaximumLocationsPerArtifact + 1 {
		rows = append(rows, []driver.Value{
			[]byte(document), "gcs", fmt.Sprintf("gs://bucket/object-%d", index), "1", "us-central1",
		})
	}
	state := &sqltest.State{Query: func(_ context.Context, query string, arguments []driver.NamedValue) (driver.Rows, error) {
		if !strings.Contains(query, "LIMIT $2") {
			t.Fatalf("the location page is unbounded: %q", query)
		}
		if got := arguments[1].Value; got != int64(artifacts.MaximumLocationsPerArtifact)+1 {
			t.Fatalf("limit=%v; the page must read one row past the cap to detect an overfull set", got)
		}
		return sqltest.NewRows([]string{"document", "provider", "uri", "generation", "region"}, rows...), nil
	}}
	store, _ := newStore(t, state)
	locations, err := store.Locations(context.Background(), ref.Digest)
	if err == nil {
		t.Fatalf("an overfull location page was returned as %d locations", len(locations))
	}
	if reason := faults.ReasonOf(err); reason != "artifact_location_page_overflow" {
		t.Fatalf("reason=%s", reason)
	}
}

func TestArtifactWritesRejectInvalidInputBeforeSQL(t *testing.T) {
	t.Parallel()
	ref := artifactRef()
	cases := map[string]func(*Store) error{
		"missing_digest": func(store *Store) error {
			broken := ref
			broken.Digest = identifiers.Digest{}
			return store.Register(context.Background(), broken, nil)
		},
		"missing_media_type": func(store *Store) error {
			broken := ref
			broken.MediaType = ""
			return store.Put(context.Background(), broken)
		},
		"size_out_of_range": func(store *Store) error {
			broken := ref
			broken.SizeBytes = 1 << 63
			return store.Put(context.Background(), broken)
		},
		"location_identity_mismatch": func(store *Store) error {
			drifted := ref
			drifted.SizeBytes = 1
			return store.Register(context.Background(), ref, []artifacts.Location{artifactLocation(drifted, 0)})
		},
		"incomplete_location": func(store *Store) error {
			return store.PutLocation(context.Background(), artifacts.Location{Artifact: ref, Provider: "gcs", URI: "gs://bucket/key"})
		},
	}
	for name, attempt := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			state := &sqltest.State{Exec: func(context.Context, string, []driver.NamedValue) (driver.Result, error) {
				t.Fatal("an invalid artifact reached the database")
				return nil, nil
			}}
			store, _ := newStore(t, state)
			if err := attempt(store); err == nil {
				t.Fatal("an invalid artifact was accepted")
			}
		})
	}
}

// The store must satisfy the interface the domain declares, not one this
// package invented. If Catalog grows a method, this stops compiling.
func TestStoreSatisfiesTheArtifactCatalogContract(t *testing.T) {
	t.Parallel()
	var _ artifacts.Catalog = (*Store)(nil)
}
