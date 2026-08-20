# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""The host-tool scan has one exemption; these tests hold both of its edges.

CONTRACT_IMPLEMENTATIONS exists because a check that forbids a string cannot avoid
containing it. Two failure modes follow, and each has a test here:

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


def write(root: Path, relative: str, text: str) -> None:
    path = root / relative
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(text, encoding="utf-8")


def rust_fixture(root: Path) -> None:
    """The four files check() reads before it ever walks the tree.

    The pattern scan shares check() with the Rust version contract, which reads Cargo.toml,
    versions.nix and flake.nix unguarded. Without these a fixture tree dies on FileNotFoundError
    somewhere unrelated to what the test is about; with them the returned list is the scan's,
    so a test can assert on the whole list instead of filtering it.
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


def run(files: dict[str, str]) -> list[str]:
    with tempfile.TemporaryDirectory() as directory:
        root = Path(directory)
        rust_fixture(root)
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
