# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Deterministic ingestion output evidence; this module does not publish."""

from __future__ import annotations

import hashlib
import json
from dataclasses import dataclass

from .record import CanonicalRecord
from .validation import RejectedRecord


@dataclass(frozen=True, slots=True)
class IngestionResult:
    records: tuple[CanonicalRecord, ...]
    rejected: tuple[RejectedRecord, ...]
    input_records: int
    duplicate_records: int
    stage_idempotency_key: str

    def __post_init__(self) -> None:
        if self.input_records != len(self.records) + len(self.rejected) + self.duplicate_records:
            raise ValueError("ingestion result counts do not reconcile")
        if self.input_records < 0 or self.duplicate_records < 0:
            raise ValueError("ingestion result counts must be non-negative")
        if not self.stage_idempotency_key or len(self.stage_idempotency_key) > 1024:
            raise ValueError("ingestion idempotency key is invalid")

    def canonical_document(self) -> str:
        value = {
            "schema_version": 1,
            "stage_idempotency_key": self.stage_idempotency_key,
            "input_records": self.input_records,
            "accepted_records": len(self.records),
            "rejected_records": len(self.rejected),
            "duplicate_records": self.duplicate_records,
            "records": [
                {"source_digest": record.source_record_digest, "digest": record.digest}
                for record in self.records
            ],
            "rejections": [
                {
                    "source_digest": rejection.source_record_digest,
                    "issues": [
                        {"field": issue.field, "code": issue.code, "detail": issue.detail}
                        for issue in rejection.issues
                    ],
                }
                for rejection in self.rejected
            ],
        }
        return json.dumps(value, sort_keys=True, separators=(",", ":")) + "\n"

    @property
    def manifest_digest(self) -> str:
        return "sha256:" + hashlib.sha256(self.canonical_document().encode("utf-8")).hexdigest()
