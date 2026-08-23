# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""The Cargo workspace and the Bazel manifest inventory must not drift apart unnoticed.

`check_cargo_bazel_alignment` used to assert that the literal string `crate.from_cargo(` occurred
somewhere in `MODULE.bazel` and stop there. Every case below is one the old gate returned PASS
for, including the two that were live in the tree: `libs/rust/process_os` missing from the
`manifests` list, and `ipc_os`/`process_os` missing from the `production_sources` filegroup.

The negative cases matter more than the positive one. A gate is only worth its runtime if some
input makes it fail, so each test names a specific corruption and pins the message that reports
it -- including the two parser cases, because a checker that silently fails to parse its input is
the same fail-open defect in a new place.
"""

from __future__ import annotations

import importlib.util
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]
ANALYSIS = ROOT / "tools/analysis"

# The checker imports its sibling `check_code_docs_alignment` for the retired-crate roster. That
# import resolves only when tools/analysis is on sys.path, which run_architecture_checks.py does
# for itself; a test module has to arrange it explicitly rather than inherit it from whichever
# test file happened to be collected first.
if str(ANALYSIS) not in sys.path:
    sys.path.insert(0, str(ANALYSIS))


def load(name: str, path: Path):
    spec = importlib.util.spec_from_file_location(name, path)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"unable to load {path}")
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    spec.loader.exec_module(module)
    return module


alignment = load("check_cargo_bazel_alignment", ANALYSIS / "check_cargo_bazel_alignment.py")

MEMBERS = ("faults", "record_io")


def _crate(root: Path, name: str, deps: tuple[str, ...]) -> None:
    directory = root / "libs/rust" / name
    (directory / "src").mkdir(parents=True)
    (directory / "src/lib.rs").write_text("", encoding="utf-8")
    manifest = ["[package]", f'name = "mindclade_{name}"', "", "[dependencies]"]
    manifest += [f'mindclade_{dep} = {{ path = "../{dep}" }}' for dep in deps]
    (directory / "Cargo.toml").write_text("\n".join(manifest) + "\n", encoding="utf-8")
    edges = "".join(f'        "//libs/rust/{dep}:mindclade_{dep}",\n' for dep in deps)
    (directory / "BUILD.bazel").write_text(
        f'rust_library(\n    name = "mindclade_{name}",\n'
        '    srcs = glob(["src/**/*.rs"]),\n'
        f"    deps = [\n{edges}    ],\n)\n\n"
        'filegroup(\n    name = "package_sources",\n    srcs = glob(["**"]),\n)\n',
        encoding="utf-8",
    )


def _module_bazel(manifests: list[str]) -> str:
    entries = "".join(f'        "{label}",\n' for label in manifests)
    return (
        "# The Rust crate universe is derived from Cargo.\ncrate = use_extension(\n"
        '    "@rules_rust//crate_universe:extensions.bzl",\n    "crate",\n)\n'
        'crate.from_cargo(\n    name = "crates",\n'
        '    cargo_lockfile = "//:Cargo.lock",\n'
        f'    manifests = [\n{entries}    ],\n)\nuse_repo(crate, "crates")\n'
    )


def _libs_rust_build(srcs: list[str]) -> str:
    entries = "".join(f'        "{label}",\n' for label in srcs)
    return (
        f'filegroup(\n    name = "production_sources",\n    srcs = [\n{entries}    ],\n)\n\n'
        'filegroup(\n    name = "all_sources",\n'
        '    srcs = [":production_sources"] + glob(["*.md"]),\n)\n'
    )


def _repo(tmp_path: Path) -> Path:
    root = tmp_path
    (root / "libs/rust").mkdir(parents=True)
    _crate(root, "faults", ())
    _crate(root, "record_io", ("faults",))

    members = "".join(f'    "libs/rust/{name}",\n' for name in MEMBERS)
    (root / "Cargo.toml").write_text(f"[workspace]\nmembers = [\n{members}]\n", encoding="utf-8")
    (root / "MODULE.bazel").write_text(
        _module_bazel(["//:Cargo.toml"] + [f"//libs/rust/{n}:Cargo.toml" for n in MEMBERS]),
        encoding="utf-8",
    )
    (root / "libs/rust/BUILD.bazel").write_text(
        _libs_rust_build([f"//libs/rust/{n}:package_sources" for n in MEMBERS]), encoding="utf-8"
    )
    return root


def _rewrite(root: Path, relative: str, old: str, new: str) -> None:
    path = root / relative
    text = path.read_text()
    assert old in text, f"fixture no longer contains {old!r}"
    path.write_text(text.replace(old, new, 1), encoding="utf-8")


def test_clean_fixture_passes(tmp_path: Path) -> None:
    assert alignment.check(_repo(tmp_path)) == []


def test_member_missing_from_manifests_is_rejected(tmp_path: Path) -> None:
    """The live defect: `libs/rust/process_os` was a member that MODULE.bazel never listed."""
    root = _repo(tmp_path)
    _rewrite(root, "MODULE.bazel", '        "//libs/rust/record_io:Cargo.toml",\n', "")

    assert any(
        "MODULE.bazel manifests: omits '//libs/rust/record_io:Cargo.toml' for Cargo workspace "
        "member 'libs/rust/record_io'" in error
        for error in alignment.check(root)
    )


def test_commented_out_manifest_entry_does_not_count(tmp_path: Path) -> None:
    """A `#`-commented label is not a declaration; a line-oriented grep would be fooled by it."""
    root = _repo(tmp_path)
    _rewrite(
        root,
        "MODULE.bazel",
        '        "//libs/rust/record_io:Cargo.toml",\n',
        '        # "//libs/rust/record_io:Cargo.toml",\n',
    )

    assert any(
        "omits '//libs/rust/record_io:Cargo.toml'" in error for error in alignment.check(root)
    )


def test_phantom_manifest_entry_is_rejected(tmp_path: Path) -> None:
    """The stale direction: an entry survives the crate directory it named."""
    root = _repo(tmp_path)
    _rewrite(
        root,
        "MODULE.bazel",
        "    manifests = [\n",
        '    manifests = [\n        "//libs/rust/ghost_crate:Cargo.toml",\n',
    )

    assert any(
        "tracks '//libs/rust/ghost_crate:Cargo.toml', but libs/rust/ghost_crate/Cargo.toml "
        "does not exist" in error
        for error in alignment.check(root)
    )


def test_retired_epoch_crate_in_manifests_is_rejected(tmp_path: Path) -> None:
    """The 2026-08 crates were removed, not deprecated, and lingered in four manifests."""
    root = _repo(tmp_path)
    _rewrite(
        root,
        "MODULE.bazel",
        "    manifests = [\n",
        '    manifests = [\n        "//libs/rust/clock:Cargo.toml",\n',
    )

    assert any("tracks retired crate 'clock'" in error for error in alignment.check(root))


def test_missing_workspace_root_manifest_is_rejected(tmp_path: Path) -> None:
    root = _repo(tmp_path)
    _rewrite(root, "MODULE.bazel", '        "//:Cargo.toml",\n', "")

    assert any(
        "omits the workspace root manifest '//:Cargo.toml'" in error
        for error in alignment.check(root)
    )


def test_from_cargo_without_manifests_list_is_rejected(tmp_path: Path) -> None:
    """Exactly what the old gate blessed: the call is present, the inventory is not."""
    root = _repo(tmp_path)
    text = (root / "MODULE.bazel").read_text()
    head, _, tail = text.partition("    manifests = [")
    (root / "MODULE.bazel").write_text(head + tail.partition("    ],\n")[2], encoding="utf-8")

    assert any(
        "crate.from_cargo declares no manifests list" in error for error in alignment.check(root)
    )


def test_missing_from_cargo_call_is_rejected(tmp_path: Path) -> None:
    root = _repo(tmp_path)
    _rewrite(root, "MODULE.bazel", "crate.from_cargo(", "crate.from_annotation(")

    assert any(
        "must derive third-party Rust deps from the Cargo workspace" in error
        for error in alignment.check(root)
    )


def test_duplicate_manifest_entry_is_rejected(tmp_path: Path) -> None:
    root = _repo(tmp_path)
    _rewrite(
        root,
        "MODULE.bazel",
        '        "//libs/rust/faults:Cargo.toml",\n',
        '        "//libs/rust/faults:Cargo.toml",\n        "//libs/rust/faults:Cargo.toml",\n',
    )

    assert any(
        "lists '//libs/rust/faults:Cargo.toml' more than once" in error
        for error in alignment.check(root)
    )


def test_manifest_label_must_name_a_cargo_toml(tmp_path: Path) -> None:
    root = _repo(tmp_path)
    _rewrite(
        root,
        "MODULE.bazel",
        '"//libs/rust/faults:Cargo.toml"',
        '"//libs/rust/faults:BUILD.bazel"',
    )

    assert any(
        "must point at a Cargo.toml, not 'BUILD.bazel'" in error for error in alignment.check(root)
    )


def test_mismatched_bracket_is_reported_not_raised(tmp_path: Path) -> None:
    """A malformed list must fail the gate, never abort the suite and never pass by default."""
    root = _repo(tmp_path)
    _rewrite(root, "MODULE.bazel", "    ],\n)\nuse_repo", "\n)\nuse_repo")

    assert any(
        "MODULE.bazel: mismatched ')' in bracket nesting" in error
        for error in alignment.check(root)
    )


def test_truncated_module_bazel_is_reported_not_raised(tmp_path: Path) -> None:
    """A list that simply never closes reaches EOF instead of a closing bracket."""
    root = _repo(tmp_path)
    path = root / "MODULE.bazel"
    path.write_text(path.read_text().split("    ],")[0], encoding="utf-8")

    assert any("MODULE.bazel: unbalanced '('" in error for error in alignment.check(root))


def test_crate_missing_from_production_sources_is_rejected(tmp_path: Path) -> None:
    """The second live defect: the filegroup listed 23 of the 25 crates on disk."""
    root = _repo(tmp_path)
    _rewrite(root, "libs/rust/BUILD.bazel", '        "//libs/rust/faults:package_sources",\n', "")

    assert any(
        "production_sources: omits '//libs/rust/faults:package_sources' for the crate at "
        "libs/rust/faults" in error
        for error in alignment.check(root)
    )


def test_unknown_entry_in_production_sources_is_rejected(tmp_path: Path) -> None:
    root = _repo(tmp_path)
    _rewrite(
        root,
        "libs/rust/BUILD.bazel",
        "    srcs = [\n",
        '    srcs = [\n        "//libs/rust/ghost_crate:package_sources",\n',
    )

    assert any(
        "lists '//libs/rust/ghost_crate:package_sources', which is not a crate in libs/rust"
        in error
        for error in alignment.check(root)
    )


def test_crate_without_package_sources_target_is_rejected(tmp_path: Path) -> None:
    """A named filegroup that the crate does not define makes the aggregate unbuildable."""
    root = _repo(tmp_path)
    _rewrite(root, "libs/rust/faults/BUILD.bazel", 'name = "package_sources"', 'name = "sources"')

    assert any(
        "has no package_sources target in libs/rust/faults/BUILD.bazel" in error
        for error in alignment.check(root)
    )


def test_production_sources_filegroup_must_exist(tmp_path: Path) -> None:
    root = _repo(tmp_path)
    _rewrite(root, "libs/rust/BUILD.bazel", 'name = "production_sources"', 'name = "sources"')

    assert any(
        "no filegroup named 'production_sources'" in error for error in alignment.check(root)
    )
