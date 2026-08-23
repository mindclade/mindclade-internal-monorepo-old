#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Every static gate must be falsifiable: some input has to make it fail.

A review of this repository found ten gates that could not fail, or were not running at all:
two whose exclusion filter matched the absolute path and therefore scanned zero files; one that
skipped the comparison whenever its classifier returned `None`; one that asserted a literal call
name existed and never inspected the call; one that asserted two files existed; one whose
unanchored regex was satisfied by the `@generated` comment above the thing it meant to inspect;
three maturity gates that accepted any non-empty string as evidence; and a `production_dependency`
policy that no code read. That is not ten coincidences. It is one missing discipline: nobody had
ever watched those gates fail.

This module supplies the discipline. For every checker registered in
`run_architecture_checks.CHECKS` it demands a *falsifier* -- a named defect, injected into a
disposable copy of the tree, that the checker must report. Coverage is enforced in both
directions, because one-directional coverage is how the original ten survived:

  * a registered checker with no falsifier fails this gate, so a new gate cannot land untested;
  * a falsifier naming no registered checker fails this gate, so a renamed or deleted checker
    cannot leave behind a fixture that silently exercises nothing.

Four further properties keep the meta-gate itself from becoming the eleventh instance:

  * **Delta, not emptiness.** A fixture passes only when the mutated run reports an error the
    unmutated run did not. "The checker printed something" is not evidence; "the checker printed
    something *because of this defect*" is. Each falsifier also pins a substring of the expected
    message, so a mutation that trips an unrelated invariant is not counted as coverage.
  * **Anchored mutations.** `ScratchTree.replace` and `.scrub` require their anchor text to be
    present, and raise otherwise. When the repository drifts out from under a fixture, the
    fixture fails loudly instead of quietly injecting nothing -- which is exactly the failure
    mode of the original ten.
  * **Revert is verified.** After a checker's fixtures have run, the checker is run once more
    against the reverted tree and must report precisely its baseline again. A fixture that leaks
    state would otherwise contaminate every fixture after it.
  * **Self-falsification.** `check` first runs this harness against a checker that cannot fail
    and requires the harness to reject it, and against one that can and requires the harness to
    accept it. A meta-gate nobody has watched fail is the defect this module exists to prevent,
    so it demonstrates its own failure on every run.

Checkers that do not have a fixture yet are recorded in `UNFALSIFIED_BASELINE` with the reason.
That is a ratchet, not an allowlist: it lets no violation through -- every checker still runs and
still fails the build -- and it is asserted as an exact set, so a checker that acquires a fixture
must be removed from it. It shrinks or it breaks.

`tools/analysis/tests/test_gate_falsifiability.py` exercises the remaining directions: a stale
fixture, a missing fixture, a stale baseline entry, a mutation whose anchor has drifted, a
fixture body that raises, a fixture that trips an unrelated error, a checker that crashes, a
checker that cannot read a fixture root, and a fixture that leaks state past its revert.
"""

from __future__ import annotations

import argparse
import os
import shutil
import sys
import tempfile
from collections.abc import Callable, Iterable, Iterator, Mapping, Sequence
from contextlib import contextmanager
from dataclasses import dataclass, field
from pathlib import Path

CheckFn = Callable[[Path], list[str]]
CheckRegistry = Sequence[tuple[str, CheckFn]]

# The name this module is registered under in run_architecture_checks.CHECKS.
#
# The harness audits every *other* registered checker and exempts itself, because auditing
# itself would recurse without bound. That exemption is the one hole in "every checker has a
# fixture", so it is not left implicit: `check` refuses to pass unless this exact name is
# registered, and discharges the exemption by running `_self_falsification_evidence` -- real
# evidence, produced on every run, that the harness rejects a gate that cannot fail.
SELF_CHECK_NAME = "gate falsifiability"

# Directories that are build output, tool caches, or other checkouts rather than source. Copying
# them would multiply the fixture tree by orders of magnitude, and several of them (agent
# worktrees in particular) are full copies of this repository, which every whole-tree checker
# would then report twice.
UNCOPIED_DIRECTORIES = (
    ".git",
    ".claude",
    ".codex-worktrees",
    ".direnv",
    ".mypy_cache",
    ".pytest_cache",
    ".ruff_cache",
    ".venv",
    "__pycache__",
    "bazel-*",
    "node_modules",
    "target",
)


class FixtureError(RuntimeError):
    """A falsifier could not inject its defect.

    Raised rather than ignored: a mutation whose anchor text has moved injects nothing, and a
    fixture that injects nothing turns the gate it covers back into a gate nobody has watched
    fail. The harness reports it as a meta-gate failure, never as a passing fixture.
    """


class ScratchTree:
    """A disposable copy of the repository that records and reverts every edit.

    The real tree is never touched. Mutations go through this narrow API rather than through
    `open()` so that every edit is journalled -- which makes it revertible exactly, and makes a
    failing meta-gate run legible ("injected X, checker said nothing") instead of merely red.
    """

    def __init__(self, root: Path) -> None:
        self.root = root.resolve()
        self._original: dict[Path, bytes | None] = {}
        # Directories this tree created, deepest first. Restoring file contents is not enough:
        # several checkers gate on a *directory* existing -- `libs/rust/common` and the retired
        # compatibility crates are both directory-existence rules -- so a fixture that wrote
        # `libs/rust/clock/src/lib.rs` left `libs/rust/clock/` behind after revert and every
        # later fixture inherited a violation it did not inject. The harness's own revert
        # verification caught this; it is exactly the contamination that check exists for.
        self._created_directories: list[Path] = []
        self._journal: list[str] = []

    def _target(self, relative: str) -> Path:
        path = Path(os.path.normpath(self.root / relative))
        # Containment is checked on the normalised path, before any I/O. A fixture that reached
        # outside the scratch tree would be mutating the developer's checkout, which is the one
        # thing this harness must never do.
        if not path.is_relative_to(self.root):
            raise FixtureError(f"fixture path escapes the scratch tree: {relative}")
        return path

    def _record(self, path: Path) -> None:
        if path in self._original:
            return
        self._original[path] = path.read_bytes() if path.is_file() else None

    def read(self, relative: str) -> str:
        path = self._target(relative)
        if not path.is_file():
            raise FixtureError(f"fixture source is missing from the tree: {relative}")
        return path.read_text(encoding="utf-8", errors="replace")

    def write(self, relative: str, text: str) -> None:
        """Create or overwrite a file, creating parent directories as needed."""
        path = self._target(relative)
        self._record(path)
        self._make_parents(path)
        path.write_text(text, encoding="utf-8")
        self._journal.append(f"wrote {relative}")

    def _make_parents(self, path: Path) -> None:
        missing = []
        parent = path.parent
        while parent != self.root and not parent.exists():
            missing.append(parent)
            parent = parent.parent
        path.parent.mkdir(parents=True, exist_ok=True)
        # Deepest first, so revert can remove them in an order where each is already empty.
        self._created_directories = missing + self._created_directories

    def delete(self, relative: str) -> None:
        path = self._target(relative)
        if not path.is_file():
            raise FixtureError(f"fixture cannot delete a file that is not there: {relative}")
        self._record(path)
        path.unlink()
        self._journal.append(f"deleted {relative}")

    def replace(self, relative: str, old: str, new: str) -> None:
        """Substitute `old` with `new`, requiring exactly one occurrence.

        The uniqueness requirement is the anti-drift mechanism. `str.replace` on absent text is
        a silent no-op, and a no-op mutation lets a fixture report success while proving nothing.
        An anchor that has moved, or that now matches twice, is a fixture bug and is raised as
        one rather than papered over.
        """
        text = self.read(relative)
        occurrences = text.count(old)
        if occurrences != 1:
            raise FixtureError(
                f"{relative}: fixture anchor occurs {occurrences} times, expected exactly 1: "
                f"{old!r}"
            )
        self._overwrite(relative, text.replace(old, new))
        self._journal.append(f"edited {relative}")

    def scrub(self, relative: str, needle: str, replacement: str) -> None:
        """Replace every occurrence of `needle`, requiring at least one.

        Distinct from `replace` because a token contract is satisfied by *any* occurrence of the
        token -- the prose comment above a function counts. Removing only the declaration would
        leave the checker satisfied and the fixture would prove the opposite of what it claims.
        """
        text = self.read(relative)
        if needle not in text:
            raise FixtureError(f"{relative}: fixture anchor is absent: {needle!r}")
        self._overwrite(relative, text.replace(needle, replacement))
        self._journal.append(f"scrubbed {needle!r} from {relative}")

    def append(self, relative: str, text: str) -> None:
        """Append to an existing file, requiring that it exists."""
        self._overwrite(relative, self.read(relative) + text)
        self._journal.append(f"appended to {relative}")

    def _overwrite(self, relative: str, text: str) -> None:
        path = self._target(relative)
        self._record(path)
        path.write_text(text, encoding="utf-8")

    def journal(self) -> str:
        return "; ".join(self._journal) if self._journal else "no edit was made"

    def revert(self) -> None:
        for path, previous in self._original.items():
            if previous is None:
                if path.is_file():
                    path.unlink()
            else:
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes(previous)
        for directory in self._created_directories:
            # Only directories this tree created, and only while empty. `rmdir` rather than
            # `rmtree`: if something else put a file there, losing it silently would be worse
            # than leaving the directory, and the revert verification will report the residue.
            if directory.is_dir() and not any(directory.iterdir()):
                directory.rmdir()
        self._created_directories.clear()
        self._original.clear()
        self._journal.clear()


@contextmanager
def materialize(root: Path) -> Iterator[ScratchTree]:
    """Copy `root` into a scratch directory outside it and yield a mutable handle.

    Outside it, deliberately: every whole-tree checker in the suite walks the repository with
    `rglob`, so a fixture tree nested inside the checkout would be found and reported by the very
    checkers this harness is measuring.
    """
    holder = tempfile.mkdtemp(prefix="mindclade-falsifiability-")
    try:
        destination = Path(holder) / "repository"
        shutil.copytree(
            root,
            destination,
            symlinks=True,
            ignore=shutil.ignore_patterns(*UNCOPIED_DIRECTORIES),
        )
        yield ScratchTree(destination)
    finally:
        shutil.rmtree(holder, ignore_errors=True)


@dataclass(frozen=True)
class Falsifier:
    """One defect that a named checker must report.

    `expect` is a substring of the message the checker is required to emit. It is not decoration:
    without it a mutation that happened to trip some *other* invariant would count as coverage,
    and the gate under test would still never have been watched failing for its own reason.
    """

    check: str
    defect: str
    expect: str
    inject: Callable[[ScratchTree], None] = field(repr=False)


def _go_source(package: str, import_path: str) -> str:
    return f'package {package}\n\nimport _ "{import_path}"\n'


FALSIFIERS: tuple[Falsifier, ...] = (
    Falsifier(
        check="build/toolchain",
        defect="the Cargo workspace rust-version drifts away from the Nix pin",
        expect="Nix Rust version does not match Cargo rust-version",
        inject=lambda tree: tree.replace(
            "Cargo.toml", 'rust-version = "1.97.1"', 'rust-version = "1.90.0"'
        ),
    ),
    Falsifier(
        check="build/toolchain",
        defect="a Bzlmod repository regains a legacy WORKSPACE file",
        expect="legacy WORKSPACE is forbidden",
        inject=lambda tree: tree.write("WORKSPACE", "# reintroduced by a fixture\n"),
    ),
    Falsifier(
        check="artifact GC",
        defect="the GC receipt validator disappears from the control-plane contract",
        expect="control/artifacts/gc.go: missing ValidateGCReceipt",
        inject=lambda tree: tree.scrub(
            "control/artifacts/gc.go", "ValidateGCReceipt", "checkReceipt"
        ),
    ),
    Falsifier(
        check="foundation hardening",
        defect="the cargo-deny policy the hardening contract requires is removed",
        expect="missing hardening contract: deny.toml",
        inject=lambda tree: tree.delete("deny.toml"),
    ),
    Falsifier(
        check="foundation hardening",
        defect="Rust qualification is allowed to regenerate the committed lockfile",
        expect="must never mutate Cargo.lock",
        inject=lambda tree: tree.append(
            "tools/qualification/rust/qualify.py", '\n_FIXTURE = "generate-lockfile"\n'
        ),
    ),
    Falsifier(
        check="enforced decisions",
        defect="an enforced decision points at a checker that is no longer in the tree",
        expect="one-root-go-module: missing tools/analysis/check_go_modules.py",
        inject=lambda tree: tree.delete("tools/analysis/check_go_modules.py"),
    ),
    Falsifier(
        check="component ownership",
        defect="ownership metadata outlives the component it describes",
        expect="fixture.orphan: ownership metadata has no declared component",
        inject=lambda tree: tree.append(
            "architecture/component_ownership.toml",
            '\n[component."fixture.orphan"]\n'
            'owner = "nobody"\n'
            'criticality = "tier-2"\n'
            'language = "go"\n'
            "security_review = false\n",
        ),
    ),
    Falsifier(
        check="code/docs alignment",
        defect="a crate retired by the 2026-08 consolidation reappears under libs/rust",
        expect="libs/rust/clock: retired compatibility crate must remain removed",
        inject=lambda tree: tree.write("libs/rust/clock/src/lib.rs", "// fixture\n"),
    ),
    Falsifier(
        check="dependency layers",
        defect="a libs/go foundation package imports the control plane above it",
        expect="forbidden dependency go.mindclade.dev/control/",
        inject=lambda tree: tree.write(
            "libs/go/faults/fixture_inversion.go",
            _go_source("faults", "go.mindclade.dev/control/orchestration"),
        ),
    ),
    Falsifier(
        check="Go layers and paved roads",
        defect="a Layer 0 package imports a Layer 2 package",
        expect="higher layer",
        inject=lambda tree: tree.write(
            "libs/go/faults/fixture_layering.go",
            _go_source("faults", "go.mindclade.dev/libs/go/servicekit"),
        ),
    ),
    Falsifier(
        check="Go modules",
        defect="a nested Go module boundary appears under libs/go",
        expect="unexpected Go module boundary: libs/go/faults/go.mod",
        inject=lambda tree: tree.write(
            "libs/go/faults/go.mod", "module go.mindclade.dev/libs/go/faults\n\ngo 1.26\n"
        ),
    ),
    Falsifier(
        check="libs/go admission",
        defect="an unadmitted dumping-ground package appears under libs/go",
        expect="libs/go/utils: forbidden dumping-ground/domain name",
        inject=lambda tree: tree.write("libs/go/utils/util.go", "package utils\n"),
    ),
    # The four shapes of PR #141's defect. Each one made `bazelw` exit 0 while its log said the
    # build did not complete, so each one has to be watched failing somewhere that reads the
    # evidence rather than the exit code.
    Falsifier(
        check="lockfile freshness",
        defect="Cargo.lock is edited without regenerating MODULE.bazel.lock",
        expect="Cargo.lock: changed since MODULE.bazel.lock was generated",
        # Appending is enough and is the point: crate_universe records the file's SHA-256, so any
        # byte of drift invalidates the extension and fails analysis for every Rust target.
        inject=lambda tree: tree.append("Cargo.lock", "\n# injected by a falsifying fixture\n"),
    ),
    Falsifier(
        check="lockfile freshness",
        defect="a Cargo workspace member is added without regenerating MODULE.bazel.lock",
        expect=(
            "libs/rust/fixture_member/Cargo.toml: not recorded as a crate_universe input in "
            "MODULE.bazel.lock"
        ),
        # The shape the digest comparison cannot catch on its own: a manifest that did not exist
        # when the lock was written has no recorded digest to mismatch, so only the roster
        # completeness check sees it.
        inject=lambda tree: (
            tree.replace(
                "Cargo.toml",
                '"tools/qualification/rust/perf_probe",',
                '"tools/qualification/rust/perf_probe",\n    "libs/rust/fixture_member",',
            ),
            tree.write(
                "libs/rust/fixture_member/Cargo.toml",
                '[package]\nname = "mindclade_fixture_member"\nversion = "0.0.0"\n'
                'edition = "2024"\n',
            ),
        )[-1],
    ),
    Falsifier(
        check="lockfile freshness",
        defect="a go.mod requirement is bumped without regenerating go.sum",
        expect="go.sum does not authenticate connectrpc.com/connect v1.18.2",
        inject=lambda tree: tree.replace(
            "go.mod", "connectrpc.com/connect v1.18.1", "connectrpc.com/connect v1.18.2"
        ),
    ),
    Falsifier(
        check="lockfile freshness",
        defect="the Bzlmod lockfile is absent, so nothing pins the dependency closure",
        expect="MODULE.bazel.lock is missing",
        # Guards the checker itself. An absent lockfile has no digests to compare, and the one
        # outcome that must never follow from "there was nothing to compare" is a pass.
        inject=lambda tree: tree.delete("MODULE.bazel.lock"),
    ),
    Falsifier(
        check="protocol graph completeness",
        defect="a promoted protobuf source is added without any Bazel graph edges",
        expect=("protocols/proto/mindclade/common/v1/falsifier_orphan.proto has no proto_library"),
        inject=lambda tree: tree.write(
            "protocols/proto/mindclade/common/v1/falsifier_orphan.proto",
            'syntax = "proto3";\npackage mindclade.common.v1;\n',
        ),
    ),
    Falsifier(
        check="Rust workspace",
        defect="the forbidden libs/rust/common catch-all crate reappears",
        expect="libs/rust/common is forbidden",
        inject=lambda tree: tree.write("libs/rust/common/src/lib.rs", "// fixture\n"),
    ),
    Falsifier(
        check="Rust workspace",
        defect="a workspace member's path dependency points at a crate that is not there",
        expect="path dependency missing",
        inject=lambda tree: tree.replace(
            "services/node_agent/Cargo.toml",
            'mindclade_faults = { path = "../../libs/rust/faults" }',
            'mindclade_faults = { path = "../../libs/rust/faults_absent" }',
        ),
    ),
    Falsifier(
        check="protocol graph completeness",
        defect="a promoted .proto is added without a proto_library to own it",
        expect="fixture_orphan.proto has no proto_library in",
        # The exact shape the checker was written for: buf still lints and projects the file,
        # so the omission is invisible everywhere except a walk of the real tree.
        inject=lambda tree: tree.write(
            "protocols/proto/mindclade/common/v1/fixture_orphan.proto",
            'syntax = "proto3";\n\n'
            "package mindclade.common.v1;\n\n"
            "message FixtureOrphan {\n  string value = 1;\n}\n",
        ),
    ),
    Falsifier(
        check="blueprint scaffold consistency",
        defect="a blueprint path is listed twice in the manifest",
        expect="Cargo.toml is listed more than once in the blueprint manifest",
        inject=lambda tree: tree.append(
            "docs/blueprint/production-monorepo-paths.txt", "Cargo.toml\n"
        ),
    ),
    Falsifier(
        check="blueprint authority",
        defect="the blueprint reserves a generator whose entire output surface must be absent",
        expect="reserved generator claims go output",
        inject=lambda tree: tree.replace(
            "docs/blueprint/production-monorepo-paths.txt",
            "tools/codegen/generate_event_catalog.py\n",
            "tools/codegen/generate_event_catalog.py\ntools/codegen/generate_go_sdk.py\n",
        ),
    ),
    Falsifier(
        check="protocol graph completeness",
        defect=(
            "a .proto reaches the tree with no proto_library, so it is absent from the "
            "descriptor set, the compatibility baseline, and the Go and Python bindings "
            "while buf still lints and projects it"
        ),
        expect="has no proto_library in",
        inject=lambda tree: tree.write(
            "protocols/proto/mindclade/common/v1/unwired.proto",
            'syntax = "proto3";\n\npackage mindclade.common.v1;\n\n'
            "message Unwired {\n  string value = 1;\n}\n",
        ),
    ),
)


# Checkers that do not have a falsifying fixture yet, each with the reason.
#
# A ratchet, in the sense CLAUDE.md sanctions -- not an allowlist. It does not let a violation
# through: every checker here still runs, and still fails the build when it finds something. What
# it records is the coverage frontier, and it is asserted as an EXACT set in both directions:
#
#   * a checker that is neither covered nor listed here fails the gate, so a newly added gate
#     cannot land without a fixture;
#   * a checker listed here that HAS acquired a fixture also fails the gate, so the list cannot
#     be left stale. It shrinks or it breaks; it cannot quietly grow.
#
# Adding an entry is therefore a deliberate, reviewable act with a reason attached, which is the
# opposite of the silent fail-open that produced the ten original defects.
UNFALSIFIED_BASELINE: dict[str, str] = {
    "Cargo/Bazel Rust alignment": (
        "the checker parses MODULE.bazel's crate.from_cargo call and cross-references the "
        "workspace inventory; a fixture has to corrupt both sides coherently to isolate one "
        "invariant"
    ),
    "Go command composition": "needs a Go command fixture that violates composition, not layering",
    "Go test signals": "needs a fixture Go test whose signal is absent rather than merely weak",
    "MLOps static contracts": "contract inventory not yet surveyed for a single-invariant anchor",
    "Rust implementation": "needs a fixture that trips one bound without tripping the others",
    "Rust package manifest": (
        "covered by tests/test_rust_package_manifest.py in the emerging by-hand pattern; needs "
        "porting to a Falsifier rather than a second fixture written from scratch"
    ),
    "affected presubmit": (
        "the largest checker in the suite (1500+ lines) and the one with the most cross-file "
        "invariants; it needs its own survey before a fixture can isolate one of them"
    ),
    "component maturity": (
        "covered by tests/test_component_maturity.py, which builds a hermetic components.toml "
        "per case; needs porting to a Falsifier"
    ),
    "control-plane commands": "needs a Go command fixture; same survey as Go command composition",
    "dependency budgets": "budget inputs not yet surveyed for a single-invariant anchor",
    "foundation consumption": "needs a Go import-graph fixture; same survey as Go layers",
    "generated artifacts": (
        "verify_generated regenerates bindings from protocols/; a fixture must corrupt a "
        "generated artifact without tripping the codegen lane, and protocols/ and tools/codegen "
        "are both under active change"
    ),
    "production dependencies": "landed in #129 after this harness; not yet surveyed",
}


def _run(fn: CheckFn, root: Path) -> frozenset[str]:
    return frozenset(fn(root))


def _describe(errors: Iterable[str], limit: int = 3) -> str:
    listed = sorted(errors)[:limit]
    return "; ".join(listed) if listed else "(nothing)"


def audit(
    root: Path,
    checks: CheckRegistry,
    falsifiers: Sequence[Falsifier],
    *,
    exempt: frozenset[str] = frozenset(),
    pending: Mapping[str, str] | None = None,
    report: Callable[[str], None] | None = None,
) -> list[str]:
    """Require every checker in `checks` to be falsified by a fixture in `falsifiers`.

    `root` is never modified: it is copied first, and every fixture mutates only the copy. The
    return value is a list of meta-gate failures, in the shape every checker here uses.

    `exempt` names checkers the caller has discharged by other means. It exists for exactly one
    caller -- `check`, exempting this harness from auditing itself -- and every exempt name must
    still be registered, so the exemption cannot outlive the thing it exempts.

    `pending` is the coverage ratchet: names mapped to the reason no fixture exists yet. It is
    asserted exactly, so an entry that acquires a fixture becomes a failure until it is removed.
    """
    say = report or (lambda _message: None)
    waiting = dict(pending or {})
    errors: list[str] = []

    registered: dict[str, CheckFn] = {}
    for name, fn in checks:
        if name in registered:
            errors.append(
                f"duplicate checker name {name!r} in the check registry; fixture coverage cannot "
                "be attributed to one of two entries sharing a name"
            )
        registered[name] = fn

    covered: dict[str, list[Falsifier]] = {}
    for falsifier in falsifiers:
        covered.setdefault(falsifier.check, []).append(falsifier)

    for name in sorted(exempt - set(registered)):
        errors.append(
            f"stale exemption: {name!r} is exempt from falsification but is not a registered "
            "checker"
        )
    for name in sorted(set(covered) - set(registered)):
        errors.append(
            f"stale fixture: {name!r} names no registered checker, so its falsifiers exercise "
            "nothing; delete them or repair the name"
        )
    for name in sorted(set(waiting) - set(registered)):
        errors.append(
            f"stale baseline entry: {name!r} is recorded as awaiting a fixture but is not a "
            "registered checker; remove it from UNFALSIFIED_BASELINE"
        )
    for name in sorted(set(waiting) & set(covered)):
        errors.append(
            f"stale baseline entry: {name!r} now has a falsifying fixture; remove it from "
            "UNFALSIFIED_BASELINE. The baseline is a ratchet and only moves down"
        )
    for name in sorted(set(registered) - set(covered) - exempt - set(waiting)):
        errors.append(
            f"unfalsifiable gate: {name!r} has no falsifying fixture, so nobody has watched it "
            f"fail; add a Falsifier for it in {Path(__file__).name}"
        )
    for name in sorted(set(waiting) & set(registered) - set(covered)):
        say(f"PENDING  {name}: {waiting[name]}")

    runnable = sorted((set(registered) & set(covered)) - exempt)
    if not runnable:
        return errors

    with materialize(root) as tree:
        for name in runnable:
            fn = registered[name]
            try:
                baseline = _run(fn, tree.root)
            except Exception as exc:  # Reported, never swallowed.
                errors.append(
                    f"{name}: raised {type(exc).__name__} on an unmutated fixture root ({exc}); "
                    "the checker cannot be pointed at a fixture tree, so its falsifiability "
                    "cannot be measured"
                )
                continue

            for falsifier in covered[name]:
                errors.extend(_verify(tree, name, fn, baseline, falsifier, say))
                tree.revert()

            try:
                restored = _run(fn, tree.root)
            except Exception as exc:  # Reported, never swallowed.
                errors.append(f"{name}: raised {type(exc).__name__} after fixture revert ({exc})")
                continue
            if restored != baseline:
                errors.append(
                    f"{name}: a fixture leaked state past its revert; the reverted tree no longer "
                    f"matches the baseline (added {_describe(restored - baseline)}, dropped "
                    f"{_describe(baseline - restored)})"
                )
    return errors


def _verify(
    tree: ScratchTree,
    name: str,
    fn: CheckFn,
    baseline: frozenset[str],
    falsifier: Falsifier,
    say: Callable[[str], None],
) -> list[str]:
    label = f"{name} / {falsifier.defect}"
    try:
        falsifier.inject(tree)
    except FixtureError as exc:
        return [f"{label}: fixture could not inject its defect: {exc}"]
    except Exception as exc:  # Reported, never swallowed.
        # A broken fixture body must surface as a meta-gate failure naming the fixture, not as a
        # traceback out of the whole suite. The partial edit is still reverted by the caller,
        # because every write is journalled before it happens.
        return [f"{label}: fixture raised {type(exc).__name__} while injecting its defect ({exc})"]

    try:
        observed = _run(fn, tree.root)
    except Exception as exc:  # Reported, never swallowed.
        return [
            f"{label}: the checker raised {type(exc).__name__} ({exc}) instead of reporting a "
            "violation; a stack trace is not a gate result"
        ]

    introduced = observed - baseline
    if not introduced:
        return [
            f"{label}: injected [{tree.journal()}] and the checker never reported a violation; "
            "this gate cannot fail"
        ]
    # The *matching* message, not the first one sorted. A mutation often trips more than one
    # invariant, and printing an incidental neighbour makes the evidence log claim the fixture
    # proved something it did not.
    matched = sorted(message for message in introduced if falsifier.expect in message)
    if not matched:
        return [
            f"{label}: the checker failed, but not for the injected reason. Expected a message "
            f"containing {falsifier.expect!r}, got: {_describe(introduced)}"
        ]
    say(f"FALSIFIED {label}\n    injected: {tree.journal()}\n    reported: {matched[0]}")
    return []


def _self_falsification_evidence(report: Callable[[str], None] | None = None) -> list[str]:
    """Prove on every run that this harness rejects a gate that cannot fail.

    The meta-gate is exempt from its own audit -- auditing itself would not terminate -- so its
    non-vacuity has to be established some other way. It is established here, inline, against a
    one-file fixture root and an injected registry. Both directions are asserted, because a
    harness that reported a failure unconditionally would satisfy the first case: the vacuous
    checker must be rejected, and the honest one must be accepted.
    """
    say = report or (lambda _message: None)
    errors: list[str] = []
    with tempfile.TemporaryDirectory(prefix="mindclade-selftest-") as holder:
        root = Path(holder) / "tree"
        root.mkdir()
        (root / "subject.txt").write_text("intact\n", encoding="utf-8")

        def vacuous(_root: Path) -> list[str]:
            return []

        def honest(scratch: Path) -> list[str]:
            content = (scratch / "subject.txt").read_text(encoding="utf-8").strip()
            return [] if content == "intact" else [f"subject.txt was tampered with: {content}"]

        def tamper(tree: ScratchTree) -> None:
            tree.write("subject.txt", "tampered\n")

        def probe(name: str) -> list[Falsifier]:
            return [
                Falsifier(
                    check=name,
                    defect="the subject file is rewritten",
                    expect="tampered",
                    inject=tamper,
                )
            ]

        vacuous_result = audit(root, [("vacuous probe", vacuous)], probe("vacuous probe"))
        if not any("never reported a violation" in message for message in vacuous_result):
            errors.append(
                "the falsifiability harness accepted a checker that cannot fail; the meta-gate "
                f"is itself vacuous (it reported: {_describe(vacuous_result)})"
            )
        else:
            say("SELF-TEST rejected a checker that cannot fail")

        honest_result = audit(root, [("honest probe", honest)], probe("honest probe"))
        if honest_result:
            errors.append(
                "the falsifiability harness rejected a checker that does fail on its fixture; a "
                f"harness that fails unconditionally proves nothing (it reported: "
                f"{_describe(honest_result)})"
            )
        else:
            say("SELF-TEST accepted a checker that does fail on its fixture")
    return errors


def _registered_checks() -> CheckRegistry:
    # Imported inside the function on purpose. run_architecture_checks imports this module at
    # module scope in order to register it, so a module-scope import back would be circular. By
    # the time any check runs, that module is fully initialised.
    import run_architecture_checks

    return run_architecture_checks.CHECKS


def _print(message: str) -> None:
    print(message, flush=True)


def _silent(_message: str) -> None:
    return


def check(root: Path, report: Callable[[str], None] = _print) -> list[str]:
    """Entry point used by run_architecture_checks, which calls every checker as `fn(root)`.

    The reporter therefore defaults to printing rather than to silence. The evidence -- which
    defect was injected into which gate, and what it reported -- is the product of this check;
    a run that emitted only "PASS gate falsifiability" would be a claim without its receipt,
    which is the shape of the problem this module exists to fix.
    """
    checks = _registered_checks()
    errors = _self_falsification_evidence(report)
    if SELF_CHECK_NAME not in {name for name, _ in checks}:
        errors.append(
            f"the falsifiability meta-gate is not registered as {SELF_CHECK_NAME!r} in "
            "run_architecture_checks.CHECKS; a meta-gate that does not run enforces nothing"
        )
    errors.extend(
        audit(
            root,
            checks,
            FALSIFIERS,
            exempt=frozenset({SELF_CHECK_NAME}),
            pending=UNFALSIFIED_BASELINE,
            report=report,
        )
    )
    return errors


def main(argv: Sequence[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Prove every static gate can fail.")
    parser.add_argument("--repo", type=Path, default=Path(__file__).resolve().parents[2])
    parser.add_argument(
        "--quiet",
        action="store_true",
        help="Suppress the per-fixture evidence and print only failures.",
    )
    args = parser.parse_args(argv)
    report = None if args.quiet else (lambda message: print(message, flush=True))
    errors = check(args.repo.resolve(), report)
    for error in errors:
        print(error)
    if errors:
        print("gate falsifiability check failed")
        return 1
    print(f"gate falsifiability check passed ({len(FALSIFIERS)} fixtures)")
    return 0


if __name__ == "__main__":
    sys.dont_write_bytecode = True
    HERE = Path(__file__).resolve().parent
    if str(HERE) not in sys.path:
        sys.path.insert(0, str(HERE))
    raise SystemExit(main())
