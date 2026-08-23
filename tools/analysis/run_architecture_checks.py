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
import check_enforced_decisions
import check_foundation_consumption
import check_foundation_hardening
import check_go_command_composition
import check_go_layers
import check_go_modules
import check_go_test_signal
import check_libs_go_admission
import check_mlops_contracts
import check_rust_implementation
import check_rust_package_manifest
import check_rust_workspace


def _go_layers(root: Path) -> list[str]:
    """Adapt the Go layering checker, which reports structured violations."""
    return [violation.render(root) for violation in check_go_layers.check(root)]


# Missing paths are measured separately by the materialization ratchet. Every other invariant
# exported by the checker gates by default, so adding an invariant cannot silently fail open.
_BLUEPRINT_UNGATED = frozenset({"missing_paths"})

_BLUEPRINT_DEFECT_MESSAGES = {
    "duplicate_paths": "is listed more than once in the blueprint manifest",
    "noncanonical_paths": "does not use the canonical POSIX repository-relative spelling",
    "unexpected_empty_paths": "is materialized but unexpectedly empty",
    "unsafe_paths": "is absolute or escapes the repository root or contains a symbolic link",
}


def _blueprint_scaffold(root: Path) -> list[str]:
    """Adapt the blueprint scaffold checker, which reports a structured result.

    Gates every invariant the checker reports except `missing_paths`, even though the checker's
    own CLI exits non-zero for missing paths too. The manifest describes the repository's target
    state, so unmaterialized paths measure progress rather than immediate correctness.

    The count is already gated, as a ratchet, by tests/integration/test_blueprint_scaffold.py
    against its MATERIALIZATION_BASELINE. That constant is not shared with this module, on
    purpose:

      * it would invert the dependency. The architecture suite owns one pinned PyYAML dependency
        for exact workflow semantics; that module imports pytest at top level, so sharing the
        constant would expand the production checker into a test-runner dependency.
      * two baselines drift. The first change to lower only one of them leaves the other quietly
        permitting the regression it was written to catch.

    The subtraction from the checker's exported keys is deliberate: a newly added invariant is
    gated by default rather than silently omitted from a duplicated allowlist. The ratchet owns
    the count; this check owns the remaining invariants.
    """
    relpath = check_blueprint_scaffold.MANIFEST_RELPATH
    result = check_blueprint_scaffold.check(root, root / relpath)
    if not check_blueprint_scaffold.has_failures(result, include_missing=False):
        return []
    errors = list(cast("list[str]", result["manifest_errors"]))
    errors.extend(
        f"{path} {_BLUEPRINT_DEFECT_MESSAGES.get(key, 'violates a blueprint manifest invariant')}"
        for key in check_blueprint_scaffold.DEFECT_KEYS
        if key not in _BLUEPRINT_UNGATED
        for path in cast("list[str]", result[key])
    )
    return errors


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
    ("foundation consumption", check_foundation_consumption.check),
    ("control-plane commands", check_control_plane_commands.check),
    ("Go command composition", check_go_command_composition.check),
    ("Go layers and paved roads", _go_layers),
    ("Go modules", check_go_modules.check),
    ("Go test signals", check_go_test_signal.check),
    ("libs/go admission", check_libs_go_admission.check),
    ("MLOps static contracts", check_mlops_contracts.check),
    ("Rust workspace", check_rust_workspace.check),
    ("Rust package manifest", check_rust_package_manifest.check),
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
