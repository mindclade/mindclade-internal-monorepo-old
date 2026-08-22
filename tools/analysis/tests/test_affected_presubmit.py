# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

import json
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]
ANALYSIS = ROOT / "tools/analysis"
if str(ANALYSIS) not in sys.path:
    sys.path.insert(0, str(ANALYSIS))

import check_affected_presubmit  # noqa: E402

from ci.common.affected_contract import GlobalInputContract  # noqa: E402


def _contract() -> GlobalInputContract:
    return GlobalInputContract(
        exact_paths=frozenset({"MODULE.bazel"}),
        prefixes=("ci/",),
        review_boundaries=(
            ("", ("ci", "tools")),
            ("tools", ("analysis",)),
        ),
    )


def test_reviewed_authority_inventory_passes() -> None:
    assert not check_affected_presubmit._review_boundary_errors(
        _contract(),
        ("ci/common/affected.py", "tools/analysis/check_affected_presubmit.py"),
    )


def test_new_top_level_authority_fails_closed() -> None:
    errors = check_affected_presubmit._review_boundary_errors(
        _contract(),
        (
            "build-support/config.json",
            "ci/common/affected.py",
            "tools/analysis/check_affected_presubmit.py",
        ),
    )
    assert errors == ["[AFFECTED-GLOBAL-006] root authority 'build-support' is not reviewed"]


def test_new_tools_authority_fails_closed() -> None:
    errors = check_affected_presubmit._review_boundary_errors(
        _contract(),
        (
            "ci/common/affected.py",
            "tools/analysis/check_affected_presubmit.py",
            "tools/graph/config.json",
        ),
    )
    assert errors == ["[AFFECTED-GLOBAL-006] tools authority 'graph' is not reviewed"]


def test_graph_native_activation_cannot_bypass_evidence(tmp_path: Path) -> None:
    source = ROOT / "ci/common/affected_global_inputs.json"
    payload = json.loads(source.read_text(encoding="utf-8"))
    payload["activation"]["state"] = "active"
    candidate = tmp_path / "affected_global_inputs.json"
    candidate.write_text(json.dumps(payload), encoding="utf-8")
    assert check_affected_presubmit._activation_errors(candidate) == [
        "[AFFECTED-GLOBAL-009] graph-native activation must remain blocked pending evidence"
    ]
