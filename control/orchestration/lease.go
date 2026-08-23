// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package orchestration

import (
	"go.mindclade.dev/control/runtime_authority"
	"go.mindclade.dev/libs/go/storage/lease"
)

// AttemptLeaseKey derives the lease key that fences one stage of one job.
//
// The key is scoped to the stage, not the attempt: the lease is what makes
// attempt N+1 wait for attempt N to lose ownership, so both attempts must
// contend for the same key. Keying by attempt would hand every retry its own
// uncontended lease and fence nothing at all.
func AttemptLeaseKey(runID, jobID, stageID string) (lease.Key, error) {
	if err := validateID(runID, "run", "run_id"); err != nil {
		return "", err
	}
	if err := validateID(jobID, "job", "job_id"); err != nil {
		return "", err
	}
	if err := validateID(stageID, "stage", "stage_id"); err != nil {
		return "", err
	}
	key, err := lease.ParseKey("control-plane/orchestration/" + runID + "/" + jobID + "/" + stageID)
	if err != nil {
		return "", invalid("attempt_lease_key_invalid", "attempt lease key is invalid", err)
	}
	return key, nil
}

// ValidateAttemptFencing rejects a stale or absent fence for a stage attempt.
//
// It delegates to control/runtime_authority so the control plane has exactly one
// fencing rule. A second implementation here would be a second place for the
// floor comparison to drift, and the two would disagree precisely when a stale
// worker was trying to commit.
func ValidateAttemptFencing(stageID string, token uint64, floor runtime_authority.FencingFloor) error {
	if err := validateID(stageID, "stage", "stage_id"); err != nil {
		return err
	}
	return runtime_authority.ValidateFencing(stageID, token, floor)
}

// FenceFromLease reads the fencing token out of an acquired lease.
//
// storage/lease increments Version on every acquire and renew, which is what
// makes it monotonic and therefore usable as a fence. Callers must carry this
// value into every durable write the attempt performs, because the fence is only
// protective at the commit boundary.
func FenceFromLease(held lease.Lease) (uint64, error) {
	if err := held.Validate(); err != nil {
		return 0, invalid("attempt_lease_invalid", "attempt lease is invalid", err)
	}
	if held.Version == 0 {
		return 0, invalid("fencing_token_required", "an acquired lease must carry a non-zero version", nil)
	}
	return held.Version, nil
}
