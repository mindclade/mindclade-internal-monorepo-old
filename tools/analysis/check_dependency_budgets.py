#!/usr/bin/env python3
from __future__ import annotations

import argparse
import re
import tomllib
from pathlib import Path

GO_IMPORT = re.compile(r'"(mindclade\.internal/[^\"]+)"')


def internal_deps(path: Path, language: str) -> set[str]:
    deps = set()
    if language == "go":
        for p in path.rglob("*.go"):
            if p.name.endswith("_test.go"):
                continue
            deps.update(GO_IMPORT.findall(p.read_text(errors="replace")))
    elif language == "rust":
        c = path / "Cargo.toml"
        if c.exists():
            data = tomllib.loads(c.read_text(errors="replace"))
            for name, spec in (data.get("dependencies") or {}).items():
                if name.startswith("mindclade_") and isinstance(spec, dict) and "path" in spec:
                    deps.add(name)
    return deps


def check(root: Path):
    cfg = tomllib.loads((root / "architecture/dependency_budgets.toml").read_text())
    errors = []
    for b in cfg.get("budget", []):
        path = root / b["path"]
        deps = internal_deps(path, b["language"])
        maxd = int(b.get("max_internal_direct", 10**9))
        if len(deps) > maxd:
            errors.append(
                f"{b['path']}: {len(deps)} direct internal deps exceeds budget {maxd}: {sorted(deps)}"
            )
        allowed = b.get("allowed_prefixes")
        forbidden = b.get("forbidden_prefixes", [])
        if allowed:
            for d in deps:
                if not any(d.startswith(x) for x in allowed):
                    errors.append(f"{b['path']}: dependency outside allowlist: {d}")
        for d in deps:
            if any(d.startswith(x) for x in forbidden):
                errors.append(f"{b['path']}: forbidden dependency: {d}")
    return errors


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--repo", type=Path, default=Path(__file__).resolve().parents[2])
    a = ap.parse_args()
    e = check(a.repo.resolve())
    [print(x) for x in e]
    print(
        "dependency budget check passed" if not e else f"dependency budget check failed: {len(e)}"
    )
    return 1 if e else 0


if __name__ == "__main__":
    raise SystemExit(main())
