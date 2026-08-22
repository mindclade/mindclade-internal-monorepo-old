# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Fail-closed verification for immutable TileLang qualification manifests."""

from __future__ import annotations

import argparse
import hmac
import json
import os
import stat
from pathlib import Path

from kernels.manifest import QualificationManifest

MAXIMUM_MANIFEST_BYTES = 8 * 1024 * 1024


def _require_digest(value: str | None, name: str) -> str:
    if (
        value is None
        or len(value) != 64
        or any(character not in "0123456789abcdef" for character in value)
    ):
        raise ValueError(f"{name} must be a lowercase SHA-256 digest")
    return value


def _read_regular_file(path: Path, maximum_bytes: int) -> bytes:
    flags = os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0)
    try:
        descriptor = os.open(path, flags)
    except OSError as exc:
        raise ValueError(f"refusing unreadable or symbolic-link manifest {path}") from exc
    try:
        metadata = os.fstat(descriptor)
        if not stat.S_ISREG(metadata.st_mode):
            raise ValueError("qualification manifest must be a regular file")
        if metadata.st_size > maximum_bytes:
            raise ValueError("qualification manifest exceeds the byte limit")
        with os.fdopen(descriptor, "rb", closefd=False) as handle:
            payload = handle.read(maximum_bytes + 1)
        if len(payload) > maximum_bytes:
            raise ValueError("qualification manifest exceeds the byte limit")
        return payload
    finally:
        os.close(descriptor)


def verify_manifest(
    manifest: QualificationManifest,
    *,
    expected_manifest_digest: str | None = None,
    allow_empty: bool = False,
    environment_digest: str | None = None,
    toolchain_digest: str | None = None,
    artifact_digests: frozenset[str] | None = None,
) -> dict[str, object]:
    trusted_manifest_digest = _require_digest(
        expected_manifest_digest,
        "expected_manifest_digest",
    )
    if not hmac.compare_digest(manifest.digest, trusted_manifest_digest):
        raise ValueError("qualification manifest does not match the trusted content identity")
    if not manifest.records and not allow_empty:
        raise ValueError("production manifest verification rejects an empty record set")
    trusted_environment: str | None = environment_digest
    trusted_toolchain: str | None = toolchain_digest
    if manifest.records:
        trusted_environment = _require_digest(environment_digest, "environment_digest")
        trusted_toolchain = _require_digest(toolchain_digest, "toolchain_digest")
        if not artifact_digests:
            raise ValueError("production verification requires trusted artifact digests")
        for digest in artifact_digests:
            _require_digest(digest, "artifact_digest")
    for record in manifest.records:
        if record.environment_digest != trusted_environment:
            raise ValueError("qualification environment digest does not match the expected runtime")
        if record.toolchain_digest != trusted_toolchain:
            raise ValueError("qualification toolchain digest does not match the expected runtime")
        if artifact_digests is None or record.artifact_digest not in artifact_digests:
            raise ValueError("qualification artifact digest is not in the trusted artifact set")
    return {
        "architectures": sorted({record.architecture for record in manifest.records}),
        "manifest_digest": manifest.digest,
        "records": len(manifest.records),
        "revocations": len(manifest.revocations),
        "schema_version": 2,
        "structurally_valid": True,
        "targets": sorted({record.target for record in manifest.records}),
        "trusted_identity_matched": True,
    }


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("manifest", type=Path)
    parser.add_argument("--expected-manifest-digest", required=True)
    parser.add_argument("--allow-empty", action="store_true")
    parser.add_argument("--environment-digest")
    parser.add_argument("--toolchain-digest")
    parser.add_argument("--artifact-digest", action="append", default=[])
    return parser


def main() -> int:
    args = _parser().parse_args()
    manifest = QualificationManifest.from_json(
        _read_regular_file(args.manifest, MAXIMUM_MANIFEST_BYTES)
    )
    summary = verify_manifest(
        manifest,
        expected_manifest_digest=args.expected_manifest_digest,
        allow_empty=args.allow_empty,
        environment_digest=args.environment_digest,
        toolchain_digest=args.toolchain_digest,
        artifact_digests=frozenset(args.artifact_digest),
    )
    print(json.dumps(summary, sort_keys=True, separators=(",", ":")))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
