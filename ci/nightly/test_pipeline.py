# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

import json
import sys
from datetime import UTC, datetime, timedelta
from pathlib import Path

import pytest

from ci.common import affected
from ci.nightly import pipeline
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


def test_bazel_execution_requires_an_explicit_cache_route(
    monkeypatch: pytest.MonkeyPatch, tmp_path: Path
) -> None:
    failures: list[dict[str, object]] = []
    monkeypatch.setattr(
        pipeline,
        "load_contract",
        lambda _path: NightlyContract(
            mode="full",
            analysis_targets=("//...",),
            test_targets=("//...",),
        ),
    )
    monkeypatch.setattr(
        pipeline.affected,
        "write_failure_evidence",
        lambda *args, **kwargs: failures.append(kwargs),
    )
    monkeypatch.setattr(
        sys,
        "argv",
        [
            "pipeline.py",
            "--event",
            "schedule",
            "--ref",
            "refs/heads/main",
            "--evidence-dir",
            str(tmp_path),
        ],
    )
    assert pipeline.main() == 2
    error = failures[0]["error"]
    assert isinstance(error, affected.SelectionError)
    assert error.code == "AFFECTED-SELECT-020"


@pytest.mark.parametrize(
    ("event", "cache_mode", "cache_role"),
    [
        ("schedule", "disk", "writer"),
        ("schedule", "remote", "writer"),
        ("workflow_dispatch", "disk", "writer"),
    ],
)
def test_governed_cache_route_is_verified_but_not_injected_into_executor(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
    event: str,
    cache_mode: str,
    cache_role: str,
) -> None:
    runner_temp = tmp_path / "runner"
    started_file = runner_temp / "bazel-job-started"
    evidence = tmp_path / "evidence"
    head = "1" * 40
    contract = NightlyContract(
        mode="full",
        analysis_targets=("//...",),
        test_targets=("//...",),
    )
    selection = affected.Selection(
        mode="full",
        reason="workflow_full",
        changes=(),
        seeds=("//...",),
        analysis_targets=("//...",),
        test_targets=("//...",),
        base_sha=None,
        head_sha=head,
        event=event,
    )
    checkout_calls: list[tuple[tuple[object, ...], dict[str, object]]] = []
    execution_calls: list[tuple[tuple[object, ...], dict[str, object]]] = []
    monkeypatch.setattr(pipeline, "load_contract", lambda _path: contract)
    monkeypatch.setattr(
        pipeline.affected,
        "assert_clean_checkout",
        lambda *args, **kwargs: checkout_calls.append((args, kwargs)),
    )
    monkeypatch.setattr(pipeline.affected, "load_job_started_epoch", lambda *args, **kwargs: 123)
    monkeypatch.setattr(pipeline.affected, "select", lambda *args, **kwargs: selection)
    monkeypatch.setattr(
        pipeline.affected,
        "execute_selection",
        lambda *args, **kwargs: execution_calls.append((args, kwargs)) or 0,
    )
    monkeypatch.setattr(
        sys,
        "argv",
        [
            "pipeline.py",
            "--event",
            event,
            "--ref",
            "refs/heads/main",
            "--head",
            head,
            "--evidence-dir",
            str(evidence),
            "--job-started-at-file",
            str(started_file),
            "--runner-temp",
            str(runner_temp),
            "--cache-mode",
            cache_mode,
            "--cache-role",
            cache_role,
        ],
    )

    assert pipeline.main() == 0
    assert checkout_calls == [
        (
            (head,),
            {
                "event": event,
                "runner_temp": runner_temp,
                "cache_mode": cache_mode,
                "cache_role": cache_role,
            },
        )
    ]
    assert execution_calls == [
        (
            (selection, evidence),
            {"job_started_epoch": 123},
        )
    ]


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
