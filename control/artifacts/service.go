// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package artifacts

import "context"

type Service struct{ Catalog Catalog }

// Register commits the artifact identity and its locations.
//
// Every rejection the service itself can make is made before the first write:
// the catalog binds a digest to immutable metadata permanently, so a
// registration that failed halfway used to leave that binding behind and poison
// the digest -- a corrected retry with different metadata then failed with an
// identity conflict against state the caller never asked to keep.
//
// The writes themselves are one Catalog.Register call rather than a Put
// followed by N PutLocation calls. Those were separate round trips with no
// commit boundary between them, so a crash after the identity landed left a
// registered artifact whose bytes nothing could find. Replay recovered it --
// both writes are idempotent for identical content -- but only if a replay ever
// happened, and nothing in this package guaranteed one: the window was silent
// and unbounded, not self-healing.
//
// Register is therefore atomic at the seam, and the durable implementation
// makes it one statement. It is deliberately not the audit-and-outbox durable
// mutation path from libs/go/CONSUMPTION.md, and the reason is a limit rather
// than a claim of sufficiency: no composition root constructs a Catalog and no
// event contract names artifact registration, so a recorder and an outbox topic
// added here would be machinery with no publisher and no subscriber. What that
// path buys -- one commit for the domain write -- is bought here by the single
// statement. What it also buys, an audited and published record of the
// mutation, is genuinely absent and stays absent until a caller exists. Adding
// the catalog to a request path means adding both.
func (s Service) Register(ctx context.Context, r Ref, locations ...Location) error {
	if s.Catalog == nil {
		return invalid("artifact_catalog_unavailable", "artifact catalog is unavailable", nil)
	}
	if err := validateRegistration(r, locations); err != nil {
		return err
	}
	return s.Catalog.Register(ctx, r, locations)
}
