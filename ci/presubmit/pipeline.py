# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Affected presubmit orchestrator; Bazel remains the test execution authority."""

from __future__ import annotations

import argparse
import importlib.util
import subprocess
import sys
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]


def load_affected():
    path = REPO / "ci/common/affected.py"
    spec = importlib.util.spec_from_file_location("mindclade_affected", path)
    # spec_from_file_location returns None when the path has no extension Python knows how to
    # load — a .md, a directory, anything that is not importable. That is the failure this
    # guards: REPO is derived from parents[2], so an edit to this file's location silently
    # repoints the path, and landing on a non-Python file is the plausible way that goes wrong.
    # A merely missing .py still yields a spec and fails clearly at exec_module.
    #
    # Guarded before the first dereference rather than after, and raising rather than asserting:
    # module_from_spec(spec) used to run first, and `assert` is removed entirely by `python -O`.
    if spec is None or spec.loader is None:
        raise RuntimeError(f"unable to load {path}")
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


def run(command):
    return subprocess.call(command, cwd=REPO)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--static-only", action="store_true")
    parser.add_argument("--base")
    args = parser.parse_args()
    if run([sys.executable, str(REPO / "tools/analysis/run_architecture_checks.py")]):
        return 1
    if args.static_only:
        return 0
    affected = load_affected()
    changed = affected.git_changed(args.base)
    if affected.rust_qualification_required(changed):
        if run(
            [
                sys.executable,
                str(REPO / "tools/qualification/rust/qualify.py"),
                "--mode",
                "presubmit",
            ]
        ):
            return 1
    else:
        print("Skipping full Rust qualification: no Rust/runtime/toolchain inputs changed")
    targets = affected.select(changed)
    print("Affected Bazel targets:", *targets, sep="\n  ")
    return run([str(REPO / "tools/dev/bazelw"), "test", *targets])


if __name__ == "__main__":
    raise SystemExit(main())
