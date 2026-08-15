#!/usr/bin/env python3
from __future__ import annotations

import argparse
import tomllib
from pathlib import Path


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
