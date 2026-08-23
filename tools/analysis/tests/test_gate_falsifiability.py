# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""The meta-gate must itself be falsifiable, or it is the eleventh instance of the defect.

`check_gate_falsifiability` exists because ten gates in this repository could not fail and
nobody noticed. A harness that enforces falsifiability but cannot itself fail would reproduce
that defect one level up, with more ceremony. Every test below therefore drives the harness with
a synthetic registry and asserts on the *failure* it must produce -- a checker that cannot fail,
a fixture that names nothing, a checker with no fixture, an anchor that has drifted, a mutation
that trips an unrelated invariant, a checker that crashes, and a fixture that leaks past its
revert. The two acceptance tests are the paired direction: a harness that failed unconditionally
would satisfy every rejection test and prove nothing at all.

Nothing here imports `run_architecture_checks`. The registry the harness audits is a parameter,
which is what makes these cases hermetic; the real registry's coverage is enforced by the gate
itself in the static presubmit, not by re-implementing that bookkeeping here.
"""

from __future__ import annotations

import importlib.util
import sys
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[3]
ANALYSIS = ROOT / "tools/analysis"


def load(name: str, path: Path):
    spec = importlib.util.spec_from_file_location(name, path)
    # None means the path is not importable -- a non-Python extension or a directory, which is
    # how a drifting ROOT (parents[3]) goes wrong. Raised rather than asserted: `python -O`
    # drops asserts, and this runs at import time where the message is all anyone gets.
    if spec is None or spec.loader is None:
        raise RuntimeError(f"unable to load {path}")
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    spec.loader.exec_module(module)
    return module


harness = load("check_gate_falsifiability", ANALYSIS / "check_gate_falsifiability.py")


@pytest.fixture
def source(tmp_path: Path) -> Path:
    """A one-file stand-in for the repository, used as the tree the harness copies."""
    root = tmp_path / "source"
    root.mkdir()
    (root / "subject.txt").write_text("intact\n", encoding="utf-8")
    return root


def content_checker(scratch: Path) -> list[str]:
    """Reports exactly when subject.txt has been altered. The honest baseline."""
    text = (scratch / "subject.txt").read_text(encoding="utf-8").strip()
    return [] if text == "intact" else [f"subject.txt was tampered with: {text}"]


def tamper(tree) -> None:
    tree.write("subject.txt", "tampered\n")


def falsifier(check: str, *, expect: str = "tampered", inject=tamper):
    return harness.Falsifier(
        check=check, defect="the subject file is rewritten", expect=expect, inject=inject
    )


def test_a_checker_that_cannot_fail_is_rejected(source: Path) -> None:
    """The headline property. This is the case the original ten gates would all have hit."""
    errors = harness.audit(source, [("vacuous", lambda _root: [])], [falsifier("vacuous")])
    assert any("this gate cannot fail" in error for error in errors), errors


def test_a_checker_that_can_fail_is_accepted(source: Path) -> None:
    """The paired direction: a harness that failed unconditionally would prove nothing."""
    assert harness.audit(source, [("content", content_checker)], [falsifier("content")]) == []


def test_a_checker_with_no_fixture_is_rejected(source: Path) -> None:
    errors = harness.audit(source, [("content", content_checker)], [])
    assert any("unfalsifiable gate: 'content'" in error for error in errors), errors


def test_a_fixture_naming_no_checker_is_rejected(source: Path) -> None:
    """Stale-fixture detection. One-directional coverage is how the original ten survived."""
    errors = harness.audit(
        source, [("content", content_checker)], [falsifier("content"), falsifier("renamed")]
    )
    assert any("stale fixture: 'renamed'" in error for error in errors), errors


def test_an_exemption_for_an_unregistered_checker_is_rejected(source: Path) -> None:
    errors = harness.audit(
        source,
        [("content", content_checker)],
        [falsifier("content")],
        exempt=frozenset({"departed"}),
    )
    assert any("stale exemption: 'departed'" in error for error in errors), errors


def test_a_drifted_anchor_fails_loudly(source: Path) -> None:
    """A mutation that no longer matches must fail, not silently inject nothing.

    This is the failure mode the original defects shared: the gate ran, matched nothing, and
    reported success. A fixture that quietly stops mutating restores exactly that hole.
    """
    errors = harness.audit(
        source,
        [("content", content_checker)],
        [falsifier("content", inject=lambda tree: tree.replace("subject.txt", "absent", "x"))],
    )
    assert any("fixture anchor occurs 0 times" in error for error in errors), errors


def test_a_broken_fixture_body_is_reported_not_raised(source: Path) -> None:
    """A typo in a fixture must name the fixture, not blow up the whole suite."""

    def broken(tree) -> None:
        tree.no_such_method("subject.txt")

    errors = harness.audit(
        source, [("content", content_checker)], [falsifier("content", inject=broken)]
    )
    assert any("raised AttributeError while injecting" in error for error in errors), errors


def test_a_failure_for_an_unrelated_reason_is_not_coverage(source: Path) -> None:
    """The mutated run must fail *for the injected reason*, not merely fail."""
    errors = harness.audit(
        source,
        [("content", content_checker)],
        [falsifier("content", expect="permission denied")],
    )
    assert any("not for the injected reason" in error for error in errors), errors


def test_a_checker_that_crashes_is_reported(source: Path) -> None:
    def explodes(scratch: Path) -> list[str]:
        if (scratch / "subject.txt").read_text(encoding="utf-8").strip() != "intact":
            raise ValueError("unparseable")
        return []

    errors = harness.audit(source, [("explodes", explodes)], [falsifier("explodes")])
    assert any("a stack trace is not a gate result" in error for error in errors), errors


def test_a_checker_that_cannot_read_a_fixture_root_is_reported(tmp_path: Path) -> None:
    """A checker that only works against the real checkout cannot be measured at all."""
    root = tmp_path / "source"
    root.mkdir()
    errors = harness.audit(
        root,
        [("absent input", lambda scratch: [(scratch / "gone.txt").read_text()])],
        [falsifier("absent input")],
    )
    assert any("on an unmutated fixture root" in error for error in errors), errors


def test_a_fixture_that_leaks_past_its_revert_is_reported(source: Path) -> None:
    """Edits must go through ScratchTree, or a later fixture inherits an earlier one's defect."""

    def leak(tree) -> None:
        tamper(tree)
        # Deliberately behind the journal's back, which is what an ad-hoc `open()` in a fixture
        # would do.
        (tree.root / "leaked.txt").write_text("residue\n", encoding="utf-8")

    def sees_residue(scratch: Path) -> list[str]:
        errors = content_checker(scratch)
        if (scratch / "leaked.txt").exists():
            errors.append("leaked.txt is present")
        return errors

    errors = harness.audit(source, [("residue", sees_residue)], [falsifier("residue", inject=leak)])
    assert any("leaked state past its revert" in error for error in errors), errors


def test_the_audited_tree_is_never_modified(source: Path) -> None:
    before = {path: path.read_bytes() for path in source.rglob("*") if path.is_file()}
    harness.audit(
        source,
        [("content", content_checker)],
        [
            falsifier("content"),
            falsifier("content", inject=lambda tree: tree.delete("subject.txt")),
        ],
    )
    after = {path: path.read_bytes() for path in source.rglob("*") if path.is_file()}
    assert after == before


def test_a_fixture_cannot_escape_the_scratch_tree(tmp_path: Path) -> None:
    tree = harness.ScratchTree(tmp_path)
    with pytest.raises(harness.FixtureError, match="escapes the scratch tree"):
        tree.write("../outside.txt", "no")


def test_reverting_restores_deleted_and_created_files(tmp_path: Path) -> None:
    tree = harness.ScratchTree(tmp_path)
    (tmp_path / "kept.txt").write_text("original\n", encoding="utf-8")
    tree.delete("kept.txt")
    tree.write("nested/new.txt", "added\n")
    tree.revert()
    assert (tmp_path / "kept.txt").read_text(encoding="utf-8") == "original\n"
    assert not (tmp_path / "nested/new.txt").exists()


def test_scrub_requires_at_least_one_occurrence(tmp_path: Path) -> None:
    tree = harness.ScratchTree(tmp_path)
    (tmp_path / "gc.go").write_text("// Token\nfunc Token() {}\n", encoding="utf-8")
    tree.scrub("gc.go", "Token", "Other")
    assert "Token" not in (tmp_path / "gc.go").read_text(encoding="utf-8")
    with pytest.raises(harness.FixtureError, match="fixture anchor is absent"):
        tree.scrub("gc.go", "Token", "Other")


def test_self_falsification_evidence_passes(tmp_path: Path) -> None:
    """The evidence `check` produces on every run must be clean in a healthy tree."""
    assert harness._self_falsification_evidence() == []


def test_a_pending_checker_is_not_required_to_have_a_fixture(source: Path) -> None:
    """The coverage ratchet: an unwritten fixture is recorded with a reason, not ignored."""
    errors = harness.audit(
        source,
        [("content", content_checker)],
        [],
        pending={"content": "fixture not written yet"},
    )
    assert errors == []


def test_a_pending_entry_that_gained_a_fixture_is_rejected(source: Path) -> None:
    """The ratchet only moves down. A covered checker may not stay on the pending list."""
    errors = harness.audit(
        source,
        [("content", content_checker)],
        [falsifier("content")],
        pending={"content": "fixture not written yet"},
    )
    assert any("now has a falsifying fixture" in error for error in errors), errors


def test_a_pending_entry_for_an_unregistered_checker_is_rejected(source: Path) -> None:
    errors = harness.audit(
        source, [("content", content_checker)], [falsifier("content")], pending={"gone": "reason"}
    )
    assert any("stale baseline entry: 'gone'" in error for error in errors), errors


def test_every_pending_entry_carries_a_reason() -> None:
    """An entry with no reason is an allowlist; an entry with one is a tracked debt."""
    for name, reason in harness.UNFALSIFIED_BASELINE.items():
        assert reason.strip(), name


def test_every_registered_falsifier_pins_a_message() -> None:
    """An empty `expect` would match every message and reduce the gate to "it printed"."""
    for entry in harness.FALSIFIERS:
        assert entry.expect.strip(), entry
        assert entry.defect.strip(), entry
