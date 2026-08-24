#!/usr/bin/env bash
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

set -euo pipefail

scenario="${1:-}"
mode="${2:-execute}"
if [[ -z "$scenario" ]]; then
  echo "usage: $0 SCENARIO [--list]" >&2
  exit 64
fi
if [[ "$mode" == "--list" ]]; then
  printf '%s: test\n' "$scenario"
  exit 0
fi
if [[ "$mode" != "execute" ]]; then
  echo "unexpected argument: $mode" >&2
  exit 64
fi

case "$scenario" in
  database_loss)
    package=./services/control_plane/internal/store/postgres
    test_name=TestLivePostgresDatabaseLossIsQualified
    ;;
  transaction_rollback)
    package=./services/control_plane/internal/store/postgres
    test_name=TestLivePostgresRollbackLeavesNoPartialEvidence
    ;;
  lease_loss)
    package=./services/control_plane/internal/store/postgres
    test_name=TestLivePostgresLeaseLossRejectsStaleOwner
    ;;
  duplicate_event)
    package=./libs/go/coordination/inbox
    test_name=TestProcessAndReplay
    ;;
  # The four scenarios below were added with the orchestration/scheduling durability
  # work. They differ from the five above in what they inject: those drive a provider
  # or a lease into a fault, these drive a *composition* into one -- a promotion whose
  # placement was already appended, a projected column that no longer matches the
  # document it was derived from, an admission decided against a fleet that has since
  # moved, and an expiry backlog larger than one bounded sweep.
  placement_rollback)
    package=./services/control_plane/internal/store/postgres/orchestration
    test_name=TestLivePostgresPromotionPlacesWorkInTheSameTransaction
    ;;
  scheduling_projection_drift)
    package=./services/control_plane/internal/store/postgres/scheduling
    test_name=TestLivePostgresRejectsProjectionDriftFromTheDocument
    ;;
  scheduling_stale_snapshot)
    package=./services/control_plane/internal/store/postgres/scheduling
    test_name=TestLivePostgresFingerprintStalenessIsDecidedInsideTheWrite
    ;;
  scheduling_expiry_backlog)
    package=./services/control_plane/internal/store/postgres/scheduling
    test_name=TestLivePostgresExpirySweepDrainsABacklogAcrossMutations
    ;;
  retry_exhaustion)
    package=./libs/go/retry
    test_name=TestExecutorRetriesExplicitFaultAndExhausts
    ;;
  *)
    echo "unknown failure-injection scenario: $scenario" >&2
    exit 64
    ;;
esac

GOWORK=off go test -race -count=1 -run "^${test_name}$" "$package"
