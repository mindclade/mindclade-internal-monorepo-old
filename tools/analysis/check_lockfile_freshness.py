#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""A committed lockfile must still describe the inputs it was generated from.

The defect this exists for, three times in one day. PR #141 changed `Cargo.lock` and left
`MODULE.bazel.lock` alone. `Cargo.lock` is a recorded input of the `crate_universe` module
extension, so under `--config=ci`'s `--lockfile_mode=error` the extension refuses to evaluate and
**every** Rust target in the repository fails analysis -- 49 of 144 analyzed, `Build did NOT
complete successfully`. Two further agents hit the same class from the other direction: adding a
workspace member, or a dependency edge, invalidates the same lock.

Why it kept getting reported as green is the part that matters. Bazel *skips* targets whose
analysis fails rather than failing them, and `--keep_going` lets the process exit 0, so `bazelw`
returned success while its own log said the build did not complete. Three agents read the exit
code and reported a passing build. The failure also takes ~30s of analysis to appear, in the slow
lane, long after the author has moved on.

So this checker is static, stdlib-only, and takes milliseconds: it recomputes digests that the
lockfile itself records and compares them to the bytes on disk.

**Detect, never generate.** CI must not regenerate a lockfile to make a build pass:

  * `--lockfile_mode=error` is a deliberate fail-closed choice; regenerating in CI is
    `--lockfile_mode=update` with extra steps, which is the "weaken a gate to make a change pass"
    that CLAUDE.md forbids by name.
  * lockfiles are dependency *authorities*. `security/rust-supply-chain.toml` sets
    `require_lockfile = true` and `allow_git_dependencies = false`; if CI regenerates, the
    authority becomes whatever resolution CI happened to reach, reviewed by nobody, and
    `cargo deny check` audits a file no human approved.
  * a stale lockfile is information -- somebody changed dependencies without seeing the
    second-order effect. Auto-healing means the next person does not learn either.

The precedent is `check_license_headers.py`: CI checks, `--fix` is for humans. `--fix` here runs
the sanctioned regeneration commands and then *re-runs the check* to say whether the drift is
actually gone, because the one thing this module must not do is trust a Bazel exit code.

What it detects
---------------

*Bazel.* `MODULE.bazel.lock` records, per module extension, a `recordedInputs` map whose
`FILE:@@//<path>` keys carry the SHA-256 of the file as it stood when the extension was last
evaluated. That digest is plain `sha256(bytes)`, so it is exactly recomputable here. Every
main-repository file any extension records is verified. For `crate_universe` specifically the
roster is also checked for completeness: `Cargo.lock`, the workspace root `Cargo.toml`, and every
`[workspace].members` manifest must appear among the recorded inputs, which is what catches an
*added* member -- a new manifest has no recorded digest to mismatch.

*Go.* `go.sum` must authenticate the `go.mod` of every module the main module requires. That is
the invariant `docs/architecture/build-and-toolchains.md` already states and nothing enforced
offline.

What it does NOT detect -- stated plainly, because a checker that claims more than it verifies is
the failure mode this repository is paying for:

  * **`MODULE.bazel` edits are not fully covered.** The lock summarises the module graph as
    `usagesDigest` and `bzlTransitiveDigest`, neither of which is recomputable without evaluating
    Starlark. A `bazel_dep` version bump, or a change to a `crate.from_cargo` attribute other
    than one that moves a recorded file, is invisible here. `registryFileHashes` staleness is
    likewise out of scope. Only `bazelw mod deps` proves those.
  * **Extension inputs outside the main repository are not verified.** `FILE:` keys naming an
    external repo (`@@rules_rust+//...`) are counted but not hashed; the file is not in this tree.
  * **`ENV:` and `REPO_MAPPING:` inputs are not evaluated.** They depend on the invoking
    environment and the resolved module graph.
  * **Go coverage is one-directional.** Extra `go.sum` lines are normal (the file spans the
    pruned module graph, not just this module's requires), so only missing authentication is an
    error. Completeness and tidiness of the transitive graph still need the connected lane's
    `go mod download all` / `go mod verify` / `go mod tidy -diff`.

Usage::

    python3 tools/analysis/check_lockfile_freshness.py           # check, exit 1 on drift
    python3 tools/analysis/check_lockfile_freshness.py --fix     # developer-run regeneration

Everything here is stdlib-only (`json`, `hashlib`, `tomllib`) so it runs in the `--static-only`
lane alongside its siblings.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import re
import subprocess
import sys
import tomllib
from pathlib import Path

BAZEL_LOCK = "MODULE.bazel.lock"
CARGO_LOCK = "Cargo.lock"
CARGO_MANIFEST = "Cargo.toml"

# The lockfile schema versions this module has been read against. Bazel bumps `lockFileVersion`
# when the layout changes, and a layout this parser does not understand must be a loud failure
# rather than a quiet pass: an unrecognised shape would yield zero recorded inputs, and zero
# recorded inputs verified is indistinguishable here from everything being fresh. The current
# manifest resolves Bazel 9.1.1, which writes version 26.
KNOWN_LOCK_FILE_VERSIONS = frozenset({26})

# Bazel's canonical label for the crate_universe extension. Matched on the suffix because the
# rules_rust apparent-repository prefix (`@@rules_rust+`) carries a version-dependent mangling.
CRATE_EXTENSION_SUFFIX = "//crate_universe:extensions.bzl%crate"

# A `FILE:` recorded input naming a file in this repository, followed by its SHA-256.
MAIN_REPO_FILE = re.compile(r"^FILE:@@//(?P<path>\S+) (?P<digest>[0-9a-f]{64})$")

# The sanctioned regeneration commands, named verbatim in every failure message so a red CI run
# tells the author exactly what to run. These are never run by the check path.
BAZEL_REPIN_COMMAND = (
    "tools/dev/nixw develop .#ci-bazel --command tools/dev/bazelw mod deps --lockfile_mode=update"
)
GO_TIDY_COMMAND = "tools/dev/nixw develop .#ci --command go mod tidy"

# Every Go module admitted by check_go_modules.py, as a directory relative to the root.
GO_MODULE_DIRECTORIES = ("", "sdk/go")


class LockfileError(RuntimeError):
    """A lockfile could not be read or parsed, so its freshness cannot be evaluated.

    Raised rather than swallowed. An unreadable, absent, or malformed lockfile is the one input
    that must never produce a pass: a checker that treats "I could not parse it" as "nothing was
    wrong" is a gate that cannot fail, which is the class of defect this tree has already found
    ten instances of. `check` converts it into a reported error; the CLI prints it and exits 1.
    """


def _sha256(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def _load_bazel_lock(root: Path) -> dict:
    path = root / BAZEL_LOCK
    if not path.is_file():
        raise LockfileError(f"{BAZEL_LOCK} is missing; regenerate it with: {BAZEL_REPIN_COMMAND}")
    try:
        raw = path.read_text(encoding="utf-8")
    except (OSError, UnicodeDecodeError) as error:
        raise LockfileError(f"{BAZEL_LOCK} could not be read: {error}") from error
    try:
        document = json.loads(raw)
    except json.JSONDecodeError as error:
        raise LockfileError(f"{BAZEL_LOCK} is not valid JSON: {error}") from error
    if not isinstance(document, dict):
        raise LockfileError(f"{BAZEL_LOCK} is not a JSON object")

    version = document.get("lockFileVersion")
    if version not in KNOWN_LOCK_FILE_VERSIONS:
        known = ", ".join(str(v) for v in sorted(KNOWN_LOCK_FILE_VERSIONS))
        raise LockfileError(
            f"{BAZEL_LOCK} declares lockFileVersion {version!r}, which this checker has not been "
            f"read against (known: {known}). Bazel changed the lockfile layout: re-verify "
            "tools/analysis/check_lockfile_freshness.py against the new schema and widen "
            "KNOWN_LOCK_FILE_VERSIONS. Failing rather than passing, because an unparsed layout "
            "yields zero verified inputs and would otherwise look identical to a fresh lock."
        )

    extensions = document.get("moduleExtensions")
    if not isinstance(extensions, dict) or not extensions:
        raise LockfileError(f"{BAZEL_LOCK} records no module extensions")
    return document


def _recorded_inputs(extension: object, label: str) -> list[str]:
    """Every recorded input across an extension's platform segments, as `"<key> <value>"`.

    Lock format 26 writes `recordedInputs` as a list of already-joined strings
    (`"FILE:@@//Cargo.lock <sha256>"`). Earlier formats wrote a key/value object. Both are
    accepted and normalised to the joined form; anything else is a parse failure, because a shape
    this module silently skipped would verify nothing and still print PASS.
    """
    if not isinstance(extension, dict):
        raise LockfileError(f"{BAZEL_LOCK}: extension {label} is not an object")
    keys: list[str] = []
    for segment, payload in extension.items():
        if not isinstance(payload, dict):
            raise LockfileError(
                f"{BAZEL_LOCK}: extension {label} segment {segment} is not an object"
            )
        recorded = payload.get("recordedInputs", [])
        if isinstance(recorded, dict):
            recorded = [f"{key} {value}" for key, value in recorded.items()]
        if not isinstance(recorded, list) or any(not isinstance(k, str) for k in recorded):
            raise LockfileError(
                f"{BAZEL_LOCK}: extension {label} segment {segment} has a malformed recordedInputs"
            )
        keys.extend(recorded)
    return keys


def _workspace_manifests(root: Path) -> list[str]:
    """Repository-relative `Cargo.toml` paths for the workspace root and every member.

    Members are read from `[workspace].members`, glob entries included, because a glob member is
    the shape most likely to add a crate without anybody editing a roster by hand.
    """
    manifest = root / CARGO_MANIFEST
    if not manifest.is_file():
        raise LockfileError(f"{CARGO_MANIFEST} is missing; the Cargo workspace root is not there")
    try:
        document = tomllib.loads(manifest.read_text(encoding="utf-8"))
    except (OSError, UnicodeDecodeError, tomllib.TOMLDecodeError) as error:
        raise LockfileError(f"{CARGO_MANIFEST} could not be parsed: {error}") from error

    workspace = document.get("workspace")
    if not isinstance(workspace, dict):
        raise LockfileError(f"{CARGO_MANIFEST} declares no [workspace] table")
    members = workspace.get("members")
    if not isinstance(members, list) or not members:
        raise LockfileError(f"{CARGO_MANIFEST} declares no [workspace].members")

    found = {CARGO_MANIFEST}
    for member in members:
        if not isinstance(member, str):
            raise LockfileError(f"{CARGO_MANIFEST}: [workspace].members holds a non-string entry")
        if any(character in member for character in "*?["):
            found.update(
                (directory / CARGO_MANIFEST).relative_to(root).as_posix()
                for directory in sorted(root.glob(member))
                if (directory / CARGO_MANIFEST).is_file()
            )
            continue
        found.add(f"{member}/{CARGO_MANIFEST}")
    return sorted(found)


def _check_recorded_digests(root: Path, document: dict) -> list[str]:
    """Every main-repository file an extension recorded must still hash to the recorded digest."""
    errors: list[str] = []
    verified = 0
    for label, extension in document["moduleExtensions"].items():
        for key in _recorded_inputs(extension, label):
            if not key.startswith("FILE:"):
                continue
            match = MAIN_REPO_FILE.match(key)
            if match is None:
                # Either an external-repository input, which is not in this tree, or a shape this
                # parser does not model. An unmodelled FILE: shape in the main repository is a
                # parse failure, not something to skip past.
                if key.startswith("FILE:@@//"):
                    raise LockfileError(
                        f"{BAZEL_LOCK}: extension {label} records an unparseable main-repository "
                        f"input: {key!r}"
                    )
                continue
            verified += 1
            relative = match["path"]
            path = root / relative
            if not path.is_file():
                errors.append(
                    f"{relative}: recorded as an input of {label} in {BAZEL_LOCK} but absent from "
                    f"the tree. Regenerate the lock: {BAZEL_REPIN_COMMAND}"
                )
                continue
            actual = _sha256(path)
            if actual != match["digest"]:
                errors.append(
                    f"{relative}: changed since {BAZEL_LOCK} was generated (recorded "
                    f"{match['digest'][:12]}..., on disk {actual[:12]}...). {label} will refuse "
                    f"to evaluate under --lockfile_mode=error and every target it feeds fails "
                    f"analysis. Regenerate the lock: {BAZEL_REPIN_COMMAND}"
                )
    if verified == 0:
        # Nothing was compared, so nothing was proven. Reported as a failure because a checker
        # that verifies an empty set prints PASS forever.
        raise LockfileError(
            f"{BAZEL_LOCK} records no main-repository file inputs; there is nothing to verify, "
            "which means this gate cannot fail. Re-verify the lockfile layout."
        )
    return errors


def _check_crate_roster(root: Path, document: dict) -> list[str]:
    """`crate_universe` must record `Cargo.lock` and every workspace manifest as an input.

    The digest comparison above catches a *changed* file. It cannot catch an *added* one: a
    workspace member that reached the tree after the last repin has no recorded digest to
    mismatch, and that is precisely the second shape of this defect that landed today.
    """
    labels = [
        label for label in document["moduleExtensions"] if label.endswith(CRATE_EXTENSION_SUFFIX)
    ]
    if not labels:
        raise LockfileError(
            f"{BAZEL_LOCK} records no {CRATE_EXTENSION_SUFFIX} extension, so the Rust dependency "
            "closure it is supposed to pin is unverifiable"
        )

    recorded = {
        match["path"]
        for label in labels
        for key in _recorded_inputs(document["moduleExtensions"][label], label)
        if (match := MAIN_REPO_FILE.match(key)) is not None
    }

    expected = [CARGO_LOCK, *_workspace_manifests(root)]
    return [
        f"{path}: not recorded as a crate_universe input in {BAZEL_LOCK}, so the lock predates "
        f"it. Regenerate the lock: {BAZEL_REPIN_COMMAND}"
        for path in expected
        if path not in recorded
    ]


def check_bazel_lock(root: Path) -> list[str]:
    """Report every way `MODULE.bazel.lock` is stale relative to the files it records."""
    try:
        document = _load_bazel_lock(root)
        return _check_recorded_digests(root, document) + _check_crate_roster(root, document)
    except LockfileError as error:
        return [str(error)]


def _module_requirements(text: str, where: str) -> list[tuple[str, str]]:
    """`(module, version)` for every `require` in a `go.mod`, both block and single-line forms."""
    requirements: list[tuple[str, str]] = []
    in_block = False
    for number, raw in enumerate(text.splitlines(), start=1):
        line = raw.split("//", 1)[0].strip()
        if not line:
            continue
        if in_block:
            if line == ")":
                in_block = False
                continue
            fields = line.split()
            if len(fields) != 2:
                raise LockfileError(f"{where}:{number}: unparseable require entry: {raw.strip()!r}")
            requirements.append((fields[0], fields[1]))
            continue
        if line == "require (":
            in_block = True
            continue
        if line.startswith("require "):
            fields = line.split()
            if len(fields) != 3:
                raise LockfileError(f"{where}:{number}: unparseable require line: {raw.strip()!r}")
            requirements.append((fields[1], fields[2]))
    if in_block:
        raise LockfileError(f"{where}: a require block is never closed")
    return requirements


def _replaced_modules(text: str) -> set[str]:
    """Modules redirected by a `replace` directive.

    Excluded from the checksum requirement: a replacement pointing at a directory has no module
    zip and no `go.sum` line, so demanding one would be a gate that fires on correct trees.
    """
    replaced: set[str] = set()
    in_block = False
    for raw in text.splitlines():
        line = raw.split("//", 1)[0].strip()
        if not line:
            continue
        if in_block:
            if line == ")":
                in_block = False
                continue
        elif line == "replace (":
            in_block = True
            continue
        elif line.startswith("replace "):
            line = line[len("replace ") :]
        else:
            continue
        if "=>" in line:
            replaced.add(line.split("=>", 1)[0].split()[0])
    return replaced


def _sum_entries(path: Path, where: str) -> set[tuple[str, str]]:
    try:
        text = path.read_text(encoding="utf-8")
    except (OSError, UnicodeDecodeError) as error:
        raise LockfileError(f"{where} could not be read: {error}") from error
    entries: set[tuple[str, str]] = set()
    for number, raw in enumerate(text.splitlines(), start=1):
        if not raw.strip():
            continue
        fields = raw.split()
        if len(fields) != 3:
            raise LockfileError(f"{where}:{number}: malformed go.sum line: {raw.strip()!r}")
        entries.add((fields[0], fields[1]))
    return entries


def _check_go_module(root: Path, directory: str) -> list[str]:
    prefix = f"{directory}/" if directory else ""
    mod_relative = f"{prefix}go.mod"
    sum_relative = f"{prefix}go.sum"
    mod_path = root / mod_relative
    if not mod_path.is_file():
        raise LockfileError(f"{mod_relative} is missing")
    try:
        mod_text = mod_path.read_text(encoding="utf-8")
    except (OSError, UnicodeDecodeError) as error:
        raise LockfileError(f"{mod_relative} could not be read: {error}") from error

    requirements = _module_requirements(mod_text, mod_relative)
    replaced = _replaced_modules(mod_text)
    wanted = [(name, version) for name, version in requirements if name not in replaced]
    if not wanted:
        return []

    sum_path = root / sum_relative
    if not sum_path.is_file():
        return [
            f"{sum_relative} is missing while {mod_relative} requires {len(wanted)} module(s); "
            f"regenerate it: {GO_TIDY_COMMAND}"
        ]
    entries = _sum_entries(sum_path, sum_relative)
    return [
        f"{sum_relative} does not authenticate {name} {version}, required by {mod_relative}. The "
        f"go.mod checksum is absent, so the module graph is unverifiable offline. Regenerate: "
        f"{GO_TIDY_COMMAND}"
        for name, version in wanted
        if (name, f"{version}/go.mod") not in entries
    ]


def check_go_sums(root: Path) -> list[str]:
    """Report every `go.mod` requirement its module's `go.sum` fails to authenticate."""
    errors: list[str] = []
    for directory in GO_MODULE_DIRECTORIES:
        try:
            errors.extend(_check_go_module(root, directory))
        except LockfileError as error:
            errors.append(str(error))
    return errors


def check(root: Path) -> list[str]:
    return check_bazel_lock(root) + check_go_sums(root)


def _fix(root: Path) -> int:
    """Run the sanctioned regeneration commands, then re-check.

    Re-checking is the point. `bazel` reports exit 0 for a run whose own log says the build did
    not complete, so the return code of the repin command is not evidence that anything was
    repinned. The digests are.
    """
    commands: list[str] = []
    if check_bazel_lock(root):
        commands.append(BAZEL_REPIN_COMMAND)
    if check_go_sums(root):
        commands.append(GO_TIDY_COMMAND)
    if not commands:
        print("lockfiles are already fresh; nothing to regenerate")
        return 0

    for rendered in commands:
        print(f"running: {rendered}")
        result = subprocess.run(rendered.split(), cwd=root, check=False)
        # Deliberately not returned. Exit status is reported for the log only; freshness is
        # decided below by recomputing the digests.
        print(f"  (exit status {result.returncode}; freshness is decided by the re-check)")

    remaining = check(root)
    if remaining:
        print("", file=sys.stderr)
        print("regeneration did not resolve the drift:", file=sys.stderr)
        for error in remaining:
            print(f"  {error}", file=sys.stderr)
        return 1
    print("lockfiles regenerated; review and commit the result")
    return 0


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Detect a stale lockfile.")
    parser.add_argument(
        "--fix",
        action="store_true",
        help="developer-run: execute the sanctioned regeneration commands, then re-check",
    )
    parser.add_argument("--root", type=Path, default=Path(__file__).resolve().parents[2])
    args = parser.parse_args(argv)
    root = args.root.resolve()

    if args.fix:
        return _fix(root)

    errors = check(root)
    if errors:
        print(f"{len(errors)} lockfile freshness violation(s):", file=sys.stderr)
        for error in errors:
            print(f"  {error}", file=sys.stderr)
        print("", file=sys.stderr)
        print(
            "CI never regenerates a lockfile: --lockfile_mode=error is fail-closed on purpose, "
            "and the committed lock is the reviewed dependency authority. Regenerate it "
            "yourself, or run: python3 tools/analysis/check_lockfile_freshness.py --fix",
            file=sys.stderr,
        )
        return 1
    print("lockfile freshness check passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
