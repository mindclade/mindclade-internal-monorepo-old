// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package releases

import (
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
	"sort"
	"strings"
)

func invalid(reason, message string, cause error) error {
	if cause == nil {
		return faults.New(faults.CodeInvalidArgument, message, faults.WithReason(reason), faults.WithOperation("control.registry.releases"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return faults.Wrap(cause, faults.CodeInvalidArgument, message, faults.WithReason(reason), faults.WithOperation("control.registry.releases"), faults.WithRetryPolicy(faults.NoRetry()))
}
func (g EvidenceGraph) Validate() error {
	if _, err := identifiers.ParseID(g.ReleaseID); err != nil {
		return invalid("release_id_invalid", "release id must be canonical", err)
	}
	if !g.SubjectDigest.Valid() || !g.PolicyDigest.Valid() || g.PolicyEpoch == 0 {
		return invalid("evidence_graph_header_invalid", "subject, policy digest, and epoch are required", nil)
	}
	nodes := map[string]EvidenceNode{}
	for _, n := range g.Nodes {
		if n.NodeID == "" || !n.Kind.Valid() || !n.SubjectDigest.Equal(g.SubjectDigest) || !n.PolicyDigest.Valid() || n.Created.IsZero() {
			return invalid("evidence_node_invalid", "evidence node is incomplete or references the wrong subject", nil)
		}
		if n.Artifact.Digest.Valid() {
			if err := n.Artifact.Validate(); err != nil {
				return err
			}
		}
		if _, ok := nodes[n.NodeID]; ok {
			return invalid("duplicate_evidence_node", "evidence node ids must be unique", nil)
		}
		nodes[n.NodeID] = n
	}
	adj := map[string][]string{}
	for _, e := range g.Edges {
		if _, ok := nodes[e.From]; !ok {
			return invalid("evidence_edge_source_missing", "evidence edge source is missing", nil)
		}
		if _, ok := nodes[e.To]; !ok {
			return invalid("evidence_edge_target_missing", "evidence edge target is missing", nil)
		}
		if e.Relation == "" {
			return invalid("evidence_edge_relation_required", "evidence edge relation is required", nil)
		}
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
	var b strings.Builder
	b.WriteString("release-evidence/v1\n" + g.ReleaseID + "\n" + g.SubjectDigest.String() + "\n" + g.PolicyDigest.String() + "\n")
	for _, n := range nodes {
		b.WriteString(n.NodeID + "|" + string(n.Kind) + "|" + n.Artifact.Digest.String() + "|" + n.PolicyDigest.String() + "|")
		if n.Passed {
			b.WriteString("1\n")
		} else {
			b.WriteString("0\n")
		}
	}
	for _, e := range edges {
		b.WriteString(e.From + "|" + e.To + "|" + e.Relation + "\n")
	}
	return identifiers.SHA256([]byte(b.String())), nil
}
