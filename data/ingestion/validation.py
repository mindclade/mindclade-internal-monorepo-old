# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Stable rejection evidence for canonical ingestion records."""

from __future__ import annotations

from dataclasses import dataclass

from data.contracts import DatasetContract, ValidationIssue, validate_record

from .record import CanonicalRecord


@dataclass(frozen=True, slots=True, order=True)
class RejectedRecord:
    source_record_digest: str
    issues: tuple[ValidationIssue, ...]

    def __post_init__(self) -> None:
        if not self.issues:
            raise ValueError("rejected record requires at least one issue")


def validate_canonical(
    record: CanonicalRecord, contract: DatasetContract
) -> tuple[ValidationIssue, ...]:
    if not isinstance(record, CanonicalRecord):
        raise TypeError("record must be a CanonicalRecord")
    if not isinstance(contract, DatasetContract):
        raise TypeError("contract must be a DatasetContract")
    return validate_record(record.values, contract)
