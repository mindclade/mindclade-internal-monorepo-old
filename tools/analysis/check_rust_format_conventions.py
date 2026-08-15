#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Fail on obviously unformatted Rust while rustfmt remains the authoritative gate."""

from __future__ import annotations

import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
RUST_ROOTS = (
    ROOT / "libs/rust",
    ROOT / "services",
    ROOT / "serving/runtime",
    ROOT / "protocols/rust",
)
MAX_LINE_BYTES = 160
COMPACT = re.compile(
    r"(?:;\s*(?:pub\s+)?(?:mod|use|fn|struct|enum|impl)\b|}\s*(?:pub\s+)?(?:fn|struct|enum|impl)\b)"
)


def check() -> list[str]:
    errors: list[str] = []
    for base in RUST_ROOTS:
        if not base.exists():
            continue
        for path in sorted(base.rglob("*.rs")):
            rel = path.relative_to(ROOT)
            for line_number, line in enumerate(path.read_text().splitlines(), 1):
                if len(line.encode()) > MAX_LINE_BYTES:
                    errors.append(f"{rel}:{line_number}: line exceeds {MAX_LINE_BYTES} bytes")
                if COMPACT.search(line):
                    errors.append(f"{rel}:{line_number}: compressed Rust declaration/statement")
    return errors


def main() -> int:
    errors = check()
    for error in errors:
        print(error)
    if errors:
        print(f"Rust source-format convention check failed: {len(errors)}")
        return 1
    print("Rust source-format convention check passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
