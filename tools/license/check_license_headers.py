#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#
"""Proprietary license headers for the monorepo.

The infrastructure repositories share a bash implementation of this
(`scripts/license-header-check.sh`). It is not reusable here: it assumes every source file
takes `#` comments, which is true of Terraform and YAML and false of Go, Rust, TypeScript,
and protobuf. This is the same policy expressed for a tree with two comment syntaxes.

Placement rules that matter, and why:

  shebang           stays on line 1. It is only meaningful as the literal first byte.
  //go:build        a Go build constraint may be preceded only by blank lines and line
                    comments, so a `//` header above it is legal — but the constraint must
                    keep a blank line after it or the toolchain reads it as package doc.
  package doc       a comment block touching `package X` with no blank line between becomes
                    that package's doc comment. Every header written here is followed by a
                    blank line so the header never silently becomes documentation.
  module docstring  a Python docstring must be the first *statement*, not the first line.
                    Comments above it do not displace it.

Usage:
    tools/license/check_license_headers.py --check [paths...]
    tools/license/check_license_headers.py --fix   [paths...]

With no paths, walks the repository from its root.
"""

from __future__ import annotations

import argparse
import re
import subprocess
import sys
from collections.abc import Iterable, Sequence
from pathlib import Path

# Most of this tree already carries a copyright line, in a shorter form than the estate's
# canonical block — "Copyright 2026 Mindclade. All rights reserved.", sometimes followed by
# "Confidential, proprietary, and trade-secret information.". Those files need the old block
# REPLACED, not a second one stacked above it. This pattern is what identifies the block to
# remove; it is deliberately narrow, so a comment that merely mentions a licence in passing is
# not mistaken for a header.
LICENSE_LINE = re.compile(
    r"copyright|all rights reserved|confidential|proprietary|trade-secret"
    r"|spdx-license-identifier|licensed under",
    re.IGNORECASE,
)

REPO_ROOT = Path(__file__).resolve().parents[2]
HEADER_FILE = REPO_ROOT / ".github" / "MINDCLADE_PROPRIETARY_SOURCE_HEADER.txt"

# Extension to comment token. The canonical header on disk is written with `#`; the `//`
# variant is derived from it so the two can never drift apart in wording.
HASH_SUFFIXES = frozenset(
    {
        ".py",
        ".pyi",
        ".yaml",
        ".yml",
        ".toml",
        ".sh",
        ".bash",
        ".nix",
        ".bzl",
        # Terraform and Terragrunt. Every infrastructure repository in the estate enforces the
        # header on these; this tool omitted them, so 96 of the monorepo's 100 .tf files carried
        # no header at all while the same files in bootstrap or github-config would fail CI.
        ".tf",
        ".hcl",
    }
)
SLASH_SUFFIXES = frozenset({".go", ".rs", ".ts", ".tsx", ".proto"})

# Bazel files carry no suffix that identifies them.
HASH_NAMES = frozenset({"BUILD.bazel", "MODULE.bazel", "WORKSPACE", "BUILD"})

# Generated, vendored, or machine-owned. A header in any of these is either overwritten on the
# next regeneration or attaches Mindclade's copyright to something that is not ours.
SKIP_DIR_PARTS = frozenset(
    {
        # Installed agent skills are independently licensed tooling. Their package-level
        # LICENSE controls; stamping individual files would falsely claim Mindclade ownership.
        ".agents",
        ".git",
        "node_modules",
        "target",
        "__pycache__",
        ".venv",
        "bazel-out",
        ".ruff_cache",
        ".pytest_cache",
        ".mypy_cache",
        # Generated clients and schemas are recreated from their owning source contract. The
        # generator, source, and distributed artifact carry the applicable notice instead.
        "generated",
        "vendor",
    }
)
# Generated lockfiles. `.terraform.lock.hcl` matches .hcl above but is written by
# `terraform init` and rewritten wholesale on every provider change, so a header in it is
# removed the next time anyone runs init. The infra repos' bash checker excludes it for the
# same reason.
SKIP_NAMES = frozenset(
    {
        "MODULE.bazel.lock",
        ".terraform.lock.hcl",
        "argocd.lock.yaml",
        "pnpm-lock.yaml",
        # Next.js rewrites this reference file and explicitly marks it as machine-owned.
        "next-env.d.ts",
    }
)


def header_lines(token: str) -> list[str]:
    """The three legal header lines rendered with the given comment token."""
    raw = HEADER_FILE.read_text(encoding="utf-8").splitlines()[:3]
    if len(raw) != 3:
        raise SystemExit(f"{HEADER_FILE} does not contain the expected 3-line header block")
    # Strip the canonical '#' and re-render. Preserves wording, changes only the token.
    return [f"{token} {line.lstrip('#').strip()}".rstrip() for line in raw]


def comment_token(path: Path) -> str | None:
    if path.name in SKIP_NAMES or path.name.endswith((".lock.hcl", ".lock.yaml")):
        return None
    if path.name in HASH_NAMES:
        return "#"
    if path.suffix in HASH_SUFFIXES:
        return "#"
    if path.suffix in SLASH_SUFFIXES:
        return "//"
    return None


def is_skipped(path: Path) -> bool:
    return any(part in SKIP_DIR_PARTS for part in path.parts)


def tracked_files() -> Iterable[Path]:
    """Walk git's index rather than the filesystem, so ignored files never appear."""
    out = subprocess.run(
        ["git", "-C", str(REPO_ROOT), "ls-files", "-z"],
        capture_output=True,
        text=True,
        check=True,
    ).stdout
    for rel in out.split("\0"):
        if rel:
            yield REPO_ROOT / rel


def prologue_length(lines: Sequence[str], token: str) -> int:
    """Number of leading lines that must stay above the header.

    A shebang and a YAML document start both qualify: each is meaningful only as the literal
    first line of a file, so neither can be pushed down to make room for the header.

    A Go build constraint does not qualify. `//go:build` is legal below a line-comment block,
    so keeping it under the header means one placement rule instead of two.
    """
    if lines and (lines[0].startswith("#!") or lines[0].strip() == "---"):
        return 1
    return 0


def has_header(lines: Sequence[str], token: str, expected: Sequence[str]) -> bool:
    start = prologue_length(lines, token)
    return list(lines[start : start + len(expected)]) == list(expected)


def existing_block_length(lines: Sequence[str], token: str, start: int) -> int:
    """Length of the licence comment block already at `start`, or 0 if there is none.

    Consumes the run of leading comment lines that read as a licence header, including the
    bare `#` / `//` line the canonical block ends on. Stops at the first comment line that is
    ordinary prose, so a file whose header is immediately followed by a real explanatory
    comment keeps that comment.
    """
    index = start
    saw_license = False
    while index < len(lines):
        line = lines[index]
        if not line.startswith(token):
            break
        content = line[len(token) :].strip()
        if LICENSE_LINE.search(content):
            saw_license = True
            index += 1
            continue
        # The canonical block closes on a bare comment token. Only absorb one if a licence
        # line came first — otherwise this is the blank separator above unrelated prose.
        if content == "" and saw_license:
            index += 1
            continue
        break
    return index - start if saw_license else 0


def add_header(path: Path, token: str, expected: Sequence[str]) -> None:
    lines = path.read_text(encoding="utf-8").splitlines()
    start = prologue_length(lines, token)

    body = lines[start + existing_block_length(lines, token, start) :]
    # Drop blank lines the removed block left behind, so repeated runs cannot accumulate them.
    while body and not body[0].strip():
        body.pop(0)
    # A blank line between the header and whatever follows. Without it a `//` header directly
    # above `package foo` becomes that package's doc comment.
    if body:
        body = ["", *body]

    rebuilt = [*lines[:start], *expected, *body]
    path.write_text("\n".join(rebuilt) + "\n", encoding="utf-8")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument("--check", action="store_true", help="report files missing a header")
    mode.add_argument("--fix", action="store_true", help="insert missing headers in place")
    parser.add_argument("paths", nargs="*", type=Path)
    args = parser.parse_args()

    if not HEADER_FILE.exists():
        raise SystemExit(f"missing {HEADER_FILE}")

    rendered = {token: header_lines(token) for token in ("#", "//")}

    candidates = [p.resolve() for p in args.paths] if args.paths else list(tracked_files())

    offenders: list[Path] = []
    fixed = 0
    for path in candidates:
        if is_skipped(path) or not path.is_file():
            continue
        token = comment_token(path)
        if token is None:
            continue
        try:
            lines = path.read_text(encoding="utf-8").splitlines()
        except UnicodeDecodeError:
            continue
        expected = rendered[token]
        if has_header(lines, token, expected):
            continue
        if args.fix:
            add_header(path, token, expected)
            fixed += 1
        else:
            offenders.append(path)

    if args.fix:
        print(f"Added proprietary headers to {fixed} file(s).")
        return 0

    for path in offenders[:50]:
        print(f"Missing proprietary header: {path.relative_to(REPO_ROOT)}")
    if len(offenders) > 50:
        print(f"... and {len(offenders) - 50} more")
    if offenders:
        print(f"\n{len(offenders)} file(s) missing the proprietary header.")
        print("Run: tools/license/check_license_headers.py --fix")
        return 1
    print("All source files carry the proprietary header.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
