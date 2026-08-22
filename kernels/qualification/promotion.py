# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Pure promotion policy; publishing records remains a separate authority."""

from __future__ import annotations

import hashlib
import math
from dataclasses import dataclass

from kernels.api.specs import ExecutionMode
from kernels.manifest import QualificationRecord
from kernels.qualification.evidence import QualificationEvidence


@dataclass(frozen=True, slots=True)
class PromotionPolicy:
    minimum_speedup: float = 1.05
    maximum_relative_mad: float = 0.05
    maximum_p95_ratio: float = 1.0
    maximum_memory_ratio: float = 1.0
    minimum_samples: int = 32
    minimum_process_repeats: int = 3
    required_sanitizers: frozenset[str] = frozenset(
        {"memcheck", "racecheck", "initcheck", "synccheck"}
    )

    def __post_init__(self) -> None:
        ratios = (
            self.minimum_speedup,
            self.maximum_relative_mad,
            self.maximum_p95_ratio,
            self.maximum_memory_ratio,
        )
        if any(
            isinstance(value, bool)
            or not isinstance(value, int | float)
            or not math.isfinite(value)
            or value <= 0
            for value in ratios
        ):
            raise ValueError("promotion ratios must be finite and positive")
        if self.minimum_speedup <= 1.0:
            raise ValueError("minimum_speedup must require a measurable improvement")
        if self.maximum_relative_mad > 1.0:
            raise ValueError("maximum_relative_mad must not exceed one")
        if (
            isinstance(self.minimum_samples, bool)
            or not isinstance(self.minimum_samples, int)
            or self.minimum_samples < 5
        ):
            raise ValueError("minimum_samples must be an integer of at least five")
        if (
            isinstance(self.minimum_process_repeats, bool)
            or not isinstance(self.minimum_process_repeats, int)
            or self.minimum_process_repeats <= 0
        ):
            raise ValueError("minimum_process_repeats must be a positive integer")
        if not self.required_sanitizers or any(
            not isinstance(tool, str) or not tool.strip() for tool in self.required_sanitizers
        ):
            raise ValueError("required_sanitizers must contain named tools")

    def evaluate(self, evidence: QualificationEvidence) -> tuple[str, ...]:
        failures: list[str] = []
        if not evidence.numerical.passed:
            failures.append("numerical")
        if not self.required_sanitizers.issubset(evidence.numerical.sanitizer_tools):
            failures.append("sanitizer_coverage")
        if not evidence.candidate_executed:
            if not evidence.fallback_verified:
                failures.append("fallback")
            return tuple(failures)
        if evidence.performance.samples < self.minimum_samples:
            failures.append("samples")
        if evidence.performance.process_repeats < self.minimum_process_repeats:
            failures.append("process_repeats")
        if evidence.performance.speedup < self.minimum_speedup:
            failures.append("speedup")
        if evidence.performance.relative_mad > self.maximum_relative_mad:
            failures.append("variance")
        if evidence.performance.p95_ratio > self.maximum_p95_ratio:
            failures.append("p95")
        if evidence.performance.memory_ratio > self.maximum_memory_ratio:
            failures.append("memory")
        return tuple(failures)


def qualification_candidates(
    inference: QualificationEvidence,
    training: QualificationEvidence,
    *,
    policy: PromotionPolicy,
    target: str,
    architecture: str,
    toolchain: str,
    approved_by: str,
    created_at: str,
) -> tuple[QualificationRecord, QualificationRecord]:
    """Create an inseparable reciprocal pair after every gate passes."""

    if any(
        not isinstance(value, str) or not value.strip()
        for value in (target, architecture, toolchain, approved_by, created_at)
    ):
        raise ValueError("target, architecture, toolchain, and approver are required")
    if target != inference.request.target or architecture != inference.request.architecture:
        raise ValueError("promotion target must match the canonical qualification request")
    if toolchain != inference.toolchain:
        raise ValueError("promotion toolchain must match the canonical evidence toolchain")
    expected_toolchain_digest = hashlib.sha256(toolchain.encode()).hexdigest()
    if inference.toolchain_digest != expected_toolchain_digest:
        raise ValueError("toolchain digest does not match the declared toolchain")
    if (
        inference.execution_mode != ExecutionMode.INFERENCE
        or training.execution_mode != ExecutionMode.TRAINING
    ):
        raise ValueError("qualification requires inference evidence followed by training evidence")
    if (
        inference.paired_request_digest != training.request_digest
        or training.paired_request_digest != inference.request_digest
    ):
        raise ValueError("qualification evidence pairs must be reciprocal")
    shared_fields = (
        "implementation_digest",
        "source_revision",
        "generated_source_digest",
        "artifact_digest",
        "toolchain",
        "toolchain_digest",
        "environment_digest",
        "soak_digest",
        "attestation_digest",
    )
    if any(getattr(inference, field) != getattr(training, field) for field in shared_fields):
        raise ValueError("qualification evidence pairs must share immutable runtime identity")

    failures = tuple(
        f"{evidence.execution_mode.value}:{failure}"
        for evidence in (inference, training)
        for failure in policy.evaluate(evidence)
    )
    if failures:
        raise ValueError(f"qualification policy failed: {', '.join(failures)}")

    evidence_digests = (inference.digest, training.digest)
    return (
        QualificationRecord(
            request_digest=inference.request_digest,
            paired_request_digest=training.request_digest,
            execution_mode=ExecutionMode.INFERENCE,
            implementation_digest=inference.implementation_digest,
            evidence_digests=evidence_digests,
            environment_digest=inference.environment_digest,
            toolchain_digest=inference.toolchain_digest,
            artifact_digest=inference.artifact_digest,
            target=target,
            architecture=architecture,
            toolchain=toolchain,
            approved_by=approved_by,
            created_at=created_at,
        ),
        QualificationRecord(
            request_digest=training.request_digest,
            paired_request_digest=inference.request_digest,
            execution_mode=ExecutionMode.TRAINING,
            implementation_digest=inference.implementation_digest,
            evidence_digests=evidence_digests,
            environment_digest=inference.environment_digest,
            toolchain_digest=inference.toolchain_digest,
            artifact_digest=inference.artifact_digest,
            target=target,
            architecture=architecture,
            toolchain=toolchain,
            approved_by=approved_by,
            created_at=created_at,
        ),
    )
