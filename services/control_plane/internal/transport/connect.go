// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package transport

import (
	"net/http"

	"mindclade.internal/libs/go/connectx"
	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/servicekit/production"
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
