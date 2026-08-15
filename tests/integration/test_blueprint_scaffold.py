# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Repository-level target-state scaffold coverage.

Split into three tests on purpose, because they were one test asserting three different kinds
of thing and the weakest of them decided the result.

`docs/blueprint/production-monorepo-paths.txt` describes the repository's TARGET state. Asserting
on every pull request that the target has already been reached measures project progress, not
correctness — and REPOSITORY_STATUS.md says as much in the other direction: "presence
communicates architecture and ownership, not release readiness". A gate that fails for a reason
no reviewer can act on is one people learn to ignore, which costs the two assertions below it
that are worth acting on.

So:

  consistency  — invariants that hold today and would be real defects if they broke. Gates.
  ratchet      — materialization may not go backwards. Gates, and is actionable: a PR either
                 leaves the number alone or lowers it.
  completeness — the 100% target. Marked `nightly`, so the gap stays measured and visible
                 without blocking work that is not about closing it.
"""

from __future__ import annotations

import importlib.util
from pathlib import Path

import pytest

# Blueprint paths not yet materialized, as of the last time this number was lowered.
#
# A RATCHET, not a budget. It may only ever go down, and the test below fails if the true count
# drops without this constant following it — otherwise the number drifts upward in spirit,
# silently permitting regressions that earlier work had already ruled out.
#
# What remains is mostly a `.buildkite/` pipeline tree, plus .editorconfig, .bazelrc, the issue
# and PR templates, and six infra/terraform BUILD files. Note the blueprint also reserves
# .github/dependabot.yml, which contradicts the estate's later decision to disable Dependabot in
# favour of Renovate — so reaching 0 requires reconciling the manifest, not only writing files.
MATERIALIZATION_BASELINE = 48


def _load_checker(root: Path):
    path = root / "tools/analysis/check_blueprint_scaffold.py"
    spec = importlib.util.spec_from_file_location("check_blueprint_scaffold", path)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"unable to load {path}")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def _check() -> dict:
    root = Path(__file__).resolve().parents[2]
    module = _load_checker(root)
    return module.check(root, root / "docs/blueprint/production-monorepo-paths.txt")


def test_blueprint_manifest_is_internally_consistent() -> None:
    """The manifest itself is well-formed, and nothing materialized is wrong.

    These hold today. A path listed twice, a file that exists but is empty where the manifest
    expects content, or a path that escapes the repository are all defects in the manifest or
    the tree rather than work not yet done — which is why they gate and coverage does not.
    """
    result = _check()
    assert result["duplicate_paths"] == [], "the manifest lists a path more than once"
    assert result["unexpected_empty_paths"] == [], "a materialized path is unexpectedly empty"
    assert result["unsafe_paths"] == [], "the manifest names a path outside the repository"


def test_materialization_does_not_regress() -> None:
    """Materialization is a ratchet: the count of missing paths may only go down.

    Deleting a materialized path, or adding a blueprint entry with nothing behind it, fails
    here. Materializing one also fails here — deliberately, and with the number to change.
    """
    missing = len(_check()["missing_paths"])

    assert missing <= MATERIALIZATION_BASELINE, (
        f"{missing} blueprint paths are unmaterialized, up from {MATERIALIZATION_BASELINE}. "
        "Either materialize what this change removed, or — if the manifest gained entries on "
        "purpose — raise MATERIALIZATION_BASELINE in the same commit and say why."
    )
    assert missing == MATERIALIZATION_BASELINE, (
        f"{MATERIALIZATION_BASELINE - missing} more blueprint path(s) are materialized than the "
        f"baseline records. Lower MATERIALIZATION_BASELINE to {missing} in this commit; a ratchet "
        "that is not tightened stops catching the next regression."
    )


@pytest.mark.nightly
def test_every_blueprint_path_is_materialized() -> None:
    """The target state, in full. Reported nightly, not gated per pull request.

    This is the assertion the other two were hiding inside. It is expected to fail until the
    repository is complete; what it provides is a measured, dated gap rather than an impression.
    """
    result = _check()
    assert result["missing_paths"] == []
    assert result["coverage_percent"] == 100.0
