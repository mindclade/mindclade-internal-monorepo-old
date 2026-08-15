// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package releases

import "context"

type Repository interface {
	PutGraph(context.Context, EvidenceGraph) error
	PutRelease(context.Context, Release) error
}
type Service struct {
	Repository Repository
	Policy     PromotionPolicy
}

func (s Service) Promote(ctx context.Context, r Release, g EvidenceGraph) error {
	if s.Repository == nil {
		return invalid("release_repository_unavailable", "release repository is unavailable", nil)
	}
	if err := s.Policy.Evaluate(g); err != nil {
		return err
	}
	digest, err := g.Digest()
	if err != nil {
		return err
	}
	if !r.ModelBundleDigest.Equal(g.SubjectDigest) || !r.EvidenceGraphDigest.Equal(digest) {
		return invalid("release_subject_or_graph_mismatch", "release subject or evidence graph digest does not match", nil)
	}
	if r.Channel == "" || r.Status != "qualified" {
		return invalid("release_not_qualified", "release must be qualified before promotion", nil)
	}
	if err = s.Repository.PutGraph(ctx, g); err != nil {
		return err
	}
	r.Status = "promoted"
	return s.Repository.PutRelease(ctx, r)
}
