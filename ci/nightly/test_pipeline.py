# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

import json
from datetime import UTC, datetime, timedelta
from pathlib import Path

import pytest

from ci.common import affected
from ci.nightly.pipeline import NightlyContract, load_contract
from ci.nightly.qualify_latency import Metric, load_metric, qualify


def test_committed_contract_is_full_graph() -> None:
    contract = load_contract(Path(__file__).with_name("targets.yaml"))
    assert contract.mode == "full"
    assert contract.analysis_targets == ("//...",)
    assert contract.test_targets == ("//...",)


def test_contract_rejects_unknown_fields() -> None:
    with pytest.raises(ValueError, match="unknown"):
        NightlyContract.from_dict(
            {
                "schema_version": 1,
                "mode": "full",
                "analysis_targets": ["//..."],
                "test_targets": ["//..."],
                "unexpected": True,
            }
        )


@pytest.mark.parametrize("mode", ["affected", "", None, True])
def test_contract_rejects_non_full_mode(mode: object) -> None:
    with pytest.raises(ValueError, match="mode"):
        NightlyContract.from_dict(
            {
                "schema_version": 1,
                "mode": mode,
                "analysis_targets": ["//..."],
                "test_targets": ["//..."],
            }
        )


def test_contract_loader_rejects_duplicate_keys(tmp_path: Path) -> None:
    path = tmp_path / "targets.yaml"
    path.write_text(
        '{"schema_version":1,"mode":"full","mode":"full",'
        '"analysis_targets":["//..."],"test_targets":["//..."]}',
        encoding="utf-8",
    )
    with pytest.raises(affected.SelectionError) as captured:
        load_contract(path)
    assert captured.value.code == "AFFECTED-SELECT-018"


def test_contract_loader_redacts_invalid_utf8(tmp_path: Path) -> None:
    path = tmp_path / "targets.yaml"
    path.write_bytes(b"\xffsecret-nightly-content")
    with pytest.raises(affected.SelectionError) as captured:
        load_contract(path)
    assert captured.value.code == "AFFECTED-SELECT-017"
    assert "secret-nightly-content" not in str(captured.value)


def test_latency_qualification_holds_burn_in_then_enforces_p95() -> None:
    now = datetime(2026, 9, 30, tzinfo=UTC)
    recent = [
        Metric(completed_at=now - timedelta(days=20), elapsed_seconds=600.0) for _ in range(20)
    ]
    payload, exit_code = qualify(recent, now=now)
    assert payload["status"] == "burn_in"
    assert exit_code == 0

    qualified = [Metric(completed_at=now - timedelta(days=29), elapsed_seconds=900.0)] + [
        Metric(completed_at=now - timedelta(days=index), elapsed_seconds=900.0)
        for index in range(20)
    ]
    payload, exit_code = qualify(qualified, now=now)
    assert payload["status"] == "passed"
    assert payload["p95_seconds"] == 900.0
    assert exit_code == 0

    slow = [
        *qualified[:-2],
        Metric(completed_at=now - timedelta(days=1), elapsed_seconds=1900.0),
        Metric(completed_at=now - timedelta(days=2), elapsed_seconds=1900.0),
    ]
    payload, exit_code = qualify(slow, now=now)
    assert payload["status"] == "failed"
    assert exit_code == 1


def test_metric_loader_filters_full_runs_and_rejects_invalid_latency(tmp_path: Path) -> None:
    path = tmp_path / "metric.json"
    payload = {
        "schema_version": 1,
        "event": "pull_request",
        "mode": "full",
        "completed_at": "2026-08-22T12:00:00Z",
        "job_elapsed_seconds": 10.0,
    }
    path.write_text(json.dumps(payload), encoding="utf-8")
    assert load_metric(path) is None
    payload["mode"] = "affected"
    payload["job_elapsed_seconds"] = True
    path.write_text(json.dumps(payload), encoding="utf-8")
    with pytest.raises(ValueError, match="latency"):
        load_metric(path)
