// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package identity carries caller authentication and authorization. Only roles
// that terminate external requests need it; background roles authenticate
// nothing.
package identity

import (
	"go.mindclade.dev/libs/go/auth"
	"go.mindclade.dev/libs/go/servicekit"
	"go.mindclade.dev/libs/go/servicekit/production"
	"go.mindclade.dev/services/control_plane/internal/foundation"
)

type Controls struct {
	Authenticator auth.Authenticator
	Authorizer    auth.Authorizer
}

func (controls Controls) declarations() []foundation.Declaration {
	return []foundation.Declaration{
		{Capability: production.CapabilityAuthentication, Present: !foundation.IsNil(controls.Authenticator)},
		{Capability: production.CapabilityAuthorization, Present: !foundation.IsNil(controls.Authorizer)},
	}
}

func (controls Controls) Capabilities() []production.Capability {
	return foundation.Present(controls.declarations())
}

func (controls Controls) ServiceOptions() []servicekit.Option { return nil }

func (controls Controls) Register(builder *production.Builder) error {
	return foundation.Register(builder, controls.declarations())
}
