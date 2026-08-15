#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Check first-party Rust Cargo workspace and Bazel package alignment."""

from __future__ import annotations

import re
import tomllib
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


def check(root: Path) -> list[str]:
    errors = []
    ws = tomllib.loads((root / "Cargo.toml").read_text())
    members = ws.get("workspace", {}).get("members", [])
    for member in members:
        directory = root / member
        cargo = directory / "Cargo.toml"
        build = directory / "BUILD.bazel"
        if not cargo.exists():
            errors.append(f"{member}: workspace member missing Cargo.toml")
            continue
        if not build.exists():
            errors.append(f"{member}: missing BUILD.bazel")
            continue
        data = tomllib.loads(cargo.read_text())
        text = build.read_text()
        lib = data.get("lib") or (
            {"name": data["package"]["name"].replace("-", "_")}
            if (directory / "src/lib.rs").exists()
            else None
        )
        if lib:
            target = lib.get("name", data["package"]["name"].replace("-", "_"))
            if not re.search(rf'name\s*=\s*"{re.escape(target)}"', text):
                errors.append(f"{member}: Bazel missing library target {target}")
        if 'srcs = glob(["src/**/*.rs"])' not in text:
            errors.append(f"{member}: Bazel must include complete Rust source glob")
        for dep, val in data.get("dependencies", {}).items():
            if isinstance(val, dict) and val.get("path"):
                depdir = (directory / val["path"]).resolve()
                dd = tomllib.loads((depdir / "Cargo.toml").read_text())
                target = (dd.get("lib") or {"name": dd["package"]["name"].replace("-", "_")}).get(
                    "name", dd["package"]["name"].replace("-", "_")
                )
                label = f"//{depdir.relative_to(root).as_posix()}:{target}"
            else:
                package = val.get("package", dep) if isinstance(val, dict) else dep
                label = f"@crates//:{package}"
            if label not in text:
                errors.append(f"{member}: Bazel missing Cargo dependency {label}")
    if "crate.from_cargo(" not in (root / "MODULE.bazel").read_text():
        errors.append(
            "MODULE.bazel: Crate Universe must derive third-party Rust deps from Cargo workspace"
        )
    return errors


def main() -> int:
    errors = check(ROOT)
    for e in errors:
        print(e)
    if errors:
        return 1
    print("Cargo/Bazel Rust alignment passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
