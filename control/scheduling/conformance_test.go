// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package scheduling_test

import (
	"context"
	"testing"
	"time"

	"go.mindclade.dev/control/orchestration"
	"go.mindclade.dev/control/scheduling"
	"go.mindclade.dev/control/scheduling/schedulingtest"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
)

// The compile-time form, so the compiler is the check. A runtime nil comparison
// against a value the constructor cannot return proves nothing and is rejected
// by staticcheck; this line fails the build the moment MemoryRepository stops
// satisfying either the durable seam or the configuration surface the shared
// suite needs.
var _ schedulingtest.Fleet = (*scheduling.MemoryRepository)(nil)

// The shared suite is what makes MemoryRepository and the PostgreSQL store
// answerable to one contract. The reference adapter's own doc comment calls it
// "the executable specification of the transaction the SQL implementation
// owes", and until something ran both against one suite that claim rested on
// two independently written test files agreeing by coincidence.
func TestMemoryRepositoryConformance(t *testing.T) {
	schedulingtest.Conformance(t, func(_ testing.TB) scheduling.Repository {
		// Zero asks for DefaultReservationBound. The suite never approaches it,
		// and the bound is the reference adapter's alone -- see below.
		return scheduling.NewMemoryRepository(0)
	})
}

// The record-count bound is the one rule the shared suite deliberately leaves
// out, so it is asserted here instead of nowhere.
//
// It exists because this adapter is an in-process map nothing evicts. The
// PostgreSQL store does not have it and must not grow one: refusing to record a
// reservation because a table has many rows would leave the cluster holding
// capacity that ledger cannot see, which is data loss wearing a capacity
// signal's clothes.
func TestMemoryRepositoryFailsClosedAtItsRecordBound(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, time.August, 23, 6, 0, 0, 0, time.UTC)
	repository := scheduling.NewMemoryRepository(1)

	domain, err := scheduling.DomainFor(scheduling.WorkloadClassBatchCPU)
	if err != nil {
		t.Fatalf("batch-cpu capacity domain: %v", err)
	}
	quota := scheduling.Demand{
		scheduling.ResourceCPU:              64_000,
		scheduling.ResourceMemory:           256 << 30,
		scheduling.ResourceEphemeralStorage: 1 << 40,
		scheduling.ResourcePods:             128,
	}
	if err := repository.PutQuota(ctx, domain, quota); err != nil {
		t.Fatalf("PutQuota: %v", err)
	}

	reserve := func() error {
		snapshot, err := repository.Snapshot(ctx, at)
		if err != nil {
			return err
		}
		placement, err := snapshot.Place(boundedPlacementRequest(t), at)
		if err != nil {
			return err
		}
		candidate, err := scheduling.NewReservation(
			boundedID(t, "reservation"), placement, 9, scheduling.DefaultReservationTTL)
		if err != nil {
			return err
		}
		_, _, err = repository.Reserve(ctx, snapshot, candidate, at)
		return err
	}

	if err := reserve(); err != nil {
		t.Fatalf("the first reservation did not fit a bound of one: %v", err)
	}
	err = reserve()
	if !faults.IsCode(err, faults.CodeResourceExhausted) || !faults.IsReason(err, "reservation_store_bound") {
		t.Fatalf("a reservation past the record bound = %s/%q (%v), want resource_exhausted/reservation_store_bound",
			faults.CodeOf(err), faults.ReasonOf(err), err)
	}
}

// boundedPlacementRequest mints one placement request with fresh run
// coordinates, so two calls produce two distinct placement keys and the second
// Reserve reaches the record bound instead of replaying the first.
func boundedPlacementRequest(t *testing.T) scheduling.PlacementRequest {
	t.Helper()
	return scheduling.PlacementRequest{
		Admission: scheduling.AdmissionRequest{
			WorkloadID:  boundedID(t, "workload"),
			Tenant:      "alpha-lab",
			Workspace:   "conformance",
			StageKind:   orchestration.StagePreprocess,
			Pool:        scheduling.PoolFeaturizationCPU,
			Accelerator: scheduling.AcceleratorNone,
			Priority:    scheduling.PriorityBatch,
			Demand: scheduling.Demand{
				scheduling.ResourceCPU:              2_000,
				scheduling.ResourceMemory:           4 << 30,
				scheduling.ResourceEphemeralStorage: 8 << 30,
				scheduling.ResourcePods:             1,
			},
			Replicas: 1,
		},
		RunID:   boundedID(t, "run").String(),
		StageID: boundedID(t, "stage").String(),
		Attempt: 1,
	}
}

func boundedID(t *testing.T, kind string) identifiers.ID {
	t.Helper()
	id, err := identifiers.NewID(identifiers.MustParseKind(kind))
	if err != nil {
		t.Fatalf("mint a %s identifier: %v", kind, err)
	}
	return id
}
