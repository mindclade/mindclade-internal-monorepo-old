// Copyright 2026 Mindclade. All rights reserved.
package artifacts

import "context"

type Service struct{ Catalog Catalog }

func (s Service) Register(ctx context.Context, r Ref, locations ...Location) error {
	if s.Catalog == nil {
		return invalid("artifact_catalog_unavailable", "artifact catalog is unavailable", nil)
	}
	if err := s.Catalog.Put(ctx, r); err != nil {
		return err
	}
	for _, l := range locations {
		if !l.Artifact.EqualIdentity(r) {
			return invalid("artifact_location_identity_mismatch", "artifact location identity does not match registered artifact", nil)
		}
		if err := s.Catalog.PutLocation(ctx, l); err != nil {
			return err
		}
	}
	return nil
}
