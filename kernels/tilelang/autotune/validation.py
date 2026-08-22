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
        _require_sha256("candidate_digest", self.candidate_digest)
        if not isinstance(self.status, CandidateStatus):
            raise TypeError("status must be a CandidateStatus")
        if self.failure_digest is not None:
            _require_sha256("failure_digest", self.failure_digest)
        if self.generated_source_digest is not None:
            _require_sha256("generated_source_digest", self.generated_source_digest)
        if self.latency is not None and not isinstance(self.latency, LatencyDistribution):
            raise TypeError("latency must be a LatencyDistribution")

        if self.status == CandidateStatus.PASSED:
            if self.latency is None or self.generated_source_digest is None:
                raise ValueError("passing candidates require latency and generated source identity")
            if self.failure_digest is not None:
                raise ValueError("passing candidates cannot carry a failure identity")
        elif self.latency is not None or self.generated_source_digest is not None:
            raise ValueError("failed candidates cannot carry latency or generated source identity")


def _require_sha256(name: str, value: object) -> None:
    if (
        not isinstance(value, str)
        or len(value) != 64
        or any(character not in "0123456789abcdef" for character in value)
    ):
        raise ValueError(f"{name} must be a lowercase SHA-256 digest")
