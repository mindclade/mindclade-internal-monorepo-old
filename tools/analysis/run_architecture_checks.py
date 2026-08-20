#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Single presubmit entry point for architecture invariants."""

from __future__ import annotations

import os
import sys
from pathlib import Path
from typing import cast

sys.dont_write_bytecode = True
HERE = Path(__file__).resolve().parent
if str(HERE) not in sys.path:
    sys.path.insert(0, str(HERE))
import check_affected_presubmit
import check_artifact_gc_contract
import check_blueprint_scaffold
import check_build_toolchain_contract
import check_cargo_bazel_alignment
import check_code_docs_alignment
import check_component_maturity
import check_component_ownership
import check_control_plane_commands
import check_dependency_budgets
import check_dependency_layers
import check_dns_caa_alignment
import check_enforced_decisions
import check_foundation_consumption
import check_foundation_hardening
import check_go_layers
import check_go_modules
import check_libs_go_admission
import check_rust_implementation
import check_rust_workspace

def _go_layers(root: Path) -> list[str]:
    """Adapt the Go layering checker, which reports structured violations."""
    return [violation.render(root) for violation in check_go_layers.check(root)]


_BLUEPRINT_MANIFEST = "docs/blueprint/production-monorepo-paths.txt"

# The three defects the blueprint manifest can carry that are wrong TODAY, whatever fraction of
# the target state has been built, paired with how to say so.
_BLUEPRINT_DEFECTS = (
    ("duplicate_paths", "is listed more than once in the blueprint manifest"),
    ("unsafe_paths", "is absolute or escapes the repository root"),
    ("unexpected_empty_paths", "is materialized but unexpectedly empty"),
)


def _blueprint_scaffold(root: Path) -> list[str]:
    """Adapt the blueprint scaffold checker, which reports a structured result.

    Gates on the three defects above and DELIBERATELY NOT on `missing_paths`, even though the
    checker's own CLI exits non-zero for those too. The manifest describes the repository's
    TARGET state, so unmaterialized paths measure progress, not correctness, and a presubmit
    that is red for work nobody on the pull request can do is one people learn to route around.

    The count is already gated, as a ratchet, by tests/integration/test_blueprint_scaffold.py
    against its MATERIALIZATION_BASELINE. That constant is not shared with this module, on
    purpose:

      * it would invert the dependency. tools/analysis/ is a stdlib-only presubmit binary; that
        module imports pytest at top level, so sharing the constant would make the architecture
        suite refuse to start wherever pytest is not installed.
      * two baselines drift. The first change to lower only one of them leaves the other quietly
        permitting the regression it was written to catch.

    So: one owner per assertion. The ratchet owns the count, this owns the invariants.
    """
    manifest = root / _BLUEPRINT_MANIFEST
    if not manifest.is_file():
        return [f"blueprint manifest is missing: {_BLUEPRINT_MANIFEST}"]
    result = check_blueprint_scaffold.check(root, manifest)
    return [
        f"{path} {defect}"
        for key, defect in _BLUEPRINT_DEFECTS
        for path in cast("list[str]", result[key])
    ]


CHECKS = [
    ("build/toolchain", check_build_toolchain_contract.check),
    ("affected presubmit", check_affected_presubmit.check),
    ("artifact GC", check_artifact_gc_contract.check),
    ("blueprint scaffold consistency", _blueprint_scaffold),
    ("foundation hardening", check_foundation_hardening.check),
    ("Cargo/Bazel Rust alignment", check_cargo_bazel_alignment.check),
    ("component maturity", check_component_maturity.check),
    ("enforced decisions", check_enforced_decisions.check),
    ("component ownership", check_component_ownership.check),
    ("code/docs alignment", check_code_docs_alignment.check),
    ("dependency budgets", check_dependency_budgets.check),
    ("dependency layers", check_dependency_layers.check),
    ("DNS CAA/cert-manager alignment", check_dns_caa_alignment.check),
    ("foundation consumption", check_foundation_consumption.check),
    ("control-plane commands", check_control_plane_commands.check),
    ("Go layers and paved roads", _go_layers),
    ("Go modules", check_go_modules.check),
    ("libs/go admission", check_libs_go_admission.check),
    ("Rust workspace", check_rust_workspace.check),
    ("Rust implementation", check_rust_implementation.check),
]


def main() -> int:
    root = Path(os.environ.get("BUILD_WORKSPACE_DIRECTORY", HERE.parents[1])).resolve()
    errors = []
    for name, fn in CHECKS:
        found = fn(root)
        if found:
            errors.extend(f"{name}: {e}" for e in found)
        else:
            print(f"PASS {name}")
    for e in errors:
        print(e)
    return 1 if errors else 0


if __name__ == "__main__":
    raise SystemExit(main())
