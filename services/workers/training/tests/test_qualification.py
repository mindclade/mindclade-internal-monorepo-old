# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import json
import sys
from pathlib import Path

import pytest

from services.workers.training.qualification import main, run_local


def test_local_source_check_is_explicitly_non_connected(tmp_path: Path) -> None:
    result = run_local(tmp_path.resolve(), "source-check")
    assert result["schema_version"] == "mindclade.dev/reference-training-local/v1"
    assert result["connected_qualification"] is False
    assert result["phase"] == "local-cpu-source-check"
    assert result["output_count"] == 4
    assert result["optimizer_steps"] == 8


def test_cli_emits_exactly_one_json_line_for_local_check(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
    capsys: pytest.CaptureFixture[str],
) -> None:
    monkeypatch.setattr(
        sys,
        "argv",
        [
            "training-qualification",
            "--phase",
            "local-cpu-source-check",
            "--scratch",
            str(tmp_path.resolve()),
            "--run-id",
            "source-check",
        ],
    )
    assert main() == 0
    captured = capsys.readouterr()
    lines = captured.out.splitlines()
    assert len(lines) == 1
    assert json.loads(lines[0])["connected_qualification"] is False
    assert captured.err == ""


def test_connected_h100_phase_remains_fail_closed(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
    capsys: pytest.CaptureFixture[str],
) -> None:
    monkeypatch.setattr(
        sys,
        "argv",
        [
            "training-qualification",
            "--phase",
            "h100-1g-smoke",
            "--checkpoint-socket",
            str(tmp_path / "checkpoint.sock"),
        ],
    )
    assert main() == 2
    captured = capsys.readouterr()
    assert captured.out == ""
    assert "fail-closed" in captured.err
