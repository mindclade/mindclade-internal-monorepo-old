# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Regression tests for build-toolchain ownership and pytest runfiles safety.

The host-tool scan has narrow enforcement-file exemptions; these tests hold both edges.

CONTRACT_IMPLEMENTATIONS exists because checks that forbid host strings cannot avoid
containing them. Two failure modes follow, and each has a test here:

  * too narrow — the Nix mirror of this check spells out the same host paths and
    package-manager invocations as pattern literals, so leaving it out of the allowlist makes
    the Python check and the Nix check mutually exclusive, and the only green tree is one
    with a control deleted.
  * too broad — skipping tools/build/nix/checks/ wholesale would exempt every future check
    in that directory, so a new one that really did reach for a host tool would pass.

The fixtures write the mirror's own text rather than spelling the literals out here, and
nothing below quotes a forbidden string either. Not style: this file is a .py, .py is in
SCAN, and a test that names one of these strings in order to prove the scan catches it is
itself caught — as this docstring was, on its first run.
"""

from __future__ import annotations

import importlib.util
import sys
import tempfile
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


contract = load(
    "check_build_toolchain_contract", ROOT / "tools/analysis/check_build_toolchain_contract.py"
)

MIRROR = "tools/build/nix/checks/no-host-tools.nix"
# A plausible neighbour rather than one of the checks that exists, so the case reads as "the
# next check somebody adds here" instead of as a claim about a committed file.
SIBLING = "tools/build/nix/checks/vendored-toolchain.nix"

RUST_VERSION = "1.99.0"
PYTEST_MACRO = """\
load("@rules_python//python:defs.bzl", "py_test")

def pytest_test(name, srcs, legacy_create_init = {default}, **kwargs):
    py_test(
        name = name,
        srcs = srcs,
        legacy_create_init = {forwarded},
        **kwargs
    )
"""
MODULE = """\
rust_toolchains = use_extension("@rules_rust//rust:extensions.bzl", "rust")
rust_toolchains.toolchain(
    edition = "2024",
    versions = ["{version}"],
)
use_repo(rust_toolchains, "rust_toolchains")
register_toolchains("@rust_toolchains//:all")

python = use_extension("@rules_python//python/extensions:python.bzl", "python")
python.single_version_override(
    python_version = "3.14.7",
    sha256 = {{
        "aarch64-apple-darwin": "{sha}",
        "aarch64-unknown-linux-gnu": "{sha}",
        "x86_64-unknown-linux-gnu": "{sha}",
    }},
    urls = [
        "https://github.com/astral-sh/python-build-standalone/releases/download/20260805/cpython-{{python_version}}+20260805-{{platform}}-install_only_stripped.tar.gz",
    ],
)
python.override(minor_mapping = {{"3.14": "3.14.7"}})
python.toolchain(is_default = True, python_version = "3.14")

pip = use_extension("@rules_python//python/extensions:pip.bzl", "pip")
pip.parse(
    download_only = True,
    experimental_index_url = "https://pypi.org/simple",
    experimental_index_url_overrides = {{
        "torch": "https://download.pytorch.org/whl/cpu",
    }},
    experimental_target_platforms = [
        "linux_aarch64",
        "linux_x86_64",
        "osx_aarch64",
    ],
    hub_name = "pypi",
    requirements_by_platform = {{
        "//:requirements.darwin.lock.txt": "osx_aarch64",
        "//:requirements.lock.txt": "linux_*",
    }},
)
"""
LOCK = """\
# uv pip compile --python-platform {platform}

torch==2.13.0{suffix} \\
    --hash=sha256:{digest}
"""


def write(root: Path, relative: str, text: str) -> None:
    path = root / relative
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(text, encoding="utf-8")


def contract_fixture(root: Path) -> None:
    """The files check() reads before it ever walks the tree.

    The pattern scan shares check() with the Rust version contract, which reads Cargo.toml,
    versions.nix and flake.nix unguarded, and with the pytest initializer contract. Without
    these a fixture tree fails somewhere unrelated to what the test is about; with them the
    returned list is the scan's, so a test can assert on the whole list instead of filtering
    it.
    """
    write(root, "Cargo.toml", f'[workspace.package]\nrust-version = "{RUST_VERSION}"\n')
    write(root, "tools/build/nix/versions.nix", f'{{\n  rust = "{RUST_VERSION}";\n}}\n')
    write(
        root,
        "flake.nix",
        '{\n  inputs.rust-overlay.url = "github:oxalica/rust-overlay";\n'
        "  # toolchains/rust.nix builds the pinned toolchain from that overlay.\n}\n",
    )
    write(root, "tools/qualification/rust/common.py", f'EXPECTED = "{RUST_VERSION}"\n')
    write(root, "tools/build/nix/toolchain-manifest.json", '{"tools":{"python":"3.14.7"}}\n')
    write(root, "MODULE.bazel", MODULE.format(version=RUST_VERSION, sha="0" * 64))
    patterns = ",\n".join(f'    "{pattern}"' for pattern in sorted(contract.REQUIRED_REPO_IGNORES))
    write(root, "REPO.bazel", f"ignore_directories([\n{patterns},\n])\n")
    write(
        root,
        contract.PYTEST_MACRO,
        PYTEST_MACRO.format(default="False", forwarded="legacy_create_init"),
    )
    write(
        root,
        "requirements.lock.txt",
        LOCK.format(platform="linux", suffix="+cpu", digest="0" * 64),
    )
    write(
        root,
        "requirements.darwin.lock.txt",
        LOCK.format(platform="aarch64-apple-darwin", suffix="", digest="1" * 64),
    )


def run(files: dict[str, str]) -> list[str]:
    with tempfile.TemporaryDirectory() as directory:
        root = Path(directory)
        contract_fixture(root)
        for relative, text in files.items():
            write(root, relative, text)
        return contract.check(root)


def mirror_text() -> str:
    """The committed Nix mirror, used as fixture content.

    Read rather than inlined for the reason in the module docstring, and read from the real
    file rather than approximated so the fixture cannot drift into something the scan ignores —
    test_the_exemption_is_still_earned is what stops that going unnoticed.
    """
    return (ROOT / MIRROR).read_text(encoding="utf-8")


# check() walks everything under the root it is given, so the whole-tree assertions below only
# mean something against a checkout. Under Bazel this runs in a runfiles tree holding the
# declared data and nothing else, where the walk would report a missing Cargo.toml rather than
# a policy violation; //tools/analysis:architecture_policy scans the committed tree there.
IS_CHECKOUT = (ROOT / "MODULE.bazel").is_file() and (ROOT / "Cargo.toml").is_file()
checkout_only = pytest.mark.skipif(
    not IS_CHECKOUT, reason="whole-tree scan; needs a checkout, not a runfiles tree"
)


@checkout_only
def test_the_committed_tree_satisfies_the_contract() -> None:
    assert contract.check(ROOT) == []


def test_the_mirror_is_exempt_at_its_own_path() -> None:
    assert run({MIRROR: mirror_text()}) == []


def test_the_same_text_at_a_sibling_path_is_still_reported() -> None:
    """The exemption is a path allowlist, not a skip of tools/build/nix/checks/."""
    errors = run({SIBLING: mirror_text()})
    assert errors, "a host-tool literal outside the allowlist must still be reported"
    assert all(error.startswith(f"{SIBLING}:") for error in errors), errors


def test_the_exemption_does_not_extend_to_the_rest_of_the_scan() -> None:
    """Both files present: one exempt, one not, from identical bytes."""
    errors = run({MIRROR: mirror_text(), SIBLING: mirror_text()})
    assert errors, "the sibling must be reported even alongside the exempt mirror"
    assert all(error.startswith(f"{SIBLING}:") for error in errors), errors


def test_the_exemption_is_still_earned() -> None:
    """A stale allowlist entry is a blind spot, not a no-op.

    Each entry has to name a file that exists and that still contains what it is exempted for.
    If a mirror stops naming host tools, the fix is to drop the entry, not to leave the scan
    unable to see the path.
    """
    for relative in sorted(contract.CONTRACT_IMPLEMENTATIONS):
        path = ROOT / relative
        assert path.is_file(), f"{relative} is allowlisted but is not in the tree"
        text = path.read_text(encoding="utf-8")
        assert any(rx.search(text) for rx in contract.FORBIDDEN), (
            f"{relative} no longer contains a forbidden pattern; drop it from "
            "CONTRACT_IMPLEMENTATIONS rather than leaving the scan blind to that path"
        )


def test_repository_traversal_contract_requires_repo_policy() -> None:
    with tempfile.TemporaryDirectory() as directory:
        errors = contract.repository_traversal_contract(Path(directory))
    assert errors == ["REPO.bazel is required for globbed generated-directory ignores"]


def test_repository_traversal_contract_reports_each_missing_pattern() -> None:
    with tempfile.TemporaryDirectory() as directory:
        root = Path(directory)
        write(root, "REPO.bazel", "ignore_directories([])\n")
        errors = contract.repository_traversal_contract(root)
    assert errors == [
        f"REPO.bazel must ignore generated directory pattern {pattern}"
        for pattern in sorted(contract.REQUIRED_REPO_IGNORES)
    ]


def test_rust_version_contract_rejects_an_implicit_bazel_toolchain() -> None:
    with tempfile.TemporaryDirectory() as directory:
        root = Path(directory)
        contract_fixture(root)
        write(root, "MODULE.bazel", 'module(name = "fixture")\n')
        errors = contract.rust_version_contract(root)
    assert errors == ["MODULE.bazel must configure the root rules_rust toolchain extension"]


def test_rust_version_contract_rejects_bazel_version_drift() -> None:
    with tempfile.TemporaryDirectory() as directory:
        root = Path(directory)
        contract_fixture(root)
        write(root, "MODULE.bazel", MODULE.format(version="9.99.9", sha="0" * 64))
        errors = contract.rust_version_contract(root)
    assert errors == ["Bazel rules_rust version does not match Cargo rust-version"]


def test_python_repository_contract_accepts_target_aware_wheel_resolution() -> None:
    with tempfile.TemporaryDirectory() as directory:
        root = Path(directory)
        write(root, "MODULE.bazel", MODULE.format(version=RUST_VERSION, sha="0" * 64))
        assert contract.python_repository_resolution_contract(root) == []


def test_python_toolchain_contract_rejects_an_unpatched_minor_mapping() -> None:
    with tempfile.TemporaryDirectory() as directory:
        root = Path(directory)
        contract_fixture(root)
        module = (root / "MODULE.bazel").read_text(encoding="utf-8")
        write(
            root,
            "MODULE.bazel",
            module.replace('python.override(minor_mapping = {"3.14": "3.14.7"})\n', ""),
        )

        errors = contract.python_toolchain_version_contract(root)

    assert errors == ["Bazel Python 3.14 must resolve to the Nix patch version 3.14.7"]


def test_python_repository_contract_rejects_host_pip_and_shared_indexes() -> None:
    with tempfile.TemporaryDirectory() as directory:
        root = Path(directory)
        write(
            root,
            "MODULE.bazel",
            """\
pip = use_extension("@rules_python//python/extensions:pip.bzl", "pip")
pip.parse(
    experimental_extra_index_urls = ["https://download.pytorch.org/whl/cpu"],
    hub_name = "pypi",
    requirements_by_platform = {
        "//:requirements.darwin.lock.txt": "osx_aarch64",
        "//:requirements.lock.txt": "linux_*",
    },
)
""",
        )
        errors = contract.python_repository_resolution_contract(root)
    assert errors == [
        "root pypi repository must be wheel-only",
        "root pypi repository must use the canonical PyPI simple index",
        "root pypi repository must route Torch exclusively to the CPU index",
        "root pypi repository must declare the supported Linux and Apple targets",
    ]


def test_python_platform_lock_contract_rejects_a_universal_torch_lock() -> None:
    with tempfile.TemporaryDirectory() as directory:
        root = Path(directory)
        contract_fixture(root)
        write(
            root,
            "requirements.lock.txt",
            LOCK.format(platform="linux", suffix="+cpu", digest="0" * 64)
            + LOCK.format(platform="aarch64-apple-darwin", suffix="", digest="1" * 64),
        )
        errors = contract.python_platform_lock_contract(root)
    assert errors == [
        "requirements.lock.txt must contain exactly one unambiguous torch requirement"
    ]


def test_python_platform_lock_contract_rejects_cross_platform_torch_metadata() -> None:
    with tempfile.TemporaryDirectory() as directory:
        root = Path(directory)
        contract_fixture(root)
        write(
            root,
            "requirements.darwin.lock.txt",
            LOCK.format(platform="aarch64-apple-darwin", suffix="+cpu", digest="1" * 64),
        )
        errors = contract.python_platform_lock_contract(root)
    assert errors == [
        "requirements.darwin.lock.txt must select the Darwin Torch version without a local suffix"
    ]


def test_python_platform_lock_contract_rejects_a_global_secondary_index() -> None:
    with tempfile.TemporaryDirectory() as directory:
        root = Path(directory)
        contract_fixture(root)
        write(
            root,
            "requirements.lock.txt",
            "--extra-index-url https://download.pytorch.org/whl/cpu\n"
            + LOCK.format(platform="linux", suffix="+cpu", digest="0" * 64),
        )
        errors = contract.python_platform_lock_contract(root)
    assert errors == ["requirements.lock.txt must not expose package indexes to every requirement"]


def test_pytest_init_contract_requires_the_shared_macro() -> None:
    with tempfile.TemporaryDirectory() as directory:
        errors = contract.pytest_init_contract(Path(directory))
    assert errors == [
        f"{contract.PYTEST_MACRO} is required for pytest package-initializer governance"
    ]


def test_pytest_init_contract_accepts_explicit_source_authority() -> None:
    with tempfile.TemporaryDirectory() as directory:
        root = Path(directory)
        write(
            root,
            contract.PYTEST_MACRO,
            PYTEST_MACRO.format(default="False", forwarded="legacy_create_init"),
        )
        errors = contract.pytest_init_contract(root)
    assert errors == []


def test_pytest_init_contract_rejects_legacy_synthesis_by_default() -> None:
    with tempfile.TemporaryDirectory() as directory:
        root = Path(directory)
        write(
            root,
            contract.PYTEST_MACRO,
            PYTEST_MACRO.format(default="True", forwarded="legacy_create_init"),
        )
        errors = contract.pytest_init_contract(root)
    assert errors == [
        f"{contract.PYTEST_MACRO}: pytest_test must default legacy_create_init to False"
    ]


def test_pytest_init_contract_rejects_a_non_forwarded_setting() -> None:
    with tempfile.TemporaryDirectory() as directory:
        root = Path(directory)
        write(
            root,
            contract.PYTEST_MACRO,
            PYTEST_MACRO.format(default="False", forwarded="False"),
        )
        errors = contract.pytest_init_contract(root)
    assert errors == [
        f"{contract.PYTEST_MACRO}: pytest_test must forward legacy_create_init explicitly to py_test"
    ]
