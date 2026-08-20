// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package transport

import (
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/servicekit/production"
	"go.mindclade.dev/services/control_plane/internal/bootstrap"
)

// Bundle combines the transport mechanisms constructed for one process. It
// does not own routes or generated service registration.
type Bundle struct {
	HTTP    *HTTP
	GRPC    *GRPC
	Connect bool
}

// Components converts the bundle into the standard bootstrap representation.
func (bundle Bundle) Components() (bootstrap.Components, error) {
	components := bootstrap.Components{}
	if bundle.HTTP != nil {
		mechanism, err := bundle.HTTP.Mechanism()
		if err != nil {
			return bootstrap.Components{}, err
		}
		components.Mechanisms = append(components.Mechanisms, mechanism)
	}
	if bundle.GRPC != nil {
		mechanism, err := bundle.GRPC.Mechanism()
		if err != nil {
			return bootstrap.Components{}, err
		}
		components.Mechanisms = append(components.Mechanisms, mechanism)
	}
	if bundle.Connect {
		if bundle.HTTP == nil {
			return bootstrap.Components{}, faults.New(
				faults.CodeFailedPrecondition,
				"Connect handlers require the HTTP transport",
				faults.WithReason("connect_without_http"),
				faults.WithOperation("controlplane.transport.Bundle.Components"),
				faults.WithRetryPolicy(faults.NoRetry()),
			)
		}
		components.Passive = append(components.Passive, production.CapabilityConnect)
	}
	if len(components.Mechanisms) == 0 {
		return bootstrap.Components{}, faults.New(
			faults.CodeFailedPrecondition,
			"transport bundle contains no network mechanism",
			faults.WithReason("empty_transport_bundle"),
			faults.WithOperation("controlplane.transport.Bundle.Components"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	return components, nil
}
