# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

import sys

import pytest

from libs.python.errors import DeadlineExceeded, ResourceExhausted
from libs.python.testing import run_process


def test_process_captures_output_without_a_shell() -> None:
    result = run_process([sys.executable, "-c", "print('ready')"])
    assert result.stdout == "ready\n"
    assert result.stderr == ""


def test_process_enforces_deadline_and_output_limit() -> None:
    with pytest.raises(DeadlineExceeded):
        run_process([sys.executable, "-c", "import time; time.sleep(1)"], timeout_seconds=0.05)
    with pytest.raises(ResourceExhausted):
        run_process(
            [sys.executable, "-c", "print('x' * 1000)"],
            maximum_output_bytes=100,
        )


def test_process_rejects_invalid_runtime_options() -> None:
    with pytest.raises(ValueError, match=r"pathlib\.Path"):
        run_process([sys.executable, "-c", "pass"], cwd=".")  # type: ignore[arg-type]
    with pytest.raises(ValueError, match="boolean"):
        run_process([sys.executable, "-c", "pass"], check=1)  # type: ignore[arg-type]
