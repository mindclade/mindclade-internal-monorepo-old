// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package postgres

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"

	"go.mindclade.dev/control/artifacts"
	"go.mindclade.dev/control/registry/releases"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/storage/sql/sqltest"
)

const testReleaseID = "release_0000000003e870008000000000000000"

func evidenceGraph(t *testing.T) releases.EvidenceGraph {
	t.Helper()
	subject := identifiers.SHA256String("model-bundle")
	policy := identifiers.SHA256String("promotion-policy")
	graph := releases.EvidenceGraph{
		ReleaseID:     testReleaseID,
		SubjectDigest: subject,
		PolicyDigest:  policy,
		PolicyEpoch:   7,
		Nodes: []releases.EvidenceNode{
			{
				NodeID:        "build",
				Kind:          releases.EvidenceModelBundle,
				SubjectDigest: subject,
				PolicyDigest:  policy,
				Passed:        true,
				Created:       writeTime,
				Artifact: artifacts.Ref{
					Digest:        subject,
					SizeBytes:     4096,
					MediaType:     "application/vnd.mindclade.model-bundle",
					LogicalKind:   "model_bundle",
					SchemaVersion: 1,
				},
			},
			{
				NodeID:        "evaluation",
				Kind:          releases.EvidenceEvaluation,
				SubjectDigest: subject,
				PolicyDigest:  policy,
				Passed:        true,
				Created:       writeTime,
			},
		},
		Edges: []releases.EvidenceEdge{{From: "evaluation", To: "build", Relation: "evaluates"}},
	}
	if _, err := graph.Digest(); err != nil {
		t.Fatalf("evidence graph fixture is invalid: %v", err)
	}
	return graph
}

func qualifiedRelease(t *testing.T) releases.Release {
	t.Helper()
	graph := evidenceGraph(t)
	digest, err := graph.Digest()
	if err != nil {
		t.Fatal(err)
	}
	return releases.Release{
		ReleaseID:           testReleaseID,
		ModelBundleDigest:   graph.SubjectDigest,
		EvidenceGraphDigest: digest,
		Channel:             "stable",
		Status:              "qualified",
	}
}

func TestPutGraphInsertsWhenAbsent(t *testing.T) {
	t.Parallel()
	state := &sqltest.State{Exec: func(_ context.Context, query string, arguments []driver.NamedValue) (driver.Result, error) {
		if !strings.Contains(query, "ON CONFLICT (release_id) DO NOTHING") || len(arguments) != 7 {
			t.Fatalf("query=%q args=%d", query, len(arguments))
		}
		return driver.RowsAffected(1), nil
	}}
	store, _ := newStore(t, state)
	if err := store.PutGraph(context.Background(), evidenceGraph(t)); err != nil {
		t.Fatal(err)
	}
}

// Promote writes the graph before the release, so a retried promotion re-writes
// the same graph. That must succeed rather than block the retry.
func TestPutGraphIsIdempotentForTheSameGraph(t *testing.T) {
	t.Parallel()
	graph := evidenceGraph(t)
	digest, err := graph.Digest()
	if err != nil {
		t.Fatal(err)
	}
	state := &sqltest.State{
		Exec: func(context.Context, string, []driver.NamedValue) (driver.Result, error) {
			return driver.RowsAffected(0), nil
		},
		Query: func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
			return sqltest.NewRows([]string{"graph_digest"}, []driver.Value{digest.String()}), nil
		},
	}
	store, _ := newStore(t, state)
	if err := store.PutGraph(context.Background(), graph); err != nil {
		t.Fatalf("re-writing an identical graph was rejected: %v", err)
	}
}

// The release quotes the graph digest. Rewriting the graph under a release
// identifier that already holds one would silently invalidate that quote.
func TestPutGraphRefusesToRewriteASealedGraph(t *testing.T) {
	t.Parallel()
	state := &sqltest.State{
		Exec: func(context.Context, string, []driver.NamedValue) (driver.Result, error) {
			return driver.RowsAffected(0), nil
		},
		Query: func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
			return sqltest.NewRows([]string{"graph_digest"},
				[]driver.Value{identifiers.SHA256String("a-different-graph").String()}), nil
		},
	}
	store, _ := newStore(t, state)
	err := store.PutGraph(context.Background(), evidenceGraph(t))
	if err == nil {
		t.Fatal("a sealed evidence graph was overwritten")
	}
	if reason := faults.ReasonOf(err); reason != "evidence_graph_immutable" {
		t.Fatalf("reason=%s", reason)
	}
}

func TestPutGraphRefusesAnInvalidGraph(t *testing.T) {
	t.Parallel()
	graph := evidenceGraph(t)
	graph.Edges = append(graph.Edges, releases.EvidenceEdge{From: "build", To: "absent", Relation: "cites"})
	state := &sqltest.State{Exec: func(context.Context, string, []driver.NamedValue) (driver.Result, error) {
		t.Fatal("an invalid evidence graph reached the database")
		return nil, nil
	}}
	store, _ := newStore(t, state)
	if err := store.PutGraph(context.Background(), graph); err == nil {
		t.Fatal("an evidence graph with a dangling edge was accepted")
	}
}

// A zero ResourceVersion means the release does not exist yet.
func TestPutReleaseInsertsAtVersionOne(t *testing.T) {
	t.Parallel()
	state := &sqltest.State{Exec: func(_ context.Context, query string, arguments []driver.NamedValue) (driver.Result, error) {
		if !strings.Contains(query, "INSERT INTO") || !strings.Contains(query, "resource_version") {
			t.Fatalf("query=%q", query)
		}
		if len(arguments) != 6 {
			t.Fatalf("args=%d", len(arguments))
		}
		return driver.RowsAffected(1), nil
	}}
	store, _ := newStore(t, state)
	if err := store.PutRelease(context.Background(), qualifiedRelease(t)); err != nil {
		t.Fatal(err)
	}
}

// Inserting onto an identifier that already exists is the same event as losing
// a swap: someone advanced this release first.
func TestPutReleaseReportsAnExistingIdentifierAsConflict(t *testing.T) {
	t.Parallel()
	state := &sqltest.State{Exec: func(context.Context, string, []driver.NamedValue) (driver.Result, error) {
		return driver.RowsAffected(0), nil
	}}
	store, _ := newStore(t, state)
	err := store.PutRelease(context.Background(), qualifiedRelease(t))
	if err == nil {
		t.Fatal("an insert onto an existing release identifier succeeded")
	}
	if faults.CodeOf(err) != faults.CodeConflict {
		t.Fatalf("code=%v", faults.CodeOf(err))
	}
	if reason := faults.ReasonOf(err); reason != "release_resource_version_conflict" {
		t.Fatalf("reason=%s", reason)
	}
}

func TestPutReleaseSwapsOnTheStoredResourceVersion(t *testing.T) {
	t.Parallel()
	release := qualifiedRelease(t)
	release.ResourceVersion = 4
	release.Status = "promoted"
	state := &sqltest.State{Exec: func(_ context.Context, query string, arguments []driver.NamedValue) (driver.Result, error) {
		if !strings.Contains(query, "resource_version=resource_version+1") ||
			!strings.Contains(query, "AND resource_version=$7") {
			t.Fatalf("query=%q", query)
		}
		if got := arguments[6].Value; got != int64(4) {
			t.Fatalf("swapped on version %v, want 4", got)
		}
		return driver.RowsAffected(1), nil
	}}
	store, _ := newStore(t, state)
	if err := store.PutRelease(context.Background(), release); err != nil {
		t.Fatal(err)
	}
}

// A lost swap must not be reported as success. Promote would otherwise believe
// it had promoted a release that another writer had already moved on.
func TestPutReleaseReportsALostSwapAsConflict(t *testing.T) {
	t.Parallel()
	release := qualifiedRelease(t)
	release.ResourceVersion = 4
	state := &sqltest.State{Exec: func(context.Context, string, []driver.NamedValue) (driver.Result, error) {
		return driver.RowsAffected(0), nil
	}}
	store, _ := newStore(t, state)
	err := store.PutRelease(context.Background(), release)
	if err == nil {
		t.Fatal("a lost compare-and-swap was reported as success")
	}
	if faults.CodeOf(err) != faults.CodeConflict {
		t.Fatalf("code=%v", faults.CodeOf(err))
	}
	if policy := faults.RetryPolicyOf(err); policy.Retryable() {
		t.Fatal("a lost swap was marked retryable; the caller must re-read first")
	}
}

func TestPutReleaseRequiresItsIdentityAndDigests(t *testing.T) {
	t.Parallel()
	cases := map[string]func(*releases.Release){
		"missing_identifier":    func(r *releases.Release) { r.ReleaseID = "" },
		"missing_subject":       func(r *releases.Release) { r.ModelBundleDigest = identifiers.Digest{} },
		"missing_evidence_link": func(r *releases.Release) { r.EvidenceGraphDigest = identifiers.Digest{} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			release := qualifiedRelease(t)
			mutate(&release)
			state := &sqltest.State{Exec: func(context.Context, string, []driver.NamedValue) (driver.Result, error) {
				t.Fatal("an incomplete release reached the database")
				return nil, nil
			}}
			store, _ := newStore(t, state)
			if err := store.PutRelease(context.Background(), release); err == nil {
				t.Fatal("an incomplete release was accepted")
			}
		})
	}
}

// Both release writes must be composable into the caller's commit: Promote
// writes the graph and the release, and a partial commit would leave a release
// quoting a graph that was never stored.
func TestReleaseWritesJoinTheCallersTransaction(t *testing.T) {
	t.Parallel()
	state := &sqltest.State{Exec: func(context.Context, string, []driver.NamedValue) (driver.Result, error) {
		return driver.RowsAffected(1), nil
	}}
	store, _ := newStore(t, state)
	if err := store.PutGraph(context.Background(), evidenceGraph(t)); err != nil {
		t.Fatal(err)
	}
	if err := store.PutRelease(context.Background(), qualifiedRelease(t)); err != nil {
		t.Fatal(err)
	}
	if state.Begins.Load() != 0 || state.Commits.Load() != 0 {
		t.Fatalf("the store opened its own transaction: begins=%d commits=%d",
			state.Begins.Load(), state.Commits.Load())
	}
}

// The store must satisfy the interfaces the domain declares, not ones this
// package invented. If a domain seam grows a method, this stops compiling.
func TestStoreSatisfiesTheDomainRepositoryContracts(t *testing.T) {
	t.Parallel()
	var _ releases.Repository = (*Store)(nil)
}
