# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""The `libs/rust` package manifests must reproduce the Cargo manifests exactly.

Every declaration these tests exercise drifted in the real tree at once — the retired 2026-08
compatibility crates outlived their deletion in all four machine-readable manifests, and the two
crates added afterwards never reached any of them. Each case below pins one direction of that
drift so the reconciliation cannot silently rot back.
"""

from __future__ import annotations

import importlib.util
import json
import sys
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[3]


def load(name: str, path: Path):
    spec = importlib.util.spec_from_file_location(name, path)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"unable to load {path}")
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    spec.loader.exec_module(module)
    return module


manifest_check = load(
    "check_rust_package_manifest", ROOT / "tools/analysis/check_rust_package_manifest.py"
)


def _crate(root: Path, name: str, deps: list[str], extra: str = "") -> None:
    directory = root / "libs/rust" / name
    (directory / "src").mkdir(parents=True)
    (directory / "tests").mkdir(parents=True)
    (directory / "src/lib.rs").write_text("", encoding="utf-8")
    (directory / "tests/integration.rs").write_text("", encoding="utf-8")
    lines = ["[package]", f'name = "mindclade_{name}"', "", "[dependencies]"]
    lines += [f'mindclade_{d} = {{ path = "../{d}" }}' for d in deps]
    (directory / "Cargo.toml").write_text("\n".join(lines) + "\n" + extra, encoding="utf-8")


def _repo(tmp_path: Path, layers: dict[str, int], crates: dict[str, list[str]]) -> Path:
    root = tmp_path
    (root / "libs/rust").mkdir(parents=True)
    for name, deps in crates.items():
        _crate(root, name, deps)

    members = "".join(f'    "libs/rust/{name}",\n' for name in crates)
    (root / "Cargo.toml").write_text(
        "[workspace]\nmembers = [\n"
        + members
        + ']\n\n[workspace.package]\nedition = "2024"\nrust-version = "1.97.1"\n',
        encoding="utf-8",
    )

    libs = root / "libs/rust"
    (libs / "layers.json").write_text(
        json.dumps({"schema_version": 2, "layers": layers, "compatibility_only": []}),
        encoding="utf-8",
    )
    (libs / "stability.json").write_text(
        json.dumps(
            {
                "schema_version": 2,
                "stable": sorted(crates),
                "evolving": [],
                "compatibility": [],
            }
        ),
        encoding="utf-8",
    )
    roster = ", ".join(f"`{n}`" for n in sorted(crates))
    (libs / "README.md").write_text(
        f"# Foundation\n\n## Production-facing crates\n\n{roster}.\n", encoding="utf-8"
    )
    bullets = "\n".join(
        f"- Layer {layer}: " + ", ".join(f"`{n}`" for n in sorted(crates) if layers[n] == layer)
        for layer in sorted(set(layers.values()))
    )
    (libs / "LAYERS.md").write_text(f"# Layers\n\n{bullets}\n", encoding="utf-8")
    (libs / "public_api_baseline.json").write_text(json.dumps({"crates": {}}), encoding="utf-8")

    # The two derived declarations are written by the checker itself, which is the contract:
    # they are generated, not transcribed. `write` also returns the hand-declared inputs'
    # errors, which some cases below deliberately provoke, so they are not asserted here.
    manifest_check.write(root)
    return root


def _clean(tmp_path: Path) -> Path:
    return _repo(
        tmp_path,
        layers={"faults": 0, "record_io": 1},
        crates={"faults": [], "record_io": ["faults"]},
    )


def test_generated_manifests_satisfy_their_own_checker(tmp_path: Path) -> None:
    assert manifest_check.check(_clean(tmp_path)) == []


def test_retired_compatibility_crate_in_layers_is_rejected(tmp_path: Path) -> None:
    root = _clean(tmp_path)
    path = root / "libs/rust/layers.json"
    data = json.loads(path.read_text())
    data["layers"]["clock"] = 5
    path.write_text(json.dumps(data), encoding="utf-8")

    assert any("retired compatibility crate 'clock'" in e for e in manifest_check.check(root))


def test_crate_missing_from_package_manifest_is_rejected(tmp_path: Path) -> None:
    root = _clean(tmp_path)
    path = root / "libs/rust/PACKAGE_MANIFEST.json"
    data = json.loads(path.read_text())
    data["packages"] = [p for p in data["packages"] if p["directory"] != "record_io"]
    path.write_text(json.dumps(data), encoding="utf-8")

    assert any(
        "PACKAGE_MANIFEST.json: omits crate 'record_io'" in e for e in manifest_check.check(root)
    )


def test_stale_dependency_row_is_rejected(tmp_path: Path) -> None:
    """The catalog's dependency column is the copy source for new imports; it must be exact."""
    root = _clean(tmp_path)
    path = root / "libs/rust/PACKAGE_CATALOG.md"
    path.write_text(path.read_text().replace("| `faults` |", "| `faults`, `clock` |"), "utf-8")

    assert any(
        "PACKAGE_CATALOG.md[record_io]: dependency column" in e for e in manifest_check.check(root)
    )


def test_stale_catalog_status_column_is_rejected(tmp_path: Path) -> None:
    root = _clean(tmp_path)
    path = root / "libs/rust/PACKAGE_CATALOG.md"
    path.write_text(path.read_text().replace("| stable |", "| evolving |", 1), encoding="utf-8")

    assert any("status column is 'evolving'" in e for e in manifest_check.check(root))


def test_crate_missing_from_readme_roster_is_rejected(tmp_path: Path) -> None:
    root = _clean(tmp_path)
    path = root / "libs/rust/README.md"
    path.write_text(path.read_text().replace(", `record_io`", ""), encoding="utf-8")

    assert any("README.md: omits crate 'record_io'" in e for e in manifest_check.check(root))


def test_upward_layer_dependency_is_rejected(tmp_path: Path) -> None:
    root = _repo(
        tmp_path,
        layers={"faults": 2, "record_io": 1},
        crates={"faults": [], "record_io": ["faults"]},
    )
    assert any(
        "libs/rust/record_io (layer 1) depends upward on faults (layer 2)" in e
        for e in manifest_check.check(root)
    )


def test_cfg_gated_upward_dependency_is_rejected(tmp_path: Path) -> None:
    """A `[target.'cfg(...)'] ` edge is a real production edge; `ipc` already ships unix/windows."""
    root = _clean(tmp_path)
    cargo = root / "libs/rust/faults/Cargo.toml"
    cargo.write_text(
        cargo.read_text()
        + "\n[target.'cfg(unix)'.dependencies]\nmindclade_record_io = { path = \"../record_io\" }\n",
        encoding="utf-8",
    )

    assert any(
        "libs/rust/faults (layer 0) depends upward on record_io (layer 1)" in e
        for e in manifest_check.check(root)
    )


def test_malformed_layer_value_is_reported_not_skipped(tmp_path: Path) -> None:
    """A dropped layer value would disable the upward-dependency gate for that crate."""
    root = _clean(tmp_path)
    path = root / "libs/rust/layers.json"
    data = json.loads(path.read_text())
    data["layers"]["record_io"] = "1"
    path.write_text(json.dumps(data), encoding="utf-8")

    assert any(
        "layers.json[record_io]: layer must be an integer" in e for e in manifest_check.check(root)
    )


def test_compatibility_only_must_name_a_compatibility_crate(tmp_path: Path) -> None:
    root = _clean(tmp_path)
    path = root / "libs/rust/layers.json"
    data = json.loads(path.read_text())
    data["compatibility_only"] = ["record_io"]
    path.write_text(json.dumps(data), encoding="utf-8")

    assert any("compatibility_only lists 'record_io'" in e for e in manifest_check.check(root))


def test_public_api_baseline_naming_absent_crate_is_rejected(tmp_path: Path) -> None:
    root = _clean(tmp_path)
    path = root / "libs/rust/public_api_baseline.json"
    path.write_text(json.dumps({"crates": {"mindclade_common": ["clock"]}}), encoding="utf-8")

    assert any(
        "'mindclade_common' is a forbidden catch-all crate name" in e
        for e in manifest_check.check(root)
    )


@pytest.mark.parametrize("relpath", ["libs/rust/PACKAGE_CATALOG.md", "libs/rust/LAYERS.md"])
def test_unreadable_declaration_reports_instead_of_aborting_the_suite(
    tmp_path: Path, relpath: str
) -> None:
    """run_architecture_checks calls every checker bare; an escaping exception hides 20 checks."""
    root = _clean(tmp_path)
    (root / relpath).unlink()

    errors = manifest_check.check(root)
    assert errors and any("unreadable" in e for e in errors)


def test_write_repairs_a_drifted_manifest(tmp_path: Path) -> None:
    root = _clean(tmp_path)
    path = root / "libs/rust/PACKAGE_MANIFEST.json"
    data = json.loads(path.read_text())
    data["packages"][0]["dependencies"] = ["clock"]
    path.write_text(json.dumps(data), encoding="utf-8")
    assert manifest_check.check(root) != []

    assert manifest_check.write(root) == []
    assert manifest_check.check(root) == []
