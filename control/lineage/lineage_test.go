// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package lineage

import (
	"context"
	"errors"
	"testing"

	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
)

func testGraph() Graph {
	nodes := []Node{
		{NodeID: "source", Kind: NodeSourceRevision, Digest: identifiers.SHA256String("source"), Classification: ClassificationInternal},
		{NodeID: "config", Kind: NodeResolvedConfig, Digest: identifiers.SHA256String("config"), Classification: ClassificationInternal},
		{NodeID: "image", Kind: NodeRuntimeImage, Digest: identifiers.SHA256String("image"), Classification: ClassificationInternal},
		{NodeID: "dataset", Kind: NodeDatasetSnapshot, Digest: identifiers.SHA256String("dataset"), Classification: ClassificationRestricted},
		{NodeID: "run", Kind: NodeTrainingRun, Digest: identifiers.SHA256String("run"), CanonicalID: "run_0000000003e870008000000000000000", Classification: ClassificationConfidential},
		{NodeID: "checkpoint", Kind: NodeCheckpoint, Digest: identifiers.SHA256String("checkpoint"), Classification: ClassificationConfidential},
		{NodeID: "model", Kind: NodeModelBundle, Digest: identifiers.SHA256String("model"), Classification: ClassificationConfidential},
		{NodeID: "evaluation", Kind: NodeEvaluation, Digest: identifiers.SHA256String("evaluation"), Classification: ClassificationConfidential},
		{NodeID: "safety", Kind: NodeSafetyEvidence, Digest: identifiers.SHA256String("safety"), Classification: ClassificationRestricted},
		{NodeID: "release", Kind: NodeReleaseEvidence, Digest: identifiers.SHA256String("release"), Classification: ClassificationConfidential},
	}
	return Graph{
		SchemaVersion: SchemaVersion,
		GraphID:       "lineage_0000000003e870008000000000000000",
		SubjectDigest: nodes[len(nodes)-1].Digest,
		PolicyDigest:  identifiers.SHA256String("policy"),
		Nodes:         nodes,
		Edges: []Edge{
			{From: "source", To: "run", Relation: RelationConsumedBy},
			{From: "config", To: "run", Relation: RelationConsumedBy},
			{From: "image", To: "run", Relation: RelationConsumedBy},
			{From: "dataset", To: "run", Relation: RelationConsumedBy},
			{From: "run", To: "checkpoint", Relation: RelationProduced},
			{From: "checkpoint", To: "model", Relation: RelationPackagedAs},
			{From: "model", To: "evaluation", Relation: RelationEvaluatedBy},
			{From: "model", To: "release", Relation: RelationConsumedBy},
			{From: "evaluation", To: "release", Relation: RelationConsumedBy},
			{From: "safety", To: "release", Relation: RelationConsumedBy},
		},
	}
}

func TestGraphDigestIsOrderIndependentAndRejectsCycles(t *testing.T) {
	t.Parallel()
	graph := testGraph()
	first, err := graph.Digest()
	if err != nil {
		t.Fatal(err)
	}
	for left, right := 0, len(graph.Nodes)-1; left < right; left, right = left+1, right-1 {
		graph.Nodes[left], graph.Nodes[right] = graph.Nodes[right], graph.Nodes[left]
	}
	for left, right := 0, len(graph.Edges)-1; left < right; left, right = left+1, right-1 {
		graph.Edges[left], graph.Edges[right] = graph.Edges[right], graph.Edges[left]
	}
	second, err := graph.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if !first.Equal(second) {
		t.Fatalf("digest changed with order: %s != %s", first, second)
	}

	graph.Edges = append(graph.Edges, Edge{From: "release", To: "source", Relation: RelationDerived})
	if _, err := graph.Digest(); faults.ReasonOf(err) != "lineage_graph_cycle" {
		t.Fatalf("cycle error = %v", err)
	}
}

func TestReleaseRequirementsRequireConnectedEvidence(t *testing.T) {
	t.Parallel()
	graph := testGraph()
	requirements := ReleaseRequirements{RequiredKinds: []NodeKind{
		NodeSourceRevision,
		NodeResolvedConfig,
		NodeRuntimeImage,
		NodeDatasetSnapshot,
		NodeTrainingRun,
		NodeCheckpoint,
		NodeModelBundle,
		NodeEvaluation,
		NodeSafetyEvidence,
	}}
	if err := requirements.Validate(graph); err != nil {
		t.Fatal(err)
	}
	graph.Edges = graph.Edges[:len(graph.Edges)-1]
	if err := requirements.Validate(graph); faults.ReasonOf(err) != "lineage_release_incomplete" {
		t.Fatalf("disconnected evidence error = %v", err)
	}
}

func TestMLflowProjectionWithholdsRestrictedNodesAndEdges(t *testing.T) {
	t.Parallel()
	projection, err := testGraph().MLflowProjection()
	if err != nil {
		t.Fatal(err)
	}
	if projection.RestrictedNodes != 2 || projection.Authority != "mindclade-control-plane" {
		t.Fatalf("projection = %+v", projection)
	}
	for _, node := range projection.Nodes {
		if node.Classification == ClassificationRestricted {
			t.Fatalf("restricted node leaked: %+v", node)
		}
	}
	for _, edge := range projection.Edges {
		if edge.From == "dataset" || edge.From == "safety" || edge.To == "dataset" || edge.To == "safety" {
			t.Fatalf("restricted edge leaked: %+v", edge)
		}
	}
}

type memoryRepository struct {
	graphs  map[string]Graph
	corrupt bool
}

func (repository *memoryRepository) Put(_ context.Context, digest identifiers.Digest, graph Graph) error {
	if repository.graphs == nil {
		repository.graphs = map[string]Graph{}
	}
	if existing, ok := repository.graphs[digest.String()]; ok {
		existingDigest, _ := existing.Digest()
		newDigest, _ := graph.Digest()
		if !existingDigest.Equal(newDigest) {
			return ErrGraphImmutable
		}
		return nil
	}
	repository.graphs[digest.String()] = graph
	return nil
}

func (repository *memoryRepository) Get(_ context.Context, digest identifiers.Digest) (Graph, error) {
	graph, ok := repository.graphs[digest.String()]
	if !ok {
		return Graph{}, ErrGraphNotFound
	}
	if repository.corrupt {
		graph.PolicyDigest = identifiers.SHA256String("corrupt")
	}
	return graph, nil
}

func TestServiceVerifiesStoredGraphDigest(t *testing.T) {
	t.Parallel()
	repository := &memoryRepository{}
	service := Service{Repository: repository}
	digest, err := service.Publish(context.Background(), testGraph())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Get(context.Background(), digest); err != nil {
		t.Fatal(err)
	}
	repository.corrupt = true
	if _, err := service.Get(context.Background(), digest); faults.CodeOf(err) != faults.CodeDataLoss {
		t.Fatalf("corrupt graph error = %v", err)
	}
}

// The two rejections in the Repository contract are named by the domain so that
// every implementation produces the same errors.Is target. A caller must not be
// able to tell the durable store from this reference by what it gets back.
func TestRepositoryRejectionsUseTheDomainSentinels(t *testing.T) {
	t.Parallel()
	repository := &memoryRepository{}
	graph := testGraph()
	digest, err := graph.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Put(context.Background(), digest, graph); err != nil {
		t.Fatal(err)
	}
	if err := repository.Put(context.Background(), digest, graph); err != nil {
		t.Fatalf("republishing an identical graph was rejected: %v", err)
	}

	different := testGraph()
	different.PolicyDigest = identifiers.SHA256String("a-different-policy")
	if err := repository.Put(context.Background(), digest, different); !errors.Is(err, ErrGraphImmutable) {
		t.Fatalf("rebinding a digest returned %v", err)
	}
	if _, err := repository.Get(context.Background(), identifiers.SHA256String("never-published")); !errors.Is(err, ErrGraphNotFound) {
		t.Fatalf("an absent digest returned %v", err)
	}
}
