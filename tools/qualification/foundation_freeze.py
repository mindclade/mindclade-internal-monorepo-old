#!/usr/bin/env python3
"""Foundation-freeze qualification for the final production architecture.

Offline mode proves repository/design invariants and executable Go/Python seams.
Connected mode adds the pinned, locked Rust release lane and therefore fails
closed when toolchain/lock/provider evidence is unavailable.
"""

from __future__ import annotations

import argparse
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


def run(*command: str) -> None:
    subprocess.run(command, cwd=ROOT, check=True)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--connected", action="store_true")
    args = parser.parse_args()

    run(sys.executable, "tools/analysis/run_architecture_checks.py")
    run(sys.executable, "tools/analysis/check_rust_format_conventions.py")
    run(sys.executable, "tools/analysis/check_rust_arithmetic.py")
    run(sys.executable, "tools/qualification/compatibility.py")
    run(
        sys.executable,
        "tools/qualification/failure_injection.py",
        *(["--execute"] if args.connected else []),
    )
    run(
        sys.executable,
        "tools/qualification/rust/supply_chain.py",
        *(["--connected"] if args.connected else []),
    )
    run(sys.executable, "tools/qualification/rust/performance.py")
    run(
        sys.executable,
        "-m",
        "pytest",
        "-q",
        "tests/integration/cross_language",
        "libs/python/worker_runtime/tests",
    )
    run(
        sys.executable,
        "tests/integration/vertical_slices/release_gate.py",
        *(["--require-rust"] if args.connected else []),
    )
    run(
        "go",
        "test",
        "./control/artifacts",
        "./control/ingestion",
        "./control/orchestration",
        "./control/registry/releases",
    )

    if args.connected:
        run(sys.executable, "tools/qualification/rust/qualify.py", "--mode", "release")

    print("foundation freeze gate passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
