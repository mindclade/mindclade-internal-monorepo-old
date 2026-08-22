# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Static supply-chain assertions for the Linux/amd64 MLflow OCI layout."""

from __future__ import annotations

import argparse
import json
import os
import re
import sys
import tarfile
from pathlib import Path

SHA256 = re.compile(r"^sha256:[0-9a-f]{64}$")
DARWIN_NATIVE = re.compile(
    r"(?:-darwin|macosx_[^/]*\.(?:so|whl)|\.dylib(?:$|/)|darwin[^/]*\.(?:so|whl))",
    re.IGNORECASE,
)


def document(path: Path) -> dict:
    value = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(value, dict):
        raise AssertionError(f"expected an object in {path.name}")
    return value


def blob(image: Path, reference: object, label: str) -> Path:
    if not isinstance(reference, str) or not SHA256.fullmatch(reference):
        raise AssertionError(f"{label} digest is invalid")
    path = image / "blobs" / "sha256" / reference.removeprefix("sha256:")
    if not path.is_file():
        raise AssertionError(f"{label} blob is missing")
    return path


def resolve_runfile(value: str) -> Path:
    path = Path(value)
    if path.is_absolute():
        return path
    return Path(os.environ["RUNFILES_DIR"]) / os.environ["TEST_WORKSPACE"] / path


def validate(image: Path, lock_path: Path, *, enforce_lock_digest: bool) -> str:
    index = document(image / "index.json")
    manifests = index.get("manifests")
    if not isinstance(manifests, list) or len(manifests) != 1:
        raise AssertionError("OCI index must bind exactly one manifest")
    manifest_reference = manifests[0].get("digest")
    lock_text = lock_path.read_text(encoding="utf-8")
    if not re.search(r"^kind: RuntimeImageQualificationLock$", lock_text, re.MULTILINE):
        raise AssertionError("runtime image lock kind is invalid")
    required_lock_lines = (
        "  target: //services/mlflow:image",
        "  platform: linux/amd64",
        "    version: 3.15.1",
    )
    for required_line in required_lock_lines:
        if lock_text.splitlines().count(required_line) != 1:
            raise AssertionError(f"runtime image lock drifted: {required_line.strip()}")
    digest_match = re.search(r"^  imageDigest: (sha256:[0-9a-f]{64})$", lock_text, re.MULTILINE)
    if digest_match is None:
        raise AssertionError("runtime image lock has no valid image digest")
    locked_digest = digest_match.group(1)
    if enforce_lock_digest and locked_digest != manifest_reference:
        raise AssertionError(f"runtime image lock drifted: imageDigest: {manifest_reference}")
    manifest = document(blob(image, manifest_reference, "manifest"))
    config = document(blob(image, (manifest.get("config") or {}).get("digest"), "config"))

    assert config.get("os") == "linux", "image OS is not Linux"
    assert config.get("architecture") == "amd64", "image architecture is not amd64"
    runtime = config.get("config") or {}
    assert runtime.get("User") == "65532:65532", "image runtime identity is not non-root"
    assert runtime.get("Entrypoint") == ["/opt/mindclade/services/mlflow/server"], (
        "image entrypoint drifted"
    )
    labels = runtime.get("Labels") or {}
    assert labels.get("org.opencontainers.image.version") == "3.15.1", (
        "MLflow version label drifted"
    )

    layers = manifest.get("layers")
    if not isinstance(layers, list) or len(layers) < 2:
        raise AssertionError("OCI image has no application layer")
    layer_path = blob(image, layers[-1].get("digest"), "application layer")
    with tarfile.open(layer_path, mode="r:*") as archive:
        names = archive.getnames()
    if any(DARWIN_NATIVE.search(name) for name in names):
        raise AssertionError("application layer contains a Darwin-native artifact")
    if not any(name.endswith("cpython-314-x86_64-linux-gnu.so") for name in names):
        raise AssertionError("application layer has no CPython 3.14 Linux/amd64 extension")

    required_paths = (
        "opt/mindclade/services/mlflow/server",
        "/flask_wtf/__init__.py",
        "/google/cloud/storage/__init__.py",
        "/mlflow/gateway/budget_tracker/redis.py",
        "/mlflow/server/auth/__init__.py",
        "/psycopg2/__init__.py",
        "/redis/__init__.py",
        "/slowapi/__init__.py",
        "/tiktoken/__init__.py",
        "/uvicorn/__init__.py",
        "/watchfiles/__init__.py",
    )
    for required_path in required_paths:
        if not any(required_path in name for name in names):
            raise AssertionError(f"required runtime path is absent: {required_path}")
    return str(manifest_reference)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--image", required=True)
    parser.add_argument("--lock", required=True)
    parser.add_argument("--enforce-lock-digest", action="store_true")
    args = parser.parse_args()
    executor_is_linux = sys.platform.startswith("linux")
    if args.enforce_lock_digest != executor_is_linux:
        raise AssertionError("Bazel digest enforcement does not match the executor platform")
    manifest = validate(
        resolve_runfile(args.image),
        resolve_runfile(args.lock),
        enforce_lock_digest=args.enforce_lock_digest,
    )
    mode = "digest-bound" if args.enforce_lock_digest else "structure-only"
    print(f"MLflow OCI layout passed ({manifest}; {mode})")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
