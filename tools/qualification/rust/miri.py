#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Run Miri over crates containing unsafe/OS boundary code."""

from __future__ import annotations

import argparse
import shutil
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]
PACKAGES = ("mindclade_ipc_os", "mindclade_python_bridge")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--required", action="store_true")
    args = parser.parse_args()
    cargo = shutil.which("cargo")
    if not cargo:
        return 1 if args.required else 0
    probe = subprocess.run(
        [cargo, "miri", "--version"], cwd=ROOT, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL
    )
    if probe.returncode:
        if args.required:
            print("cargo-miri unavailable", file=sys.stderr)
            return 1
        return 0
    for package in PACKAGES:
        subprocess.run([cargo, "miri", "test", "-p", package, "--locked"], cwd=ROOT, check=True)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
