# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

import sys
from pathlib import Path

import pytest

from ci.presubmit import disk_preflight, pipeline


@pytest.fixture(autouse=True)
def _satisfied_disk_preflight(monkeypatch: pytest.MonkeyPatch) -> None:
    """Hold the disk floor satisfied for the selection-governance tests.

    These tests assert what the pipeline does with events, modes, and cache routes. Letting
    them read the real filesystem would make their verdicts depend on how full the machine
    happened to be, which is the class of non-determinism this whole change exists to remove.
    `test_expensive_lanes_abort_when_the_disk_floor_is_unmet` covers the other direction.
    """
    monkeypatch.setattr(disk_preflight, "check", lambda *_args, **_kwargs: [])


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
    authority = object()
    monkeypatch.setattr(
        pipeline.affected,
        "assert_clean_checkout",
        lambda *args, **kwargs: (checkout_calls.append((args, kwargs)), authority)[1],
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
            {"bazelrc_authority": authority, "job_started_epoch": 123},
        )
    ]


def test_full_shard_uses_complete_partition_and_preserves_bazelrc_authority(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
) -> None:
    head = "1" * 40
    runner_temp = tmp_path / "runner"
    authority = object()
    selection = pipeline.affected.Selection(
        mode="full",
        reason="complete_partition:1_of_4",
        changes=(),
        seeds=("//...",),
        analysis_targets=("//pkg:library",),
        test_targets=("//pkg:library_test",),
        base_sha=None,
        head_sha=head,
        event="merge_group",
    )
    contract = type("Contract", (), {"shard_count": 4})()
    graph = object()
    executions: list[tuple[tuple[object, ...], dict[str, object]]] = []
    monkeypatch.setattr(
        pipeline.affected, "assert_clean_checkout", lambda *args, **kwargs: authority
    )
    monkeypatch.setattr(pipeline.affected, "load_job_started_epoch", lambda *args, **kwargs: 123)
    monkeypatch.setattr(pipeline.affected, "git_revision", lambda _revision: head)
    monkeypatch.setattr(pipeline.full_graph_shards, "load_contract", lambda _path: contract)
    monkeypatch.setattr(pipeline.full_graph_shards, "plan_from_bazel", lambda _contract: graph)
    monkeypatch.setattr(
        pipeline.full_graph_shards,
        "selection_for_shard",
        lambda plan, index, **kwargs: (
            selection
            if plan is graph and index == 0 and kwargs == {"event": "merge_group", "head_sha": head}
            else pytest.fail("unexpected shard selection")
        ),
    )
    monkeypatch.setattr(
        pipeline.affected,
        "execute_selection",
        lambda *args, **kwargs: executions.append((args, kwargs)) or 0,
    )
    monkeypatch.setattr(
        sys,
        "argv",
        [
            "pipeline.py",
            "--bazel-only",
            "--mode",
            "auto",
            "--event",
            "merge_group",
            "--ref",
            "refs/heads/gh-readonly-queue/main/pr-1",
            "--head",
            head,
            "--evidence-dir",
            str(tmp_path / "evidence"),
            "--job-started-at-file",
            str(runner_temp / "bazel-job-started"),
            "--runner-temp",
            str(runner_temp),
            "--cache-mode",
            "remote",
            "--cache-role",
            "writer",
            "--shard-index",
            "0",
            "--shard-count",
            "4",
        ],
    )

    assert pipeline.main() == 0
    assert executions == [
        (
            (selection, tmp_path / "evidence"),
            {"bazelrc_authority": authority, "job_started_epoch": 123},
        )
    ]


@pytest.mark.parametrize(
    "arguments",
    [
        ["--mode", "full", "--shard-index", "0"],
        ["--mode", "full", "--shard-count", "4"],
    ],
)
def test_partial_shard_arguments_fail_closed(
    monkeypatch: pytest.MonkeyPatch, arguments: list[str]
) -> None:
    monkeypatch.setattr(sys, "argv", ["pipeline.py", "--bazel-only", *arguments])
    with pytest.raises(SystemExit) as error:
        pipeline.main()
    assert error.value.code == 2


def test_pull_request_cannot_bypass_full_selector_with_shard_arguments(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
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
            "--mode",
            "auto",
            "--event",
            "pull_request",
            "--ref",
            "refs/pull/1/merge",
            "--base",
            "0" * 40,
            "--evidence-dir",
            str(tmp_path),
            "--shard-index",
            "0",
            "--shard-count",
            "4",
        ],
    )
    assert pipeline.main() == 2
    error = failures[0]["error"]
    assert isinstance(error, pipeline.affected.SelectionError)
    assert error.code == "AFFECTED-SELECT-010"


def test_expensive_lanes_abort_when_the_disk_floor_is_unmet(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """A shortfall must stop the run before Cargo and Bazel, with its own exit code.

    Exit 3 is distinct from the selection-error exit 2 on purpose: an out-of-disk abort is an
    infrastructure condition, not a governance rejection, and conflating them is how the
    original outage read as thirteen unrelated regressions.
    """
    monkeypatch.setattr(disk_preflight, "check", lambda *_args, **_kwargs: ["no room"])
    monkeypatch.setattr(pipeline, "run", lambda _command: 0)
    monkeypatch.setattr(sys, "argv", ["pipeline.py", "--bazel-only"])
    assert pipeline.main() == 3


def test_the_disk_floor_is_checked_before_any_selection_work(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """The preflight must precede selection, or it cannot prevent the expensive lanes."""
    monkeypatch.setattr(disk_preflight, "check", lambda *_args, **_kwargs: ["no room"])

    def _unreachable(*_args: object, **_kwargs: object) -> None:
        raise AssertionError("selection ran despite an unmet disk floor")

    monkeypatch.setattr(pipeline.affected, "resolve_selection_mode", _unreachable)
    monkeypatch.setattr(sys, "argv", ["pipeline.py", "--bazel-only"])
    assert pipeline.main() == 3


def test_static_only_does_not_require_the_disk_floor(monkeypatch: pytest.MonkeyPatch) -> None:
    """`--static-only` writes nothing; gating it on 16 GiB would be a floor with no reason."""
    monkeypatch.setattr(disk_preflight, "check", lambda *_args, **_kwargs: ["no room"])
    monkeypatch.setattr(pipeline, "run", lambda _command: 0)
    monkeypatch.setattr(sys, "argv", ["pipeline.py", "--static-only"])
    assert pipeline.main() == 0
