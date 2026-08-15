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
