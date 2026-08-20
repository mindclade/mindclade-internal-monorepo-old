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
