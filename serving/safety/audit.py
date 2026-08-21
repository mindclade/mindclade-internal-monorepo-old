# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Content-free, bounded audit projection for safety decisions."""

from __future__ import annotations

from dataclasses import dataclass

from .screening import ScreeningResult


@dataclass(frozen=True, slots=True)
class AuditRecord:
    request_id: str
    input_digest: str
    policy_digest: str
    decision: str
    finding_codes: tuple[str, ...]
    incomplete_screeners: tuple[str, ...]


def to_audit_record(result: ScreeningResult) -> AuditRecord:
    return AuditRecord(
        result.request_id,
        result.input_digest,
        result.policy_digest,
        result.decision.value,
        tuple(f"{finding.screener}:{finding.code}" for finding in result.findings),
        result.incomplete_screeners,
    )
