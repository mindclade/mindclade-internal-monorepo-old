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

import functools
import importlib.util
from pathlib import Path

import pytest

# Blueprint paths not yet materialized, as of the last time this number was lowered.
#
# A RATCHET, not a budget. It may only ever go down, and the test below fails if the true count
# drops without this constant following it — otherwise the number drifts upward in spirit,
# silently permitting regressions that earlier work had already ruled out.
#
# What remains is mostly a `.buildkite/` pipeline tree (23 paths), plus root dotfiles
# (.bazelrc, .bazelversion, .editorconfig, .gitattributes and friends), the .github issue and PR
# templates, the gpu/nightly/release workflows, and two infra/kubernetes base manifests. Note the
# blueprint also reserves .github/dependabot.yml, which contradicts the estate's later decision to
# disable Dependabot in favour of Renovate — so reaching 0 requires reconciling the manifest, not
# only writing files.
#
# RAISED 48 -> 49 once, deliberately, which is the one direction this number is not supposed
# to move. tests/pytest.ini is a reserved blueprint path, and materializing it as the one-line
# scaffold stub actively broke the test configuration: pytest resolves the NEAREST config file,
# so any path-scoped run under tests/ took `tests/pytest.ini` as its configfile and silently
# dropped --strict-markers, --import-mode=importlib, pythonpath and the `-m 'not nightly'`
# deselection from pyproject.toml. `pytest tests/integration/test_blueprint_scaffold.py`
# therefore ran the nightly test and failed, while the same suite passed from the repo root.
#
# A reserved path is better left unmaterialized than materialized as a stub that disables the
# tooling around it. If tests/pytest.ini is ever wanted for real it has to carry the whole
# configuration, not sit empty above it.
#
# LOWERED 49 -> 46 by the release-engineering sweep, which materialized three of the reserved
# root and workflow paths with real content rather than stubs:
#
#   .bazelversion                        an unpinned launcher lets bazelisk pick a Bazel per
#                                        machine. Pinned to the version
#                                        tools/build/nix/versions.nix declares.
#   .github/workflows/release.yml        the image build, sign and attest chain. The filename
#                                        is load-bearing: bootstrap binds the attestor
#                                        identity to this exact path.
#   .pre-commit-config.yaml              the licence-header hook every other repository in the
#                                        estate has carried since it was created.
#
# RAISED 46 -> 114 by the root-file sweep, and that number is the sum of two movements in
# opposite directions that have nothing to do with each other. Recording it as one number is
# what the ratchet forces; recording WHICH is which is what stops the raise reading as a
# licence to keep raising it.
#
#   -7   the sweep's own work. The seven reserved root dotfiles are now materialized with real
#        content rather than stubs: .bazelrc, .bazelignore, .buildifier.json, .dockerignore,
#        .editorconfig, .envrc, .gitattributes. The three root files that WERE stubs —
#        OWNERS.toml, rustfmt.toml, bazel_downloader.cfg — were already counted as
#        materialized and do not move this number, which is precisely the blind spot the
#        note above about tests/pytest.ini describes: coverage counts existence, not content.
#        No reserved root path remains unmaterialized.
#
#   +75  ALREADY BROKEN when the sweep started, and not accepted by being written down here.
#        Two in-flight migrations moved files the manifest still lists at their old paths:
#
#          training/distributed/*        54 paths, from the package-layout migration in
#                                        d74b978. The modules exist; the manifest names the
#                                        pre-migration layout.
#          libs/go/storage/outbox/*      21 paths, from the outbox move. Same shape.
#
#        Both are manifest reconciliation, not unwritten code — closing them means updating
#        docs/blueprint/production-monorepo-paths.txt to the layout that shipped, then
#        LOWERING this number by what that removes. Whichever change finishes those
#        migrations owns that edit. Until then the gate would be red for a reason no reviewer
#        of an unrelated pull request can act on, which is the exact failure mode this file's
#        opening comment exists to prevent.
#
# LOWERED 114 -> 17 by the manifest reconciliation in 1a3b46c, which is the edit the note
# directly above was waiting for. It did not materialize anything; it corrected the manifest to
# the layout that shipped, so the two migrations named above stopped being counted as missing
# work:
#
#   training/distributed/*        54 paths. d74b978 moved the flat modules into communication/,
#                                 topology/, planning/{,plans/} and state_backends/ -- 53 git
#                                 renames, most at similarity >= R082.
#   libs/go/storage/outbox/*      21 paths. Outbox is Layer 2 in libs/go/LAYERS.md, beside
#                                 inbox/leadership/workqueue, and imports retry, servicekit and
#                                 storage/lease. A Layer-1 storage/ contract root may not, and
#                                 check_go_layers.py enforces that -- so the manifest was
#                                 naming a location the layering forbids.
#   .buildkite/** (23)            the repository runs GitHub Actions; lane logic lives in ci/.
#   .../store/postgres/*.go (3)   the three stubs described in the raise above.
#   tests/pytest.ini (1)          left unmaterialized deliberately, per the opening comment.
#   workflows/{gpu,nightly} (2)   waiting on self-hosted runners, per presubmit.yml.
#   base/network-policies.yaml    relocated to policies/network-policies.yaml.
#
# LOWERED again to 5: the .github templates and metadata are written, rbac.yaml is
# materialized, and .github/dependabot.yml was dropped from the manifest rather than created --
# Dependabot is disabled org-wide in github-config and renovate.json5 owns dependency updates
# for the estate, so writing one would field a second updater against the same lockfiles. That
# was the reconciliation the note near the top of this file predicted would be needed.
#
# What remained included four libs/python/*/tests/BUILD.bazel files and one Rust runtime-core
# reservation. The Python packages now carry real test targets; only the pre-existing Rust
# reservation remains. (Any other missing paths reported locally belong to separate worktree
# changes and must not be absorbed into this ratchet.)
# This number is now a tight floor rather than 90 counts of slack. The assertion below is
# two-sided on purpose: lower it as these close, and do not raise it without saying which
# movement raised it.
#
# RECONCILED at 0 for the accepted ADR-0014 protocol-authority cleanup. Twenty-seven scaffold
# paths were removed from the blueprint when their duplicate protocol sources were deliberately
# deleted in favor of the smaller canonical descriptor surface. The target-state manifest must
# track that accepted authority; restoring duplicate schemas or raising the ratchet would both be
# regressions.
#
# KEPT at 0 when the Go test-signal gate removed 18 helper-only scaffold tests. Those files could
# never fail and therefore represented fictitious coverage. The blueprint reservations were
# removed with them rather than raising this ratchet or preserving files that the architecture
# gate now rejects. Component maturity remains explicit in components.toml until executable tests
# replace those reservations.
MATERIALIZATION_BASELINE = 0


def _load_checker(root: Path):
    path = root / "tools/analysis/check_blueprint_scaffold.py"
    spec = importlib.util.spec_from_file_location("check_blueprint_scaffold", path)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"unable to load {path}")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


@functools.cache
def _check() -> dict:
    """One scan of the manifest for the whole module.

    Cached because splitting one test into three otherwise loads the checker and stats all 4,475
    blueprint paths three times over to answer three questions about the same result. The tests
    only read it.
    """
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
