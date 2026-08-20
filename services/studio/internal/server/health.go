// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package server

import (
	"context"
	"database/sql"
	"errors"
	"sync/atomic"
	"time"

	"go.mindclade.dev/libs/go/servicekit"
)

// probeTimeout caps how long one readiness check may take.
//
// Deliberately shorter than any sane kubelet timeoutSeconds: a wedged database
// is not the same as a down one, and a probe that hangs until the kubelet gives
// up holds a connection for the whole window while reporting nothing the
// kubelet could not already infer.
const probeTimeout = 2 * time.Second

// DrainPropagationDelay is how long readiness fails before the listener closes.
//
// Removing a pod from a Service's Endpoints is NOT synchronous with its
// readiness probe failing — kube-proxy and the load-balancer backend each learn
// on their own schedule. Closing the listener the instant SIGTERM lands means
// traffic is still being routed here for however long that takes, and it arrives
// at a closed socket as a 502. This delay is that propagation window, and the
// process serves normally throughout it.
const DrainPropagationDelay = 5 * time.Second

// errDraining fails readiness once shutdown has begun.
//
// A plain error rather than a structured fault: nothing renders it. The probe
// handler reports a name and a status, and this value never leaves the process.
var errDraining = errors.New("server: draining")

// Health answers the liveness and readiness probes for one studio process.
//
// # Liveness and readiness deliberately do not give the same answer
//
// Liveness reports only whether this process is still functioning, and never
// touches the database. If it did, one database outage would fail liveness on
// every pod simultaneously — and the kubelet's response to a failed liveness
// probe is to KILL the container, converting a recoverable dependency outage
// into an estate-wide crash loop.
//
// Readiness reports whether this process should be sent traffic right now. That
// question does depend on the database, and it must answer no as soon as a
// drain starts, which is the whole reason the two are separate.
type Health struct {
	draining  atomic.Bool
	readiness *servicekit.ProbeSet
}

// NewHealth builds the probe answerer for a role.
//
// A nil db is the embed role, which has no database by design: it registers no
// readiness probe and is ready whenever it is not draining.
func NewHealth(db *sql.DB) (*Health, error) {
	set, err := servicekit.NewProbeSet(probeTimeout)
	if err != nil {
		return nil, err
	}
	if db != nil {
		if err := set.Register("database", func(ctx context.Context) error {
			return db.PingContext(ctx)
		}); err != nil {
			return nil, err
		}
	}
	return &Health{readiness: set}, nil
}

// BeginDrain makes readiness fail from here on. It is idempotent, and there is
// no way back: a process that has begun draining is on its way out.
func (h *Health) BeginDrain() { h.draining.Store(true) }

// Liveness reports process health only. See the type comment for why this must
// not consult the database.
func (h *Health) Liveness(context.Context) servicekit.ProbeReport {
	checkedAt := time.Now().UTC()
	return servicekit.ProbeReport{
		OK:        true,
		CheckedAt: checkedAt,
		Results:   []servicekit.ProbeResult{{Name: "process", OK: true, CheckedAt: checkedAt}},
	}
}

// Readiness reports whether this process should receive traffic.
func (h *Health) Readiness(ctx context.Context) servicekit.ProbeReport {
	if h.draining.Load() {
		checkedAt := time.Now().UTC()
		return servicekit.ProbeReport{
			OK:        false,
			CheckedAt: checkedAt,
			Results: []servicekit.ProbeResult{{
				Name:      "lifecycle",
				OK:        false,
				CheckedAt: checkedAt,
				Err:       errDraining,
			}},
		}
	}
	return h.readiness.Check(ctx)
}
