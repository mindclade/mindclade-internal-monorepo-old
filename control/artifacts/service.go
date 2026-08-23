// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package artifacts

import "context"

type Service struct{ Catalog Catalog }

// Register commits the artifact identity and its locations. Every rejection the
// service itself can make is made before the first write: the catalog binds a
// digest to immutable metadata permanently, so a registration that failed
// halfway used to leave that binding behind and poison the digest — a corrected
// retry with different metadata then failed with an identity conflict against
// state the caller never asked to keep.
//
// The remaining writes are still separate calls. The catalog seam has no
// transaction, so a crash between them can leave a registered identity with no
// location. That is recoverable by replaying Register, because Put and
// PutLocation are both idempotent for identical content, whereas a poisoned
// digest was not.
func (s Service) Register(ctx context.Context, r Ref, locations ...Location) error {
	if s.Catalog == nil {
		return invalid("artifact_catalog_unavailable", "artifact catalog is unavailable", nil)
	}
	if err := r.Validate(); err != nil {
		return err
	}
	for _, l := range locations {
		if err := l.Validate(); err != nil {
			return err
		}
		if !l.Artifact.EqualIdentity(r) {
			return invalid("artifact_location_identity_mismatch", "artifact location identity does not match registered artifact", nil)
		}
	}
	if err := s.Catalog.Put(ctx, r); err != nil {
		return err
	}
	for _, l := range locations {
		if err := s.Catalog.PutLocation(ctx, l); err != nil {
			return err
		}
	}
	return nil
}
