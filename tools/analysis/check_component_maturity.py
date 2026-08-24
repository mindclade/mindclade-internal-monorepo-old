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

# `ci/release/targets.yaml` is the closed release-target catalog: a release request selects one
# name from it and cannot inject a label, registry, or identity. `release_target` therefore has
# to name a catalog entry. Before this was enforced the field was a presence check on an
# arbitrary string, so `release_target = "yes"` satisfied ADR-0018's release clause — the one
# gate that separates `qualified` from `production` was the one nothing verified.
_RELEASE_CATALOG = "ci/release/targets.yaml"
_CATALOG_NAME = re.compile(r"^  ([A-Za-z0-9][A-Za-z0-9._-]*):\s*$")
_TOP_LEVEL_KEY = re.compile(r"^([A-Za-z0-9][A-Za-z0-9._-]*):\s*$")
_BAZEL_LABEL = re.compile(r"//([A-Za-z0-9_/.-]*)(?::[A-Za-z0-9_.-]+)?")

# Evidence fields whose value is a repository-relative path to a document. `qualification` has
# always been existence-checked; `slo` and `runbook` were not, which meant the two production
# gates that point at operational documents accepted a string that named no document at all.
_DOCUMENT_FIELDS = ("qualification", "slo", "runbook")

# Fields that both components.toml and architecture/component_ownership.toml can carry. They
# are two records of the same fact, and check_component_ownership.py resolves the ownership
# copy while the maturity rules resolve this one. Divergence would let a component be promoted
# against an SLO the ownership registry has never heard of.
_MIRRORED_FIELDS = ("slo", "runbook")

_PRODUCTION_RULES = (
    "requires_tests",
    "requires_qualification",
    "requires_slo",
    "requires_runbook",
    "requires_release_target",
    "requires_build_target",
)

# Roots whose Go packages must appear in components.toml.
#
# The prior defect this closes: every rule in maturity.toml is a rule ABOUT a declared
# component, so an undeclared package was neither pass nor fail — it was invisible, and
# "production may not depend on planned/scaffolded/experimental" silently did not apply to
# code the record had never heard of. `control/evidence` held 890 lines of signed
# production-eligibility policy under exactly that hole, and `control/lineage` another 455.
# Declaration was a convention someone had to remember; below it is an invariant.
#
# Entries are path prefixes, not top-level directories, so a root can be narrower than a
# directory when only part of it has an owner's decision behind it. That is what the two
# `services/` entries are: `services/studio` and `services/go_vanity` are declared and reach
# zero undeclared packages, so they are governed and can never silently regress.
#
# `services/control_plane` is NOT here, and the distinction matters. This is an allowlist of
# what is governed, not a denylist of paths exempted from a check that claims to cover them —
# no path anywhere is skipped by name inside a governed root. `services` was previously split
# into `services/go_vanity` and `services/studio` because `services/control_plane` had no SLO
# page and check_component_ownership.py requires one for any tier-0/tier-1 component at
# `implemented` or above. `docs/slo/control-plane.md` now exists and the deployable is declared,
# so the two entries collapse to plain `services` exactly as that note said they would.
_GO_DECLARATION_GOVERNED_ROOTS = ("control", "libs", "services")

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


def _release_catalog(root: Path) -> tuple[set[str], set[str]]:
    """Return (target names, Bazel package paths) declared by the release catalog.

    Deliberately line-based rather than a YAML parse. The architecture checkers are stdlib-only
    single files — `//tools/analysis:architecture_policy` globs them with no third-party dep —
    and the catalog is a closed, flat, two-space-indented mapping. An unreadable or reshaped
    catalog yields empty sets, which `check` turns into a failure rather than a pass.
    """
    try:
        text = (root / _RELEASE_CATALOG).read_text(encoding="utf-8")
    except OSError:
        return set(), set()
    names: set[str] = set()
    packages: set[str] = set()
    inside = False
    for raw in text.splitlines():
        line = raw.split("#", 1)[0].rstrip() if not raw.lstrip().startswith("#") else ""
        if not line.strip() or line.strip() == "---":
            continue
        if _TOP_LEVEL_KEY.match(line):
            inside = line.strip() == "targets:"
            continue
        if inside:
            found = _CATALOG_NAME.match(line)
            if found:
                names.add(found.group(1))
        for label in _BAZEL_LABEL.finditer(line):
            package = label.group(1).removesuffix("/...").strip("/")
            if package:
                packages.add(package)
    return names, packages


def _overlaps(left: str, right: str) -> bool:
    """True when one repository-relative package path contains the other, or they are equal."""
    return left == right or left.startswith(right + "/") or right.startswith(left + "/")


def _ownership(root: Path) -> dict[str, dict]:
    try:
        raw = (root / "architecture/component_ownership.toml").read_text(encoding="utf-8")
    except OSError:
        return {}
    return tomllib.loads(raw).get("component", {})


def check(root: Path) -> list[str]:
    data = tomllib.loads((root / "components.toml").read_text())
    policy = tomllib.loads((root / "maturity.toml").read_text())
    allowed = set(policy["statuses"])
    ownership = _ownership(root)
    components = data.get("component", [])
    catalog_names, _ = _release_catalog(root)
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
        for field in _DOCUMENT_FIELDS:
            value = c.get(field)
            if value and not (root / value).exists():
                errors.append(f"{name}: {field} file does not exist: {value}")
        for field in ("slo", "runbook", "release_target"):
            if rules.get("requires_" + field) and not c.get(field):
                errors.append(f"{name}: production component requires {field}")
        target = c.get("release_target")
        if target:
            if not catalog_names:
                errors.append(
                    f"{name}: release target {target!r} cannot be resolved; "
                    f"{_RELEASE_CATALOG} is missing or declares no targets"
                )
            elif target not in catalog_names:
                errors.append(
                    f"{name}: release target {target!r} is not in the closed catalog "
                    f"{_RELEASE_CATALOG}"
                )
        record = ownership.get(name, {})
        for field in _MIRRORED_FIELDS:
            declared = c.get(field)
            if declared and record.get(field) != declared:
                errors.append(
                    f"{name}: {field} disagrees with architecture/component_ownership.toml "
                    f"({declared!r} vs {record.get(field)!r})"
                )
    return errors


def matrix(root: Path) -> list[dict]:
    """Per-component coverage of every rule the `production` status enumerates.

    Reporting, not enforcement. `check` answers "does this component satisfy the status it
    claims"; this answers "what is between this component and the next status", which is the
    question that otherwise gets re-derived by hand every time someone asks how close the tree
    is to production. Evidence that exists but is not wired into components.toml is reported
    as `registry` rather than `met`, because an unreferenced document does not satisfy a gate.
    """
    data = tomllib.loads((root / "components.toml").read_text())
    policy = tomllib.loads((root / "maturity.toml").read_text())
    ownership = _ownership(root)
    catalog_names, catalog_packages = _release_catalog(root)
    rows = []
    for c in data.get("component", []):
        name = c.get("name", "")
        relative = c.get("path", "")
        record = ownership.get(name, {})
        tests = c.get("tests", [])
        gates: dict[str, str] = {}
        gates["tests"] = "met" if tests and all((root / t).exists() for t in tests) else "absent"
        gates["build_target"] = "met" if _has_build_target(root / relative) else "absent"
        qualification = c.get("qualification")
        gates["qualification"] = (
            "met" if qualification and (root / qualification).exists() else "absent"
        )
        for field in _MIRRORED_FIELDS:
            declared, registered = c.get(field), record.get(field)
            if declared and (root / declared).exists():
                gates[field] = "met"
            elif registered and (root / registered).exists():
                gates[field] = "registry"
            else:
                gates[field] = "absent"
        target = c.get("release_target")
        if target and target in catalog_names:
            gates["release_target"] = "met"
        elif relative and any(
            _overlaps(package, relative.rstrip("/")) for package in catalog_packages
        ):
            # Overlap in either direction. `//protocols:protobuf_contract_image` releases a
            # package that contains ten declared protobuf components, and
            # `//models/reference/weights_fixture` sits inside `models/reference`; both are
            # candidate release coverage that no component has wired up yet.
            gates["release_target"] = "registry"
        else:
            gates["release_target"] = "absent"
        status = c.get("status", "")
        blocking = sorted(
            rule.removeprefix("requires_")
            for rule in _PRODUCTION_RULES
            if gates.get(rule.removeprefix("requires_")) != "met"
        )
        rows.append(
            {
                "name": name,
                "path": relative,
                "status": status,
                "owner": c.get("owner", ""),
                "criticality": record.get("criticality", ""),
                "gates": gates,
                "production_blockers": blocking,
                # Only statuses that actually enumerate evidence. `scaffolded` and
                # `experimental` require nothing, so every component would "satisfy" them and
                # the field would say nothing about readiness.
                "satisfies": sorted(
                    s
                    for s, r in policy.get("rules", {}).items()
                    if any(k.startswith("requires_") and v for k, v in r.items())
                    and all(
                        gates.get(rule.removeprefix("requires_")) == "met"
                        for rule, required in r.items()
                        if rule.startswith("requires_") and required
                    )
                ),
            }
        )
    return rows


_SYMBOL = {"met": "Y", "registry": "~", "absent": "."}


def _render(rows: list[dict]) -> str:
    order = [rule.removeprefix("requires_") for rule in _PRODUCTION_RULES]
    head = ["tests", "qual", "slo", "runbook", "release", "build"]
    lines = [
        "Y = gate met   ~ = evidence exists but components.toml does not reference it   . = none",
        "",
        f"{'component':<44}{'status':<14}{'tier':<9}" + "".join(f"{h:<9}" for h in head),
    ]
    for row in sorted(rows, key=lambda r: r["name"]):
        cells = "".join(f"{_SYMBOL[row['gates'][g]]:<9}" for g in order)
        lines.append(f"{row['name']:<44}{row['status']:<14}{row['criticality'] or '-':<9}{cells}")
    lines.append("")
    lines.append(f"components: {len(rows)}")
    for gate in order:
        met = sum(1 for r in rows if r["gates"][gate] == "met")
        registry = sum(1 for r in rows if r["gates"][gate] == "registry")
        lines.append(f"  {gate:<16} met {met:>3}/{len(rows)}   unreferenced evidence {registry:>3}")
    for status in ("implemented", "qualified", "production"):
        eligible = sum(1 for r in rows if status in r["satisfies"])
        lines.append(f"  satisfies {status:<12} {eligible:>3}/{len(rows)}")
    return "\n".join(lines)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--repo", type=Path, default=Path(__file__).resolve().parents[2])
    ap.add_argument(
        "--matrix",
        action="store_true",
        help="report per-component coverage of the production rules instead of checking",
    )
    ap.add_argument("--json", action="store_true", help="emit the matrix as JSON")
    a = ap.parse_args()
    root = a.repo.resolve()
    if a.matrix:
        rows = matrix(root)
        print(json.dumps(rows, indent=2, sort_keys=True) if a.json else _render(rows))
        return 0
    e = check(root)
    [print(x) for x in e]
    print(
        "component maturity check passed" if not e else f"component maturity check failed: {len(e)}"
    )
    return 1 if e else 0


if __name__ == "__main__":
    raise SystemExit(main())
