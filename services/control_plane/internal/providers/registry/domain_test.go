// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package registry

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"go.mindclade.dev/control/artifacts"
	"go.mindclade.dev/control/registry/releases"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/storage/sql/sqltest"
)

type releaseRepository struct {
	graphWrites   int
	releaseWrites int
	releaseError  error
}

type releaseVerifier struct{}

func (releaseVerifier) VerifyEvidence(context.Context, releases.EvidenceGraph, releases.EvidenceNode) error {
	return nil
}

func (repository *releaseRepository) PutGraph(context.Context, releases.EvidenceGraph) error {
	repository.graphWrites++
	return nil
}

func (repository *releaseRepository) PromoteRelease(context.Context, releases.Release) error {
	repository.releaseWrites++
	return repository.releaseError
}

func releaseFixture(t *testing.T) (releases.Release, releases.EvidenceGraph) {
	t.Helper()
	subject := identifiers.SHA256String("model")
	policy := identifiers.SHA256String("policy")
	graph := releases.EvidenceGraph{
		ReleaseID:     "release_0000000003e870008000000000000000",
		SubjectDigest: subject,
		PolicyDigest:  policy,
		PolicyEpoch:   1,
		Nodes: []releases.EvidenceNode{{
			NodeID: "source", Kind: releases.EvidenceSourceCommit,
			SubjectDigest: subject, PolicyDigest: policy, Passed: true,
			Created: time.Unix(1_800_000_000, 0).UTC(),
			Artifact: artifacts.Ref{
				Digest: identifiers.SHA256String("source"), SizeBytes: 1,
				MediaType: "application/json", LogicalKind: "source", SchemaVersion: 1,
			},
		}},
	}
	digest, err := graph.Digest()
	if err != nil {
		t.Fatal(err)
	}
	return releases.Release{
		ReleaseID: graph.ReleaseID, ModelBundleDigest: subject,
		EvidenceGraphDigest: digest, Channel: "candidate", Status: "qualified", ResourceVersion: 3,
	}, graph
}

func TestReleasePromotionUsesOneTransaction(t *testing.T) {
	state := &sqltest.State{Exec: func(context.Context, string, []driver.NamedValue) (driver.Result, error) {
		return driver.RowsAffected(1), nil
	}}
	db, err := sqltest.Open(state)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	repository := &releaseRepository{}
	release, graph := releaseFixture(t)
	engine := transactionalReleaseEngine{
		beginner: db,
		service: releases.Service{
			Repository: repository,
			Policy: releases.PromotionPolicy{
				RequireAllPassed: true, ActivePolicyDigest: graph.PolicyDigest, ActivePolicyEpoch: graph.PolicyEpoch,
			},
			Verifier: releaseVerifier{},
		},
	}
	if err := engine.Promote(context.Background(), release, graph); err != nil {
		t.Fatal(err)
	}
	if repository.graphWrites != 1 || repository.releaseWrites != 1 || state.Begins.Load() != 1 || state.Commits.Load() != 1 || state.Rollbacks.Load() != 0 {
		t.Fatalf("repo=%+v begins=%d commits=%d rollbacks=%d", repository, state.Begins.Load(), state.Commits.Load(), state.Rollbacks.Load())
	}
}

func TestReleasePromotionRollsBackARejectedReleaseWrite(t *testing.T) {
	state := &sqltest.State{Exec: func(context.Context, string, []driver.NamedValue) (driver.Result, error) {
		return driver.RowsAffected(1), nil
	}}
	db, err := sqltest.Open(state)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	sentinel := errors.New("injected release write failure")
	repository := &releaseRepository{releaseError: sentinel}
	release, graph := releaseFixture(t)
	engine := transactionalReleaseEngine{
		beginner: db,
		service: releases.Service{
			Repository: repository,
			Policy: releases.PromotionPolicy{
				RequireAllPassed: true, ActivePolicyDigest: graph.PolicyDigest, ActivePolicyEpoch: graph.PolicyEpoch,
			},
			Verifier: releaseVerifier{},
		},
	}
	if err := engine.Promote(context.Background(), release, graph); !errors.Is(err, sentinel) {
		t.Fatalf("Promote error = %v, want injected failure", err)
	}
	if state.Commits.Load() != 0 || state.Rollbacks.Load() != 1 {
		t.Fatalf("commits=%d rollbacks=%d", state.Commits.Load(), state.Rollbacks.Load())
	}
}
