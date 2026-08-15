// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package production

import (
	"context"

	"mindclade.internal/libs/go/servicekit"
)

// Runtime is the immutable result of production process composition. It keeps
// the validated capability manifest and deterministic lifecycle plan beside the
// executable service so diagnostics and qualification evidence cannot drift
// from the process that actually runs.
type Runtime struct {
	manifest Manifest
	service  *servicekit.Service
	entries  []servicekit.AssemblyEntry
}

func newRuntime(manifest Manifest, service *servicekit.Service, entries []servicekit.AssemblyEntry) *Runtime {
	return &Runtime{
		manifest: manifest,
		service:  service,
		entries:  append([]servicekit.AssemblyEntry(nil), entries...),
	}
}

// Manifest returns the immutable validated production manifest.
func (runtime *Runtime) Manifest() Manifest {
	if runtime == nil {
		return Manifest{}
	}
	return runtime.manifest
}

// Entries returns a defensive copy of the deterministic lifecycle plan.
func (runtime *Runtime) Entries() []servicekit.AssemblyEntry {
	if runtime == nil {
		return nil
	}
	return append([]servicekit.AssemblyEntry(nil), runtime.entries...)
}

// Service returns the underlying servicekit lifecycle coordinator for probe
// registration and diagnostics. Process code should normally call Run or
// RunWithSignals on Runtime instead of managing Service directly.
func (runtime *Runtime) Service() *servicekit.Service {
	if runtime == nil {
		return nil
	}
	return runtime.service
}

// Run executes the process until ctx is canceled or a component exits.
func (runtime *Runtime) Run(ctx context.Context) error {
	if runtime == nil || runtime.service == nil {
		return builderFault("nil_production_runtime", "production runtime is not initialized", "servicekit.production.Runtime.Run", "", "", nil)
	}
	return runtime.service.Run(ctx)
}

// RunWithSignals installs the repository-standard signal handling and executes
// the service with bounded, reverse-order shutdown.
func (runtime *Runtime) RunWithSignals(ctx context.Context) error {
	if runtime == nil || runtime.service == nil {
		return builderFault("nil_production_runtime", "production runtime is not initialized", "servicekit.production.Runtime.RunWithSignals", "", "", nil)
	}
	return runtime.service.RunWithSignals(ctx)
}

// Shutdown requests graceful shutdown and waits for completion.
func (runtime *Runtime) Shutdown(ctx context.Context) error {
	if runtime == nil || runtime.service == nil {
		return builderFault("nil_production_runtime", "production runtime is not initialized", "servicekit.production.Runtime.Shutdown", "", "", nil)
	}
	return runtime.service.Shutdown(ctx)
}
