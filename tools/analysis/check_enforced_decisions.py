#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import tomllib
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


def check(root: Path) -> list[str]:
    data = tomllib.loads((root / "architecture/enforced_decisions.toml").read_text())
    errors = []
    ids = set()
    for item in data.get("decision", []):
        for field in ("id", "document", "checker"):
            if not item.get(field):
                errors.append(f"decision missing {field}: {item}")
        if item.get("id") in ids:
            errors.append(f"duplicate enforced decision: {item.get('id')}")
        ids.add(item.get("id"))
        for field in ("document", "checker"):
            value = item.get(field)
            if value and not (root / value).exists():
                errors.append(f"{item.get('id')}: missing {value}")
    return errors


def main() -> int:
    errors = check(ROOT)
    [print(e) for e in errors]
    if errors:
        return 1
    data = tomllib.loads((ROOT / "architecture/enforced_decisions.toml").read_text())
    print(f"enforced decision registry passed ({len(data.get('decision', []))} decisions)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
