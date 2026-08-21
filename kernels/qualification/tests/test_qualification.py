# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

import hashlib

import pytest

from kernels.qualification.evidence import QualificationEvidence
from kernels.qualification.numerical import NumericalEvidence
from kernels.qualification.performance import PerformanceEvidence
from kernels.qualification.promotion import PromotionPolicy, qualification_candidate


def _digest(value: str) -> str:
    return hashlib.sha256(value.encode()).hexdigest()


def _evidence(*, speedup: float = 1.25) -> QualificationEvidence:
    numerical = NumericalEvidence(
        cases=64,
        seeds=(0, 1, 17),
        rtol=0.02,
        atol=0.02,
        max_absolute_error=0.005,
        max_relative_error=0.01,
        forward_passed=True,
        gradient_required=True,
        gradient_passed=True,
        determinism_passed=True,
        sanitizer_passed=True,
    )
    performance = PerformanceEvidence(
        samples=50,
        warmup=10,
        candidate_median_ms=1.0,
        baseline_median_ms=speedup,
        candidate_p90_ms=1.03,
        relative_mad=0.02,
        compile_ms=100.0,
    )
    return QualificationEvidence(
        _digest("request"),
        _digest("implementation"),
        "git:0123456789abcdef",
        _digest("generated"),
        _digest("environment"),
        numerical,
        performance,
        _digest("soak"),
    )


def test_policy_creates_content_addressed_candidate_but_does_not_publish() -> None:
    record = qualification_candidate(
        _evidence(),
        policy=PromotionPolicy(),
        target="cuda",
        architecture="sm_90",
        toolchain="tilelang-0.1.13",
        approved_by="kernel-review@example.test",
        created_at="2026-08-20T12:00:00Z",
    )
    assert len(record.digest) == 64
    assert record.target == "cuda"
    assert record.environment_digest == _digest("environment")


def test_policy_rejects_insufficient_speedup() -> None:
    with pytest.raises(ValueError, match="speedup"):
        qualification_candidate(
            _evidence(speedup=1.01),
            policy=PromotionPolicy(minimum_speedup=1.05),
            target="cuda",
            architecture="sm_90",
            toolchain="tilelang-0.1.13",
            approved_by="kernel-review@example.test",
            created_at="2026-08-20T12:00:00Z",
        )


def test_failed_gradient_evidence_is_auditable_but_does_not_pass() -> None:
    evidence = NumericalEvidence(
        cases=1,
        seeds=(1,),
        rtol=1e-3,
        atol=1e-3,
        max_absolute_error=0.0,
        max_relative_error=0.0,
        forward_passed=True,
        gradient_required=True,
        gradient_passed=False,
        determinism_passed=True,
        sanitizer_passed=True,
    )

    assert not evidence.passed
    assert len(evidence.digest) == 64
