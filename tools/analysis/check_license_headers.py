# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Every source file in the monorepo opens with the proprietary header.

The other five repositories in the estate have enforced this since they were created
(`scripts/license-header-check.sh` plus a `license-headers` workflow). The monorepo — the
repository with by far the most source in it — enforced nothing, so the header was a
convention that held only for as long as everyone remembered it.

Written in Python rather than ported from the bash checker in the sibling repos because this
tree is polyglot: `//` for Go, Rust and TypeScript, `#` for Python, Starlark, YAML and TOML.
The bash version knows only `#` files.

    python3 tools/analysis/check_license_headers.py            # check, exit 1 on a violation
    python3 tools/analysis/check_license_headers.py --fix       # insert missing headers
    python3 tools/analysis/check_license_headers.py path/to/f   # check just these paths

Stdlib only, like every other checker under this directory — `ci/presubmit/pipeline.py` runs
them before anything has been installed.
"""

from __future__ import annotations

import argparse
import sys
from collections.abc import Iterable
from pathlib import Path

HEADER = (
    "Copyright © 2026 Mindclade, LLC. All Rights Reserved.",
    "Mindclade Proprietary and Confidential.",
    "SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary",
    "",
)

# Extension to comment prefix. A file type absent here is not checked — adding one is a
# deliberate act, because a type nobody thought about is better skipped than half-enforced.
HASH = "#"
SLASH = "//"
COMMENT_PREFIX: dict[str, str] = {
    ".bzl": HASH,
    ".go": SLASH,
    ".hcl": HASH,
    ".js": SLASH,
    ".jsx": SLASH,
    ".mjs": SLASH,
    ".nix": HASH,
    ".proto": SLASH,
    ".py": HASH,
    ".rs": SLASH,
    ".sh": HASH,
    ".tf": HASH,
    ".toml": HASH,
    ".ts": SLASH,
    ".tsx": SLASH,
    ".yaml": HASH,
    ".yml": HASH,
}

# Files with no extension that are still ours.
BY_NAME: dict[str, str] = {
    "BUILD.bazel": HASH,
    "MODULE.bazel": HASH,
    "WORKSPACE": HASH,
    "CODEOWNERS": HASH,
}

# Directories never descended into. Vendored, generated, or not source.
SKIP_DIRS = frozenset(
    {
        ".claude",
        ".git",
        ".generated",
        ".mypy_cache",
        ".pytest_cache",
        ".ruff_cache",
        ".venv",
        "node_modules",
        "target",
    }
)

# Paths that are generated or vendored and therefore exempt, matched as path prefixes
# relative to the repository root. Each one is a file nobody in this repository authored.
SKIP_PREFIXES: tuple[str, ...] = (
    "docs/blueprint/",
    "infra/gitops/vendor/",
    "research/notebooks/",
)

# Lock files and machine-written manifests. Rewriting them inserts a header the generator
# removes on its next run, and the diff then reads as a change nobody made.
SKIP_SUFFIXES: tuple[str, ...] = (
    ".lock",
    ".lock.hcl",
    ".lock.yaml",
    "MODULE.bazel.lock",
    "Cargo.lock",
    "pnpm-lock.yaml",
    "uv.lock",
)


def comment_prefix(path: Path) -> str | None:
    if path.name in BY_NAME:
        return BY_NAME[path.name]
    return COMMENT_PREFIX.get(path.suffix)


def is_skipped(rel: str) -> bool:
    return rel.startswith(SKIP_PREFIXES) or rel.endswith(SKIP_SUFFIXES)


def expected_block(prefix: str) -> list[str]:
    """The header as it appears in a file of this comment style.

    The fourth line is a bare comment marker with no trailing space — `#` and `//` — which is
    what every existing file in the estate carries.
    """
    return [f"{prefix} {line}".rstrip() for line in HEADER]


def leading_lines(text: str) -> tuple[int, list[str]]:
    """Return (index of the first header line, the file's lines).

    A shebang and a YAML document start (`---`) are each valid only as the literal first line
    of a file, so either may legitimately precede the header. This is the same allowance the
    sibling repositories' bash checker makes, and for the same reason: `.golangci.yml` and
    every `.pre-commit-config.yaml` open with `---` and carry a correct header underneath.
    """
    lines = text.split("\n")
    if lines and (lines[0].startswith("#!") or lines[0].rstrip() == "---"):
        return 1, lines
    return 0, lines


def has_header(text: str, prefix: str) -> bool:
    start, lines = leading_lines(text)
    want = expected_block(prefix)
    got = [line.rstrip("\r") for line in lines[start : start + len(want)]]
    return got == want


def insert_header(text: str, prefix: str) -> str:
    start, lines = leading_lines(text)
    block = expected_block(prefix)
    # A blank line after the block, unless one is already there — every file in the estate
    # separates the header from what follows.
    tail = lines[start:]
    if tail and tail[0].strip() != "":
        block = [*block, ""]
    return "\n".join([*lines[:start], *block, *tail])


def iter_sources(root: Path, explicit: Iterable[Path]) -> list[Path]:
    if explicit:
        return [p for p in explicit if p.is_file()]

    found: list[Path] = []
    stack = [root]
    while stack:
        current = stack.pop()
        for entry in current.iterdir():
            if entry.is_symlink():
                continue
            if entry.is_dir():
                if entry.name not in SKIP_DIRS:
                    stack.append(entry)
                continue
            if comment_prefix(entry) is None:
                continue
            if is_skipped(str(entry.relative_to(root))):
                continue
            found.append(entry)
    return sorted(found)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--fix", action="store_true", help="insert the header where missing")
    parser.add_argument("paths", nargs="*", type=Path, help="check only these files")
    args = parser.parse_args(argv)

    root = Path(__file__).resolve().parents[2]
    violations: list[Path] = []
    fixed: list[Path] = []

    for path in iter_sources(root, args.paths):
        prefix = comment_prefix(path)
        if prefix is None:
            continue
        if is_skipped(str(path.resolve().relative_to(root))):
            continue

        try:
            text = path.read_text(encoding="utf-8")
        except UnicodeDecodeError:
            # Binary content behind a source extension. Report rather than skip silently.
            violations.append(path)
            continue

        if has_header(text, prefix):
            continue

        if args.fix:
            path.write_text(insert_header(text, prefix), encoding="utf-8")
            fixed.append(path)
        else:
            violations.append(path)

    if fixed:
        print(f"added the proprietary header to {len(fixed)} file(s):")
        for path in fixed:
            print(f"  {path.relative_to(root)}")

    if violations:
        print(
            f"{len(violations)} file(s) are missing the proprietary licence header:",
            file=sys.stderr,
        )
        for path in violations:
            print(f"  {path.relative_to(root)}", file=sys.stderr)
        print("", file=sys.stderr)
        print(
            "Run: python3 tools/analysis/check_license_headers.py --fix",
            file=sys.stderr,
        )
        return 1

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
