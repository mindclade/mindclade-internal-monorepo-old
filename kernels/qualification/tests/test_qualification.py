# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

import hashlib
from dataclasses import replace

import pytest

from kernels.api import ExecutionMode, KernelRequest, RuntimeCompatibility, TensorSpec
from kernels.manifest import QualificationManifest
from kernels.qualification.evidence import QualificationEvidence
from kernels.qualification.numerical import NumericalEvidence
from kernels.qualification.performance import PerformanceEvidence
from kernels.qualification.promotion import PromotionPolicy, qualification_candidates
from kernels.qualification.workloads import production_workload_pairs, production_workloads


def _digest(value: str) -> str:
    return hashlib.sha256(value.encode()).hexdigest()


def _numerical(mode: ExecutionMode, *, gradient_passed: bool = True) -> NumericalEvidence:
    return NumericalEvidence(
        cases=64,
        seeds=(0, 1, 17),
        rtol=0.02,
        atol=0.02,
        max_absolute_error=0.005,
        max_relative_error=0.01,
        forward_passed=True,
        gradient_inputs=() if mode == ExecutionMode.INFERENCE else (0, 1),
        gradient_passed=gradient_passed,
        determinism_passed=True,
        adversarial_passed=True,
        sanitizer_tools=("memcheck", "racecheck", "initcheck", "synccheck"),
        sanitizer_passed=True,
        raw_results_digest=_digest(f"numerical-{mode.value}"),
    )


def _performance(*, speedup: float = 1.25) -> PerformanceEvidence:
    return PerformanceEvidence(
        samples=50,
        process_repeats=3,
        warmup=10,
        candidate_median_ms=1.0,
        baseline_median_ms=speedup,
        candidate_p90_ms=1.02,
        candidate_p95_ms=1.04,
        baseline_p95_ms=1.30,
        relative_mad=0.02,
        compile_ms=100.0,
        candidate_peak_memory_bytes=1024,
        baseline_peak_memory_bytes=2048,
        raw_results_digest=_digest(f"performance-{speedup}"),
    )


def _evidence_pair(
    *,
    speedup: float = 1.25,
) -> tuple[QualificationEvidence, QualificationEvidence]:
    value = TensorSpec((2, 2), "float32")
    inference_request = KernelRequest(
        "test.qualification",
        (value, value),
        (value,),
        "cuda",
        "sm_90",
    )
    training_request = replace(
        inference_request,
        execution_mode=ExecutionMode.TRAINING,
        gradient_inputs=(0, 1),
    )
    runtime = RuntimeCompatibility(
        target="cuda",
        architecture="sm_90",
        device_name="H100",
        device_memory_bytes=80 * 1024**3,
        driver_version="580.65",
        runtime_version="13.0",
        pytorch_version="2.9.1",
        tilelang_version="0.1.13",
        tvm_ffi_version="0.1.12",
        compiler_version="13.0",
        os_release="linux-x86_64",
        runtime_image_digest=f"sha256:{_digest('runtime-image')}",
    )
    toolchain = "tilelang-0.1.13"
    performance = _performance(speedup=speedup)

    def evidence(
        mode: ExecutionMode,
        request: KernelRequest,
        paired_request: KernelRequest,
    ) -> QualificationEvidence:
        return QualificationEvidence(
            schema_version=2,
            execution_mode=mode,
            candidate_executed=mode == ExecutionMode.INFERENCE,
            fallback_verified=mode == ExecutionMode.TRAINING,
            request=request,
            paired_request=paired_request,
            runtime_compatibility=runtime,
            toolchain=toolchain,
            request_digest=request.digest,
            paired_request_digest=paired_request.digest,
            implementation_digest=_digest("implementation"),
            source_revision=f"git:{'a' * 40}",
            generated_source_digest=_digest("generated"),
            artifact_digest=_digest("artifact"),
            toolchain_digest=_digest(toolchain),
            environment_digest=runtime.digest,
            numerical=_numerical(mode),
            performance=performance,
            soak_digest=_digest("soak"),
            attestation_digest=_digest("attestation"),
            raw_results_digest=_digest(f"raw-{mode.value}"),
        )

    return (
        evidence(ExecutionMode.INFERENCE, inference_request, training_request),
        evidence(ExecutionMode.TRAINING, training_request, inference_request),
    )


def test_policy_creates_content_addressed_pair_but_does_not_publish() -> None:
    records = qualification_candidates(
        *_evidence_pair(),
        policy=PromotionPolicy(),
        target="cuda",
        architecture="sm_90",
        toolchain="tilelang-0.1.13",
        approved_by="kernel-review@example.test",
        created_at="2026-08-20T12:00:00Z",
    )
    manifest = QualificationManifest(records)
    assert len(records[0].digest) == 64
    assert records[0].paired_request_digest == records[1].request_digest
    assert (
        manifest.qualification(
            records[0].request_digest,
            records[0].implementation_digest,
        )
        == records[0]
    )


def test_policy_rejects_insufficient_speedup_and_tail_regressions() -> None:
    with pytest.raises(ValueError, match="speedup"):
        qualification_candidates(
            *_evidence_pair(speedup=1.01),
            policy=PromotionPolicy(),
            target="cuda",
            architecture="sm_90",
            toolchain="tilelang-0.1.13",
            approved_by="kernel-review@example.test",
            created_at="2026-08-20T12:00:00Z",
        )

    inference, training = _evidence_pair()
    regressed = replace(
        inference,
        performance=replace(
            inference.performance,
            candidate_p95_ms=1.40,
            candidate_peak_memory_bytes=4096,
        ),
    )
    with pytest.raises(ValueError, match=r"inference:(p95|memory)"):
        qualification_candidates(
            regressed,
            training,
            policy=PromotionPolicy(),
            target="cuda",
            architecture="sm_90",
            toolchain="tilelang-0.1.13",
            approved_by="kernel-review@example.test",
            created_at="2026-08-20T12:00:00Z",
        )


def test_failed_gradient_evidence_is_auditable_but_does_not_pass() -> None:
    evidence = _numerical(ExecutionMode.TRAINING, gradient_passed=False)
    assert not evidence.passed
    assert len(evidence.digest) == 64


def test_inference_candidate_can_promote_only_with_verified_training_fallback() -> None:
    inference, training = _evidence_pair()
    fallback_performance = replace(
        training.performance,
        baseline_median_ms=1.0,
        baseline_p95_ms=1.04,
        candidate_peak_memory_bytes=2048,
    )
    records = qualification_candidates(
        inference,
        replace(training, performance=fallback_performance),
        policy=PromotionPolicy(),
        target="cuda",
        architecture="sm_90",
        toolchain="tilelang-0.1.13",
        approved_by="kernel-review@example.test",
        created_at="2026-08-20T12:00:00Z",
    )
    assert records[0].execution_mode == ExecutionMode.INFERENCE
    with pytest.raises(ValueError, match="verify fallback"):
        replace(training, fallback_verified=False)


def test_evidence_pair_must_be_reciprocal_and_share_runtime_identity() -> None:
    inference, training = _evidence_pair()
    with pytest.raises(ValueError, match="canonical paired request"):
        qualification_candidates(
            inference,
            replace(training, paired_request_digest=_digest("other")),
            policy=PromotionPolicy(),
            target="cuda",
            architecture="sm_90",
            toolchain="tilelang-0.1.13",
            approved_by="kernel-review@example.test",
            created_at="2026-08-20T12:00:00Z",
        )


def test_evidence_rejects_mutable_source_and_nonfinite_measurements() -> None:
    inference, _ = _evidence_pair()
    with pytest.raises(ValueError, match="immutable hexadecimal Git"):
        replace(inference, source_revision="main")
    with pytest.raises(ValueError, match="immutable hexadecimal Git"):
        replace(inference, source_revision="git:0123456789abcdef")
    with pytest.raises(ValueError, match="positive and dispersion"):
        replace(inference.performance, candidate_median_ms=float("nan"))
    with pytest.raises(ValueError, match="non-negative"):
        replace(inference.numerical, max_absolute_error=float("inf"))
    payload = inference.canonical()
    payload["candidate_executed"] = "true"
    with pytest.raises(TypeError, match="flags"):
        QualificationEvidence.from_dict(payload)
    payload = inference.canonical()
    payload["unknown"] = True
    with pytest.raises(ValueError, match="unknown"):
        QualificationEvidence.from_dict(payload)
    assert QualificationEvidence.from_dict(inference.canonical()) == inference


def test_policy_rejects_unsafe_configuration_and_toolchain_identity_drift() -> None:
    with pytest.raises(ValueError, match="measurable improvement"):
        PromotionPolicy(minimum_speedup=1.0)
    with pytest.raises(ValueError, match="toolchain"):
        qualification_candidates(
            *_evidence_pair(),
            policy=PromotionPolicy(),
            target="cuda",
            architecture="sm_90",
            toolchain="tilelang-0.1.14",
            approved_by="kernel-review@example.test",
            created_at="2026-08-20T12:00:00Z",
        )


def test_policy_rejects_target_relabeling_and_request_document_drift() -> None:
    inference, training = _evidence_pair()
    with pytest.raises(ValueError, match="canonical qualification request"):
        qualification_candidates(
            inference,
            training,
            policy=PromotionPolicy(),
            target="hip",
            architecture="gfx950",
            toolchain="tilelang-0.1.13",
            approved_by="self-asserted",
            created_at="2026-08-20T12:00:00Z",
        )
    with pytest.raises(ValueError, match="request_digest"):
        replace(inference, request_digest=_digest("different-request"))


def test_recorded_error_bounds_participate_in_numerical_pass_fail() -> None:
    evidence = _numerical(ExecutionMode.INFERENCE)
    assert evidence.passed
    assert not replace(evidence, max_absolute_error=evidence.atol * 2).passed
    assert not replace(evidence, max_relative_error=evidence.rtol * 2).passed


def test_production_workload_matrix_has_124_pairs_and_248_exact_requests() -> None:
    pairs = production_workload_pairs()
    workloads = production_workloads()
    assert len(pairs) == 124
    assert len(workloads) == 248
    assert len({request.digest for request in workloads}) == 248
    assert {request.execution_mode for request in workloads} == {
        ExecutionMode.INFERENCE,
        ExecutionMode.TRAINING,
    }
    assert {request.operation for request in workloads} == {
        "attention.sdpa",
        "diffusion.modulated_residual",
        "fp8.scaled_gemm",
        "fused.swiglu",
        "moe.grouped_gemm",
        "pairformer.triangle_incoming",
        "pairformer.triangle_outgoing",
    }
