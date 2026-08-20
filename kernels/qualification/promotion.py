# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Pure promotion policy; publishing the resulting record is a separate authority."""

from __future__ import annotations

from dataclasses import dataclass

from kernels.manifest import QualificationRecord
from kernels.qualification.evidence import QualificationEvidence


@dataclass(frozen=True, slots=True)
class PromotionPolicy:
    minimum_speedup: float = 1.05
    maximum_relative_mad: float = 0.10
    require_sanitizer: bool = True

    def evaluate(self, evidence: QualificationEvidence) -> tuple[str, ...]:
        failures: list[str] = []
        if not evidence.numerical.passed:
            failures.append("numerical")
        if self.require_sanitizer and not evidence.numerical.sanitizer_passed:
            failures.append("sanitizer")
        if evidence.performance.speedup < self.minimum_speedup:
            failures.append("speedup")
        if evidence.performance.relative_mad > self.maximum_relative_mad:
            failures.append("variance")
        return tuple(failures)


def qualification_candidate(
    evidence: QualificationEvidence,
    *,
    policy: PromotionPolicy,
    target: str,
    architecture: str,
    toolchain: str,
    approved_by: str,
    created_at: str,
) -> QualificationRecord:
    failures = policy.evaluate(evidence)
    if failures:
        raise ValueError(f"qualification policy failed: {', '.join(failures)}")
    return QualificationRecord(
        request_digest=evidence.request_digest,
        implementation_digest=evidence.implementation_digest,
        evidence_digests=(evidence.digest,),
        target=target,
        architecture=architecture,
        toolchain=toolchain,
        approved_by=approved_by,
        created_at=created_at,
    )
