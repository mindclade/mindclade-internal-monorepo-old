// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// The scheduler seam, which no single package can observe.
//
// This file replaces a tripwire. control/scheduling used to be entirely
// `const scaffold_<file>` reservations, and a test written against that would
// have asserted a constant equals itself while reporting as coverage — so the
// tripwire asserted the reason instead, and said what to write once the package
// became real: "quota and fair-share admission, placement against topology, and
// preemption ordering."
//
// Those three now have thorough in-package tests, which is where they belong;
// re-testing them here would duplicate coverage rather than add it. What no
// in-package test can reach is the seam between the domain and the composition
// root: control/scheduling declares the queue it is drained from and the handler
// contract it satisfies, services/control_plane/internal/providers/scheduler
// builds the worker that drains it, and nothing links the two at compile time.
// A mismatch there is silent — the role drains a queue nobody fills, and every
// test on both sides still passes.
package tests

import (
	"context"
	"encoding/json"
	"testing"

	"go.mindclade.dev/control/scheduling"
	"go.mindclade.dev/libs/go/coordination/workqueue"
	"go.mindclade.dev/libs/go/faults"
)

// The domain's queue name and the worker's must be one value, not two equal
// strings. They were two, and nothing compared them.
func TestPlacementQueueNameIsSharedWithTheSchedulerRole(t *testing.T) {
	if scheduling.PlacementQueue == "" {
		t.Fatal("the scheduling domain must name the queue it is drained from")
	}
	// The provider imports this constant rather than restating it, so equality
	// here is structural. Asserting the literal as well pins the wire name: a
	// rename is a queue migration, not a refactor, because in-flight items sit
	// under the old name.
	const wire = "control-plane/placement"
	if scheduling.PlacementQueue != wire {
		t.Fatalf("placement queue = %q, want %q; renaming it strands in-flight work",
			scheduling.PlacementQueue, wire)
	}
}

// A handler that acknowledges work it cannot perform is worse than one that
// retries: the item leaves the queue and the placement never happens. An
// unconfigured service must fail rather than return a nil error.
func TestAnUnconfiguredSchedulerFailsClosed(t *testing.T) {
	var service scheduling.Service
	payload, err := json.Marshal(scheduling.PlacementCommand{})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	_, err = service.Handle(context.Background(), workqueue.Item{
		Queue:   scheduling.PlacementQueue,
		Payload: payload,
	})
	if err == nil {
		t.Fatal("a scheduler with no repository must not acknowledge work")
	}
}

// A malformed payload must be terminal, not retried. Replaying it produces the
// same parse failure forever, so it belongs in the dead-letter path immediately
// — and the work queue reads that decision off the fault's retry policy.
func TestAMalformedPlacementPayloadIsTerminal(t *testing.T) {
	service := scheduling.Service{
		Repository: scheduling.NewMemoryRepository(0),
		Fence:      1,
	}
	_, err := service.Handle(context.Background(), workqueue.Item{
		Queue:   scheduling.PlacementQueue,
		Payload: []byte("{not json"),
	})
	if err == nil {
		t.Fatal("a malformed payload must be rejected")
	}
	if faults.IsRetryable(err) {
		t.Fatalf("a malformed payload must not be retryable; policy = %+v", faults.RetryPolicyOf(err))
	}
}
