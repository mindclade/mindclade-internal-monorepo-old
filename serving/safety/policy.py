# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Immutable serving-safety policy and decision vocabulary."""

from __future__ import annotations

from dataclasses import dataclass
from enum import IntEnum, StrEnum


class Decision(StrEnum):
    ALLOW = "allow"
    REVIEW = "review"
    DENY = "deny"


class Severity(IntEnum):
    INFORMATIONAL = 1
    LOW = 2
    MEDIUM = 3
    HIGH = 4
    CRITICAL = 5


@dataclass(frozen=True, slots=True)
class SafetyPolicy:
    digest: str
    revision: int
    required_screeners: tuple[str, ...]
    review_at: Severity = Severity.MEDIUM
    deny_at: Severity = Severity.HIGH
    fail_closed: bool = True
    maximum_findings: int = 128

    def validate(self) -> None:
        if not self.digest.startswith("sha256:") or len(self.digest) != 71:
            raise ValueError("safety policy digest is invalid")
        if isinstance(self.revision, bool) or self.revision <= 0:
            raise ValueError("safety policy revision must be positive")
        if not self.required_screeners or len(self.required_screeners) > 64:
            raise ValueError("safety policy screener count is outside bounds")
        if tuple(sorted(set(self.required_screeners))) != self.required_screeners:
            raise ValueError("required screeners must be sorted and unique")
        if not isinstance(self.review_at, Severity) or not isinstance(self.deny_at, Severity):
            raise ValueError("safety thresholds are invalid")
        if self.review_at > self.deny_at:
            raise ValueError("review threshold exceeds deny threshold")
        if isinstance(self.maximum_findings, bool) or not 1 <= self.maximum_findings <= 4096:
            raise ValueError("safety finding limit is outside bounds")
