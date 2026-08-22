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


def test_affected_bazel_mode_requires_explicit_base(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr(sys, "argv", ["pipeline.py", "--bazel-only"])
    with pytest.raises(SystemExit) as error:
        pipeline.main()
    assert error.value.code == 2


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
