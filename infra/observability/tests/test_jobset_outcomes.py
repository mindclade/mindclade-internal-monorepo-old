# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import json
from pathlib import Path

import pytest

from infra.observability.jobset_outcomes import OutcomeLedger, atomic_write, load_checkpoint


def jobset(
    uid: str, terminal: str | None, *, reason: str = "", namespace: str = "training"
) -> dict:
    conditions = []
    if terminal is not None:
        conditions.append({"type": terminal, "status": "True", "reason": reason})
    return {
        "apiVersion": "jobset.x-k8s.io/v1alpha2",
        "kind": "JobSet",
        "metadata": {
            "name": "private-job-name",
            "namespace": namespace,
            "uid": uid,
            "resourceVersion": "17",
        },
        "status": {"conditions": conditions},
    }


def test_terminal_transitions_are_idempotent_and_bounded_cardinality() -> None:
    ledger = OutcomeLedger("development-compute", 10)
    assert ledger.observe(jobset("uid-1", None)) is False
    assert ledger.observe(jobset("uid-1", "Completed")) is True
    assert ledger.observe(jobset("uid-1", "Completed")) is False
    assert ledger.observe(jobset("uid-2", "Failed", reason="DeadlineExceeded")) is True
    metrics = ledger.openmetrics()
    assert 'result="completed",reason_class="completed"} 1' in metrics
    assert 'result="failed",reason_class="deadline"} 1' in metrics
    assert "private-job-name" not in metrics
    assert "uid-1" not in metrics
    assert 'mindclade_jobset_outcome_ledger_capacity{cluster="development-compute"} 10' in metrics
    assert (
        'mindclade_jobset_outcome_ledger_utilization_ratio{cluster="development-compute"} 0.2'
        in metrics
    )


def test_conflicting_terminal_state_fails_closed() -> None:
    ledger = OutcomeLedger("development-compute", 10)
    ledger.observe(jobset("uid-1", "Completed"))
    with pytest.raises(ValueError, match="changed terminal outcome"):
        ledger.observe(jobset("uid-1", "Failed"))


def test_full_ledger_refuses_lossy_eviction() -> None:
    ledger = OutcomeLedger("development-compute", 1)
    ledger.observe(jobset("uid-1", "Completed"))
    with pytest.raises(RuntimeError, match="refusing lossy eviction"):
        ledger.observe(jobset("uid-2", "Completed"))


def test_checkpoint_round_trip_preserves_replay_protection(tmp_path: Path) -> None:
    checkpoint = tmp_path / "outcomes.json"
    ledger = OutcomeLedger("development-compute", 10)
    ledger.observe(jobset("uid-1", "Completed"))
    atomic_write(checkpoint, json.dumps(ledger.snapshot()))
    restored = load_checkpoint(checkpoint, "development-compute", 10)
    assert restored.observe(jobset("uid-1", "Completed")) is False
    assert restored.counts() == ledger.counts()
    assert checkpoint.stat().st_mode & 0o777 == 0o600


def test_checkpoint_identity_change_is_rejected(tmp_path: Path) -> None:
    checkpoint = tmp_path / "outcomes.json"
    ledger = OutcomeLedger("development-compute", 10)
    atomic_write(checkpoint, json.dumps(ledger.snapshot()))
    with pytest.raises(ValueError, match="identity"):
        load_checkpoint(checkpoint, "staging-compute", 10)


def test_checkpoint_cannot_coerce_boolean_bound() -> None:
    value = OutcomeLedger("development-compute", 10).snapshot()
    value["maximumTerminalUids"] = True
    with pytest.raises(ValueError, match="exact types"):
        OutcomeLedger.restore(value)


def test_checkpoint_with_broad_permissions_is_rejected(tmp_path: Path) -> None:
    checkpoint = tmp_path / "outcomes.json"
    checkpoint.write_text(json.dumps(OutcomeLedger("development-compute", 10).snapshot()))
    checkpoint.chmod(0o644)
    with pytest.raises(ValueError, match="permissions"):
        load_checkpoint(checkpoint, "development-compute", 10)
