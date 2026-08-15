// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package servicekit

import (
	"testing"
	"time"

	"mindclade.internal/libs/go/faults"
)

func TestStateStringAndTerminal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		state    State
		name     string
		terminal bool
	}{
		{StateNew, "new", false},
		{StateStarting, "starting", false},
		{StateRunning, "running", false},
		{StateDraining, "draining", false},
		{StateStopping, "stopping", false},
		{StateStopped, "stopped", true},
		{StateFailed, "failed", true},
		{State(255), "unknown", false},
	}

	for _, test := range tests {
		if got := test.state.String(); got != test.name {
			t.Errorf("State(%d).String() = %q, want %q", test.state, got, test.name)
		}
		if got := test.state.Terminal(); got != test.terminal {
			t.Errorf("State(%d).Terminal() = %v, want %v", test.state, got, test.terminal)
		}
	}
}

func TestSnapshotFields(t *testing.T) {
	t.Parallel()

	cause := faults.New(
		faults.CodeUnavailable,
		"dependency unavailable",
		faults.WithField("dependency", "postgres"),
	)
	snapshot := Snapshot{
		Name:  "api",
		State: StateFailed,
		Since: time.Date(2026, 8, 12, 17, 0, 0, 0, time.UTC),
		Cause: cause,
	}
	fields := snapshot.Fields()
	if fields[FieldServiceName] != "api" || fields[FieldServiceState] != "failed" || fields["dependency"] != "postgres" {
		t.Fatalf("unexpected fields: %#v", fields)
	}
	fields[FieldServiceName] = "mutated"
	if snapshot.Fields()[FieldServiceName] != "api" {
		t.Fatal("Snapshot.Fields returned shared state")
	}
}
