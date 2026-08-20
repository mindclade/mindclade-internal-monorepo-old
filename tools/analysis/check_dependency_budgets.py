#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import argparse
import json
import re
import tomllib
from pathlib import Path

# WHAT THIS CHECK IS FOR, since the name no longer quite fits.
#
# It used to enforce two things: a COUNT of direct internal dependencies per component
# (`max_internal_direct`), and a set of prefix rules saying which components a component may
# reach. The count is gone — see ADR-0024, which supersedes ADR-0023.
#
# The short version of why: a count measures how much of a component exists, not whether its
# design holds. Every breach in this repository's history was resolved by raising the number,
# because every breach was a component legitimately growing. `services/control_plane` went
# 40 -> 44 -> 62 -> 68 in a single branch as provider factories materialized, and each step was
# correct. A rule that is right to relax every time it fires is not a rule; it is a speed bump
# that teaches people to edit the config.
#
# The prefix rules are the invariant worth having, and they are the opposite: a Go service
# importing another Go service, or a Rust lib reaching past its allowlist, is a layering
# violation regardless of how many imports either has. Those never want relaxing, and when one
# fires the right response is to change the code.

# Must track go.mod's module directive, and architecture/dependency_budgets.toml's
# allowed_prefixes/forbidden_prefixes with it. The cutover to go.mindclade.dev updated the toml
# and left this pattern behind, so `internal_deps` returned an empty set for every Go component:
# max_internal_direct compared 0 against the budget, the allowlist had nothing to reject, and the
# forbidden prefixes had nothing to match. The check passed because it was looking at nothing.
GO_IMPORT = re.compile(r'"(go\.mindclade\.dev/[^\"]+)"')

# Comments are stripped before the pattern above is applied. A quoted module path in prose is
# not a dependency: services/go_vanity documents its most-specific-first matching with
# `"go.mindclade.dev/tools"` and `"go.mindclade.dev/toolsmith"`, neither of which is a package,
# and counting them pushed `services` two over its budget. Safe because nothing but the package
# clause and comments may precede a Go import block, so no real import can be swallowed.
GO_COMMENT = re.compile(r"/\*.*?\*/|//[^\n]*", re.S)


def internal_deps(path: Path, language: str) -> set[str]:
    deps = set()
    if language == "go":
        for p in path.rglob("*.go"):
            if p.name.endswith("_test.go"):
                continue
            deps.update(GO_IMPORT.findall(GO_COMMENT.sub("", p.read_text(errors="replace"))))
    elif language == "rust":
        c = path / "Cargo.toml"
        if c.exists():
            data = tomllib.loads(c.read_text(errors="replace"))
            for name, spec in (data.get("dependencies") or {}).items():
                if name.startswith("mindclade_") and isinstance(spec, dict) and "path" in spec:
                    deps.add(name)
    elif language == "typescript":
        # package.json, not the import sites. A TS file's `import` can name a path alias, a
        # relative path, or the package — three spellings of one edge — whereas the manifest
        # names every workspace package this one is allowed to resolve at all. It is also what
        # pnpm enforces: a package not declared here does not resolve at runtime, so the
        # manifest is the boundary rather than a description of one.
        manifest = path / "package.json"
        if manifest.exists():
            data = json.loads(manifest.read_text(errors="replace"))
            for section in ("dependencies", "devDependencies", "peerDependencies"):
                for name in (data.get(section) or {}):
                    if name.startswith("@mindclade/"):
                        deps.add(name)
    return deps


def check(root: Path):
    cfg = tomllib.loads((root / "architecture/dependency_budgets.toml").read_text())
    errors = []
    for b in cfg.get("budget", []):
        path = root / b["path"]
        deps = internal_deps(path, b["language"])

        # A budget that declares neither list constrains nothing, and a silent no-op is worse
        # than an absent entry: it reads in review as though the component is governed. Now
        # that the counts are gone, an entry with no prefixes is always a mistake.
        if not b.get("allowed_prefixes") and not b.get("forbidden_prefixes"):
            errors.append(
                f"{b['path']}: budget declares neither allowed_prefixes nor forbidden_prefixes, "
                f"so it enforces nothing — give it a rule or delete the entry"
            )

        if b.get("max_internal_direct") is not None:
            errors.append(
                f"{b['path']}: max_internal_direct is no longer supported (ADR-0024 supersedes "
                f"ADR-0023) — remove the key; counts measured how much of a component existed, "
                f"not whether its layering held"
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
