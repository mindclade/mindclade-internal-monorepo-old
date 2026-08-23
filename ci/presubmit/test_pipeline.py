# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

import sys
from pathlib import Path

import pytest

from ci.presubmit import pipeline


def test_static_only_runs_architecture_without_bazel(monkeypatch: pytest.MonkeyPatch) -> None:
    commands: list[list[str]] = []
    monkeypatch.setattr(pipeline, "run", lambda command: commands.append(command) or 0)
    monkeypatch.setattr(sys, "argv", ["pipeline.py", "--static-only"])
    assert pipeline.main() == 0
    assert commands == [
        [sys.executable, str(pipeline.REPO / "tools/analysis/run_architecture_checks.py")]
    ]


def test_auto_mode_requires_a_governed_event(monkeypatch: pytest.MonkeyPatch) -> None:
    failures: list[dict[str, object]] = []
    monkeypatch.setattr(
        pipeline.affected,
        "write_failure_evidence",
        lambda *args, **kwargs: failures.append(kwargs),
    )
    monkeypatch.setattr(sys, "argv", ["pipeline.py", "--bazel-only", "--event", "local"])
    assert pipeline.main() == 2
    assert len(failures) == 1
    error = failures[0]["error"]
    assert isinstance(error, pipeline.affected.SelectionError)
    assert error.code == "AFFECTED-SELECT-010"


def test_bazel_execution_requires_an_explicit_cache_route(
    monkeypatch: pytest.MonkeyPatch, tmp_path: Path
) -> None:
    failures: list[dict[str, object]] = []
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
            "--bazel-only",
            "--event",
            "pull_request",
            "--ref",
            "refs/pull/1/merge",
            "--base",
            "0" * 40,
            "--evidence-dir",
            str(tmp_path),
        ],
    )
    assert pipeline.main() == 2
    error = failures[0]["error"]
    assert isinstance(error, pipeline.affected.SelectionError)
    assert error.code == "AFFECTED-SELECT-020"


def test_local_full_bazel_allows_no_cache_route_or_runtime_option_injection(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    selection = pipeline.affected.Selection(
        mode="full",
        reason="explicit_full",
        changes=(),
        seeds=("//...",),
        analysis_targets=("//...",),
        test_targets=("//...",),
        base_sha=None,
        head_sha="1" * 40,
        event="merge_group",
    )
    executions: list[dict[str, object]] = []
    monkeypatch.setattr(pipeline.affected, "select", lambda *args, **kwargs: selection)
    monkeypatch.setattr(
        pipeline.affected,
        "execute_selection",
        lambda *args, **kwargs: executions.append(kwargs) or 0,
    )
    monkeypatch.setattr(
        sys,
        "argv",
        [
            "pipeline.py",
            "--bazel-only",
            "--mode",
            "full",
            "--event",
            "local",
        ],
    )
    assert pipeline.main() == 0
    assert executions == [{"job_started_epoch": None}]


@pytest.mark.parametrize(
    ("event", "ref", "base", "cache_mode", "cache_role"),
    [
        ("pull_request", "refs/pull/1/merge", "0" * 40, "disk", "reader"),
        ("pull_request", "refs/pull/1/merge", "0" * 40, "remote", "reader"),
        ("merge_group", "refs/heads/gh-readonly-queue/main/pr-1", "", "disk", "reader"),
        ("merge_group", "refs/heads/gh-readonly-queue/main/pr-1", "", "remote", "writer"),
        ("push", "refs/heads/main", "", "disk", "writer"),
        ("push", "refs/heads/main", "", "remote", "writer"),
    ],
)
def test_governed_cache_route_is_verified_but_not_injected_into_executor(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
    event: str,
    ref: str,
    base: str,
    cache_mode: str,
    cache_role: str,
) -> None:
    runner_temp = tmp_path / "runner"
    started_file = runner_temp / "bazel-job-started"
    head = "1" * 40
    selection = pipeline.affected.Selection(
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
            "--bazel-only",
            "--mode",
            "auto",
            "--base",
            base,
            "--event",
            event,
            "--ref",
            ref,
            "--head",
            head,
            "--evidence-dir",
            str(tmp_path / "evidence"),
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
            (selection, tmp_path / "evidence"),
            {"job_started_epoch": 123},
        )
    ]
