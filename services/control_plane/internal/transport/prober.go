// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package transport

import (
	"context"
	"sync/atomic"
	"time"

	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/servicekit"
	"go.mindclade.dev/libs/go/servicekit/production"
)

// Prober forwards health probes to the assembled service. Handlers and
// registrars are built before the service exists, so the runtime is attached
// by the bootstrap Bind hook and read atomically from request goroutines.
//
// Its shape satisfies the probe contracts of all three transports -- httpx,
// connectx, and grpcx health -- so one prober answers for every surface a
// process serves, and they cannot disagree.
type Prober struct {
	runtime atomic.Pointer[production.Runtime]
}

// Bind attaches the assembled runtime. It is the bootstrap Bind hook.
func (value *Prober) Bind(runtime *production.Runtime) error {
	if value == nil || runtime == nil {
		return faults.New(
			faults.CodeInvalidArgument,
			"health prober cannot bind a nil runtime",
			faults.WithReason("nil_bound_runtime"),
			faults.WithOperation("controlplane.transport.Prober.Bind"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	value.runtime.Store(runtime)
	return nil
}

func (value *Prober) Liveness(ctx context.Context) servicekit.ProbeReport {
	runtime := value.runtime.Load()
	if runtime == nil || runtime.Service() == nil {
		return unboundReport()
	}
	return runtime.Service().Liveness(ctx)
}

func (value *Prober) Readiness(ctx context.Context) servicekit.ProbeReport {
	runtime := value.runtime.Load()
	if runtime == nil || runtime.Service() == nil {
		return unboundReport()
	}
	return runtime.Service().Readiness(ctx)
}

// unboundReport fails closed for the window between the listener opening and
// the runtime being bound, so an orchestrator never sees a premature ready.
func unboundReport() servicekit.ProbeReport {
	now := time.Now().UTC()
	return servicekit.ProbeReport{
		OK:        false,
		CheckedAt: now,
		Results:   []servicekit.ProbeResult{{Name: "runtime", OK: false, CheckedAt: now}},
	}
}
