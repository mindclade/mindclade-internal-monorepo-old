// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package tasks carries durable background work: the leased queue and the
// workers that drain it.
package tasks

import (
	"go.mindclade.dev/libs/go/coordination/workqueue"
	"go.mindclade.dev/libs/go/servicekit"
	"go.mindclade.dev/libs/go/servicekit/production"
	"go.mindclade.dev/services/control_plane/internal/foundation"
)

type Mechanisms struct {
	Queue   workqueue.Store
	Workers map[string]servicekit.Component
}

func (mechanisms Mechanisms) declarations() []foundation.Declaration {
	declarations := []foundation.Declaration{
		{Capability: production.CapabilityWorkQueueStore, Present: !foundation.IsNil(mechanisms.Queue)},
	}
	// Workers are registered by name in stable order so a process assembles the
	// same way on every start.
	for _, name := range foundation.SortedKeys(mechanisms.Workers) {
		component := mechanisms.Workers[name]
		if component.Name == "" {
			continue
		}
		declarations = append(declarations, foundation.Declaration{
			Capability: production.CapabilityWorkQueueWorker,
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
