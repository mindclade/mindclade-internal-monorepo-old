// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package lineage

import (
	"sort"
	"strings"

	"go.mindclade.dev/libs/go/identifiers"
)

const (
	SchemaVersion = 1
	MaximumNodes  = 4096
	MaximumEdges  = 16384
)

type NodeKind string

const (
	NodeSourceRevision    NodeKind = "source_revision"
	NodeResolvedConfig    NodeKind = "resolved_config"
	NodeRuntimeImage      NodeKind = "runtime_image"
	NodeDatasetSnapshot   NodeKind = "dataset_snapshot"
	NodeFeatureSnapshot   NodeKind = "feature_snapshot"
	NodeReferenceDatabase NodeKind = "reference_database"
	NodeTrainingRun       NodeKind = "training_run"
	NodeTrainingAttempt   NodeKind = "training_attempt"
	NodeCheckpoint        NodeKind = "checkpoint"
	NodeModelBundle       NodeKind = "model_bundle"
	NodeRuntimeBundle     NodeKind = "runtime_bundle"
	NodeEvaluation        NodeKind = "evaluation"
	NodeSafetyEvidence    NodeKind = "safety_evidence"
	NodeReleaseEvidence   NodeKind = "release_evidence"
	NodeDeployment        NodeKind = "deployment"
)

func (kind NodeKind) Valid() bool {
	switch kind {
	case NodeSourceRevision, NodeResolvedConfig, NodeRuntimeImage, NodeDatasetSnapshot,
		NodeFeatureSnapshot, NodeReferenceDatabase, NodeTrainingRun, NodeTrainingAttempt,
		NodeCheckpoint, NodeModelBundle, NodeRuntimeBundle, NodeEvaluation,
		NodeSafetyEvidence, NodeReleaseEvidence, NodeDeployment:
		return true
	default:
		return false
	}
}

type Classification string

const (
	ClassificationInternal     Classification = "internal"
	ClassificationConfidential Classification = "confidential"
	ClassificationRestricted   Classification = "restricted"
)

func (classification Classification) Valid() bool {
	switch classification {
	case ClassificationInternal, ClassificationConfidential, ClassificationRestricted:
		return true
	default:
		return false
	}
}

// Node contains only immutable identity. Locations, credentials, presigned URLs,
// raw samples, prompts, and model bytes deliberately cannot be represented here.
type Node struct {
	NodeID         string             `json:"node_id"`
	Kind           NodeKind           `json:"kind"`
	Digest         identifiers.Digest `json:"digest"`
	CanonicalID    string             `json:"canonical_id,omitempty"`
	Classification Classification     `json:"classification"`
}

// Graph uses input-to-output edge direction and is content addressed by Digest.
type Graph struct {
	SchemaVersion int                `json:"schema_version"`
	GraphID       string             `json:"graph_id"`
	SubjectDigest identifiers.Digest `json:"subject_digest"`
	PolicyDigest  identifiers.Digest `json:"policy_digest"`
	Nodes         []Node             `json:"nodes"`
	Edges         []Edge             `json:"edges"`
}

func (graph Graph) Validate() error {
	if graph.SchemaVersion != SchemaVersion {
		return invalid("lineage_schema_unsupported", "lineage schema version is unsupported", nil)
	}
	if _, err := identifiers.ParseIDKind(graph.GraphID, identifiers.MustParseKind("lineage")); err != nil {
		return invalid("lineage_graph_id_invalid", "lineage graph id must be canonical", err)
	}
	if !graph.SubjectDigest.Valid() || !graph.PolicyDigest.Valid() {
		return invalid("lineage_graph_header_invalid", "lineage subject and policy digests are required", nil)
	}
	if len(graph.Nodes) == 0 || len(graph.Nodes) > MaximumNodes {
		return invalid("lineage_node_count_invalid", "lineage node count is outside bounds", nil)
	}
	if len(graph.Edges) > MaximumEdges {
		return invalid("lineage_edge_count_invalid", "lineage edge count is outside bounds", nil)
	}

	nodes := make(map[string]Node, len(graph.Nodes))
	identities := make(map[string]struct{}, len(graph.Nodes))
	subjects := 0
	for _, node := range graph.Nodes {
		if !validName(node.NodeID) || !node.Kind.Valid() || !node.Digest.Valid() || !node.Classification.Valid() {
			return invalid("lineage_node_invalid", "lineage node is incomplete or invalid", nil)
		}
		if node.CanonicalID != "" {
			if _, err := identifiers.ParseID(node.CanonicalID); err != nil {
				return invalid("lineage_canonical_id_invalid", "lineage canonical id is invalid", err)
			}
		}
		if _, exists := nodes[node.NodeID]; exists {
			return invalid("lineage_node_duplicate", "lineage node ids must be unique", nil)
		}
		identity := string(node.Kind) + "|" + node.Digest.String()
		if _, exists := identities[identity]; exists {
			return invalid("lineage_identity_duplicate", "lineage kind and digest identities must be unique", nil)
		}
		nodes[node.NodeID] = node
		identities[identity] = struct{}{}
		if node.Digest.Equal(graph.SubjectDigest) {
			subjects++
		}
	}
	if subjects != 1 {
		return invalid("lineage_subject_missing", "lineage graph must contain exactly one subject node", nil)
	}

	adjacency := make(map[string][]string, len(graph.Nodes))
	edges := make(map[string]struct{}, len(graph.Edges))
	for _, edge := range graph.Edges {
		if _, exists := nodes[edge.From]; !exists {
			return invalid("lineage_edge_source_missing", "lineage edge source is missing", nil)
		}
		if _, exists := nodes[edge.To]; !exists {
			return invalid("lineage_edge_target_missing", "lineage edge target is missing", nil)
		}
		if edge.From == edge.To || !edge.Relation.Valid() {
			return invalid("lineage_edge_invalid", "lineage edge is invalid", nil)
		}
		identity := edge.From + "|" + edge.To + "|" + string(edge.Relation)
		if _, exists := edges[identity]; exists {
			return invalid("lineage_edge_duplicate", "lineage edges must be unique", nil)
		}
		edges[identity] = struct{}{}
		adjacency[edge.From] = append(adjacency[edge.From], edge.To)
	}

	state := make(map[string]uint8, len(graph.Nodes))
	var visit func(string) error
	visit = func(node string) error {
		switch state[node] {
		case 1:
			return invalid("lineage_graph_cycle", "lineage graph must be acyclic", nil)
		case 2:
			return nil
		}
		state[node] = 1
		for _, target := range adjacency[node] {
			if err := visit(target); err != nil {
				return err
			}
		}
		state[node] = 2
		return nil
	}
	for node := range nodes {
		if err := visit(node); err != nil {
			return err
		}
	}
	return nil
}

func (graph Graph) Digest() (identifiers.Digest, error) {
	if err := graph.Validate(); err != nil {
		return identifiers.Digest{}, err
	}
	nodes := append([]Node(nil), graph.Nodes...)
	edges := append([]Edge(nil), graph.Edges...)
	sort.Slice(nodes, func(left, right int) bool { return nodes[left].NodeID < nodes[right].NodeID })
	sort.Slice(edges, func(left, right int) bool {
		return edgeKey(edges[left]) < edgeKey(edges[right])
	})

	var document strings.Builder
	document.WriteString("mindclade-lineage/v1\n")
	document.WriteString(graph.GraphID + "\n")
	document.WriteString(graph.SubjectDigest.String() + "\n")
	document.WriteString(graph.PolicyDigest.String() + "\n")
	for _, node := range nodes {
		document.WriteString(node.NodeID + "|" + string(node.Kind) + "|" + node.Digest.String() + "|" + node.CanonicalID + "|" + string(node.Classification) + "\n")
	}
	for _, edge := range edges {
		document.WriteString(edgeKey(edge) + "\n")
	}
	return identifiers.SHA256String(document.String()), nil
}

func edgeKey(edge Edge) string {
	return edge.From + "|" + edge.To + "|" + string(edge.Relation)
}

func validName(value string) bool {
	if len(value) < 2 || len(value) > 128 || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' && character != '_' {
			return false
		}
	}
	return true
}
