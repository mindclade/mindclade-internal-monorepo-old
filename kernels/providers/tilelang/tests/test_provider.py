# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

import os
import threading
from concurrent.futures import ThreadPoolExecutor
from dataclasses import replace

import pytest

from kernels.api import ExecutionMode, KernelRequest, TensorLayout, TensorSpec
from kernels.defaults import default_registry
from kernels.providers.tilelang import registry as tilelang_registry
from kernels.providers.tilelang.adapter import CompiledKernelCache
from kernels.providers.tilelang.capabilities import _required_environment
from kernels.providers.tilelang.fused import BASELINE_ELEMENTWISE
from kernels.providers.tilelang.manifest import validate_runtime_versions
from kernels.tilelang.targets import HOPPER


def test_compilation_cache_is_single_flight_and_failures_are_retryable() -> None:
    cache = CompiledKernelCache(max_entries=2)
    started = threading.Event()
    release = threading.Event()
    calls = 0
    calls_lock = threading.Lock()
    compiled = object()

    def compile_once() -> object:
        nonlocal calls
        with calls_lock:
            calls += 1
        started.set()
        assert release.wait(timeout=5)
        return compiled

    with ThreadPoolExecutor(max_workers=8) as executor:
        futures = [executor.submit(cache.get_or_compile, "shared", compile_once) for _ in range(8)]
        assert started.wait(timeout=5)
        release.set()
        assert all(future.result(timeout=5) is compiled for future in futures)
    assert calls == 1

    def fail() -> object:
        raise RuntimeError("compiler failed")

    with pytest.raises(RuntimeError, match="compiler failed"):
        cache.get_or_compile("retry", fail)
    recovered = object()
    assert cache.get_or_compile("retry", lambda: recovered) is recovered


def test_compilation_cache_is_bounded_and_resets_in_forked_children(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    callbacks: dict[str, object] = {}

    def register_at_fork(**values: object) -> None:
        callbacks.update(values)

    monkeypatch.setattr(os, "register_at_fork", register_at_fork)
    cache = CompiledKernelCache(max_entries=2)
    cache.get_or_compile("a", object)
    cache.get_or_compile("b", object)
    cache.get_or_compile("c", object)
    assert len(cache) == 2
    callback = callbacks["after_in_child"]
    assert callable(callback)
    callback()
    assert len(cache) == 0


def test_runtime_manifest_rejects_unreviewed_versions() -> None:
    validate_runtime_versions("0.1.13", "0.1.11")
    validate_runtime_versions("0.1.13", "0.1.12+local")
    with pytest.raises(ValueError, match="exactly"):
        validate_runtime_versions("0.1.14", "0.1.12")
    with pytest.raises(ValueError, match="must satisfy"):
        validate_runtime_versions("0.1.13", "0.1.13")


def test_runtime_identity_requires_explicit_immutable_environment(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.delenv("MINDCLADE_RUNTIME_IMAGE_DIGEST", raising=False)
    with pytest.raises(RuntimeError, match="MINDCLADE_RUNTIME_IMAGE_DIGEST"):
        _required_environment("MINDCLADE_RUNTIME_IMAGE_DIGEST", oci_digest=True)
    monkeypatch.setenv("MINDCLADE_RUNTIME_IMAGE_DIGEST", "sha256:not-a-digest")
    with pytest.raises(RuntimeError, match="64 lowercase"):
        _required_environment("MINDCLADE_RUNTIME_IMAGE_DIGEST", oci_digest=True)
    expected = f"sha256:{'a' * 64}"
    monkeypatch.setenv("MINDCLADE_RUNTIME_IMAGE_DIGEST", expected)
    assert _required_environment("MINDCLADE_RUNTIME_IMAGE_DIGEST", oci_digest=True) == expected


def test_registry_exposes_only_reviewed_hopper_runtime_variants() -> None:
    registry = default_registry()
    tilelang = [
        candidate
        for operation in (
            "attention.sdpa",
            "diffusion.modulated_residual",
            "fp8.scaled_gemm",
            "fused.swiglu",
            "moe.grouped_gemm",
            "pairformer.triangle_incoming",
            "pairformer.triangle_outgoing",
        )
        for candidate in registry.candidates(operation)
        if candidate.identity.provider.value == "tilelang"
    ]
    assert tilelang
    assert all(".sm_90" in candidate.identity.name for candidate in tilelang)
    assert any(candidate.identity.name.endswith(".causal") for candidate in tilelang)
    assert any(candidate.identity.name.endswith(".silu") for candidate in tilelang)
    assert len({candidate.identity.digest for candidate in tilelang}) == len(tilelang)


def test_swiglu_invoke_compiles_once_and_calls_the_compiled_kernel(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    tilelang_registry._COMPILED_KERNELS.clear()
    gate = object()
    up = object()
    result = object()
    compilations: list[tuple[object, str, object]] = []

    def compile_kernel(
        schedule: object,
        *,
        dtype: str,
        target: object,
    ) -> object:
        compilations.append((schedule, dtype, target))

        def invoke(actual_gate: object, actual_up: object) -> object:
            assert actual_gate is gate
            assert actual_up is up
            return result

        return invoke

    monkeypatch.setattr(tilelang_registry, "make_swiglu_kernel", compile_kernel)
    target = {"kind": "cuda", "architecture": "sm_90"}

    assert (
        tilelang_registry._invoke_swiglu(
            gate,
            up,
            schedule=BASELINE_ELEMENTWISE,
            target=target,
        )
        is result
    )
    assert (
        tilelang_registry._invoke_swiglu(
            gate,
            up,
            schedule=BASELINE_ELEMENTWISE,
            target=target,
        )
        is result
    )
    assert compilations == [(BASELINE_ELEMENTWISE, "bfloat16", target)]
    tilelang_registry._COMPILED_KERNELS.clear()


def test_attention_contract_binds_semantics_layout_and_execution_mode() -> None:
    registry = default_registry()
    candidate = next(
        item
        for item in registry.candidates("attention.sdpa")
        if item.identity.name == "tilelang.attention.sm_90.float16.dense"
    )
    q = TensorSpec((2, 8, 65, 64), "float16", TensorLayout.BHSD, alignment=16)
    kv = TensorSpec((2, 8, 79, 64), "float16", TensorLayout.BHSD, alignment=16)
    request = KernelRequest(
        "attention.sdpa",
        (q, kv, kv),
        (q,),
        "cuda",
        "sm_90",
        (("causal", "false"), ("mask", "none"), ("scale", "default")),
    )
    assert candidate.rejection_reason(request, HOPPER.capabilities) is None
    causal = replace(
        request,
        semantics=(("causal", "true"), ("mask", "none"), ("scale", "default")),
    )
    assert candidate.rejection_reason(causal, HOPPER.capabilities) == "attention_causal_variant"
    training = replace(
        request,
        execution_mode=ExecutionMode.TRAINING,
        gradient_inputs=(0, 1, 2),
    )
    assert candidate.rejection_reason(training, HOPPER.capabilities) == "execution_mode"


def test_scaled_gemm_contract_accepts_mn_tails_but_rejects_signature_drift() -> None:
    registry = default_registry()
    candidate = next(
        item
        for item in registry.candidates("fp8.scaled_gemm")
        if item.identity.name == "tilelang.scaled_gemm.sm_90.float16.none"
    )
    a = TensorSpec((130, 48), "float16", alignment=16)
    b = TensorSpec((48, 70), "float16", alignment=16)
    scale = TensorSpec((1,), "float32", alignment=4)
    output = TensorSpec((130, 70), "bfloat16", alignment=16)
    request = KernelRequest(
        "fp8.scaled_gemm",
        (a, b, scale, scale),
        (output,),
        "cuda",
        "sm_90",
        (("activation", "none"), ("scale_granularity", "per_tensor")),
    )
    assert candidate.rejection_reason(request, HOPPER.capabilities) is None
    assert (
        candidate.rejection_reason(replace(request, inputs=(a, b)), HOPPER.capabilities)
        == "input_arity"
    )
    fp32_output = replace(output, dtype="float32")
    assert (
        candidate.rejection_reason(
            replace(request, outputs=(fp32_output,)),
            HOPPER.capabilities,
        )
        == "output_dtype"
    )
    bfloat16_a = replace(a, dtype="bfloat16")
    bfloat16_b = replace(b, dtype="bfloat16")
    assert (
        candidate.rejection_reason(
            replace(request, inputs=(bfloat16_a, bfloat16_b, scale, scale)),
            HOPPER.capabilities,
        )
        == "input_dtype"
    )
