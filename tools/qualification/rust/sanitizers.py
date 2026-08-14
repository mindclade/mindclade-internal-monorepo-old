#!/usr/bin/env python3
"""Sanitizer qualification for OS/provider/runtime leaf crates."""

from __future__ import annotations

import argparse
import os
import shutil
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]
PACKAGES = ("mindclade_ipc_os", "mindclade-runtime-gateway", "mindclade-runtime-host")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--sanitizer", choices=("address", "thread"), default="address")
    parser.add_argument("--required", action="store_true")
    args = parser.parse_args()
    cargo = shutil.which("cargo")
    if not cargo:
        return 1 if args.required else 0
    env = os.environ.copy()
    env["RUSTFLAGS"] = f"-Zsanitizer={args.sanitizer}"
    env["RUSTDOCFLAGS"] = env["RUSTFLAGS"]
    for package in PACKAGES:
        result = subprocess.run(
            [cargo, "+nightly", "test", "-p", package, "--target", "x86_64-unknown-linux-gnu", "--locked"],
            cwd=ROOT,
            env=env,
            check=False,
        )
        if result.returncode:
            return result.returncode
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
