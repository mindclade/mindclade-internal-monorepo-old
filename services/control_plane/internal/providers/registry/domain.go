// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

package registry

import (
	"context"
	"database/sql"

	"go.mindclade.dev/control/registry/models"
	"go.mindclade.dev/control/registry/releases"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/storage/sql/transaction"
)

// modelEngine is the domain surface the registry HTTP adapter consumes.
// Concrete repository selection stays in the role factory; the transport can
// be tested without a database.
type modelEngine interface {
	Publish(context.Context, models.Descriptor) (models.Descriptor, error)
	Resolve(context.Context, identifiers.Digest) (models.Descriptor, error)
}

type releaseEngine interface {
	Promote(context.Context, releases.Release, releases.EvidenceGraph) error
}

type domains struct {
	models   modelEngine
	releases releaseEngine
}

// transactionalReleaseEngine supplies the commit boundary the reusable
// release policy intentionally does not own. PutGraph and PutRelease resolve
// the transaction from context, so both durable writes either commit together
// or neither does.
type transactionalReleaseEngine struct {
	beginner transaction.Beginner
	service  releases.Service
}

func (engine transactionalReleaseEngine) Promote(ctx context.Context, release releases.Release, graph releases.EvidenceGraph) error {
	return transaction.RunVoid(ctx, engine.beginner, transaction.Options{Isolation: sql.LevelSerializable},
		func(txContext context.Context, _ *sql.Tx) error {
			return engine.service.Promote(txContext, release, graph)
		})
}
