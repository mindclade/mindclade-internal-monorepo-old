# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Keeps the Python suite's reported number equal to the number actually verified.

193 of this repository's 221 Python test files were a single `assert True` under a
`\"\"\"Scaffold test for ...\"\"\"` docstring. `reusable-uv-ci.yml` runs them as a required
check, so the suite was green, and green meant nothing — the gate manufactured confidence in
untested code. Go and Rust are genuinely tested by contrast (236 Go test files with two
`t.Skip`; zero `#[ignore]` and zero `unimplemented!` in Rust), so this was narrowly a Python
problem.

Those files now skip rather than pass, which fixes the reported number. This file is what
stops the problem coming back, in the same spirit as
`tests/integration/test_blueprint_scaffold.py`:

  no_new_scaffolds  — a RATCHET on how many placeholders exist. Gates, and is actionable: a
                      PR either leaves the number alone or lowers it.
  none_assert_true  — the specific regression. A scaffold that goes back to `assert True`
                      rejoins the pass count and becomes invisible again.
  every_file_real   — the target state. Marked `nightly`, so the gap stays measured without
                      blocking work that is not about closing it.

Counting by MARKER rather than by file contents is deliberate. `@pytest.mark.scaffold` is a
declaration a reviewer can see in the diff; a heuristic over source text is something a
future refactor breaks silently.
"""

from __future__ import annotations

import ast
import functools
from pathlib import Path

import pytest

# Python test files that are placeholders rather than tests, as of the last time this number
# was lowered.
#
# A RATCHET, not a budget. It may only ever go down. Implementing something and writing its
# tests lowers it; the test below fails if the true count drops without this constant
# following, because a ratchet that is not tightened stops catching the next regression.
#
# RAISED 193 -> 225 to record a re-partitioning that d74b978 ("refactor(training): migrate
# distributed package layout") performed without touching this number, leaving the gate red at
# HEAD for every unrelated pull request. Verified by scanning the commit and its parent: the
# parent counts exactly 193, the commit counts 225.
#
# The +32 is a net of two movements, and the net is what makes it look worse than it is:
#
#   -8   coarse per-package placeholders, one per subpackage
#        (training/distributed/{backends,debug,moe,parallelism,pipeline,plans}/tests plus
#        tests/test_mesh.py and tests/test_plan.py).
#   +40  finer-grained placeholders, one per module the migration split those subpackages
#        into — communication/, moe/, parallelism/, pipeline/, planning/, state_backends/,
#        topology/ and the package root.
#
# So no test was deleted and no covered code became uncovered: the same unwritten surface is
# now reserved at module granularity instead of package granularity. `training` is
# `status = "scaffolded"` in components.toml, which is the status under which reserved
# placeholders are legitimate.
#
# This raise is bounded to that migration. It is not licence to add placeholders elsewhere:
# any further increase has to name its own cause the way this one does, and the honest way to
# lower it is to implement training/distributed and write the 40 tests.
# LOWERED 225 -> 222 by the libs/python Layer 0/1 foundation, which implemented three packages
# and wrote real tests for each:
#
#   libs/python/errors/tests/test_codes.py           -> test_codes, test_retry, test_base
#   libs/python/identifiers/tests/test_digest.py     -> test_digest, test_resource, test_version,
#                                                       test_artifact, test_golden_vectors
#   libs/python/serialization/tests/test_canonical.py -> test_canonical
#
# Three placeholders removed, twelve real test modules added. The remaining libs/python
# scaffolds were subsequently materialized by explicit repository-owner direction, with bounded
# contracts, real package tests and Bazel targets despite not yet having in-tree consumers.
# Config's two placeholders were replaced by behavior tests and Bazel targets in the
# production-hardening pass, lowering the ratchet by two.
# LOWERED 222 -> 220 by materializing config's loader and merge coverage.
# LOWERED 220 -> 214 by materializing all six placeholder tests under artifacts, distributed,
# geometry, observability, and testing.
SCAFFOLD_BASELINE = 213

# Gitignored tool directories. Every entry is something that can hold a copy of the tree
# without being part of it, which this scan would otherwise count.
#
# `.claude` joined the list because it holds agent worktrees — full checkouts of this
# repository. Two concurrent sessions put 3110 test files under it, and the ratchet counted
# every one, reporting 2910 scaffolds against a baseline of 225. The gate was not wrong about
# the tree; it was reading three copies of it. CI never saw this because CI has no worktrees,
# which is the worst shape for a gate to fail in: unrunnable locally, green remotely.
_SKIP_DIRS = {
    ".claude",
    ".git",
    ".mypy_cache",
    ".pytest_cache",
    ".ruff_cache",
    ".venv",
    "node_modules",
    "target",
}


def _repo_root() -> Path:
    return Path(__file__).resolve().parents[2]


def _is_scaffold_marker(decorator: ast.expr) -> bool:
    """True for `@pytest.mark.scaffold`.

    Matched as a decorator in the AST rather than as the substring "pytest.mark.scaffold",
    which this file's own prose contains — the substring version counted this module as the
    194th scaffold. Text search cannot tell a marker from a mention of one.
    """
    return (
        isinstance(decorator, ast.Attribute)
        and decorator.attr == "scaffold"
        and isinstance(decorator.value, ast.Attribute)
        and decorator.value.attr == "mark"
        and isinstance(decorator.value.value, ast.Name)
        and decorator.value.value.id == "pytest"
    )


@functools.cache
def _scan() -> tuple[tuple[str, ...], tuple[str, ...]]:
    """Return (scaffold files, files still asserting a literal True).

    One walk for the whole module, cached, because three tests ask three questions about the
    same result and the tree is wide.

    Parsed with `ast` rather than grepped. `assert True` in a string, in a comment, or inside
    a docstring explaining why not to write one are all things a text search cannot tell apart
    from the real thing.
    """
    root = _repo_root()
    scaffolds: list[str] = []
    vacuous: list[str] = []

    for path in root.rglob("test_*.py"):
        if any(part in _SKIP_DIRS for part in path.relative_to(root).parts):
            continue

        source = path.read_text(encoding="utf-8")
        rel = str(path.relative_to(root))

        try:
            tree = ast.parse(source, filename=str(path))
        except SyntaxError:  # pragma: no cover - a broken test file fails its own collection
            continue

        for node in ast.walk(tree):
            if isinstance(node, ast.FunctionDef | ast.AsyncFunctionDef) and any(
                _is_scaffold_marker(d) for d in node.decorator_list
            ):
                scaffolds.append(rel)
                break

        for node in ast.walk(tree):
            if (
                isinstance(node, ast.Assert)
                and isinstance(node.test, ast.Constant)
                and node.test.value is True
            ):
                vacuous.append(rel)
                break

    return tuple(sorted(scaffolds)), tuple(sorted(vacuous))


def test_no_test_asserts_a_literal_true() -> None:
    """`assert True` is a pass that verifies nothing, and it is invisible in a green run.

    This is the regression that mattered: a scaffold marked skipped is honest, and the same
    file with the skip removed and the assert restored is back to inflating the count.
    """
    _, vacuous = _scan()

    assert not vacuous, (
        "these test files assert a literal True, which pytest counts as a passing test:\n  "
        + "\n  ".join(vacuous)
        + "\nSkip the test (`@pytest.mark.scaffold` plus `pytest.skip(...)`) if there is "
        "nothing to test yet, or assert something real."
    )


def test_scaffold_count_does_not_regress() -> None:
    """Placeholders are a ratchet: their number may only go down.

    Adding a new scaffold fails here. So does removing one without lowering the baseline —
    deliberately, and with the number to change named in the message.
    """
    scaffolds, _ = _scan()
    count = len(scaffolds)

    assert count <= SCAFFOLD_BASELINE, (
        f"{count} scaffold test files, up from {SCAFFOLD_BASELINE}. A new placeholder is a "
        "test file that will report as skipped forever. Write the test, or — if the scaffold "
        "is genuinely wanted — raise SCAFFOLD_BASELINE in the same commit and say why."
    )
    assert count == SCAFFOLD_BASELINE, (
        f"{SCAFFOLD_BASELINE - count} scaffold(s) fewer than the baseline records. Lower "
        f"SCAFFOLD_BASELINE to {count} in this commit; a ratchet that is not tightened stops "
        "catching the next regression."
    )


@pytest.mark.nightly
def test_every_python_test_file_tests_something() -> None:
    """The target state, in full. Reported nightly, not gated per pull request.

    Expected to fail until every scaffold has an implementation behind it. What it provides is
    a measured, dated gap rather than an impression.
    """
    scaffolds, _ = _scan()
    assert scaffolds == (), f"{len(scaffolds)} test files are still placeholders"
