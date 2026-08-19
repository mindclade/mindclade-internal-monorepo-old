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
SCAFFOLD_BASELINE = 193

_SKIP_DIRS = {
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
            if isinstance(node, ast.FunctionDef | ast.AsyncFunctionDef):
                if any(_is_scaffold_marker(d) for d in node.decorator_list):
                    scaffolds.append(rel)
                    break

        for node in ast.walk(tree):
            if isinstance(node, ast.Assert) and isinstance(node.test, ast.Constant):
                if node.test.value is True:
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
