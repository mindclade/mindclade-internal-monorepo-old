# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Fail-closed comparison of local and Buildfarm Bazel result evidence."""

from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path
from typing import Any

SHA256_PREFIX = "sha256:"


def _read(path: Path) -> dict[str, Any]:
    value = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(value, dict):
        raise ValueError(f"{path}: evidence must be a JSON object")
    return value


def _digest(path: Path) -> str:
    return SHA256_PREFIX + hashlib.sha256(path.read_bytes()).hexdigest()


def _validate_common(value: dict[str, Any], *, mode: str) -> None:
    exact = {
        "schemaVersion",
        "mode",
        "bazelVersion",
        "platform",
        "executionImage",
        "toolchainManifest",
        "targets",
        "networkAccess",
        "hostPathInputs",
        "remoteExecution",
    }
    if set(value) != exact:
        raise ValueError(f"{mode}: evidence fields must be exact: {sorted(exact)}")
    if value["schemaVersion"] != 1 or value["mode"] != mode:
        raise ValueError(f"{mode}: schemaVersion=1 and mode={mode!r} are required")
    if value["bazelVersion"] != "9.1.1":
        raise ValueError(f"{mode}: Bazel 9.1.1 is required")
    if value["platform"] not in {"linux/amd64", "linux/arm64"}:
        raise ValueError(f"{mode}: platform must be linux/amd64 or linux/arm64")
    for field in ("executionImage", "toolchainManifest"):
        if not isinstance(value[field], str) or not value[field].startswith(SHA256_PREFIX):
            raise ValueError(f"{mode}: {field} must be a sha256 digest")
    targets = value["targets"]
    if not isinstance(targets, dict) or not targets:
        raise ValueError(f"{mode}: at least one target output digest is required")
    if any(
        not isinstance(label, str)
        or not label.startswith("//")
        or not isinstance(digest, str)
        or not digest.startswith(SHA256_PREFIX)
        for label, digest in targets.items()
    ):
        raise ValueError(f"{mode}: target map must contain Bazel labels and sha256 digests")
    if value["networkAccess"] is not False or value["hostPathInputs"] != []:
        raise ValueError(f"{mode}: network and host-path inputs must be absent")


def compare(local: dict[str, Any], remote: dict[str, Any]) -> dict[str, Any]:
    _validate_common(local, mode="local")
    _validate_common(remote, mode="remote")
    if local["remoteExecution"] is not None:
        raise ValueError("local: remoteExecution must be null")
    metadata = remote["remoteExecution"]
    required_remote = {
        "backend",
        "endpoint",
        "executedActions",
        "cacheOnly",
        "invocationId",
    }
    if not isinstance(metadata, dict) or set(metadata) != required_remote:
        raise ValueError("remote: remoteExecution fields are not exact")
    if metadata["backend"] != "buildfarm-2.17.0":
        raise ValueError("remote: backend must be buildfarm-2.17.0")
    if not isinstance(metadata["endpoint"], str) or not metadata["endpoint"].startswith("grpcs://"):
        raise ValueError("remote: a TLS grpcs:// endpoint is required")
    if (
        metadata["cacheOnly"] is not False
        or not isinstance(metadata["executedActions"], int)
        or metadata["executedActions"] < 1
    ):
        raise ValueError("remote: evidence must prove at least one remotely executed action")
    if not isinstance(metadata["invocationId"], str) or not metadata["invocationId"].strip():
        raise ValueError("remote: invocationId is required")

    comparable = ("bazelVersion", "platform", "executionImage", "toolchainManifest", "targets")
    differences = [field for field in comparable if local[field] != remote[field]]
    if differences:
        raise ValueError("local/remote parity differs: " + ", ".join(differences))
    return {
        "schemaVersion": 1,
        "verdict": "pass",
        "platform": remote["platform"],
        "targets": sorted(remote["targets"]),
        "remoteInvocationId": metadata["invocationId"],
        "remoteExecutedActions": metadata["executedActions"],
    }


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--local", required=True, type=Path)
    parser.add_argument("--remote", required=True, type=Path)
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()
    result = compare(_read(args.local), _read(args.remote))
    result["localEvidenceDigest"] = _digest(args.local)
    result["remoteEvidenceDigest"] = _digest(args.remote)
    rendered = json.dumps(result, indent=2, sort_keys=True) + "\n"
    if args.output:
        args.output.write_text(rendered, encoding="utf-8")
    else:
        print(rendered, end="")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
