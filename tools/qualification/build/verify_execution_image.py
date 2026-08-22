# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Validate the immutable Buildfarm and Nix execution-image release contract."""

from __future__ import annotations

import argparse
import json
import re
from pathlib import Path
from typing import Any

DIGEST = re.compile(r"^sha256:[0-9a-f]{64}$")
PLATFORMS = {"linux/amd64", "linux/arm64"}
SYSTEMS = {"aarch64-linux", "x86_64-linux"}
COMMIT = "7a2b98e6b9bdda948631d403a92c159aa33b196e"


def validate_lock(value: dict[str, Any]) -> None:
    if value.get("schemaVersion") != 1:
        raise ValueError("image lock schemaVersion must be 1")
    buildfarm = value.get("buildfarm")
    if not isinstance(buildfarm, dict):
        raise ValueError("buildfarm lock is required")
    if buildfarm.get("version") != "2.17.0" or buildfarm.get("sourceCommit") != COMMIT:
        raise ValueError("Buildfarm must resolve 2.17.0 to its verified source commit")
    images = buildfarm.get("images")
    if not isinstance(images, dict) or set(images) != {"server", "worker"}:
        raise ValueError("server and worker locks are required")
    for name, image in images.items():
        if set(image) != {"repository", "indexDigest", "platforms"}:
            raise ValueError(f"{name}: image lock fields are not exact")
        if ":" in image["repository"].rsplit("/", 1)[-1] or "@" in image["repository"]:
            raise ValueError(f"{name}: repository must not include a tag or digest")
        if not DIGEST.fullmatch(image["indexDigest"]):
            raise ValueError(f"{name}: indexDigest is invalid")
        if set(image["platforms"]) != PLATFORMS:
            raise ValueError(f"{name}: AMD64 and ARM64 child manifests are required")
        if any(not DIGEST.fullmatch(digest) for digest in image["platforms"].values()):
            raise ValueError(f"{name}: platform digest is invalid")
        if len(set(image["platforms"].values())) != 2:
            raise ValueError(f"{name}: platform manifests must be distinct")
    execution = value.get("executionBase")
    if not isinstance(execution, dict):
        raise ValueError("executionBase lock is required")
    if execution.get("authority") != "nix" or set(execution.get("requiredSystems", [])) != SYSTEMS:
        raise ValueError("Nix must own both native Linux execution bases")
    if execution.get("requiredUser") != "65532:65532":
        raise ValueError("execution base must run as uid/gid 65532")
    if execution.get("bazelVersion") != "9.1.1":
        raise ValueError("execution base must pin Bazel 9.1.1")
    if execution.get("publicationState") != "blocked-pending-native-build-and-attestation":
        raise ValueError("source lock must remain blocked until native attestation")


def validate_attestation(value: dict[str, Any]) -> None:
    exact = {"schemaVersion", "image", "user", "bazelVersion", "platforms", "rebuilds"}
    if set(value) != exact or value["schemaVersion"] != 1:
        raise ValueError("attestation fields are not exact")
    if not re.fullmatch(r"[^\s@]+@sha256:[0-9a-f]{64}", value["image"]):
        raise ValueError("attested image must be an immutable registry digest")
    if value["user"] != "65532:65532" or value["bazelVersion"] != "9.1.1":
        raise ValueError("attested user/Bazel version differs from source contract")
    if set(value["platforms"]) != PLATFORMS:
        raise ValueError("attestation requires native AMD64 and ARM64 manifests")
    for platform, digest in value["platforms"].items():
        if not DIGEST.fullmatch(digest):
            raise ValueError(f"{platform}: invalid manifest digest")
        rebuilds = value["rebuilds"].get(platform)
        if not isinstance(rebuilds, list) or len(rebuilds) != 2 or len(set(rebuilds)) != 1:
            raise ValueError(f"{platform}: two identical independent rebuild digests are required")
        if rebuilds[0] != digest:
            raise ValueError(f"{platform}: rebuild digest differs from published manifest")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--lock", required=True, type=Path)
    parser.add_argument("--attestation", type=Path)
    args = parser.parse_args()
    value = json.loads(args.lock.read_text(encoding="utf-8"))
    validate_lock(value)
    if args.attestation:
        validate_attestation(json.loads(args.attestation.read_text(encoding="utf-8")))
    print("execution image contract: PASS")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
