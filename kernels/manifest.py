# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Immutable, digest-addressed kernel qualification records."""

from __future__ import annotations

import hashlib
import json
from dataclasses import dataclass
from datetime import datetime
from typing import Any

from kernels.api.specs import ExecutionMode

_SHA256_LENGTH = 64


def _require_digest(value: str, field: str) -> None:
    if len(value) != _SHA256_LENGTH or any(c not in "0123456789abcdef" for c in value):
        raise ValueError(f"{field} must be a lowercase SHA-256 digest")


@dataclass(frozen=True, slots=True)
class QualificationRecord:
    """One half of an inference/training qualification evidence pair."""

    request_digest: str
    paired_request_digest: str
    execution_mode: ExecutionMode
    implementation_digest: str
    evidence_digests: tuple[str, ...]
    environment_digest: str
    toolchain_digest: str
    artifact_digest: str
    target: str
    architecture: str
    toolchain: str
    approved_by: str
    created_at: str

    def __post_init__(self) -> None:
        for value, field in (
            (self.request_digest, "request_digest"),
            (self.paired_request_digest, "paired_request_digest"),
            (self.implementation_digest, "implementation_digest"),
            (self.environment_digest, "environment_digest"),
            (self.toolchain_digest, "toolchain_digest"),
            (self.artifact_digest, "artifact_digest"),
        ):
            _require_digest(value, field)
        if self.request_digest == self.paired_request_digest:
            raise ValueError("paired requests must have distinct identities")
        if not self.evidence_digests:
            raise ValueError("at least one immutable evidence digest is required")
        if len(self.evidence_digests) != len(set(self.evidence_digests)):
            raise ValueError("evidence digests must be unique")
        for digest in self.evidence_digests:
            _require_digest(digest, "evidence_digest")
        parsed = datetime.fromisoformat(self.created_at.replace("Z", "+00:00"))
        if parsed.tzinfo is None:
            raise ValueError("created_at must include a timezone")
        if not all((self.target, self.architecture, self.toolchain, self.approved_by)):
            raise ValueError("target, architecture, toolchain, and approver are required")

    def canonical(self) -> dict[str, Any]:
        return {
            "approved_by": self.approved_by,
            "architecture": self.architecture,
            "artifact_digest": self.artifact_digest,
            "created_at": self.created_at,
            "evidence_digests": list(self.evidence_digests),
            "environment_digest": self.environment_digest,
            "execution_mode": self.execution_mode.value,
            "implementation_digest": self.implementation_digest,
            "paired_request_digest": self.paired_request_digest,
            "request_digest": self.request_digest,
            "target": self.target,
            "toolchain": self.toolchain,
            "toolchain_digest": self.toolchain_digest,
        }

    @property
    def digest(self) -> str:
        payload = json.dumps(self.canonical(), sort_keys=True, separators=(",", ":"))
        return hashlib.sha256(payload.encode()).hexdigest()


@dataclass(frozen=True, slots=True)
class RevocationRecord:
    qualification_digest: str
    reason: str
    revoked_at: str

    def __post_init__(self) -> None:
        _require_digest(self.qualification_digest, "qualification_digest")
        if not self.reason.strip():
            raise ValueError("revocation reason must be non-empty")
        parsed = datetime.fromisoformat(self.revoked_at.replace("Z", "+00:00"))
        if parsed.tzinfo is None:
            raise ValueError("revoked_at must include a timezone")

    def canonical(self) -> dict[str, str]:
        return {
            "qualification_digest": self.qualification_digest,
            "reason": self.reason,
            "revoked_at": self.revoked_at,
        }


@dataclass(frozen=True, slots=True)
class QualificationManifest:
    records: tuple[QualificationRecord, ...] = ()
    revocations: tuple[RevocationRecord, ...] = ()

    def __post_init__(self) -> None:
        canonical_records = tuple(
            sorted(
                self.records,
                key=lambda item: (item.request_digest, item.implementation_digest),
            )
        )
        canonical_revocations = tuple(
            sorted(self.revocations, key=lambda item: item.qualification_digest)
        )
        object.__setattr__(self, "records", canonical_records)
        object.__setattr__(self, "revocations", canonical_revocations)
        keys = [(record.request_digest, record.implementation_digest) for record in self.records]
        if len(keys) != len(set(keys)):
            raise ValueError("qualification records must have unique request/implementation keys")
        by_key = {
            (record.request_digest, record.implementation_digest): record for record in self.records
        }
        for record in self.records:
            pair = by_key.get((record.paired_request_digest, record.implementation_digest))
            if pair is None:
                raise ValueError("every qualification requires its paired execution-mode record")
            if pair.paired_request_digest != record.request_digest:
                raise ValueError("qualification pairs must be reciprocal")
            if pair.execution_mode == record.execution_mode:
                raise ValueError("qualification pairs must cover inference and training")
            shared = (
                "implementation_digest",
                "evidence_digests",
                "environment_digest",
                "toolchain_digest",
                "artifact_digest",
                "target",
                "architecture",
                "toolchain",
                "approved_by",
            )
            if any(getattr(pair, field) != getattr(record, field) for field in shared):
                raise ValueError("qualification pairs must share immutable evidence identity")
        qualification_digests = {record.digest for record in self.records}
        revoked = [record.qualification_digest for record in self.revocations]
        if len(revoked) != len(set(revoked)):
            raise ValueError("qualification revocations must be unique")
        if not set(revoked).issubset(qualification_digests):
            raise ValueError("revocations must refer to records present in the manifest")

    @property
    def revoked_digests(self) -> frozenset[str]:
        return frozenset(item.qualification_digest for item in self.revocations)

    def qualification(
        self, request_digest: str, implementation_digest: str
    ) -> QualificationRecord | None:
        for record in self.records:
            if (
                record.request_digest == request_digest
                and record.implementation_digest == implementation_digest
            ):
                pair = next(
                    (
                        candidate
                        for candidate in self.records
                        if candidate.request_digest == record.paired_request_digest
                        and candidate.implementation_digest == implementation_digest
                    ),
                    None,
                )
                if record.digest in self.revoked_digests or (
                    pair is not None and pair.digest in self.revoked_digests
                ):
                    return None
                return record
        return None

    def to_json(self) -> str:
        payload = {
            "records": [
                record.canonical()
                for record in sorted(
                    self.records,
                    key=lambda item: (item.request_digest, item.implementation_digest),
                )
            ],
            "revocations": [
                record.canonical()
                for record in sorted(
                    self.revocations,
                    key=lambda item: item.qualification_digest,
                )
            ],
            "schema_version": 2,
        }
        return json.dumps(payload, sort_keys=True, separators=(",", ":"))

    @property
    def digest(self) -> str:
        """Content identity for the complete canonical manifest."""

        return hashlib.sha256(self.to_json().encode()).hexdigest()

    @classmethod
    def from_json(cls, payload: str | bytes) -> QualificationManifest:
        raw = json.loads(payload)
        if not isinstance(raw, dict):
            raise ValueError("qualification manifest must be a JSON object")
        expected_keys = {"schema_version", "records", "revocations"}
        if set(raw) != expected_keys:
            raise ValueError("qualification manifest contains missing or unknown fields")
        if raw.get("schema_version") != 2:
            raise ValueError("unsupported qualification manifest schema")
        if not isinstance(raw["records"], list) or not isinstance(raw["revocations"], list):
            raise ValueError("qualification records and revocations must be JSON arrays")
        records = tuple(
            QualificationRecord(
                **{
                    **item,
                    "evidence_digests": tuple(item["evidence_digests"]),
                    "execution_mode": ExecutionMode(item["execution_mode"]),
                }
            )
            for item in raw.get("records", [])
        )
        revocations = tuple(RevocationRecord(**item) for item in raw.get("revocations", []))
        return cls(records=records, revocations=revocations)
