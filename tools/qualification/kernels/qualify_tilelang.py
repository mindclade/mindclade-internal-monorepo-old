# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Evaluate paired evidence and emit an unsigned candidate manifest."""

from __future__ import annotations

import argparse
import contextlib
import json
import os
import stat
import tempfile
from pathlib import Path

from kernels.manifest import QualificationManifest
from kernels.qualification.evidence import QualificationEvidence
from kernels.qualification.promotion import PromotionPolicy, qualification_candidates

MAXIMUM_EVIDENCE_BYTES = 16 * 1024 * 1024
MAXIMUM_MANIFEST_BYTES = 8 * 1024 * 1024


def _read_regular_file(path: Path, maximum_bytes: int) -> bytes:
    flags = os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0)
    try:
        descriptor = os.open(path, flags)
    except OSError as exc:
        raise ValueError(f"refusing unreadable or symbolic-link input {path}") from exc
    try:
        metadata = os.fstat(descriptor)
        if not stat.S_ISREG(metadata.st_mode):
            raise ValueError("qualification inputs must be regular files")
        if metadata.st_size > maximum_bytes:
            raise ValueError("qualification input exceeds its byte limit")
        with os.fdopen(descriptor, "rb", closefd=False) as handle:
            payload = handle.read(maximum_bytes + 1)
        if len(payload) > maximum_bytes:
            raise ValueError("qualification input exceeds its byte limit")
        return payload
    finally:
        os.close(descriptor)


def load_evidence(path: Path) -> QualificationEvidence:
    payload = json.loads(_read_regular_file(path, MAXIMUM_EVIDENCE_BYTES))
    if not isinstance(payload, dict):
        raise ValueError("qualification evidence must be a JSON object")
    return QualificationEvidence.from_dict(payload)


def candidate_manifest(
    inference: QualificationEvidence,
    training: QualificationEvidence,
    *,
    existing: QualificationManifest,
    policy: PromotionPolicy,
    target: str,
    architecture: str,
    toolchain: str,
    approved_by: str,
    created_at: str,
    expected_implementation_digest: str,
    expected_artifact_digest: str,
    expected_environment_digest: str,
    expected_source_revision: str,
) -> QualificationManifest:
    expected = {
        "implementation_digest": expected_implementation_digest,
        "artifact_digest": expected_artifact_digest,
        "environment_digest": expected_environment_digest,
        "source_revision": expected_source_revision,
    }
    for evidence in (inference, training):
        for field, identity in expected.items():
            if getattr(evidence, field) != identity:
                raise ValueError(f"qualification {field} does not match its trusted input")
    records = qualification_candidates(
        inference,
        training,
        policy=policy,
        target=target,
        architecture=architecture,
        toolchain=toolchain,
        approved_by=approved_by,
        created_at=created_at,
    )
    return QualificationManifest((*existing.records, *records), existing.revocations)


def _atomic_write(path: Path, payload: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    descriptor, temporary = tempfile.mkstemp(prefix=f".{path.name}.", dir=path.parent)
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8") as handle:
            handle.write(payload)
            handle.flush()
            os.fsync(handle.fileno())
        try:
            os.link(temporary, path, follow_symlinks=False)
        except FileExistsError as exc:
            raise ValueError("refusing to overwrite an immutable candidate manifest") from exc
        directory = os.open(path.parent, os.O_RDONLY | getattr(os, "O_DIRECTORY", 0))
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
    finally:
        with contextlib.suppress(FileNotFoundError):
            os.unlink(temporary)


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--inference-evidence", required=True, type=Path)
    parser.add_argument("--training-evidence", required=True, type=Path)
    parser.add_argument("--existing-manifest", type=Path)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--target", required=True)
    parser.add_argument("--architecture", required=True)
    parser.add_argument("--toolchain", required=True)
    parser.add_argument("--approved-by", required=True)
    parser.add_argument("--created-at", required=True)
    parser.add_argument("--expected-implementation-digest", required=True)
    parser.add_argument("--expected-artifact-digest", required=True)
    parser.add_argument("--expected-environment-digest", required=True)
    parser.add_argument("--expected-source-revision", required=True)
    return parser


def main() -> int:
    args = _parser().parse_args()
    existing = (
        QualificationManifest.from_json(
            _read_regular_file(args.existing_manifest, MAXIMUM_MANIFEST_BYTES)
        )
        if args.existing_manifest is not None
        else QualificationManifest()
    )
    manifest = candidate_manifest(
        load_evidence(args.inference_evidence),
        load_evidence(args.training_evidence),
        existing=existing,
        policy=PromotionPolicy(),
        target=args.target,
        architecture=args.architecture,
        toolchain=args.toolchain,
        approved_by=args.approved_by,
        created_at=args.created_at,
        expected_implementation_digest=args.expected_implementation_digest,
        expected_artifact_digest=args.expected_artifact_digest,
        expected_environment_digest=args.expected_environment_digest,
        expected_source_revision=args.expected_source_revision,
    )
    _atomic_write(args.output, manifest.to_json())
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
