# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Immutable, digest-addressed kernel qualification records."""

from __future__ import annotations

import hashlib
import json
from dataclasses import dataclass
from datetime import datetime
from typing import Any

_SHA256_LENGTH = 64


def _require_digest(value: str, field: str) -> None:
    if len(value) != _SHA256_LENGTH or any(c not in "0123456789abcdef" for c in value):
        raise ValueError(f"{field} must be a lowercase SHA-256 digest")


@dataclass(frozen=True, slots=True)
class QualificationRecord:
    request_digest: str
    implementation_digest: str
    evidence_digests: tuple[str, ...]
    environment_digest: str
    target: str
    architecture: str
    toolchain: str
    approved_by: str
    created_at: str

    def __post_init__(self) -> None:
        _require_digest(self.request_digest, "request_digest")
        _require_digest(self.implementation_digest, "implementation_digest")
        if not self.evidence_digests:
            raise ValueError("at least one immutable evidence digest is required")
        for digest in self.evidence_digests:
            _require_digest(digest, "evidence_digest")
        _require_digest(self.environment_digest, "environment_digest")
        parsed = datetime.fromisoformat(self.created_at.replace("Z", "+00:00"))
        if parsed.tzinfo is None:
            raise ValueError("created_at must include a timezone")
        if not all((self.target, self.architecture, self.toolchain, self.approved_by)):
            raise ValueError("target, architecture, toolchain, and approver are required")

    def canonical(self) -> dict[str, Any]:
        return {
            "approved_by": self.approved_by,
            "architecture": self.architecture,
            "created_at": self.created_at,
            "evidence_digests": list(self.evidence_digests),
            "environment_digest": self.environment_digest,
            "implementation_digest": self.implementation_digest,
            "request_digest": self.request_digest,
            "target": self.target,
            "toolchain": self.toolchain,
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


@dataclass(frozen=True, slots=True)
class QualificationManifest:
    records: tuple[QualificationRecord, ...] = ()
    revocations: tuple[RevocationRecord, ...] = ()

    def __post_init__(self) -> None:
        keys = [(record.request_digest, record.implementation_digest) for record in self.records]
        if len(keys) != len(set(keys)):
            raise ValueError("qualification records must have unique request/implementation keys")

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
                and record.digest not in self.revoked_digests
            ):
                return record
        return None

    @classmethod
    def from_json(cls, payload: str | bytes) -> QualificationManifest:
        raw = json.loads(payload)
        if raw.get("schema_version") != 1:
            raise ValueError("unsupported qualification manifest schema")
        records = tuple(QualificationRecord(**item) for item in raw.get("records", []))
        revocations = tuple(RevocationRecord(**item) for item in raw.get("revocations", []))
        return cls(records=records, revocations=revocations)
