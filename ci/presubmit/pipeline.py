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
    module = importlib.util.module_from_spec(spec)
    assert spec.loader
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
