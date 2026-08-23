// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"

	_ "github.com/lib/pq"

	"go.mindclade.dev/control/lineage"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/storage/sql/transaction"
)

// These run against a real PostgreSQL server. A fake driver can only prove the
// shape of a query string; it cannot prove that ON CONFLICT, the primary key,
// or the bound CHECKs behave the way the store believes. The immutability rule
// this store exists to hold is asserted here against real SQL.

// liveLineageGraph builds a valid provenance DAG: source, config, image and
// dataset feed a training run, which produces a checkpoint, packaged as a model
// bundle, evaluated and released. seed varies the content so two calls produce
// two different digests.
func liveLineageGraph(seed string) lineage.Graph {
	nodes := []lineage.Node{
		{NodeID: "source", Kind: lineage.NodeSourceRevision, Digest: identifiers.SHA256String("source-" + seed), Classification: lineage.ClassificationInternal},
		{NodeID: "config", Kind: lineage.NodeResolvedConfig, Digest: identifiers.SHA256String("config-" + seed), Classification: lineage.ClassificationInternal},
		{NodeID: "image", Kind: lineage.NodeRuntimeImage, Digest: identifiers.SHA256String("image-" + seed), Classification: lineage.ClassificationInternal},
		{NodeID: "dataset", Kind: lineage.NodeDatasetSnapshot, Digest: identifiers.SHA256String("dataset-" + seed), Classification: lineage.ClassificationRestricted},
		{NodeID: "run", Kind: lineage.NodeTrainingRun, Digest: identifiers.SHA256String("run-" + seed), Classification: lineage.ClassificationConfidential},
		{NodeID: "checkpoint", Kind: lineage.NodeCheckpoint, Digest: identifiers.SHA256String("checkpoint-" + seed), Classification: lineage.ClassificationConfidential},
		{NodeID: "model", Kind: lineage.NodeModelBundle, Digest: identifiers.SHA256String("model-" + seed), Classification: lineage.ClassificationConfidential},
		{NodeID: "evaluation", Kind: lineage.NodeEvaluation, Digest: identifiers.SHA256String("evaluation-" + seed), Classification: lineage.ClassificationConfidential},
		{NodeID: "release", Kind: lineage.NodeReleaseEvidence, Digest: identifiers.SHA256String("release-" + seed), Classification: lineage.ClassificationConfidential},
	}
	return lineage.Graph{
		SchemaVersion: lineage.SchemaVersion,
		GraphID:       "lineage_0000000003e870008000000000000000",
		SubjectDigest: nodes[len(nodes)-1].Digest,
		PolicyDigest:  identifiers.SHA256String("policy"),
		Nodes:         nodes,
		Edges: []lineage.Edge{
			{From: "source", To: "run", Relation: lineage.RelationConsumedBy},
			{From: "config", To: "run", Relation: lineage.RelationConsumedBy},
			{From: "image", To: "run", Relation: lineage.RelationConsumedBy},
			{From: "dataset", To: "run", Relation: lineage.RelationConsumedBy},
			{From: "run", To: "checkpoint", Relation: lineage.RelationProduced},
			{From: "checkpoint", To: "model", Relation: lineage.RelationPackagedAs},
			{From: "model", To: "evaluation", Relation: lineage.RelationEvaluatedBy},
			{From: "model", To: "release", Relation: lineage.RelationConsumedBy},
			{From: "evaluation", To: "release", Relation: lineage.RelationConsumedBy},
		},
	}
}

func mustLineageDigest(t *testing.T, graph lineage.Graph) identifiers.Digest {
	t.Helper()
	digest, err := graph.Digest()
	if err != nil {
		t.Fatal(err)
	}
	return digest
}

// The headline property: a digest is bound to one graph forever, and republishing
// that same graph is a no-op rather than a conflict.
//
// Both halves matter. Without idempotence a caller that lost a response has no
// safe retry. Without the rebinding refusal, a release that cites provenance by
// digest could have that provenance swapped underneath it.
func TestLivePostgresLineagePutIsIdempotentAndRefusesRebinding(t *testing.T) {
	store, db := livePostgresStore(t)
	repository := store.LineageGraphs()
	ctx := context.Background()

	graph := liveLineageGraph("original")
	digest := mustLineageDigest(t, graph)
	if err := repository.Put(ctx, digest, graph); err != nil {
		t.Fatal(err)
	}
	// Replay of identical provenance: the ordinary case after a lost response.
	if err := repository.Put(ctx, digest, graph); err != nil {
		t.Fatalf("replaying an identical graph was rejected: %v", err)
	}
	// A re-encoding is not a rebinding. The canonical digest is order
	// independent, so a caller that shuffled its slices is publishing the same
	// provenance and must not be told the digest is taken.
	shuffled := liveLineageGraph("original")
	for left, right := 0, len(shuffled.Nodes)-1; left < right; left, right = left+1, right-1 {
		shuffled.Nodes[left], shuffled.Nodes[right] = shuffled.Nodes[right], shuffled.Nodes[left]
	}
	for left, right := 0, len(shuffled.Edges)-1; left < right; left, right = left+1, right-1 {
		shuffled.Edges[left], shuffled.Edges[right] = shuffled.Edges[right], shuffled.Edges[left]
	}
	if err := repository.Put(ctx, digest, shuffled); err != nil {
		t.Fatalf("a reordered encoding of the same graph was rejected: %v", err)
	}
	var rows int64
	if err := db.QueryRow("SELECT count(*) FROM " + store.LineageGraphTable()).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("idempotent republication wrote %d rows, want 1", rows)
	}

	// A different graph filed under the first graph's digest. This is the
	// rebinding the whole store exists to refuse.
	different := liveLineageGraph("substituted")
	if mustLineageDigest(t, different).Equal(digest) {
		t.Fatal("the two fixtures share a digest; the test would assert nothing")
	}
	err := repository.Put(ctx, digest, different)
	if !errors.Is(err, lineage.ErrGraphImmutable) {
		t.Fatalf("Put rebound the digest: %v", err)
	}
	if reason := faults.ReasonOf(err); reason != "lineage_digest_binding_mismatch" {
		t.Fatalf("reason=%s", reason)
	}
	if faults.RetryPolicyOf(err).Retryable() {
		t.Fatal("a rebinding was reported as retryable")
	}

	// And the stored graph is untouched: the refusal must not have half-written.
	stored, err := repository.Get(ctx, digest)
	if err != nil {
		t.Fatal(err)
	}
	if !stored.Nodes[0].Digest.Equal(graph.Nodes[0].Digest) {
		t.Fatalf("the rejected Put mutated the stored graph: %#v", stored.Nodes[0])
	}
	if err := db.QueryRow("SELECT count(*) FROM " + store.LineageGraphTable()).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("a rejected Put left %d rows, want 1", rows)
	}
}

// The pre-insert digest check is one guard. The other is at the row: if a row
// already holds a different graph -- a genuine digest collision, or a row an
// operator edited -- a matching-digest Put must still refuse rather than
// silently report success against provenance that is not what it published.
func TestLivePostgresLineageRefusesARowHoldingDifferentProvenance(t *testing.T) {
	store, db := livePostgresStore(t)
	repository := store.LineageGraphs()
	ctx := context.Background()

	graph := liveLineageGraph("row-guard")
	digest := mustLineageDigest(t, graph)
	if err := repository.Put(ctx, digest, graph); err != nil {
		t.Fatal(err)
	}
	// Substitute the stored document behind the store's back, keeping the key.
	// This is the durable state a collision or a manual edit would leave.
	substituted, err := json.Marshal(liveLineageGraph("intruder"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE "+store.LineageGraphTable()+" SET document=$1 WHERE graph_digest=$2", substituted, digest.String()); err != nil {
		t.Fatal(err)
	}

	err = repository.Put(ctx, digest, graph)
	if !errors.Is(err, lineage.ErrGraphImmutable) {
		t.Fatalf("Put accepted a digest already holding different provenance: %v", err)
	}
	if reason := faults.ReasonOf(err); reason != lineage.ReasonGraphImmutable {
		t.Fatalf("reason=%s want %s", reason, lineage.ReasonGraphImmutable)
	}
	if !faults.IsCode(err, faults.CodeFailedPrecondition) {
		t.Fatalf("code=%s want %s", faults.CodeOf(err), faults.CodeFailedPrecondition)
	}
}

// A corrupted row must reach the caller as CodeDataLoss through
// lineage.Service, not as wrong data and not as some other code.
//
// The store deliberately does not recompute the digest on read. That check
// lives in lineage.Service.Get, and a duplicate of it here would consume the
// mismatch first and report it under a different code -- leaving the service's
// detector unreachable and the documented failure mode unobservable. This test
// is what holds that division in place.
func TestLivePostgresLineageCorruptedRowSurfacesAsDataLoss(t *testing.T) {
	store, db := livePostgresStore(t)
	service := lineage.Service{Repository: store.LineageGraphs()}
	ctx := context.Background()

	graph := liveLineageGraph("corruption")
	digest, err := service.Publish(ctx, graph)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Get(ctx, digest); err != nil {
		t.Fatalf("a healthy graph did not round trip: %v", err)
	}

	// Rewrite the document to a different valid graph while keeping the key --
	// the shape of corruption that a digest check catches and a schema check
	// does not, because the row stays perfectly well formed.
	corrupted, err := json.Marshal(liveLineageGraph("corruption-substituted"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE "+store.LineageGraphTable()+" SET document=$1 WHERE graph_digest=$2", corrupted, digest.String()); err != nil {
		t.Fatal(err)
	}

	returned, err := service.Get(ctx, digest)
	if !faults.IsCode(err, faults.CodeDataLoss) {
		t.Fatalf("corrupted graph err=%v code=%s, want %s", err, faults.CodeOf(err), faults.CodeDataLoss)
	}
	if reason := faults.ReasonOf(err); reason != "lineage_digest_mismatch" {
		t.Fatalf("reason=%s want lineage_digest_mismatch", reason)
	}
	if len(returned.Nodes) != 0 {
		t.Fatalf("a corrupted read returned graph content: %#v", returned)
	}
}

func TestLivePostgresLineageRoundTripAndAbsence(t *testing.T) {
	store, _ := livePostgresStore(t)
	repository := store.LineageGraphs()
	ctx := context.Background()

	graph := liveLineageGraph("round-trip")
	digest := mustLineageDigest(t, graph)
	if err := repository.Put(ctx, digest, graph); err != nil {
		t.Fatal(err)
	}
	stored, err := repository.Get(ctx, digest)
	if err != nil {
		t.Fatal(err)
	}
	storedDigest := mustLineageDigest(t, stored)
	if !storedDigest.Equal(digest) {
		t.Fatalf("stored digest=%s want %s", storedDigest, digest)
	}
	// Restricted classification is domain state, not a projection, so it must
	// survive the round trip -- MLflowProjection withholds those nodes and
	// would withhold nothing if the flag were lost in storage.
	restricted := 0
	for _, node := range stored.Nodes {
		if node.Classification == lineage.ClassificationRestricted {
			restricted++
		}
	}
	if restricted != 1 {
		t.Fatalf("restricted node count=%d want 1", restricted)
	}

	_, err = repository.Get(ctx, identifiers.SHA256String("never-published"))
	if !errors.Is(err, lineage.ErrGraphNotFound) {
		t.Fatalf("an unpublished digest resolved: %v", err)
	}
	if !faults.IsCode(err, faults.CodeNotFound) {
		t.Fatalf("absence code=%s want %s", faults.CodeOf(err), faults.CodeNotFound)
	}
}

// The store must join the caller's transaction rather than open its own, so a
// publication rolls back with the unit of work containing it. A repository that
// committed independently would leave provenance for a release that never
// happened.
func TestLivePostgresLineageRollsBackWithTheCallersTransaction(t *testing.T) {
	store, db := livePostgresStore(t)
	repository := store.LineageGraphs()
	sentinel := errors.New("injected after lineage write")

	graph := liveLineageGraph("rollback")
	digest := mustLineageDigest(t, graph)
	err := transaction.RunVoid(context.Background(), db, transaction.Options{Isolation: sql.LevelSerializable},
		func(ctx context.Context, _ *sql.Tx) error {
			if err := repository.Put(ctx, digest, graph); err != nil {
				return err
			}
			return sentinel
		})
	if !errors.Is(err, sentinel) {
		t.Fatalf("transaction error = %v, want the injected failure", err)
	}
	var rows int64
	if err := db.QueryRow("SELECT count(*) FROM " + store.LineageGraphTable()).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("lineage rows survived a rollback = %d", rows)
	}
}

// Graph bounds are enforced by the domain before the insert, and again by the
// table. Asserting the domain rejection is what proves an over-sized graph
// never reaches SQL at all.
func TestLivePostgresLineageRejectsAnUnboundedGraph(t *testing.T) {
	store, db := livePostgresStore(t)
	repository := store.LineageGraphs()
	ctx := context.Background()

	graph := liveLineageGraph("bounds")
	oversized := graph
	oversized.Nodes = make([]lineage.Node, lineage.MaximumNodes+1)
	copy(oversized.Nodes, graph.Nodes)
	// Digest fails validation on an over-sized graph, so the caller cannot even
	// name a digest for it; publish it under the healthy graph's digest to
	// prove the store refuses rather than truncates.
	if err := repository.Put(ctx, mustLineageDigest(t, graph), oversized); !faults.IsCode(err, faults.CodeInvalidArgument) {
		t.Fatalf("an over-sized graph was accepted: %v", err)
	}
	var rows int64
	if err := db.QueryRow("SELECT count(*) FROM " + store.LineageGraphTable()).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("a rejected over-sized graph wrote %d rows", rows)
	}
}

// A store whose lineage table name never passed validation must refuse every
// call rather than format an unchecked name into SQL.
func TestLineageStoreRefusesAnUnconfiguredTable(t *testing.T) {
	t.Parallel()
	if _, err := New(nil, WithLineageGraphTable("lineage; DROP TABLE mindclade_audit_records")); err == nil {
		t.Fatal("store accepted an unsafe lineage table name")
	}
	if _, err := LineageGraphDDL("Lineage"); err == nil {
		t.Fatal("DDL accepted an unsafe lineage table name")
	}
}
