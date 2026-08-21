// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package lineage

import (
	"context"
	"sort"

	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
)

type Service struct {
	Repository Repository
}

func (service Service) Publish(ctx context.Context, graph Graph) (identifiers.Digest, error) {
	if service.Repository == nil {
		return identifiers.Digest{}, unavailable("lineage_repository_unavailable", "lineage repository is unavailable")
	}
	digest, err := graph.Digest()
	if err != nil {
		return identifiers.Digest{}, err
	}
	if err := service.Repository.Put(ctx, digest, graph); err != nil {
		return identifiers.Digest{}, err
	}
	return digest, nil
}

func (service Service) Get(ctx context.Context, digest identifiers.Digest) (Graph, error) {
	if service.Repository == nil {
		return Graph{}, unavailable("lineage_repository_unavailable", "lineage repository is unavailable")
	}
	if !digest.Valid() {
		return Graph{}, invalid("lineage_digest_invalid", "lineage digest is invalid", nil)
	}
	graph, err := service.Repository.Get(ctx, digest)
	if err != nil {
		return Graph{}, err
	}
	actual, err := graph.Digest()
	if err != nil {
		return Graph{}, err
	}
	if !actual.Equal(digest) {
		return Graph{}, faults.New(
			faults.CodeDataLoss,
			"stored lineage graph digest does not match",
			faults.WithReason("lineage_digest_mismatch"),
			faults.WithOperation("control.lineage"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	return graph, nil
}

// ReleaseRequirements defines graph kinds that must have a path to the release subject.
type ReleaseRequirements struct {
	RequiredKinds []NodeKind
}

func (requirements ReleaseRequirements) Validate(graph Graph) error {
	if err := graph.Validate(); err != nil {
		return err
	}
	if len(requirements.RequiredKinds) == 0 || len(requirements.RequiredKinds) > len(graph.Nodes) {
		return invalid("lineage_requirements_invalid", "lineage requirements are outside bounds", nil)
	}
	seen := make(map[NodeKind]struct{}, len(requirements.RequiredKinds))
	for _, kind := range requirements.RequiredKinds {
		if !kind.Valid() {
			return invalid("lineage_requirement_kind_invalid", "lineage requirement kind is invalid", nil)
		}
		if _, exists := seen[kind]; exists {
			return invalid("lineage_requirement_duplicate", "lineage requirement kinds must be unique", nil)
		}
		seen[kind] = struct{}{}
	}

	subjectID := ""
	byKind := make(map[NodeKind][]string, len(requirements.RequiredKinds))
	for _, node := range graph.Nodes {
		if node.Digest.Equal(graph.SubjectDigest) {
			subjectID = node.NodeID
		}
		byKind[node.Kind] = append(byKind[node.Kind], node.NodeID)
	}
	reverse := make(map[string][]string, len(graph.Nodes))
	for _, edge := range graph.Edges {
		reverse[edge.To] = append(reverse[edge.To], edge.From)
	}
	ancestors := map[string]struct{}{subjectID: {}}
	queue := []string{subjectID}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, parent := range reverse[current] {
			if _, exists := ancestors[parent]; exists {
				continue
			}
			ancestors[parent] = struct{}{}
			queue = append(queue, parent)
		}
	}
	for _, kind := range requirements.RequiredKinds {
		connected := false
		for _, nodeID := range byKind[kind] {
			if _, exists := ancestors[nodeID]; exists {
				connected = true
				break
			}
		}
		if !connected {
			return invalid("lineage_release_incomplete", "required lineage kind is absent or disconnected: "+string(kind), nil)
		}
	}
	return nil
}

// Projection is a metadata-system mirror. Restricted node identities are withheld.
type Projection struct {
	GraphDigest     identifiers.Digest
	SubjectDigest   identifiers.Digest
	Nodes           []Node
	Edges           []Edge
	RestrictedNodes int
	Authority       string
}

func (graph Graph) MLflowProjection() (Projection, error) {
	digest, err := graph.Digest()
	if err != nil {
		return Projection{}, err
	}
	allowed := make(map[string]struct{}, len(graph.Nodes))
	nodes := make([]Node, 0, len(graph.Nodes))
	restricted := 0
	for _, node := range graph.Nodes {
		if node.Classification == ClassificationRestricted {
			restricted++
			continue
		}
		allowed[node.NodeID] = struct{}{}
		nodes = append(nodes, node)
	}
	edges := make([]Edge, 0, len(graph.Edges))
	for _, edge := range graph.Edges {
		_, fromAllowed := allowed[edge.From]
		_, toAllowed := allowed[edge.To]
		if fromAllowed && toAllowed {
			edges = append(edges, edge)
		}
	}
	sort.Slice(nodes, func(left, right int) bool { return nodes[left].NodeID < nodes[right].NodeID })
	sort.Slice(edges, func(left, right int) bool { return edgeKey(edges[left]) < edgeKey(edges[right]) })
	return Projection{
		GraphDigest:     digest,
		SubjectDigest:   graph.SubjectDigest,
		Nodes:           nodes,
		Edges:           edges,
		RestrictedNodes: restricted,
		Authority:       "mindclade-control-plane",
	}, nil
}

func invalid(reason, message string, cause error) error {
	if cause == nil {
		return faults.New(faults.CodeInvalidArgument, message, faults.WithReason(reason), faults.WithOperation("control.lineage"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return faults.Wrap(cause, faults.CodeInvalidArgument, message, faults.WithReason(reason), faults.WithOperation("control.lineage"), faults.WithRetryPolicy(faults.NoRetry()))
}

func unavailable(reason, message string) error {
	return faults.New(faults.CodeFailedPrecondition, message, faults.WithReason(reason), faults.WithOperation("control.lineage"), faults.WithRetryPolicy(faults.NoRetry()))
}
