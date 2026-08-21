# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Aggregate-only, deterministic data-quality evidence."""

from __future__ import annotations

import datetime as dt
import hashlib
import json
import math
import re
from dataclasses import dataclass, field
from enum import StrEnum

_DIGEST = re.compile(r"sha256:[0-9a-f]{64}")
_CODE = re.compile(r"[a-z][a-z0-9._-]{0,127}")


class Severity(StrEnum):
    INFO = "info"
    WARNING = "warning"
    ERROR = "error"
    BLOCKING = "blocking"


@dataclass(frozen=True, slots=True, order=True)
class QualityFinding:
    code: str
    severity: Severity
    scope: str
    count: int
    detail: str

    def __post_init__(self) -> None:
        if not _CODE.fullmatch(self.code):
            raise ValueError("quality finding code is invalid")
        if not isinstance(self.severity, Severity):
            raise ValueError("quality finding severity is invalid")
        if not self.scope or len(self.scope) > 256 or any(ord(c) < 0x20 for c in self.scope):
            raise ValueError("quality finding scope is invalid")
        if isinstance(self.count, bool) or not isinstance(self.count, int) or self.count < 0:
            raise ValueError("quality finding count is invalid")
        if not self.detail or len(self.detail) > 1024 or any(ord(c) < 0x20 for c in self.detail):
            raise ValueError("quality finding detail is invalid")


@dataclass(frozen=True, slots=True, order=True)
class QualityMetric:
    name: str
    value: float
    unit: str

    def __post_init__(self) -> None:
        if not _CODE.fullmatch(self.name) or not _CODE.fullmatch(self.unit):
            raise ValueError("quality metric name/unit is invalid")
        if isinstance(self.value, bool) or not math.isfinite(self.value):
            raise ValueError("quality metric value must be finite")


@dataclass(frozen=True, slots=True)
class QualityReport:
    dataset_manifest_digest: str
    policy_version: str
    evaluated_at: dt.datetime
    findings: tuple[QualityFinding, ...] = field(default_factory=tuple)
    metrics: tuple[QualityMetric, ...] = field(default_factory=tuple)

    def __post_init__(self) -> None:
        if not _DIGEST.fullmatch(self.dataset_manifest_digest):
            raise ValueError("quality report dataset digest is invalid")
        if not _CODE.fullmatch(self.policy_version):
            raise ValueError("quality report policy version is invalid")
        if (
            not isinstance(self.evaluated_at, dt.datetime)
            or self.evaluated_at.tzinfo is None
            or self.evaluated_at.utcoffset() is None
        ):
            raise ValueError("quality report evaluated_at must be timezone-aware")
        findings = tuple(sorted(self.findings))
        metrics = tuple(sorted(self.metrics))
        if len(findings) > 100_000 or len(metrics) > 10_000:
            raise ValueError("quality report exceeds evidence bounds")
        if len(set(findings)) != len(findings) or len({item.name for item in metrics}) != len(
            metrics
        ):
            raise ValueError("quality report evidence must be unique")
        object.__setattr__(self, "findings", findings)
        object.__setattr__(self, "metrics", metrics)

    @property
    def passed(self) -> bool:
        return all(
            finding.severity not in {Severity.ERROR, Severity.BLOCKING} for finding in self.findings
        )

    def canonical_document(self) -> str:
        value = {
            "schema_version": 1,
            "dataset_manifest_digest": self.dataset_manifest_digest,
            "policy_version": self.policy_version,
            "evaluated_at": self.evaluated_at.astimezone(dt.UTC).isoformat(),
            "passed": self.passed,
            "findings": [
                {
                    "code": item.code,
                    "severity": item.severity.value,
                    "scope": item.scope,
                    "count": item.count,
                    "detail": item.detail,
                }
                for item in self.findings
            ],
            "metrics": [
                {"name": item.name, "value": item.value, "unit": item.unit} for item in self.metrics
            ],
        }
        return json.dumps(value, sort_keys=True, separators=(",", ":")) + "\n"

    @property
    def digest(self) -> str:
        return "sha256:" + hashlib.sha256(self.canonical_document().encode()).hexdigest()
