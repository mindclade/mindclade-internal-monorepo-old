#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Static Rust workspace/compatibility invariants for environments without rustc."""

from __future__ import annotations

import argparse
import re
import tomllib
from pathlib import Path

# A plain sibling import, with no sys.path insertion: this module only ever runs as a script from
# tools/analysis (where the interpreter puts that directory on sys.path itself) or as an import
# from run_architecture_checks.py, which inserts the directory before importing anything.
import check_code_docs_alignment

COMPAT = check_code_docs_alignment.REMOVED_COMPAT_CRATE_NAMES
# The migration epoch is over and BASELINE is empty, which is the point: the compatibility crates
# it used to grandfather are deleted, so no edge to one can be legitimate any more.
#
# It previously listed ten crates' pre-existing compatibility edges — including three entries for
# `observability`, `python_bindings`, and `retry`, directories that no longer exist. Those dead
# entries were not merely untidy. `unexpected = deps & COMPAT - allowed` meant re-adding
# `mindclade_clock = { path = "../clock" }` to `atomic_fs` was explicitly *permitted* by the gate
# written to forbid it; the reinstatement only failed because a different checker happens to
# regex the crate name. An allowlist that outlives what it grandfathers fails open.
BASELINE: dict[str, set[str]] = {}
IMPLEMENTED = [
    "libs/rust/runtime_core",
    "libs/rust/bytes_io",
    "libs/rust/manifests",
    "libs/rust/bounded_parse",
    "libs/rust/bio_formats",
    "libs/rust/worker_protocol",
    "libs/rust/worker_runtime",
    "libs/rust/gpu_host",
    "libs/rust/telemetry",
    "services/runtime_gateway",
    "services/runtime_host",
]
UNSAFE = re.compile(r"\bunsafe\b")


def check(root: Path) -> list[str]:
    errors: list[str] = []
    workspace = tomllib.loads((root / "Cargo.toml").read_text())
    members = workspace.get("workspace", {}).get("members", [])
    names: dict[str, str] = {}
    for member in members:
        cargo = root / member / "Cargo.toml"
        if not cargo.exists():
            errors.append(f"workspace member missing Cargo.toml: {member}")
            continue
        data = tomllib.loads(cargo.read_text())
        name = data.get("package", {}).get("name")
        if not name:
            errors.append(f"workspace member lacks package.name: {member}")
        elif name in names:
            errors.append(f"duplicate Rust package name {name}: {names[name]} and {member}")
        else:
            names[name] = member
        for section in ("dependencies", "dev-dependencies", "build-dependencies"):
            for dep_name, spec in (data.get(section) or {}).items():
                if isinstance(spec, dict) and "path" in spec:
                    target = (cargo.parent / spec["path"] / "Cargo.toml").resolve()
                    if not target.exists():
                        errors.append(
                            f"{member}: path dependency missing: {dep_name} -> {spec['path']}"
                        )
                        continue
                    target_data = tomllib.loads(target.read_text())
                    actual = target_data.get("package", {}).get("name")
                    expected = spec.get("package", dep_name)
                    if actual != expected:
                        errors.append(
                            f"{member}: path dependency {dep_name} expects package {expected}, found {actual} at {spec['path']}"
                        )
    if (root / "libs/rust/common").exists():
        errors.append("libs/rust/common is forbidden; use cohesive execution primitives")

    for cargo in sorted((root / "libs/rust").glob("*/Cargo.toml")):
        data = tomllib.loads(cargo.read_text())
        deps = set((data.get("dependencies") or {}).keys())
        compat = deps & COMPAT
        allowed = BASELINE.get(cargo.parent.name, set())
        unexpected = compat - allowed
        if unexpected:
            errors.append(
                f"libs/rust/{cargo.parent.name}: new compatibility dependencies forbidden: {sorted(unexpected)}"
            )

    for rel in IMPLEMENTED:
        path = root / rel
        for source in path.rglob("*.rs"):
            text = source.read_text(errors="replace")
            if "SCAFFOLD_" in text:
                errors.append(
                    f"implemented Rust component still contains scaffold constant: {source.relative_to(root)}"
                )
            # The literal text in `#![forbid(unsafe_code)]` is policy, not an unsafe block.
            scrubbed = text.replace("forbid(unsafe_code)", "")
            if UNSAFE.search(scrubbed):
                errors.append(
                    f"unsafe Rust requires an explicitly audited adapter boundary: {source.relative_to(root)}"
                )
    return errors


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--repo", type=Path, default=Path(__file__).resolve().parents[2])
    args = ap.parse_args()
    errors = check(args.repo.resolve())
    for error in errors:
        print(error)
    print(
        "Rust workspace check passed"
        if not errors
        else f"Rust workspace check failed: {len(errors)}"
    )
    return 1 if errors else 0


if __name__ == "__main__":
    raise SystemExit(main())
