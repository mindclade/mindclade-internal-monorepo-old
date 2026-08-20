// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package eventing carries the publication path: the broker endpoints and the
// transactional outbox that feeds them. A role that mutates state and tells
// anyone about it needs this and nothing else from coordination.
package eventing

import (
	"go.mindclade.dev/libs/go/coordination/outbox"
	"go.mindclade.dev/libs/go/messaging"
	"go.mindclade.dev/libs/go/servicekit"
	"go.mindclade.dev/libs/go/servicekit/production"
	"go.mindclade.dev/services/control_plane/internal/foundation"
)

type Mechanisms struct {
	Publisher    messaging.Publisher
	Subscription messaging.Subscription
	Outbox       outbox.Store
	Dispatcher   *outbox.Dispatcher
}

func (mechanisms Mechanisms) declarations() []foundation.Declaration {
	var dispatcher *servicekit.Component
	if mechanisms.Dispatcher != nil {
		component := mechanisms.Dispatcher.Component("outbox-dispatcher")
		dispatcher = &component
	}
	return []foundation.Declaration{
		{
			Capability: production.CapabilityMessaging,
			Present:    !foundation.IsNil(mechanisms.Publisher) || !foundation.IsNil(mechanisms.Subscription),
		},
		{Capability: production.CapabilityOutboxStore, Present: !foundation.IsNil(mechanisms.Outbox)},
		{Capability: production.CapabilityOutboxDispatcher, Present: dispatcher != nil, Component: dispatcher},
	}
}

func (mechanisms Mechanisms) Capabilities() []production.Capability {
	return foundation.Present(mechanisms.declarations())
}

func (mechanisms Mechanisms) ServiceOptions() []servicekit.Option { return nil }

func (mechanisms Mechanisms) Register(builder *production.Builder) error {
	return foundation.Register(builder, mechanisms.declarations())
}
