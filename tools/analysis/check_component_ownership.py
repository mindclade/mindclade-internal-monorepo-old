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
    ownership = tomllib.loads((root / "architecture/component_ownership.toml").read_text()).get(
        "component", {}
    )
    components = tomllib.loads((root / "components.toml").read_text()).get("component", [])
    errors = []
    for component in components:
        name = component["name"]
        entry = ownership.get(name)
        if entry is None:
            errors.append(f"{name}: missing component ownership metadata")
            continue
        for key in ("owner", "criticality", "language"):
            if not entry.get(key):
                errors.append(f"{name}: missing {key}")
        if entry.get("owner") != component.get("owner"):
            errors.append(
                f"{name}: owner differs between components.toml and component_ownership.toml"
            )
        if not isinstance(entry.get("security_review"), bool):
            errors.append(f"{name}: security_review must be explicit boolean")
        if entry.get("criticality") not in {"tier-0", "tier-1", "tier-2"}:
            errors.append(f"{name}: invalid criticality")
        if entry.get("criticality") in {"tier-0", "tier-1"} and component.get("status") in {
            "implemented",
            "qualified",
            "production",
        }:
            for key in ("slo", "runbook"):
                value = entry.get(key)
                if not value:
                    errors.append(
                        f"{name}: {entry.get('criticality')} implemented component missing {key}"
                    )
                elif not (root / value).exists():
                    errors.append(f"{name}: missing {value}")
    extra = set(ownership) - {c["name"] for c in components}
    for name in sorted(extra):
        errors.append(f"{name}: ownership metadata has no declared component")
    return errors


def main() -> int:
    errors = check(ROOT)
    [print(e) for e in errors]
    if errors:
        return 1
    print("component ownership metadata passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
