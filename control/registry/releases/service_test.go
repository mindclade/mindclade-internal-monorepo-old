// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package releases

import (
	"context"
	"go.mindclade.dev/control/artifacts"
	"go.mindclade.dev/libs/go/identifiers"
	"testing"
	"time"
)

type evidenceVerifierFunc func(context.Context, EvidenceGraph, EvidenceNode) error

func (verify evidenceVerifierFunc) VerifyEvidence(ctx context.Context, graph EvidenceGraph, node EvidenceNode) error {
	return verify(ctx, graph, node)
}

func configuredPolicy(graph EvidenceGraph, required ...EvidenceKind) PromotionPolicy {
	return PromotionPolicy{
		Required:           required,
		RequireAllPassed:   true,
		ActivePolicyDigest: graph.PolicyDigest,
		ActivePolicyEpoch:  graph.PolicyEpoch,
	}
}

func TestEvidenceGraphRejectsCycleAndRequiresKinds(t *testing.T) {
	id, _ := identifiers.NewID(identifiers.MustParseKind("release"))
	subject := identifiers.SHA256([]byte("model"))
	policy := identifiers.SHA256([]byte("policy"))
	a := artifacts.Ref{Digest: identifiers.SHA256([]byte("a")), SizeBytes: 1, MediaType: "application/json", LogicalKind: "evidence", SchemaVersion: 1}
	created := time.Now().UTC().Truncate(time.Millisecond)
	g := EvidenceGraph{ReleaseID: id.String(), SubjectDigest: subject, PolicyDigest: policy, PolicyEpoch: 1, Nodes: []EvidenceNode{{NodeID: "a", Kind: EvidenceSafety, Artifact: a, SubjectDigest: subject, PolicyDigest: policy, Passed: true, Created: created}, {NodeID: "b", Kind: EvidenceEvaluation, Artifact: a, SubjectDigest: subject, PolicyDigest: policy, Passed: true, Created: created}}, Edges: []EvidenceEdge{{From: "a", To: "b", Relation: "supports"}, {From: "b", To: "a", Relation: "supports"}}}
	if err := g.Validate(); err == nil {
		t.Fatal("expected cycle")
	}
	g.Edges = nil
	if err := configuredPolicy(g, EvidenceSafety, EvidenceToolchain).Evaluate(g); err == nil {
		t.Fatal("expected missing toolchain evidence")
	}
	if err := configuredPolicy(g, EvidenceSafety, EvidenceSafety).Evaluate(g); err == nil {
		t.Fatal("expected duplicate required policy kinds to be rejected")
	}
}

func TestEvidenceGraphRequiresReleaseKindAndBoundedCardinality(t *testing.T) {
	wrongID, _ := identifiers.NewID(identifiers.MustParseKind("run"))
	subject := identifiers.SHA256String("model")
	policy := identifiers.SHA256String("policy")
	graph := EvidenceGraph{
		ReleaseID: wrongID.String(), SubjectDigest: subject, PolicyDigest: policy, PolicyEpoch: 1,
		Nodes: []EvidenceNode{{NodeID: "node"}},
	}
	if err := graph.Validate(); err == nil {
		t.Fatal("expected a non-release resource ID to be rejected")
	}
	releaseID, _ := identifiers.NewID(identifiers.MustParseKind("release"))
	graph.ReleaseID = releaseID.String()
	graph.Nodes = make([]EvidenceNode, MaximumEvidenceNodes+1)
	if err := graph.Validate(); err == nil {
		t.Fatal("expected an oversized evidence node set to be rejected")
	}
	graph.Nodes = []EvidenceNode{{NodeID: "node"}}
	graph.Edges = make([]EvidenceEdge, MaximumEvidenceEdges+1)
	if err := graph.Validate(); err == nil {
		t.Fatal("expected an oversized evidence edge set to be rejected")
	}
}

func TestProductionPolicyRequiresTargetProfileAndOperationalEvidence(t *testing.T) {
	required := map[EvidenceKind]bool{}
	for _, kind := range ProductionPolicy().Required {
		required[kind] = true
	}
	for _, kind := range []EvidenceKind{
		EvidenceCheckpointResume,
		EvidenceScale,
		EvidencePerformance,
		EvidenceReliabilityQualification,
		EvidenceSLOApproval,
		EvidenceAlertFireResolve,
		EvidenceCostQualification,
		EvidenceSecurityQualification,
		EvidenceVulnerabilityScan,
		EvidenceLineage,
		EvidenceRollbackDrill,
		EvidenceH1001GQualification,
		EvidenceH1008GQualification,
	} {
		if !required[kind] {
			t.Fatalf("production policy does not require %q", kind)
		}
		if !kind.Valid() {
			t.Fatalf("production evidence kind is invalid: %q", kind)
		}
	}
}

func TestEvidenceGraphRequiresImmutableArtifactAndMatchingPolicy(t *testing.T) {
	id, _ := identifiers.NewID(identifiers.MustParseKind("release"))
	subject := identifiers.SHA256([]byte("model"))
	policy := identifiers.SHA256([]byte("policy"))
	node := EvidenceNode{
		NodeID:        "qualification",
		Kind:          EvidenceNumericalQualification,
		SubjectDigest: subject,
		PolicyDigest:  policy,
		Passed:        true,
		Created:       time.Now().UTC().Truncate(time.Millisecond),
	}
	graph := EvidenceGraph{
		ReleaseID:     id.String(),
		SubjectDigest: subject,
		PolicyDigest:  policy,
		PolicyEpoch:   1,
		Nodes:         []EvidenceNode{node},
	}
	if err := graph.Validate(); err == nil {
		t.Fatal("expected evidence without an immutable artifact to be rejected")
	}
	node.Artifact = artifacts.Ref{
		Digest: identifiers.SHA256([]byte("qualification")), SizeBytes: 1,
		MediaType: "application/json", LogicalKind: "qualification", SchemaVersion: 1,
	}
	node.PolicyDigest = identifiers.SHA256([]byte("different-policy"))
	graph.Nodes = []EvidenceNode{node}
	if err := graph.Validate(); err == nil {
		t.Fatal("expected evidence evaluated under another policy to be rejected")
	}
}

func TestPromotionRequiresConfiguredPolicyAndResolvedEvidence(t *testing.T) {
	id, _ := identifiers.NewID(identifiers.MustParseKind("release"))
	subject := identifiers.SHA256String("model")
	policy := identifiers.SHA256String("approved-policy")
	artifact := artifacts.Ref{
		Digest: identifiers.SHA256String("verified evidence"), SizeBytes: 17,
		MediaType: "application/vnd.mindclade.evidence+json", LogicalKind: "release.evidence", SchemaVersion: 1,
	}
	graph := EvidenceGraph{
		ReleaseID: id.String(), SubjectDigest: subject, PolicyDigest: policy, PolicyEpoch: 7,
		Nodes: []EvidenceNode{{
			NodeID: "numerical", Kind: EvidenceNumericalQualification, Artifact: artifact,
			SubjectDigest: subject, PolicyDigest: policy, Passed: true,
			Created: time.UnixMilli(1_800_000_000_000).UTC(),
		}},
	}
	digest, err := graph.Digest()
	if err != nil {
		t.Fatal(err)
	}
	release := Release{
		ReleaseID: id.String(), ModelBundleDigest: subject, EvidenceGraphDigest: digest,
		Channel: "candidate", Status: "qualified", ResourceVersion: 3,
	}
	repository := &memoryRepository{}
	service := Service{Repository: repository, Policy: configuredPolicy(graph, EvidenceNumericalQualification)}
	if err := service.Promote(context.Background(), release, graph); err == nil {
		t.Fatal("expected a missing evidence verifier to fail closed")
	}
	verified := 0
	service.Verifier = evidenceVerifierFunc(func(_ context.Context, observed EvidenceGraph, node EvidenceNode) error {
		if !observed.PolicyDigest.Equal(policy) || node.NodeID != "numerical" {
			t.Fatal("verifier received the wrong evidence binding")
		}
		verified++
		return nil
	})
	if err := service.Promote(context.Background(), release, graph); err != nil {
		t.Fatal(err)
	}
	if verified != 1 || repository.graphs != 1 || repository.releases != 1 {
		t.Fatalf("verified=%d graphs=%d releases=%d", verified, repository.graphs, repository.releases)
	}
}

func TestPromotionRejectsReleaseAndGraphIdentityMismatch(t *testing.T) {
	id, _ := identifiers.NewID(identifiers.MustParseKind("release"))
	otherID, _ := identifiers.NewID(identifiers.MustParseKind("release"))
	subject := identifiers.SHA256String("model")
	policy := identifiers.SHA256String("approved-policy")
	graph := EvidenceGraph{
		ReleaseID: id.String(), SubjectDigest: subject, PolicyDigest: policy, PolicyEpoch: 7,
		Nodes: []EvidenceNode{{
			NodeID: "numerical", Kind: EvidenceNumericalQualification,
			Artifact: artifacts.Ref{
				Digest: identifiers.SHA256String("verified evidence"), SizeBytes: 17,
				MediaType: "application/vnd.mindclade.evidence+json", LogicalKind: "release.evidence", SchemaVersion: 1,
			},
			SubjectDigest: subject, PolicyDigest: policy, Passed: true,
			Created: time.UnixMilli(1_800_000_000_000).UTC(),
		}},
	}
	digest, err := graph.Digest()
	if err != nil {
		t.Fatal(err)
	}
	release := Release{
		ReleaseID: otherID.String(), ModelBundleDigest: subject, EvidenceGraphDigest: digest,
		Channel: "candidate", Status: "qualified", ResourceVersion: 3,
	}
	service := Service{
		Repository: &memoryRepository{},
		Policy:     configuredPolicy(graph, EvidenceNumericalQualification),
		Verifier: evidenceVerifierFunc(func(context.Context, EvidenceGraph, EvidenceNode) error {
			t.Fatal("evidence verifier must not run for a mismatched release identity")
			return nil
		}),
	}
	if err := service.Promote(context.Background(), release, graph); err == nil {
		t.Fatal("expected release identity mismatch")
	}
}

type memoryRepository struct {
	graphs   int
	releases int
}

func (repository *memoryRepository) PutGraph(context.Context, EvidenceGraph) error {
	repository.graphs++
	return nil
}

func (repository *memoryRepository) PromoteRelease(context.Context, Release) error {
	repository.releases++
	return nil
}

func TestPromotionRejectsNondurableOrArbitraryLifecycle(t *testing.T) {
	id, _ := identifiers.NewID(identifiers.MustParseKind("release"))
	subject := identifiers.SHA256String("model")
	graph := EvidenceGraph{
		ReleaseID: id.String(), SubjectDigest: subject,
		PolicyDigest: identifiers.SHA256String("policy"), PolicyEpoch: 1,
		Nodes: []EvidenceNode{{NodeID: "node"}},
	}
	release := Release{
		ReleaseID: id.String(), ModelBundleDigest: subject,
		EvidenceGraphDigest: identifiers.SHA256String("graph"),
		Channel:             "candidate", Status: "qualified", ResourceVersion: 1,
	}
	for name, mutate := range map[string]func(*Release){
		"create":                 func(value *Release) { value.ResourceVersion = 0 },
		"resource_version_range": func(value *Release) { value.ResourceVersion = 1 << 63 },
		"arbitrary_channel":      func(value *Release) { value.Channel = "production" },
		"arbitrary_status":       func(value *Release) { value.Status = "promoted" },
		"invalid_identifier":     func(value *Release) { value.ReleaseID = "release-not-canonical" },
		"missing_subject":        func(value *Release) { value.ModelBundleDigest = identifiers.Digest{} },
		"missing_graph":          func(value *Release) { value.EvidenceGraphDigest = identifiers.Digest{} },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := release
			mutate(&candidate)
			repository := &memoryRepository{}
			service := Service{Repository: repository, Verifier: evidenceVerifierFunc(func(context.Context, EvidenceGraph, EvidenceNode) error { return nil })}
			if err := service.Promote(context.Background(), candidate, graph); err == nil {
				t.Fatal("invalid lifecycle source was accepted")
			}
			if repository.graphs != 0 || repository.releases != 0 {
				t.Fatal("invalid lifecycle source reached the repository")
			}
		})
	}
}

func TestEvidenceDigestBindsEpochAndFullArtifactIdentity(t *testing.T) {
	id, _ := identifiers.NewID(identifiers.MustParseKind("release"))
	subject := identifiers.SHA256String("model")
	policy := identifiers.SHA256String("policy")
	graph := EvidenceGraph{
		ReleaseID: id.String(), SubjectDigest: subject, PolicyDigest: policy, PolicyEpoch: 1,
		Nodes: []EvidenceNode{{
			NodeID: "source", Kind: EvidenceSourceCommit,
			Artifact:      artifacts.Ref{Digest: identifiers.SHA256String("source"), SizeBytes: 6, MediaType: "application/json", LogicalKind: "source", SchemaVersion: 1},
			SubjectDigest: subject, PolicyDigest: policy, Passed: true,
			Created: time.UnixMilli(1_800_000_000_000).UTC(),
		}},
	}
	baseline, err := graph.Digest()
	if err != nil {
		t.Fatal(err)
	}
	mutations := []func(*EvidenceGraph){
		func(candidate *EvidenceGraph) { candidate.PolicyEpoch++ },
		func(candidate *EvidenceGraph) { candidate.Nodes[0].Artifact.SizeBytes++ },
		func(candidate *EvidenceGraph) { candidate.Nodes[0].Artifact.MediaType = "application/cbor" },
		func(candidate *EvidenceGraph) { candidate.Nodes[0].Artifact.LogicalKind = "other" },
		func(candidate *EvidenceGraph) { candidate.Nodes[0].Artifact.SchemaVersion++ },
		func(candidate *EvidenceGraph) {
			candidate.Nodes[0].Created = candidate.Nodes[0].Created.Add(time.Millisecond)
		},
	}
	for _, mutate := range mutations {
		candidate := graph
		candidate.Nodes = append([]EvidenceNode(nil), graph.Nodes...)
		mutate(&candidate)
		observed, digestErr := candidate.Digest()
		if digestErr != nil {
			t.Fatal(digestErr)
		}
		if observed.Equal(baseline) {
			t.Fatal("material evidence mutation did not change the graph digest")
		}
	}
}
