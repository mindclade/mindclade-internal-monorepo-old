#!/usr/bin/env python3
"""Reject incomplete source markers in promoted foundations.

Scaffold-only domains may reserve future boundaries, but libs/go and the shared
control-plane bootstrap/foundation must contain executable implementations.
"""

from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path

MARKER = re.compile(
    r"(?:\bTODO\b|\bFIXME\b|IMPLEMENT ME|const\s+scaffold_|SCAFFOLD_PATH)", re.IGNORECASE
)


def check(root: Path) -> list[str]:
    roots = [
        root / "libs/go",
        root / "services/control_plane/internal/bootstrap",
        root / "services/control_plane/internal/foundation",
        root / "services/control_plane/internal/config",
        root / "services/control_plane/internal/transport",
    ]
    violations: list[str] = []
    for base in roots:
        if not base.exists():
            violations.append(f"{base.relative_to(root)}: required source root is missing")
            continue
        for path in base.rglob("*"):
            if not path.is_file() or "__pycache__" in path.parts:
                continue
            relative = path.relative_to(root)
            if path.stat().st_size == 0:
                violations.append(f"{relative}: zero-byte file")
                continue
            if path.suffix not in {".go", ".py", ".md", ".bazel"} and path.name != "BUILD.bazel":
                continue
            text = path.read_text(encoding="utf-8", errors="replace")
            for number, line in enumerate(text.splitlines(), 1):
                # Stable fault text such as "operation not implemented" is not
                # a source-completeness marker.
                if "operation not implemented" in line:
                    continue
                if MARKER.search(line):
                    violations.append(
                        f"{relative}:{number}: incomplete source marker: {line.strip()}"
                    )
    return sorted(violations)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--repo", type=Path, default=Path(__file__).resolve().parents[2])
    args = parser.parse_args(argv)
    root = args.repo.resolve()
    violations = check(root)
    for violation in violations:
        print(violation, file=sys.stderr)
    if violations:
        print(
            f"promoted-foundation completeness check failed with {len(violations)} violation(s)",
            file=sys.stderr,
        )
        return 1
    print("promoted-foundation completeness check passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
