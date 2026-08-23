#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Check first-party Rust Cargo workspace and Bazel package alignment.

Cargo and Bazel keep separate pictures of the Rust workspace and neither reads the other's.
`Cargo.toml`'s `[workspace].members` is what `cargo` builds; `MODULE.bazel`'s
`crate.from_cargo(manifests = [...])` is the manifest inventory Crate Universe is handed. This
module gates the two against each other.

It previously asserted only that the literal string `crate.from_cargo(` appeared somewhere in
`MODULE.bazel`, and never looked inside the call -- a gate with no failing input. It printed PASS
for as long as `libs/rust/process_os` was absent from the manifests list (24 of the 25 `libs/rust`
crates were tracked), because a substring search cannot see a missing element of the list it is
not reading.

What the manifests list is, precisely: `rules_rust` derives the resolve from the workspace root
manifest, so for a single-workspace repository like this one the member entries are *redundant*
for resolution -- it says so itself on every evaluation ("Only the workspace's Cargo.toml is
required in the `manifests` attribute ... the rest can be removed"), and `MODULE.bazel.lock`
records every member manifest as an extension input whether or not it is listed, because
`cargo metadata` reaches them through the root. So an omission here does not break a build today.
It is inventory drift: a second, hand-maintained copy of the workspace roster that this repository
has chosen to declare, sitting in the file a reader consults to learn what the Rust workspace
contains. This gate keeps that copy honest; it does not claim to prevent a build failure.

Both directions are checked, because the tree has been bitten by both:

* a member missing from `manifests` is the live drift described above;
* a `manifests` entry with no member is the stale-entry class -- the seven crates retired in the
  2026-08 epoch outlived their deletion in four separate manifests, and a roster that still
  advertises a deleted crate is exactly the document an author copies from.

The list is parsed, not matched line by line. Starlark comments and string literals can both
contain brackets, so the scanners below track quote and comment state, and anything unparseable
raises `AlignmentError` and is reported as a failure rather than skipped. A checker that quietly
declines to parse its input is the same fail-open defect wearing a different hat.
"""

from __future__ import annotations

import re
import tomllib
from pathlib import Path

# A plain sibling import, with no sys.path insertion: this module only ever runs as a script from
# tools/analysis (where the interpreter puts that directory on sys.path itself) or as an import
# from run_architecture_checks.py, which inserts the directory before importing anything.
import check_code_docs_alignment

ROOT = Path(__file__).resolve().parents[2]

LIBS_RUST = "libs/rust"

# The Cargo workspace root manifest. Crate Universe discovers the member list through it, so its
# absence is a different failure from a member being dropped and is reported separately.
ROOT_MANIFEST = "//:Cargo.toml"

# Retired in the 2026-08 runtime consolidation epoch: removed, not deprecated. Imported rather
# than restated so the roster lives in exactly one module.
REMOVED_CRATES = check_code_docs_alignment.REMOVED_COMPAT

# Each libs/rust crate exposes this filegroup for the aggregate in libs/rust/BUILD.bazel.
PACKAGE_SOURCES = "package_sources"
PRODUCTION_SOURCES = "production_sources"

_CLOSERS = {"(": ")", "[": "]", "{": "}"}


class AlignmentError(Exception):
    """A declaration could not be parsed, so its invariants cannot be evaluated."""


def _skip_string(text: str, index: int, where: str) -> int:
    """Index just past the Starlark string literal beginning at `index`."""
    marker = text[index : index + 3]
    if marker in ('"""', "'''"):
        end = text.find(marker, index + 3)
        if end == -1:
            raise AlignmentError(f"{where}: unterminated triple-quoted string")
        return end + 3
    quote = text[index]
    i = index + 1
    while i < len(text):
        if text[i] == "\\":
            i += 2
            continue
        if text[i] == quote:
            return i + 1
        if text[i] == "\n":
            break
        i += 1
    raise AlignmentError(f"{where}: unterminated string literal")


def _mask(text: str, where: str) -> str:
    """`text` with comment and string *contents* blanked, preserving every index.

    Searching the raw file for `manifests = [` would also match the phrase inside a comment or a
    docstring, and bracket counting from that offset would then read the wrong region. Masking
    keeps offsets usable against the original text while making quoted and commented-out code
    structurally invisible.

    Quote delimiters survive; only what they enclose is blanked. That is what lets a scalar
    attribute be located as `name = "` in the mask and then read out of the original text.
    """
    out = list(text)
    i = 0
    while i < len(text):
        char = text[i]
        if char == "#":
            end = text.find("\n", i)
            end = len(text) if end == -1 else end
            out[i:end] = " " * (end - i)
        elif char in ("'", '"'):
            end = _skip_string(text, i, where)
            width = 3 if text[i : i + 3] in ('"""', "'''") else 1
            out[i + width : end - width] = " " * (end - i - 2 * width)
        else:
            i += 1
            continue
        i = end
    return "".join(out)


def _close_index(masked: str, open_index: int, where: str) -> int:
    """Index of the bracket closing the one at `open_index` in already-masked Starlark."""
    stack = [_CLOSERS[masked[open_index]]]
    for i in range(open_index + 1, len(masked)):
        char = masked[i]
        if char in _CLOSERS:
            stack.append(_CLOSERS[char])
        elif char in (")", "]", "}"):
            if char != stack[-1]:
                raise AlignmentError(f"{where}: mismatched {char!r} in bracket nesting")
            stack.pop()
            if not stack:
                return i
    raise AlignmentError(f"{where}: unbalanced {masked[open_index]!r}")


def _string_items(text: str, where: str) -> list[str]:
    """Every string literal in `text`, ignoring `#` comments."""
    items: list[str] = []
    i = 0
    while i < len(text):
        char = text[i]
        if char == "#":
            end = text.find("\n", i)
            i = len(text) if end == -1 else end
        elif char in ("'", '"'):
            end = _skip_string(text, i, where)
            items.append(text[i:end].strip("\"'"))
            i = end
        else:
            i += 1
    return items


def _call_spans(masked: str, name: str, where: str) -> list[tuple[int, int]]:
    """Open/close paren indices for every `name(...)` call in masked Starlark."""
    spans = []
    for match in re.finditer(rf"(?<![\w.]){re.escape(name)}\s*\(", masked):
        opened = match.end() - 1
        spans.append((opened, _close_index(masked, opened, where)))
    return spans


def _attr_items(
    masked: str, raw: str, span: tuple[int, int], attr: str, where: str
) -> list[str] | None:
    """String literals of a list-valued keyword argument, or None when the argument is absent."""
    lo, hi = span
    matches = list(re.finditer(rf"(?<![\w.]){attr}\s*=\s*\[", masked[lo:hi]))
    if not matches:
        return None
    if len(matches) > 1:
        raise AlignmentError(f"{where}: {attr} is assigned more than once in one call")
    opened = lo + matches[0].end() - 1
    return _string_items(raw[opened + 1 : _close_index(masked, opened, where)], where)


def _attr_string(masked: str, raw: str, span: tuple[int, int], attr: str, where: str) -> str | None:
    """Value of a string-valued keyword argument, or None when the argument is absent."""
    lo, hi = span
    match = re.search(rf"(?<![\w.]){attr}\s*=\s*[\"']", masked[lo:hi])
    if not match:
        return None
    opened = lo + match.end() - 1
    return raw[opened : _skip_string(raw, opened, where)].strip("\"'")


def _read(path: Path, where: str) -> str:
    try:
        return path.read_text()
    except OSError as exc:
        raise AlignmentError(f"{where}: unreadable ({exc})") from exc


def _duplicate_errors(label: str, items: list[str]) -> list[str]:
    seen: set[str] = set()
    errors = []
    for item in items:
        if item in seen:
            errors.append(f"{label}: lists {item!r} more than once")
        seen.add(item)
    return errors


def _manifest_labels(root: Path) -> list[str]:
    """The `manifests` list of the repository's single `crate.from_cargo` call."""
    where = "MODULE.bazel"
    raw = _read(root / where, where)
    masked = _mask(raw, where)
    spans = _call_spans(masked, "crate.from_cargo", where)
    if not spans:
        raise AlignmentError(
            "MODULE.bazel: Crate Universe must derive third-party Rust deps from the Cargo "
            "workspace via crate.from_cargo(...)"
        )
    if len(spans) > 1:
        # Comparing one manifests list against one member list is the entire contract here. A
        # second crate repository would need its own declared member subset, which does not exist,
        # so guessing which call owns which members would be the fail-open move.
        raise AlignmentError(
            f"MODULE.bazel: found {len(spans)} crate.from_cargo calls; this gate compares a "
            "single manifests list against the Cargo workspace members"
        )
    labels = _attr_items(masked, raw, spans[0], "manifests", where)
    if labels is None:
        raise AlignmentError(
            "MODULE.bazel: crate.from_cargo declares no manifests list, so no workspace member "
            "manifest is tracked"
        )
    return labels


def _stale_label_error(root: Path, label: str) -> str:
    """Explain a `manifests` entry that no Cargo workspace member backs."""
    prefix = "MODULE.bazel manifests"
    match = re.fullmatch(r"//([^:]*):(.+)", label)
    if not match:
        return f"{prefix}: {label!r} is not a `//package:target` manifest label"
    package, target = match.group(1), match.group(2)
    if target != "Cargo.toml":
        return f"{prefix}: {label!r} must point at a Cargo.toml, not {target!r}"
    crate = package.rsplit("/", 1)[-1]
    if package.startswith(f"{LIBS_RUST}/") and crate in REMOVED_CRATES:
        return (
            f"{prefix}: tracks retired crate {crate!r}; it was removed in the 2026-08 epoch, not "
            "deprecated, and must not appear in the manifest inventory"
        )
    if not (root / package / "Cargo.toml").exists():
        return f"{prefix}: tracks {label!r}, but {package}/Cargo.toml does not exist"
    return (
        f"{prefix}: tracks {label!r}, which is not a [workspace].members entry in Cargo.toml; "
        "add the member or drop the manifest entry"
    )


def _check_module_manifests(root: Path, members: list[str]) -> list[str]:
    """`crate.from_cargo(manifests = ...)` must name every Cargo workspace member, and only those."""
    labels = _manifest_labels(root)
    errors = _duplicate_errors("MODULE.bazel manifests", labels)
    expected = {f"//{member}:Cargo.toml": member for member in members}
    declared = set(labels)

    if ROOT_MANIFEST not in declared:
        errors.append(
            f"MODULE.bazel manifests: omits the workspace root manifest {ROOT_MANIFEST!r}; Crate "
            "Universe discovers the member list through it"
        )
    for label, member in sorted(expected.items()):
        if label not in declared:
            errors.append(
                f"MODULE.bazel manifests: omits {label!r} for Cargo workspace member {member!r}; "
                "add it to crate.from_cargo(manifests = [...]) so the Bazel manifest inventory "
                "matches [workspace].members"
            )
    for label in sorted(declared - set(expected) - {ROOT_MANIFEST}):
        errors.append(_stale_label_error(root, label))
    return errors


def _production_sources_span(masked: str, raw: str, where: str) -> tuple[int, int]:
    for span in _call_spans(masked, "filegroup", where):
        if _attr_string(masked, raw, span, "name", where) == PRODUCTION_SOURCES:
            return span
    raise AlignmentError(f"{where}: no filegroup named {PRODUCTION_SOURCES!r}")


def _check_production_sources(root: Path) -> list[str]:
    """`libs/rust:production_sources` must aggregate every crate in the directory.

    A hand-maintained list of a directory's contents drifts from the directory; this one had
    already lost `ipc_os` and `process_os`. It cannot be derived in Starlark -- Bazel globs never
    cross a package boundary, and every crate is its own package -- so each crate's
    `package_sources` filegroup has to be named explicitly and the completeness of that naming
    has to be gated here instead.
    """
    where = f"{LIBS_RUST}/BUILD.bazel"
    raw = _read(root / where, where)
    masked = _mask(raw, where)
    span = _production_sources_span(masked, raw, where)
    srcs = _attr_items(masked, raw, span, "srcs", where)
    if srcs is None:
        raise AlignmentError(f"{where}: filegroup {PRODUCTION_SOURCES!r} declares no srcs list")

    label = f"{where} {PRODUCTION_SOURCES}"
    errors = _duplicate_errors(label, srcs)
    declared = set(srcs)
    crates = sorted(path.parent.name for path in (root / LIBS_RUST).glob("*/Cargo.toml"))
    if not crates:
        raise AlignmentError(f"{LIBS_RUST}: contains no crate manifests")
    expected = {f"//{LIBS_RUST}/{crate}:{PACKAGE_SOURCES}": crate for crate in crates}

    for src, crate in sorted(expected.items()):
        if src not in declared:
            errors.append(f"{label}: omits {src!r} for the crate at {LIBS_RUST}/{crate}")
            continue
        build = f"{LIBS_RUST}/{crate}/BUILD.bazel"
        if not re.search(rf'name\s*=\s*"{PACKAGE_SOURCES}"', _read(root / build, build)):
            errors.append(f"{label}: {src!r} has no {PACKAGE_SOURCES} target in {build}")
    for src in sorted(declared - set(expected)):
        errors.append(f"{label}: lists {src!r}, which is not a crate in {LIBS_RUST}")
    return errors


def _check_member_targets(root: Path, members: list[str]) -> list[str]:
    """Every member's BUILD.bazel must declare the library target and dependency edges Cargo does."""
    errors = []
    for member in members:
        directory = root / member
        cargo = directory / "Cargo.toml"
        build = directory / "BUILD.bazel"
        if not cargo.exists():
            errors.append(f"{member}: workspace member missing Cargo.toml")
            continue
        if not build.exists():
            errors.append(f"{member}: missing BUILD.bazel")
            continue
        data = tomllib.loads(cargo.read_text())
        text = build.read_text()
        lib = data.get("lib") or (
            {"name": data["package"]["name"].replace("-", "_")}
            if (directory / "src/lib.rs").exists()
            else None
        )
        if lib:
            target = lib.get("name", data["package"]["name"].replace("-", "_"))
            if not re.search(rf'name\s*=\s*"{re.escape(target)}"', text):
                errors.append(f"{member}: Bazel missing library target {target}")
        if 'srcs = glob(["src/**/*.rs"])' not in text:
            errors.append(f"{member}: Bazel must include complete Rust source glob")
        for dep, val in data.get("dependencies", {}).items():
            if isinstance(val, dict) and val.get("path"):
                depdir = (directory / val["path"]).resolve()
                dd = tomllib.loads((depdir / "Cargo.toml").read_text())
                target = (dd.get("lib") or {"name": dd["package"]["name"].replace("-", "_")}).get(
                    "name", dd["package"]["name"].replace("-", "_")
                )
                label = f"//{depdir.relative_to(root).as_posix()}:{target}"
            else:
                package = val.get("package", dep) if isinstance(val, dict) else dep
                label = f"@crates//:{package}"
            if label not in text:
                errors.append(f"{member}: Bazel missing Cargo dependency {label}")
    return errors


def check(root: Path) -> list[str]:
    try:
        workspace = tomllib.loads(_read(root / "Cargo.toml", "Cargo.toml")).get("workspace", {})
        members = workspace.get("members", [])
        if not members:
            raise AlignmentError("Cargo.toml: [workspace].members is empty or absent")
        return (
            _check_member_targets(root, members)
            + _check_module_manifests(root, members)
            + _check_production_sources(root)
        )
    except (AlignmentError, tomllib.TOMLDecodeError) as exc:
        # Returned rather than raised: run_architecture_checks calls every checker without a
        # try/except, so an escaping exception would abort the whole suite and hide the checks
        # queued behind this one.
        return [str(exc)]


def main() -> int:
    errors = check(ROOT)
    for error in errors:
        print(error)
    print(
        f"Cargo/Bazel Rust alignment failed: {len(errors)}"
        if errors
        else "Cargo/Bazel Rust alignment passed"
    )
    return 1 if errors else 0


if __name__ == "__main__":
    raise SystemExit(main())
