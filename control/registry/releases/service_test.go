// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package releases

import (
	"go.mindclade.dev/control/artifacts"
	"go.mindclade.dev/libs/go/identifiers"
	"testing"
	"time"
)

func TestEvidenceGraphRejectsCycleAndRequiresKinds(t *testing.T) {
	id, _ := identifiers.NewID(identifiers.MustParseKind("release"))
	subject := identifiers.SHA256([]byte("model"))
	policy := identifiers.SHA256([]byte("policy"))
	a := artifacts.Ref{Digest: identifiers.SHA256([]byte("a")), SizeBytes: 1, MediaType: "application/json", LogicalKind: "evidence", SchemaVersion: 1}
	g := EvidenceGraph{ReleaseID: id.String(), SubjectDigest: subject, PolicyDigest: policy, PolicyEpoch: 1, Nodes: []EvidenceNode{{NodeID: "a", Kind: EvidenceSafety, Artifact: a, SubjectDigest: subject, PolicyDigest: policy, Passed: true, Created: time.Now()}, {NodeID: "b", Kind: EvidenceEvaluation, Artifact: a, SubjectDigest: subject, PolicyDigest: policy, Passed: true, Created: time.Now()}}, Edges: []EvidenceEdge{{From: "a", To: "b", Relation: "supports"}, {From: "b", To: "a", Relation: "supports"}}}
	if err := g.Validate(); err == nil {
		t.Fatal("expected cycle")
	}
	g.Edges = nil
	if err := (PromotionPolicy{Required: []EvidenceKind{EvidenceSafety, EvidenceToolchain}, RequireAllPassed: true}).Evaluate(g); err == nil {
		t.Fatal("expected missing toolchain evidence")
	}
}
