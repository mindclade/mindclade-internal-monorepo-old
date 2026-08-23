#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Verify the `libs/rust` package manifests describe the crates that actually exist.

`libs/rust` publishes its inventory seven times over: the Cargo workspace member list, the
directories on disk, `PACKAGE_MANIFEST.json`, `layers.json`, `stability.json`, and the
human-readable `PACKAGE_CATALOG.md`, `LAYERS.md`, and `README.md`. Nothing reconciled them, so
every copy drifted independently: the 2026-08 epoch crates that were *removed* (not deprecated)
survived in all four machine-readable manifests, and two crates added afterwards (`ipc_os`,
`process_os`) never appeared in any of them.

That drift is not cosmetic. `check_rust_workspace.py` gates new compatibility edges and
`check_code_docs_alignment.py` gates retired crate names, but a manifest that still advertises
`clock` as a layer-5 compatibility crate is exactly the document an author copies an import
from. Reconciling the declarations against Cargo catches it one step earlier.

Everything mechanically derivable — crate name, dependency edges, source lists — is *generated*
rather than transcribed: `--write` rewrites `PACKAGE_MANIFEST.json` and `PACKAGE_CATALOG.md`
from the tree, the same shape as `check_license_headers.py --fix`. Only the two judgement calls,
layer and stability tier, stay hand-declared, and this module gates those against every place
they are repeated.
"""

from __future__ import annotations

import argparse
import json
import re
import tomllib
from pathlib import Path

# A plain sibling import, with no sys.path insertion: this module only ever runs as a script from
# tools/analysis (where the interpreter puts that directory on sys.path itself) or as an import
# from run_architecture_checks.py, which inserts the directory before importing anything.
import check_code_docs_alignment

LIBS_RUST = "libs/rust"

# Retired in the 2026-08 runtime consolidation epoch: removed, not deprecated. The bare names are
# owned by check_code_docs_alignment, which gates the `mindclade_`-prefixed spelling in source.
# This module gates the bare spelling, which is what an inventory key looks like and therefore
# what a name-prefix scan cannot see. Importing rather than restating keeps one list.
REMOVED_CRATES = check_code_docs_alignment.REMOVED_COMPAT

# `check_rust_workspace.py` already forbids the directory; the baseline file outlived it.
FORBIDDEN_CRATE_NAMES = frozenset({"mindclade_common"})

STABILITY_TIERS = ("stable", "evolving", "compatibility")
_README_ROSTER_HEADING = "## Production-facing crates"

_LAYER_BULLET = re.compile(r"^-\s*Layer\s+(\d+)\s*:(.*)$")
_BACKTICKED = re.compile(r"`([A-Za-z0-9_]+)`")
_CATALOG_ROW = re.compile(r"^\|\s*`([A-Za-z0-9_]+)`\s*\|([^|]*)\|([^|]*)\|(.*)\|\s*$")
_CATALOG_COUNT = re.compile(r"^(Production|Compatibility) crates:\s*\*\*(\d+)\*\*\s*$")


class ManifestError(Exception):
    """A declaration could not be read at all, so its invariants cannot be evaluated."""


def _read_text(path: Path) -> str:
    try:
        return path.read_text()
    except OSError as exc:
        raise ManifestError(f"{path.name}: unreadable ({exc})") from exc


def _read_json(path: Path) -> dict:
    try:
        return json.loads(_read_text(path))
    except json.JSONDecodeError as exc:
        raise ManifestError(f"{path.name}: invalid JSON ({exc})") from exc


def _read_toml(path: Path) -> dict:
    try:
        return tomllib.loads(_read_text(path))
    except tomllib.TOMLDecodeError as exc:
        raise ManifestError(f"{path.name}: invalid TOML ({exc})") from exc


def _production_path_deps(manifest: dict) -> list[str]:
    """Every internal edge that exists in a production build.

    `[dependencies]` and `[build-dependencies]`, including their `[target.'cfg(...)']` forms.
    Dev-dependencies are excluded on purpose: they link test binaries only and cannot create a
    production layering edge. Anything else is a real edge and must be visible to the layer
    direction gate — `ipc` already ships `src/unix.rs`/`src/windows.rs`, so a cfg-gated
    dependency is the expected next edit in this tree.
    """
    tables = [manifest]
    tables += list((manifest.get("target") or {}).values())
    names = set()
    for table in tables:
        if not isinstance(table, dict):
            continue
        for section in ("dependencies", "build-dependencies"):
            for spec in (table.get(section) or {}).values():
                if isinstance(spec, dict) and "path" in spec:
                    names.add(Path(spec["path"]).name)
    return sorted(names)


def _crate_facts(root: Path) -> dict[str, dict]:
    """Ground truth: what each crate directory actually declares and contains."""
    facts: dict[str, dict] = {}
    for cargo in sorted((root / LIBS_RUST).glob("*/Cargo.toml")):
        data = _read_toml(cargo)
        directory = cargo.parent
        facts[directory.name] = {
            "crate": data.get("package", {}).get("name", ""),
            "dependencies": _production_path_deps(data),
            "production_sources": sorted(
                p.relative_to(directory).as_posix() for p in (directory / "src").rglob("*.rs")
            ),
            "test_sources": sorted(
                p.relative_to(directory).as_posix() for p in (directory / "tests").rglob("*.rs")
            ),
        }
    return facts


def _inventory_errors(label: str, declared: set[str], actual: set[str]) -> list[str]:
    errors = []
    for name in sorted(declared - actual):
        if name in REMOVED_CRATES:
            errors.append(
                f"{label}: declares retired compatibility crate {name!r}; it was removed, "
                "not deprecated, and must not appear in the package inventory"
            )
        else:
            errors.append(f"{label}: declares crate {name!r} with no libs/rust/{name}/Cargo.toml")
    for name in sorted(actual - declared):
        errors.append(f"{label}: omits crate {name!r} that exists at libs/rust/{name}")
    return errors


def _check_workspace_members(workspace: dict, actual: set[str]) -> list[str]:
    members = workspace.get("members", [])
    declared = {m.split("/")[-1] for m in members if m.startswith(f"{LIBS_RUST}/")}
    return _inventory_errors("Cargo.toml workspace members", declared, actual)


def _check_layers(root: Path, actual: set[str]) -> tuple[dict[str, int], list[str]]:
    data = _read_json(root / LIBS_RUST / "layers.json")
    raw = data.get("layers") or {}
    errors = _inventory_errors("layers.json", set(raw), actual)
    layers: dict[str, int] = {}
    for name, value in raw.items():
        # `isinstance(True, int)` is True, so booleans are rejected explicitly. Dropping a
        # malformed value silently would disable every downstream layer comparison for that
        # crate — including the upward-dependency gate — while still printing PASS.
        if isinstance(value, bool) or not isinstance(value, int):
            errors.append(f"layers.json[{name}]: layer must be an integer, found {value!r}")
        else:
            layers[name] = value
    return layers, errors


def _check_stability(root: Path, actual: set[str]) -> tuple[dict[str, str], list[str]]:
    data = _read_json(root / LIBS_RUST / "stability.json")
    stability: dict[str, str] = {}
    errors: list[str] = []
    for tier in STABILITY_TIERS:
        for name in data.get(tier) or []:
            if name in stability:
                errors.append(
                    f"stability.json: {name!r} is in both {stability[name]!r} and {tier!r}"
                )
            stability[name] = tier
    errors += _inventory_errors("stability.json", set(stability), actual)
    return stability, errors


def _check_compatibility_tier(root: Path, stability: dict[str, str]) -> list[str]:
    """`layers.json.compatibility_only` must name crates that are really in that tier.

    Derived rather than pinned to "the tier is empty": LAYERS.md keeps the tier as a named
    concept for the next compatibility epoch, and declaring one legitimate facade should not
    require editing this gate.
    """
    data = _read_json(root / LIBS_RUST / "layers.json")
    errors = []
    for name in data.get("compatibility_only") or []:
        if stability.get(name) != "compatibility":
            errors.append(
                f"layers.json: compatibility_only lists {name!r}, which stability.json "
                f"buckets as {stability.get(name)!r}"
            )
    return errors


def _expected_manifest(
    workspace: dict, facts: dict[str, dict], layers: dict[str, int], stability: dict[str, str]
) -> dict:
    defaults = workspace.get("package", {})
    packages = [
        {
            "directory": name,
            "crate": truth["crate"],
            "layer": layers.get(name),
            "stability": stability.get(name),
            "production": stability.get(name) != "compatibility",
            "dependencies": truth["dependencies"],
            "production_sources": truth["production_sources"],
            "test_sources": truth["test_sources"],
        }
        for name, truth in sorted(facts.items())
    ]
    return {
        "schema_version": 2,
        "rust_version": defaults.get("rust-version"),
        "edition": defaults.get("edition"),
        "package_count": len(packages),
        "production_package_count": sum(1 for p in packages if p["production"]),
        "packages": packages,
    }


def _check_package_manifest(root: Path, expected: dict) -> list[str]:
    data = _read_json(root / LIBS_RUST / "PACKAGE_MANIFEST.json")
    packages = data.get("packages") or []
    by_directory = {p.get("directory", ""): p for p in packages}
    expected_by_directory = {p["directory"]: p for p in expected["packages"]}
    errors = _inventory_errors(
        "PACKAGE_MANIFEST.json", set(by_directory), set(expected_by_directory)
    )

    for key in ("rust_version", "edition", "package_count", "production_package_count"):
        if data.get(key) != expected[key]:
            errors.append(
                f"PACKAGE_MANIFEST.json: {key} is {data.get(key)!r}; derived value is "
                f"{expected[key]!r}"
            )

    for directory, want in sorted(expected_by_directory.items()):
        got = by_directory.get(directory)
        if got is None:
            continue
        for field in sorted(want):
            if field == "directory":
                continue
            if got.get(field) != want[field]:
                errors.append(
                    f"PACKAGE_MANIFEST.json[{directory}]: {field} is {got.get(field)!r}; "
                    f"derived value is {want[field]!r}"
                )
    if errors:
        errors.append(
            "PACKAGE_MANIFEST.json is derived; run "
            "`python3 tools/analysis/check_rust_package_manifest.py --write` to regenerate it"
        )
    return errors


def _render_catalog(expected: dict) -> str:
    packages = expected["packages"]
    compatibility = sum(1 for p in packages if not p["production"])
    lines = [
        "# Shared Rust Package Catalog",
        "",
        "Generated from the Cargo manifests by",
        "`tools/analysis/check_rust_package_manifest.py --write`, which also gates it. Edit the",
        "crate `Cargo.toml` files, `layers.json`, or `stability.json` — not this table.",
        "",
        f"Production crates: **{expected['production_package_count']}**  ",
        f"Compatibility crates: **{compatibility}**",
        "",
        "| Package | Layer | Status | Direct internal dependencies |",
        "|---|---:|---|---|",
    ]
    for p in sorted(packages, key=lambda p: (p["layer"] or 0, p["directory"])):
        deps = ", ".join(f"`{d}`" for d in p["dependencies"]) or "—"
        lines.append(f"| `{p['directory']}` | {p['layer']} | {p['stability']} | {deps} |")
    lines += [
        "",
        "Layer 5 is the compatibility tier. It is empty: the 2026-08 epoch crates were removed,",
        "not deprecated. See `MIGRATION_2026_08.md` for the replacement mechanism of each one.",
    ]
    return "\n".join(lines) + "\n"


def _check_catalog(root: Path, expected: dict) -> list[str]:
    text = _read_text(root / LIBS_RUST / "PACKAGE_CATALOG.md")
    expected_by_directory = {p["directory"]: p for p in expected["packages"]}
    compatibility = sum(1 for p in expected["packages"] if not p["production"])
    counts = {
        "Production": expected["production_package_count"],
        "Compatibility": compatibility,
    }
    rows: dict[str, tuple[int | None, str, list[str]]] = {}
    errors: list[str] = []
    for line in text.splitlines():
        declared = _CATALOG_COUNT.match(line)
        if declared and int(declared.group(2)) != counts[declared.group(1)]:
            errors.append(
                f"PACKAGE_CATALOG.md: claims {declared.group(2)} {declared.group(1).lower()} "
                f"crates; {counts[declared.group(1)]} exist"
            )
        row = _CATALOG_ROW.match(line)
        if not row:
            continue
        layer_cell = row.group(2).strip()
        rows[row.group(1)] = (
            int(layer_cell) if layer_cell.isdigit() else None,
            row.group(3).strip(),
            sorted(_BACKTICKED.findall(row.group(4))),
        )
    errors += _inventory_errors("PACKAGE_CATALOG.md", set(rows), set(expected_by_directory))
    for name, (layer, tier, deps) in sorted(rows.items()):
        want = expected_by_directory.get(name)
        if want is None:
            continue
        if layer != want["layer"]:
            errors.append(
                f"PACKAGE_CATALOG.md[{name}]: layer column is {layer!r}; "
                f"layers.json says {want['layer']!r}"
            )
        if tier != want["stability"]:
            errors.append(
                f"PACKAGE_CATALOG.md[{name}]: status column is {tier!r}; "
                f"stability.json says {want['stability']!r}"
            )
        if deps != want["dependencies"]:
            errors.append(
                f"PACKAGE_CATALOG.md[{name}]: dependency column is {deps!r}; "
                f"Cargo says {want['dependencies']!r}"
            )
    if errors:
        errors.append(
            "PACKAGE_CATALOG.md is derived; run "
            "`python3 tools/analysis/check_rust_package_manifest.py --write` to regenerate it"
        )
    return errors


def _check_layers_doc(root: Path, actual: set[str], layers: dict[str, int]) -> list[str]:
    text = _read_text(root / LIBS_RUST / "LAYERS.md")
    declared: dict[str, int] = {}
    errors: list[str] = []
    for line in text.splitlines():
        bullet = _LAYER_BULLET.match(line.strip())
        if not bullet:
            continue
        layer = int(bullet.group(1))
        for name in _BACKTICKED.findall(bullet.group(2)):
            if name in declared:
                errors.append(
                    f"LAYERS.md: {name!r} is listed in both layer {declared[name]} and {layer}"
                )
            declared[name] = layer
    errors += _inventory_errors("LAYERS.md", set(declared), actual)
    for name, layer in sorted(declared.items()):
        if name in layers and layer != layers[name]:
            errors.append(
                f"LAYERS.md[{name}]: listed under layer {layer}; layers.json says {layers[name]}"
            )
    return errors


def _check_readme_roster(root: Path, actual: set[str]) -> list[str]:
    """The README's crate roster is an inventory too, and drifted the same way the others did."""
    lines = _read_text(root / LIBS_RUST / "README.md").splitlines()
    try:
        start = lines.index(_README_ROSTER_HEADING) + 1
    except ValueError:
        return [f"README.md: missing the {_README_ROSTER_HEADING!r} roster section"]
    while start < len(lines) and not lines[start].strip():
        start += 1
    end = start
    while end < len(lines) and lines[end].strip():
        end += 1
    declared = set(_BACKTICKED.findall(" ".join(lines[start:end])))
    if not declared:
        return [f"README.md: the {_README_ROSTER_HEADING!r} section names no crates"]
    return _inventory_errors("README.md", declared, actual)


def _check_layer_directions(facts: dict[str, dict], layers: dict[str, int]) -> list[str]:
    """`libs/rust` production dependencies may never point at a higher layer."""
    errors = []
    for name, truth in sorted(facts.items()):
        if name not in layers:
            continue
        for dep in truth["dependencies"]:
            if dep in layers and layers[dep] > layers[name]:
                errors.append(
                    f"libs/rust/{name} (layer {layers[name]}) depends upward on "
                    f"{dep} (layer {layers[dep]})"
                )
    return errors


def _check_public_api_baseline(root: Path, facts: dict[str, dict]) -> list[str]:
    """One direction only: the baseline is a partial `cargo public-api` snapshot.

    Requiring full coverage would force a symbol list to be invented offline for the crates a
    connected run has not captured yet, which is exactly the unverified claim the baseline
    exists to prevent. Every crate it *does* name must still exist.
    """
    data = _read_json(root / LIBS_RUST / "public_api_baseline.json")
    real = {truth["crate"] for truth in facts.values()}
    errors = []
    for crate in sorted(data.get("crates") or {}):
        if crate in FORBIDDEN_CRATE_NAMES:
            errors.append(
                f"public_api_baseline.json: {crate!r} is a forbidden catch-all crate name and "
                "has no package in the workspace"
            )
        elif crate not in real:
            errors.append(
                f"public_api_baseline.json: {crate!r} has no package in the Cargo workspace"
            )
    return errors


def _derive(root: Path) -> tuple[dict, list[str]]:
    """Ground truth plus the hand-declared layer/stability judgement calls."""
    facts = _crate_facts(root)
    if not facts:
        raise ManifestError("libs/rust contains no crate manifests")
    actual = set(facts)
    workspace = _read_toml(root / "Cargo.toml").get("workspace", {})
    errors = _check_workspace_members(workspace, actual)
    layers, layer_errors = _check_layers(root, actual)
    errors += layer_errors
    stability, stability_errors = _check_stability(root, actual)
    errors += stability_errors
    errors += _check_compatibility_tier(root, stability)
    errors += _check_layer_directions(facts, layers)
    errors += _check_layers_doc(root, actual, layers)
    errors += _check_readme_roster(root, actual)
    errors += _check_public_api_baseline(root, facts)
    return _expected_manifest(workspace, facts, layers, stability), errors


def check(root: Path) -> list[str]:
    try:
        expected, errors = _derive(root)
        return errors + _check_package_manifest(root, expected) + _check_catalog(root, expected)
    except ManifestError as exc:
        # Returned rather than raised: run_architecture_checks calls every checker without a
        # try/except, so an escaping exception would abort the whole suite and hide the twenty
        # checks queued behind this one.
        return [str(exc)]


def write(root: Path) -> list[str]:
    """Regenerate the derived declarations, leaving the hand-declared inputs alone."""
    try:
        expected, errors = _derive(root)
    except ManifestError as exc:
        return [str(exc)]
    libs = root / LIBS_RUST
    (libs / "PACKAGE_MANIFEST.json").write_text(json.dumps(expected, indent=2) + "\n")
    (libs / "PACKAGE_CATALOG.md").write_text(_render_catalog(expected))
    return errors


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--repo", type=Path, default=Path(__file__).resolve().parents[2])
    ap.add_argument(
        "--write",
        action="store_true",
        help="regenerate PACKAGE_MANIFEST.json and PACKAGE_CATALOG.md from the Cargo manifests",
    )
    args = ap.parse_args()
    root = args.repo.resolve()
    errors = write(root) if args.write else check(root)
    for error in errors:
        print(error)
    if args.write:
        print(
            "Rust package manifest regenerated"
            if not errors
            else f"Rust package manifest regenerated with {len(errors)} unresolved inputs"
        )
    else:
        print(
            "Rust package manifest check passed"
            if not errors
            else f"Rust package manifest check failed: {len(errors)}"
        )
    return 1 if errors else 0


if __name__ == "__main__":
    raise SystemExit(main())
