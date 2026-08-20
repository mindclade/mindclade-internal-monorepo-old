// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package servicekit

import (
	"errors"
	"strings"
	"testing"

	"go.mindclade.dev/libs/go/faults"
)

func TestComponentErrorClassification(t *testing.T) {
	t.Parallel()

	cause := faults.New(
		faults.CodeUnavailable,
		"database unavailable",
		faults.WithReason("database_unavailable"),
		faults.WithField("dependency", "postgres"),
		faults.WithRetryPolicy(faults.BackoffRetry(4)),
	)
	err := &ComponentError{
		Service:   "control-plane",
		Component: "database",
		Phase:     PhaseStart,
		Err:       cause,
	}
	if !errors.Is(err, cause) {
		t.Fatal("ComponentError does not unwrap its cause")
	}
	if got := err.Error(); !strings.Contains(got, `component "database" start`) {
		t.Fatalf("ComponentError.Error() = %q", got)
	}
	if faults.CodeOf(err) != faults.CodeUnavailable || faults.ReasonOf(err) != "database_unavailable" {
		t.Fatalf("classification = %s/%q", faults.CodeOf(err), faults.ReasonOf(err))
	}
	fields := faults.FieldsOf(err)
	if fields[FieldServiceName] != "control-plane" ||
		fields[FieldComponentName] != "database" ||
		fields[FieldLifecyclePhase] != "start" ||
		fields["dependency"] != "postgres" {
		t.Fatalf("unexpected fields: %#v", fields)
	}
	if policy := faults.RetryPolicyOf(err); policy.Kind != faults.RetryKindBackoff || policy.MaxAttempts != 4 {
		t.Fatalf("retry policy = %+v", policy)
	}
}

func TestStateErrorClassification(t *testing.T) {
	t.Parallel()

	err := &StateError{Probe: "readiness", State: StateStarting}
	if got := err.Error(); !strings.Contains(got, "starting") {
		t.Fatalf("StateError.Error() = %q", got)
	}
	if faults.CodeOf(err) != faults.CodeFailedPrecondition || faults.ReasonOf(err) != "service_not_ready" {
		t.Fatalf("classification = %s/%q", faults.CodeOf(err), faults.ReasonOf(err))
	}
	fields := faults.FieldsOf(err)
	if fields[FieldProbeName] != "readiness" || fields[FieldServiceState] != "starting" {
		t.Fatalf("unexpected fields: %#v", fields)
	}
}

func TestStructuredFaultPreservesSentinel(t *testing.T) {
	t.Parallel()

	err := duplicateComponentError("api", "worker")
	if !errors.Is(err, ErrDuplicateComponent) {
		t.Fatal("structured fault does not preserve ErrDuplicateComponent")
	}
	if faults.CodeOf(err) != faults.CodeAlreadyExists || faults.ReasonOf(err) != "duplicate_component" {
		t.Fatalf("classification = %s/%q", faults.CodeOf(err), faults.ReasonOf(err))
	}
	fields := faults.FieldsOf(err)
	if fields[FieldServiceName] != "api" || fields[FieldComponentName] != "worker" {
		t.Fatalf("unexpected fields: %#v", fields)
	}
}
