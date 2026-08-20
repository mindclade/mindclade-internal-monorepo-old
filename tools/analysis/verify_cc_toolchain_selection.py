#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Prove C/C++ actions select the registered Nix toolchain and no host compiler."""

from __future__ import annotations

import argparse
import subprocess
import sys
from pathlib import Path

EXPECTED = (
    "external/+nix_toolchains+mindclade_nix_cc/bin/gcc",
    "external/+nix_toolchains+mindclade_nix_cc/bin/cxx_linker",
)
FORBIDDEN = ("/usr/bin/clang", "/opt/homebrew/", "CommandLineTools/usr/bin/")


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo", type=Path, default=Path(__file__).resolve().parents[2])
    args = parser.parse_args(argv)
    repo = args.repo.resolve()
    completed = subprocess.run(
        [
            str(repo / "tools/dev/bazelw"),
            "aquery",
            'mnemonic("CppCompile|CppLink", //tools/build/bazel/toolchains/cc:smoke_test)',
            "--include_commandline",
            "--output=textproto",
            "--curses=no",
            "--color=no",
        ],
        cwd=repo,
        capture_output=True,
        check=False,
        text=True,
    )
    if completed.returncode:
        print(completed.stderr, file=sys.stderr)
        return completed.returncode
    output = completed.stdout
    errors = []
    for expected in EXPECTED:
        if expected not in output:
            errors.append(f"registered Nix C/C++ tool was not selected: {expected}")
    for forbidden in FORBIDDEN:
        if forbidden in output:
            errors.append(f"host compiler path leaked into C/C++ actions: {forbidden}")
    if output.count('mnemonic: "CppCompile"') < 1 or output.count('mnemonic: "CppLink"') < 1:
        errors.append("smoke target did not produce both compile and link actions")
    for error in errors:
        print(error, file=sys.stderr)
    if errors:
        return 1
    print("C/C++ toolchain selection check passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
