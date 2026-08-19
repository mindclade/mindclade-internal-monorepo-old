#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import argparse
import re
import tomllib
from pathlib import Path

# Rule calls that reserve a package rather than build anything in it. `filegroup` is here
# because the scaffold BUILD files in this tree are a single `filegroup(name =
# "scaffold_files")`, which is exactly the "materialized but not implemented" state ADR-0018
# separates from an implemented capability.
_NON_BUILDING_RULES = frozenset(
    {
        "exports_files",
        "filegroup",
        "licenses",
        "load",
        "package",
        "package_group",
    }
)
_RULE_CALL = re.compile(r"^([a-z_][a-z0-9_]*)\(", re.MULTILINE)


def _has_build_target(path: Path) -> bool:
    """True when the component's subtree declares at least one building Bazel rule.

    The whole subtree, not just the component root: `libs/go` is one component whose targets
    all live in subpackages, and a root-only check would call it unimplemented.
    """
    if not path.is_dir():
        return False
    for build in path.rglob("BUILD.bazel"):
        text = build.read_text(encoding="utf-8", errors="replace")
        # Comments can contain anything, including examples of rule calls.
        text = re.sub(r"#.*", "", text)
        if any(rule not in _NON_BUILDING_RULES for rule in _RULE_CALL.findall(text)):
            return True
    return False


def check(root: Path) -> list[str]:
    data = tomllib.loads((root / "components.toml").read_text())
    policy = tomllib.loads((root / "maturity.toml").read_text())
    allowed = set(policy["statuses"])
    errors = []
    for c in data.get("component", []):
        name, path, status = c.get("name", ""), root / c.get("path", ""), c.get("status", "")
        if status not in allowed:
            errors.append(f"{name}: unknown status {status}")
            continue
        if not path.exists():
            errors.append(f"{name}: path missing: {path.relative_to(root)}")
        if not c.get("owner"):
            errors.append(f"{name}: owner missing")
        rules = policy.get("rules", {}).get(status, {})
        if rules.get("requires_tests"):
            tests = c.get("tests", [])
            if not tests:
                errors.append(f"{name}: {status} component requires tests")
            for t in tests:
                if not (root / t).exists():
                    errors.append(f"{name}: declared test path missing: {t}")
        if rules.get("requires_build_target") and not _has_build_target(path):
            errors.append(
                f"{name}: {status} component has no Bazel build target under "
                f"{c.get('path', '')} (only scaffold filegroups); ADR-0018 requires one"
            )
        if rules.get("requires_qualification") and not c.get("qualification"):
            errors.append(f"{name}: qualification evidence path missing")
        if c.get("qualification") and not (root / c["qualification"]).exists():
            errors.append(f"{name}: qualification file does not exist: {c['qualification']}")
        for field in ("slo", "runbook", "release_target"):
            if rules.get("requires_" + field) and not c.get(field):
                errors.append(f"{name}: production component requires {field}")
    return errors


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--repo", type=Path, default=Path(__file__).resolve().parents[2])
    a = ap.parse_args()
    e = check(a.repo.resolve())
    [print(x) for x in e]
    print(
        "component maturity check passed" if not e else f"component maturity check failed: {len(e)}"
    )
    return 1 if e else 0


if __name__ == "__main__":
    raise SystemExit(main())
