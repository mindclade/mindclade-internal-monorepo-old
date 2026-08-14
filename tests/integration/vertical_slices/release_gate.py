#!/usr/bin/env python3
"""Execute the four architecture-defining golden vertical release slices.

The slices qualify platform boundaries.  Where a frontier model/trainer is not
production-mature yet, deterministic reference engines exercise the same
artifact/checkpoint/protocol path and are explicitly *not* numerical evidence.
"""

from __future__ import annotations

import shutil
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]


def run(*command: str) -> None:
    subprocess.run(command, cwd=ROOT, check=True)


def cargo_test(package: str, *, required: bool) -> None:
    cargo = shutil.which("cargo")
    if cargo is None:
        if required:
            raise RuntimeError(f"release vertical qualification requires Cargo for {package}")
        return
    run(cargo, "test", "-p", package, "--all-targets", "--locked")


def data_ingestion_slice(*, require_rust: bool) -> None:
    """Source snapshot → durable workload → curation → immutable dataset seam."""
    run(
        "go",
        "test",
        "./control/ingestion",
        "./control/artifacts",
        "./control/orchestration",
        "./examples/go/ingestion_coordinator",
    )
    run(sys.executable, "-m", "pytest", "-q", "data/curation/tests")
    cargo_test("mindclade-ingestion-worker", required=require_rust)
    cargo_test("mindclade-artifact-proxy", required=require_rust)


def novafold_preprocessing_slice(*, require_rust: bool) -> None:
    """Structure preprocessing DAG → provenance/cache → Python model-worker seam."""
    run(
        sys.executable,
        "-m",
        "pytest",
        "-q",
        "preprocessing/contracts/tests",
        "preprocessing/pipeline/tests",
        "preprocessing/cache/tests",
        "preprocessing/provenance/tests",
        "preprocessing/biology/entities/tests",
        "preprocessing/biology/msa/tests",
        "preprocessing/biology/templates/tests",
        "preprocessing/biology/ligands/tests",
        "preprocessing/biology/featurization/tests",
        "serving/model_worker/tests/test_worker.py",
        "services/workers/model_worker/tests/test_smoke.py",
    )
    cargo_test("mindclade-node-agent", required=require_rust)
    cargo_test("mindclade-artifact-proxy", required=require_rust)


def online_inference_slice(*, require_rust: bool) -> None:
    """Signed control authority → Rust gateway/host → Python final batching seam."""
    run(
        sys.executable,
        "-m",
        "pytest",
        "-q",
        "services/workers/model_worker/tests/test_smoke.py",
        "tests/integration/vertical_slices/test_hardening_contracts.py",
    )
    cargo_test("mindclade-serving-runtime", required=require_rust)
    cargo_test("mindclade-runtime-gateway", required=require_rust)
    cargo_test("mindclade-runtime-host", required=require_rust)


def training_release_slice(*, require_rust: bool) -> None:
    """Reference train state → checkpoint → evaluation → release-evidence DAG seam."""
    run(
        sys.executable,
        "-m",
        "pytest",
        "-q",
        "tests/integration/vertical_slices/test_reference_training.py",
    )
    run("go", "test", "./control/registry/releases", "./control/artifacts")
    cargo_test("mindclade-node-agent", required=require_rust)
    cargo_test("mindclade-checkpoint-io", required=require_rust)


def policy_and_compatibility_gates() -> None:
    run(sys.executable, "tools/qualification/compatibility.py")
    run(sys.executable, "tools/qualification/failure_injection.py")
    run(sys.executable, "tools/qualification/rust/performance.py")
    run(
        sys.executable,
        "-m",
        "pytest",
        "-q",
        "tests/integration/vertical_slices/test_contract_slices.py",
    )


def main() -> int:
    require_rust = "--require-rust" in sys.argv[1:]
    data_ingestion_slice(require_rust=require_rust)
    novafold_preprocessing_slice(require_rust=require_rust)
    online_inference_slice(require_rust=require_rust)
    training_release_slice(require_rust=require_rust)
    policy_and_compatibility_gates()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
