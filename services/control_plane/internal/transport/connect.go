// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package transport

import (
	"net/http"

	"go.mindclade.dev/libs/go/connectx"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/servicekit/production"
)

// ConnectMount is one generated Connect handler and its canonical procedure
// path. Generated packages own handler construction; this adapter owns safe mux
// registration and capability reporting.
type ConnectMount struct {
	Path    string
	Handler http.Handler
}

// MountConnect registers all handlers fail-fast. Connect shares the HTTP
// listener lifecycle and is therefore a passive production capability.
func MountConnect(mux connectx.Mux, mounts ...ConnectMount) (production.Capability, error) {
	if len(mounts) == 0 {
		return "", faults.New(
			faults.CodeInvalidArgument,
			"at least one Connect handler is required",
			faults.WithReason("empty_connect_mounts"),
			faults.WithOperation("controlplane.transport.MountConnect"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	for _, mount := range mounts {
		if err := connectx.Mount(mux, mount.Path, mount.Handler); err != nil {
			return "", err
		}
	}
	return production.CapabilityConnect, nil
}
