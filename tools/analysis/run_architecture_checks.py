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

sys.dont_write_bytecode = True
HERE = Path(__file__).resolve().parent
if str(HERE) not in sys.path:
    sys.path.insert(0, str(HERE))
import check_affected_presubmit
import check_artifact_gc_contract
import check_build_toolchain_contract
import check_cargo_bazel_alignment
import check_code_docs_alignment
import check_component_maturity
import check_component_ownership
import check_dependency_budgets
import check_dependency_layers
import check_enforced_decisions
import check_foundation_hardening
import check_go_modules
import check_libs_go_admission
import check_rust_implementation
import check_rust_workspace

CHECKS = [
    ("build/toolchain", check_build_toolchain_contract.check),
    ("affected presubmit", check_affected_presubmit.check),
    ("artifact GC", check_artifact_gc_contract.check),
    ("foundation hardening", check_foundation_hardening.check),
    ("Cargo/Bazel Rust alignment", check_cargo_bazel_alignment.check),
    ("component maturity", check_component_maturity.check),
    ("enforced decisions", check_enforced_decisions.check),
    ("component ownership", check_component_ownership.check),
    ("code/docs alignment", check_code_docs_alignment.check),
    ("dependency budgets", check_dependency_budgets.check),
    ("dependency layers", check_dependency_layers.check),
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
