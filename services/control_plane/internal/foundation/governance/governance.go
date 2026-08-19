// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package governance carries the controls that make a mutation defensible
// after the fact: what was recorded, whether a retry is safe, what was signed,
// and which version was written.
package governance

import (
	"go.mindclade.dev/libs/go/audit"
	"go.mindclade.dev/libs/go/idempotency"
	"go.mindclade.dev/libs/go/pagination"
	"go.mindclade.dev/libs/go/servicekit"
	"go.mindclade.dev/libs/go/servicekit/production"
	"go.mindclade.dev/libs/go/signing"
	"go.mindclade.dev/services/control_plane/internal/foundation"
)

type Controls struct {
	Audit                      audit.Recorder
	Idempotency                idempotency.Store
	Signer                     signing.Signer
	Verifier                   signing.Verifier
	Pagination                 *pagination.Codec
	ResourceVersionsConfigured bool
}

func (controls Controls) declarations() []foundation.Declaration {
	return []foundation.Declaration{
		{Capability: production.CapabilityAudit, Present: !foundation.IsNil(controls.Audit)},
		{Capability: production.CapabilityIdempotency, Present: !foundation.IsNil(controls.Idempotency)},
		{
			Capability: production.CapabilitySigning,
			Present:    !foundation.IsNil(controls.Signer) && !foundation.IsNil(controls.Verifier),
		},
		{Capability: production.CapabilityPagination, Present: controls.Pagination != nil},
		{Capability: production.CapabilityResourceVersion, Present: controls.ResourceVersionsConfigured},
	}
}

func (controls Controls) Capabilities() []production.Capability {
	return foundation.Present(controls.declarations())
}

func (controls Controls) ServiceOptions() []servicekit.Option { return nil }

func (controls Controls) Register(builder *production.Builder) error {
	return foundation.Register(builder, controls.declarations())
}
