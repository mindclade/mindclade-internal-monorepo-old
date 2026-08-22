# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

from dataclasses import dataclass
from enum import StrEnum

from kernels.tilelang.autotune.objective import LatencyDistribution


class CandidateStatus(StrEnum):
    PASSED = "passed"
    ILLEGAL = "illegal"
    COMPILE_FAILED = "compile_failed"
    PARITY_FAILED = "parity_failed"
    TIMED_OUT = "timed_out"
    BENCHMARK_FAILED = "benchmark_failed"


@dataclass(frozen=True, slots=True)
class CandidateResult:
    candidate_digest: str
    status: CandidateStatus
    latency: LatencyDistribution | None = None
    failure_digest: str | None = None
    generated_source_digest: str | None = None

    def __post_init__(self) -> None:
        if self.status == CandidateStatus.PASSED:
            if self.latency is None or self.generated_source_digest is None:
                raise ValueError("passing candidates require latency and generated source identity")
        elif self.latency is not None:
            raise ValueError("failed candidates cannot carry a latency distribution")
