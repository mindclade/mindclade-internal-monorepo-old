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

# Roots whose Go packages must appear in components.toml.
#
# The prior defect this closes: every rule in maturity.toml is a rule ABOUT a declared
# component, so an undeclared package was neither pass nor fail — it was invisible, and
# "production may not depend on planned/scaffolded/experimental" silently did not apply to
# code the record had never heard of. `control/evidence` held 890 lines of signed
# production-eligibility policy under exactly that hole, and `control/lineage` another 455.
# Declaration was a convention someone had to remember; below it is an invariant.
#
# `services/` is deliberately NOT governed yet, and that is a gap rather than an exemption.
# services/control_plane, services/studio, and services/go_vanity carry ~17.5k lines of
# undeclared production Go across 59 packages, and declaring them means deciding owner,
# criticality, and tier for each — owner work, not a sweep. Adding "services" to this tuple
# is the check that proves it was done. What is not acceptable is listing those 59 paths as
# exempt inside a check that claims to cover them.
_GO_DECLARATION_GOVERNED_ROOTS = ("control", "libs")

# A scaffold placeholder as this tree writes them: a licence header, a package clause, and one
# `const scaffold_<file> = "<path>"`. Reserved space is not a component, so a directory holding
# only these is correctly absent from components.toml.
_SCAFFOLD_CONST = re.compile(r'^const\s+scaffold_[A-Za-z0-9_]+\s*=\s*"[^"]*"$')


def _is_scaffold_source(path: Path) -> bool:
    """True only when every meaningful line is a package clause or a scaffold constant.

    Deliberately conservative in the fail-closed direction: anything this does not recognise
    counts as real production Go and therefore has to be declared. A recogniser that guessed
    the other way would hand back the hole it exists to close.
    """
    for line in path.read_text(encoding="utf-8", errors="replace").splitlines():
        stripped = line.strip()
        if not stripped or stripped.startswith("//"):
            continue
        if stripped.startswith("package ") or _SCAFFOLD_CONST.match(stripped):
            continue
        return False
    return True


def _undeclared_go_packages(root: Path, declared: list[str]) -> list[str]:
    """Directories of real production Go that no components.toml entry covers.

    Coverage is by path prefix, because components are declared at the granularity their owner
    chose: `libs/go` is one entry standing for its whole subtree, while `control/registry` is
    declared per leaf.
    """
    errors = []
    for top in _GO_DECLARATION_GOVERNED_ROOTS:
        base = root / top
        if not base.is_dir():
            continue
        for directory in sorted({source.parent for source in base.rglob("*.go")}):
            relative = directory.relative_to(root).as_posix()
            if any(relative == d or relative.startswith(d + "/") for d in declared):
                continue
            sources = sorted(
                path
                for path in directory.glob("*.go")
                if not path.name.endswith("_test.go") and not _is_scaffold_source(path)
            )
            if not sources:
                continue
            errors.append(
                f"{relative}: {len(sources)} production Go file(s) and no components.toml "
                f"entry ({', '.join(path.name for path in sources)}); an undeclared package "
                "carries no status, so nothing gates depending on it"
            )
    return errors


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
    components = data.get("component", [])
    errors = _undeclared_go_packages(root, sorted({c["path"] for c in components if c.get("path")}))
    for c in components:
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
