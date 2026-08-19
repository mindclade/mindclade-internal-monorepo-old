// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package projection carries ordered event consumption: the idempotent inbox,
// the compare-and-advance cursors, and the projector loops that use both.
package projection

import (
	"go.mindclade.dev/libs/go/coordination/cursor"
	"go.mindclade.dev/libs/go/coordination/inbox"
	"go.mindclade.dev/libs/go/coordination/projector"
	"go.mindclade.dev/libs/go/servicekit"
	"go.mindclade.dev/libs/go/servicekit/production"
	"go.mindclade.dev/services/control_plane/internal/foundation"
)

type Mechanisms struct {
	Cursors    cursor.Store
	Inbox      *inbox.Processor
	Projectors map[string]*projector.Processor
}

func (mechanisms Mechanisms) declarations() []foundation.Declaration {
	declarations := []foundation.Declaration{
		{Capability: production.CapabilityCursorStore, Present: !foundation.IsNil(mechanisms.Cursors)},
		{Capability: production.CapabilityInboxProcessor, Present: mechanisms.Inbox != nil},
	}
	for _, name := range foundation.SortedKeys(mechanisms.Projectors) {
		processor := mechanisms.Projectors[name]
		if processor == nil {
			continue
		}
		component := processor.Component("projector/" + name)
		declarations = append(declarations, foundation.Declaration{
			Capability: production.CapabilityProjector,
			Present:    true,
			Component:  &component,
		})
	}
	return declarations
}

func (mechanisms Mechanisms) Capabilities() []production.Capability {
	return foundation.Present(mechanisms.declarations())
}

func (mechanisms Mechanisms) ServiceOptions() []servicekit.Option { return nil }

func (mechanisms Mechanisms) Register(builder *production.Builder) error {
	return foundation.Register(builder, mechanisms.declarations())
}
