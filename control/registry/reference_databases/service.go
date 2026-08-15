// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package reference_databases

import "context"

type Repository interface {
	Put(context.Context, Release) error
	Get(context.Context, string) (Release, error)
	SetStatus(context.Context, string, Status) error
}
type Service struct {
	Repository Repository
	Policy     PromotionPolicy
}

func (s Service) Register(ctx context.Context, r Release) error {
	if s.Repository == nil {
		return invalid("reference_repository_unavailable", "reference database repository is unavailable", nil)
	}
	if err := r.Validate(); err != nil {
		return err
	}
	return s.Repository.Put(ctx, r)
}
func (s Service) Promote(ctx context.Context, id string) error {
	if s.Repository == nil {
		return invalid("reference_repository_unavailable", "reference database repository is unavailable", nil)
	}
	r, err := s.Repository.Get(ctx, id)
	if err != nil {
		return err
	}
	if err = s.Policy.Allows(r); err != nil {
		return err
	}
	return s.Repository.SetStatus(ctx, id, StatusProduction)
}
