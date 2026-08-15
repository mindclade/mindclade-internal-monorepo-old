#!/usr/bin/env python3
"""Production Rust qualification orchestrator.

Qualification is deliberately read-only with respect to dependency locks.  The
only supported lock update path is tools/qualification/rust/update_lock.sh.
"""

from __future__ import annotations

import argparse
import subprocess
import sys

from common import ROOT, require_tool, run, verify_toolchain


def require_committed_lock() -> None:
    cargo = require_tool("cargo")
    lock = ROOT / "Cargo.lock"
    if not lock.exists() or lock.stat().st_size < 128:
        raise RuntimeError(
            "Cargo.lock is missing or empty; run tools/qualification/rust/update_lock.sh "
            "with the pinned Rust 1.97.1 toolchain and commit the result"
        )
    run([cargo, "metadata", "--locked", "--format-version", "1"])
    run([cargo, "verify-project"])


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--mode", choices=("presubmit", "nightly", "release"), default="presubmit")
    args = parser.parse_args()
    try:
        verify_toolchain()
        require_committed_lock()
        cargo = require_tool("cargo")
        run([cargo, "fmt", "--all", "--", "--check"])
        run([cargo, "test", "--workspace", "--all-targets", "--all-features", "--locked"])
        run(
            [
                cargo,
                "clippy",
                "--workspace",
                "--all-targets",
                "--all-features",
                "--locked",
                "--",
                "-D",
                "warnings",
            ]
        )
        run([cargo, "test", "--workspace", "--doc", "--locked"])
        run([cargo, "doc", "--workspace", "--all-features", "--no-deps", "--locked"])
        run([sys.executable, str(ROOT / "tools/analysis/check_rust_format_conventions.py")])
        run([sys.executable, str(ROOT / "tools/analysis/check_rust_arithmetic.py")])
        run([sys.executable, str(ROOT / "tools/analysis/check_rust_implementation.py")])
        run([sys.executable, str(ROOT / "tools/analysis/check_cargo_bazel_alignment.py")])
        run([sys.executable, str(ROOT / "tools/qualification/rust/supply_chain.py"), "--connected"])
        run([sys.executable, str(ROOT / "tools/qualification/compatibility.py")])
        run([sys.executable, str(ROOT / "tools/qualification/failure_injection.py"), "--execute"])
        run([sys.executable, str(ROOT / "tools/qualification/rust/performance.py"), "--measure"])
        if args.mode in {"nightly", "release"}:
            run([sys.executable, str(ROOT / "tools/qualification/rust/fuzz.py"), "--required"])
            run([sys.executable, str(ROOT / "tools/qualification/rust/miri.py"), "--required"])
        if args.mode == "release":
            run([sys.executable, str(ROOT / "tools/qualification/rust/release_performance.py")])
            run([sys.executable, str(ROOT / "tests/integration/cross_language/release_gate.py")])
            run(
                [
                    sys.executable,
                    str(ROOT / "tests/integration/vertical_slices/release_gate.py"),
                    "--require-rust",
                ]
            )
    except (RuntimeError, subprocess.CalledProcessError) as error:
        print(error, file=sys.stderr)
        return 1
    print(f"Rust {args.mode} qualification passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
