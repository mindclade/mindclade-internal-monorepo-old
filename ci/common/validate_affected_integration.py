# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Exercise the affected selector against the repository's real Bazel graph."""

from __future__ import annotations

import sys
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
if str(REPO) not in sys.path:
    sys.path.insert(0, str(REPO))

from ci.common import affected


def main() -> int:
    selection = affected.select(
        ["tests/fixtures/affected/base/input.txt"],
        head_sha=affected.git_revision("HEAD"),
        event="integration",
    )
    expected = "//tests/fixtures/affected/consumer:consumer_test"
    unrelated = "//tests/fixtures/affected/unrelated:unrelated_test"
    if expected not in selection.test_targets:
        raise affected.SelectionError(f"real Bazel rdeps omitted expected target {expected}")
    if unrelated in selection.test_targets:
        raise affected.SelectionError(f"real Bazel rdeps included unrelated target {unrelated}")
    print(
        "affected Bazel integration passed: "
        f"{len(selection.analysis_targets)} analysis targets, "
        f"{len(selection.test_targets)} tests"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
