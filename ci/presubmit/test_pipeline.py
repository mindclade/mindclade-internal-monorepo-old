# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

import sys

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
    monkeypatch.setattr(sys, "argv", ["pipeline.py", "--bazel-only"])
    assert pipeline.main() == 2
    assert len(failures) == 1
    error = failures[0]["error"]
    assert isinstance(error, pipeline.affected.SelectionError)
    assert error.code == "AFFECTED-SELECT-010"


def test_full_bazel_only_uses_shared_executor(monkeypatch: pytest.MonkeyPatch) -> None:
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
    monkeypatch.setattr(pipeline.affected, "select", lambda *args, **kwargs: selection)
    monkeypatch.setattr(pipeline.affected, "execute_selection", lambda *args, **kwargs: 0)
    monkeypatch.setattr(sys, "argv", ["pipeline.py", "--bazel-only", "--mode", "full"])
    assert pipeline.main() == 0
