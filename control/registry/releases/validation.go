// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package releases

import (
	"encoding/json"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
	"sort"
	"strings"
	"time"
)

const (
	MaximumEvidenceNodes   = 256
	MaximumEvidenceEdges   = 4096
	maximumResourceVersion = uint64(1<<63 - 1)
)

var releaseIDKind = identifiers.MustParseKind("release")

func invalid(reason, message string, cause error) error {
	if cause == nil {
		return faults.New(faults.CodeInvalidArgument, message, faults.WithReason(reason), faults.WithOperation("control.registry.releases"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return faults.Wrap(cause, faults.CodeInvalidArgument, message, faults.WithReason(reason), faults.WithOperation("control.registry.releases"), faults.WithRetryPolicy(faults.NoRetry()))
}

// ValidateQualifiedCandidate validates the exact durable source state accepted
// by the bounded evidence-promotion operation. Promotion is deliberately not a
// create operation or a generic channel transition.
func (r Release) ValidateQualifiedCandidate() error {
	if _, err := identifiers.ParseIDKind(r.ReleaseID, releaseIDKind); err != nil {
		return invalid("release_id_invalid", "release id must be canonical", err)
	}
	if !r.ModelBundleDigest.Valid() || !r.EvidenceGraphDigest.Valid() {
		return invalid("release_digests_required", "release subject and evidence graph digests are required", nil)
	}
	if r.Channel != "candidate" || r.Status != "qualified" {
		return invalid("release_not_qualified_candidate", "release must be a qualified candidate before promotion", nil)
	}
	if r.ResourceVersion == 0 || r.ResourceVersion > maximumResourceVersion {
		return invalid("release_resource_version_invalid", "release promotion requires a durable resource version", nil)
	}
	return nil
}

func (g EvidenceGraph) Validate() error {
	if _, err := identifiers.ParseIDKind(g.ReleaseID, releaseIDKind); err != nil {
		return invalid("release_id_invalid", "release id must be canonical", err)
	}
	if len(g.Nodes) == 0 || len(g.Nodes) > MaximumEvidenceNodes || len(g.Edges) > MaximumEvidenceEdges {
		return invalid("evidence_graph_size", "release evidence graph is outside node or edge bounds", nil)
	}
	if !g.SubjectDigest.Valid() || !g.PolicyDigest.Valid() || g.PolicyEpoch == 0 {
		return invalid("evidence_graph_header_invalid", "subject, policy digest, and epoch are required", nil)
	}
	nodes := map[string]EvidenceNode{}
	for _, n := range g.Nodes {
		if !validGraphToken(n.NodeID) || !n.Kind.Valid() || !n.SubjectDigest.Equal(g.SubjectDigest) || !n.PolicyDigest.Equal(g.PolicyDigest) || n.Created.IsZero() || n.Created.Location() != time.UTC || n.Created.Nanosecond()%int(time.Millisecond) != 0 {
			return invalid("evidence_node_invalid", "evidence node is incomplete or references the wrong subject or policy", nil)
		}
		if err := n.Artifact.Validate(); err != nil {
			return err
		}
		if n.Artifact.SizeBytes == 0 || !validArtifactMediaType(n.Artifact.MediaType) || !validGraphToken(n.Artifact.LogicalKind) {
			return invalid("evidence_artifact_contract", "release evidence artifact identity is outside bounds", nil)
		}
		if _, ok := nodes[n.NodeID]; ok {
			return invalid("duplicate_evidence_node", "evidence node ids must be unique", nil)
		}
		nodes[n.NodeID] = n
	}
	adj := map[string][]string{}
	edges := map[string]struct{}{}
	for _, e := range g.Edges {
		if _, ok := nodes[e.From]; !ok {
			return invalid("evidence_edge_source_missing", "evidence edge source is missing", nil)
		}
		if _, ok := nodes[e.To]; !ok {
			return invalid("evidence_edge_target_missing", "evidence edge target is missing", nil)
		}
		if !validGraphToken(e.Relation) {
			return invalid("evidence_edge_relation_required", "evidence edge relation must be canonical", nil)
		}
		key := e.From + "\x00" + e.To + "\x00" + e.Relation
		if _, exists := edges[key]; exists {
			return invalid("duplicate_evidence_edge", "evidence edges must be unique", nil)
		}
		edges[key] = struct{}{}
		adj[e.From] = append(adj[e.From], e.To)
	}
	state := map[string]uint8{}
	var visit func(string) error
	visit = func(id string) error {
		if state[id] == 1 {
			return invalid("evidence_graph_cycle", "release evidence graph must be acyclic", nil)
		}
		if state[id] == 2 {
			return nil
		}
		state[id] = 1
		for _, to := range adj[id] {
			if err := visit(to); err != nil {
				return err
			}
		}
		state[id] = 2
		return nil
	}
	for id := range nodes {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}

func validArtifactMediaType(value string) bool {
	if len(value) == 0 || len(value) > 256 || !strings.Contains(value, "/") {
		return false
	}
	for index := 0; index < len(value); index++ {
		if value[index] < 0x21 || value[index] > 0x7e {
			return false
		}
	}
	return true
}
func (g EvidenceGraph) Digest() (identifiers.Digest, error) {
	if err := g.Validate(); err != nil {
		return identifiers.Digest{}, err
	}
	nodes := append([]EvidenceNode(nil), g.Nodes...)
	edges := append([]EvidenceEdge(nil), g.Edges...)
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].NodeID < nodes[j].NodeID })
	sort.Slice(edges, func(i, j int) bool {
		a := edges[i].From + "|" + edges[i].To + "|" + edges[i].Relation
		b := edges[j].From + "|" + edges[j].To + "|" + edges[j].Relation
		return a < b
	})
	canonical := EvidenceGraph{
		ReleaseID:     g.ReleaseID,
		SubjectDigest: g.SubjectDigest,
		Nodes:         nodes,
		Edges:         edges,
		PolicyDigest:  g.PolicyDigest,
		PolicyEpoch:   g.PolicyEpoch,
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return identifiers.Digest{}, invalid("evidence_graph_encoding", "release evidence graph could not be encoded", err)
	}
	return identifiers.SHA256(append([]byte("release-evidence/v2\n"), encoded...)), nil
}

func validGraphToken(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for index, character := range value {
		if (character >= 'a' && character <= 'z') ||
			(index > 0 && character >= '0' && character <= '9') ||
			(index > 0 && (character == '.' || character == '_' || character == '-')) {
			continue
		}
		return false
	}
	return true
}
