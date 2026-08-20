// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package leasing carries singleton authority: the lease store and the elector
// built on it. Roles that must not run two active copies need it.
package leasing

import (
	"go.mindclade.dev/libs/go/coordination/leadership"
	"go.mindclade.dev/libs/go/servicekit"
	"go.mindclade.dev/libs/go/servicekit/production"
	"go.mindclade.dev/libs/go/storage/lease"
	"go.mindclade.dev/services/control_plane/internal/foundation"
)

type Mechanisms struct {
	Leases lease.Store
	Leader *leadership.Elector
}

func (mechanisms Mechanisms) declarations() []foundation.Declaration {
	var elector *servicekit.Component
	if mechanisms.Leader != nil {
		component := mechanisms.Leader.Component("leadership")
		elector = &component
	}
	return []foundation.Declaration{
		{Capability: production.CapabilityLeaseStore, Present: !foundation.IsNil(mechanisms.Leases)},
		{Capability: production.CapabilityLeadership, Present: elector != nil, Component: elector},
	}
}

func (mechanisms Mechanisms) Capabilities() []production.Capability {
	return foundation.Present(mechanisms.declarations())
}

func (mechanisms Mechanisms) ServiceOptions() []servicekit.Option { return nil }

func (mechanisms Mechanisms) Register(builder *production.Builder) error {
	return foundation.Register(builder, mechanisms.declarations())
}
