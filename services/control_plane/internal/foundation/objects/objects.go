// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package objects carries the non-relational stores: bulk artifact content and
// the read cache in front of it. Roles that serve or ingest artifacts need
// both; the rest need neither.
package objects

import (
	"go.mindclade.dev/libs/go/servicekit"
	"go.mindclade.dev/libs/go/servicekit/production"
	"go.mindclade.dev/libs/go/storage/blob"
	"go.mindclade.dev/libs/go/storage/cache"
	"go.mindclade.dev/services/control_plane/internal/foundation"
)

type Stores struct {
	Blobs blob.Store
	Cache cache.Store
}

func (stores Stores) declarations() []foundation.Declaration {
	return []foundation.Declaration{
		{Capability: production.CapabilityBlobStore, Present: !foundation.IsNil(stores.Blobs)},
		{Capability: production.CapabilityCache, Present: !foundation.IsNil(stores.Cache)},
	}
}

func (stores Stores) Capabilities() []production.Capability {
	return foundation.Present(stores.declarations())
}

func (stores Stores) ServiceOptions() []servicekit.Option { return nil }

func (stores Stores) Register(builder *production.Builder) error {
	return foundation.Register(builder, stores.declarations())
}
