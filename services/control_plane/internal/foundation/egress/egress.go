// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package egress carries the policy-bound outbound HTTP client. It is separate
// because exactly one role calls out to systems this control plane does not
// own, and no other role should link an outbound transport.
package egress

import (
	"go.mindclade.dev/libs/go/httpx/outbound"
	"go.mindclade.dev/libs/go/servicekit"
	"go.mindclade.dev/libs/go/servicekit/production"
	"go.mindclade.dev/services/control_plane/internal/foundation"
)

type Client struct {
	Outbound *outbound.Client
}

func (client Client) declarations() []foundation.Declaration {
	return []foundation.Declaration{
		{Capability: production.CapabilityOutboundHTTP, Present: client.Outbound != nil},
	}
}

func (client Client) Capabilities() []production.Capability {
	return foundation.Present(client.declarations())
}

func (client Client) ServiceOptions() []servicekit.Option { return nil }

func (client Client) Register(builder *production.Builder) error {
	return foundation.Register(builder, client.declarations())
}
