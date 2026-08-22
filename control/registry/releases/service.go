// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package releases

import "context"

type Repository interface {
	PutGraph(context.Context, EvidenceGraph) error
	// PromoteRelease compare-and-swaps an already durable qualified candidate.
	// It must not create a release and must match the immutable release identity,
	// candidate channel, qualified status, and resource version before changing
	// only the durable status and resource version.
	PromoteRelease(context.Context, Release) error
}

// EvidenceVerifier resolves immutable artifacts and verifies their typed
// attestation, signer authority, subject, policy, profile, and freshness. The
// caller-provided Kind and Passed fields are never sufficient promotion proof.
type EvidenceVerifier interface {
	VerifyEvidence(context.Context, EvidenceGraph, EvidenceNode) error
}

type Service struct {
	Repository Repository
	Policy     PromotionPolicy
	Verifier   EvidenceVerifier
}

func (s Service) Promote(ctx context.Context, r Release, g EvidenceGraph) error {
	if s.Repository == nil {
		return invalid("release_repository_unavailable", "release repository is unavailable", nil)
	}
	if s.Verifier == nil {
		return invalid("release_evidence_verifier_unavailable", "release evidence verifier is unavailable", nil)
	}
	if err := r.ValidateQualifiedCandidate(); err != nil {
		return err
	}
	if r.ReleaseID != g.ReleaseID {
		return invalid("release_identity_mismatch", "release and evidence graph identities do not match", nil)
	}
	if err := s.Policy.Evaluate(g); err != nil {
		return err
	}
	for _, node := range g.Nodes {
		if err := s.Verifier.VerifyEvidence(ctx, g, node); err != nil {
			return err
		}
	}
	digest, err := g.Digest()
	if err != nil {
		return err
	}
	if !r.ModelBundleDigest.Equal(g.SubjectDigest) || !r.EvidenceGraphDigest.Equal(digest) {
		return invalid("release_subject_or_graph_mismatch", "release subject or evidence graph digest does not match", nil)
	}
	if err = s.Repository.PutGraph(ctx, g); err != nil {
		return err
	}
	return s.Repository.PromoteRelease(ctx, r)
}
