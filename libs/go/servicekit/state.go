// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package servicekit

import (
	"time"

	"go.mindclade.dev/libs/go/faults"
)

// State is the lifecycle state of a Service.
type State uint8

const (
	StateNew State = iota
	StateStarting
	StateRunning
	StateDraining
	StateStopping
	StateStopped
	StateFailed
)

// String returns the stable textual state name.
func (state State) String() string {
	switch state {
	case StateNew:
		return "new"
	case StateStarting:
		return "starting"
	case StateRunning:
		return "running"
	case StateDraining:
		return "draining"
	case StateStopping:
		return "stopping"
	case StateStopped:
		return "stopped"
	case StateFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// Terminal reports whether no further lifecycle transition is expected.
func (state State) Terminal() bool {
	return state == StateStopped || state == StateFailed
}

// CanTransitionTo reports whether to is a legal successor of state.
//
// This is the repository-wide service phase graph, not a Go-only detail. The
// Rust runtime runs the same seven phases (libs/rust/servicekit/src/lifecycle.rs
// enforces this exact edge set on every transition), so a fleet controller that
// models node phases can validate a phase report from either runtime against
// one table. Keeping the two in agreement is what makes the phase a node
// reports mean the same thing as the phase the control plane expects.
//
// Two edges exist because Run must be able to end a process that never served:
// Starting -> Stopping is startup failure, and Starting -> Draining is a
// termination request that arrives before Running is announced. Neither ever
// passes through Running, which is what keeps readiness false for a service
// that never admitted traffic.
func (state State) CanTransitionTo(to State) bool {
	switch state {
	case StateNew:
		return to == StateStarting || to == StateFailed
	case StateStarting:
		return to == StateRunning || to == StateDraining || to == StateStopping || to == StateFailed
	case StateRunning:
		return to == StateDraining || to == StateStopping || to == StateFailed
	case StateDraining:
		return to == StateStopping || to == StateFailed
	case StateStopping:
		return to == StateStopped || to == StateFailed
	default:
		// StateStopped, StateFailed, and any unknown state are terminal.
		return false
	}
}

// Snapshot is an immutable view of current service lifecycle state.
type Snapshot struct {
	Name  string
	State State
	Since time.Time
	Cause error
}

// Fields returns a fresh set of structured lifecycle attributes suitable for
// logging, metrics, or tracing adapters.
func (snapshot Snapshot) Fields() faults.Fields {
	fields := faults.FieldsOf(snapshot.Cause).Merge(faults.Fields{
		FieldServiceName:  snapshot.Name,
		FieldServiceState: snapshot.State.String(),
	})
	if !snapshot.Since.IsZero() {
		fields["state_since"] = snapshot.Since.UTC().Format(time.RFC3339Nano)
	}
	return fields.Clone()
}
