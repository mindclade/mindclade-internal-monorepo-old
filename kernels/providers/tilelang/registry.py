# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Helpers for content-addressed TileLang registrations."""

from __future__ import annotations

import hashlib
import inspect
import json
from collections.abc import Callable
from dataclasses import asdict, is_dataclass
from functools import partial
from typing import Literal

from kernels.api.specs import ImplementationIdentity, Provider
from kernels.providers.tilelang.attention import (
    baseline_schedule as attention_schedule,
)
from kernels.providers.tilelang.attention import (
    make_flash_attention_kernel,
)
from kernels.providers.tilelang.attention.schedules import FlashAttentionSchedule
from kernels.providers.tilelang.diffusion import (
    BASELINE_DIFFUSION_EPILOGUE,
    make_modulated_residual_kernel,
)
from kernels.providers.tilelang.diffusion.schedules import DiffusionEpilogueSchedule
from kernels.providers.tilelang.fp8 import (
    baseline_schedule as gemm_schedule,
)
from kernels.providers.tilelang.fp8 import (
    make_scaled_gemm_kernel,
)
from kernels.providers.tilelang.fp8.schedules import GemmSchedule
from kernels.providers.tilelang.fused import (
    BASELINE_ELEMENTWISE,
    BASELINE_TRIANGLE,
    make_swiglu_kernel,
    make_triangle_multiplication_kernel,
)
from kernels.providers.tilelang.fused.schedules import ElementwiseSchedule, TriangleSchedule
from kernels.providers.tilelang.manifest import TILELANG_VERSION
from kernels.providers.tilelang.moe import (
    BASELINE_GROUPED_GEMM,
    GroupedGemmSchedule,
    make_grouped_gemm_kernel,
)
from kernels.providers.tilelang.policy import attention_shape, exact_eligibility, gemm_shape
from kernels.registry import Eligibility, KernelImplementation, KernelRegistry
from kernels.tilelang.targets import TARGETS, TargetSpec


def implementation_identity(
    name: str, factory: Callable[..., object], schedule_digest: str
) -> ImplementationIdentity:
    source = inspect.getsource(factory).encode()
    return ImplementationIdentity(
        provider=Provider.TILELANG,
        name=name,
        source_digest=hashlib.sha256(source).hexdigest(),
        compiler="tilelang",
        compiler_version=TILELANG_VERSION,
        schedule_digest=schedule_digest,
    )


def schedule_identity(schedule: object) -> str:
    payload = asdict(schedule) if is_dataclass(schedule) else repr(schedule)  # type: ignore[arg-type]
    return hashlib.sha256(
        json.dumps(payload, sort_keys=True, separators=(",", ":")).encode()
    ).hexdigest()


def registration(
    *,
    operation: str,
    name: str,
    factory: Callable[..., object],
    schedule_digest: str,
    invoke: Callable[..., object],
    eligibility: Eligibility,
    priority: int,
) -> KernelImplementation:
    return KernelImplementation(
        operation=operation,
        identity=implementation_identity(name, factory, schedule_digest),
        invoke=invoke,
        eligibility=eligibility,
        priority=priority,
    )


def _target_eligibility(
    target: TargetSpec,
    dtype: str,
    *,
    shape_check: Callable[..., str | None] | None = None,
) -> Eligibility:
    return exact_eligibility(
        targets=frozenset({target.kind}),
        architectures=frozenset({target.architecture}),
        dtypes=frozenset({dtype, "float32", "uint8"}),
        shape_check=shape_check,
    )


def _invoke_triangle(
    left: object,
    right: object,
    mask: object,
    *,
    schedule: TriangleSchedule,
    orientation: Literal["incoming", "outgoing"],
    target: str | dict[str, str] | None,
) -> object:
    kernel = make_triangle_multiplication_kernel(schedule, orientation=orientation, target=target)
    return kernel(left, right, mask)


def _invoke_attention(
    q: object,
    k: object,
    v: object,
    *,
    schedule: FlashAttentionSchedule,
    target: str | dict[str, str] | None,
) -> object:
    kernel = make_flash_attention_kernel(schedule, target=target)
    return kernel(q, k, v, causal=False)


def _invoke_scaled_gemm(
    a: object,
    b: object,
    a_scale: object,
    b_scale: object,
    *,
    schedule: GemmSchedule,
    target: str | dict[str, str] | None,
) -> object:
    kernel = make_scaled_gemm_kernel(schedule, target=target)
    return kernel(a, b, a_scale, b_scale)


def _invoke_swiglu(
    gate: object,
    up: object,
    *,
    schedule: ElementwiseSchedule,
    target: str | dict[str, str] | None,
) -> object:
    kernel = make_swiglu_kernel(schedule, dtype="bfloat16", target=target)
    return kernel(gate, up)


def _invoke_modulated_residual(
    normalized: object,
    residual: object,
    scale: object,
    shift: object,
    gate: object,
    *,
    schedule: DiffusionEpilogueSchedule,
    target: str | dict[str, str] | None,
) -> object:
    kernel = make_modulated_residual_kernel(schedule, dtype="bfloat16", target=target)
    return kernel(normalized, residual, scale, shift, gate)


def _invoke_grouped_gemm(
    tokens: object,
    weights: object,
    *,
    schedule: GroupedGemmSchedule,
    target: str | dict[str, str] | None,
) -> object:
    kernel = make_grouped_gemm_kernel(schedule, target=target)
    return kernel(tokens, weights)


def register_tilelang_candidates(registry: KernelRegistry) -> None:
    """Register reviewed source candidates; the dispatcher still requires evidence."""

    for target in TARGETS.values():
        target_config = target.tilelang_target
        for dtype in ("float16", "bfloat16"):
            attention = attention_schedule(target.architecture, dtype)
            if attention.rejection_reason(target, 64) is None:
                registry.register(
                    registration(
                        operation="attention.sdpa",
                        name=f"tilelang.attention.{target.architecture}.{dtype}",
                        factory=make_flash_attention_kernel,
                        schedule_digest=attention.digest,
                        invoke=partial(
                            _invoke_attention,
                            schedule=attention,
                            target=target_config,
                        ),
                        eligibility=_target_eligibility(target, dtype, shape_check=attention_shape),
                        priority=100,
                    )
                )

            triangle = BASELINE_TRIANGLE
            if target.kind == "hip":
                triangle = type(triangle)(
                    triangle.block_m,
                    triangle.block_n,
                    triangle.block_k,
                    triangle.threads,
                    1,
                    dtype,
                )
            else:
                triangle = type(triangle)(
                    triangle.block_m,
                    triangle.block_n,
                    triangle.block_k,
                    triangle.threads,
                    triangle.num_stages,
                    dtype,
                )
            if triangle.rejection_reason(target, 64) is None:
                for orientation in ("incoming", "outgoing"):
                    registry.register(
                        registration(
                            operation=f"pairformer.triangle_{orientation}",
                            name=(
                                f"tilelang.pairformer.{orientation}.{target.architecture}.{dtype}"
                            ),
                            factory=make_triangle_multiplication_kernel,
                            schedule_digest=triangle.digest,
                            invoke=partial(
                                _invoke_triangle,
                                schedule=triangle,
                                orientation=orientation,
                                target=target_config,
                            ),
                            eligibility=_target_eligibility(target, dtype),
                            priority=90,
                        )
                    )

        for dtype in ("float16", "bfloat16", "float8_e4m3fn"):
            if not target.capabilities.supports_dtype(dtype):
                continue
            gemm = gemm_schedule(target.architecture, dtype)
            if gemm.rejection_reason(target, gemm.block_k) is not None:
                continue
            registry.register(
                registration(
                    operation="fp8.scaled_gemm",
                    name=f"tilelang.scaled_gemm.{target.architecture}.{dtype}",
                    factory=make_scaled_gemm_kernel,
                    schedule_digest=gemm.digest,
                    invoke=partial(
                        _invoke_scaled_gemm,
                        schedule=gemm,
                        target=target_config,
                    ),
                    eligibility=_target_eligibility(target, dtype, shape_check=gemm_shape),
                    priority=100,
                )
            )

        registry.register(
            registration(
                operation="fused.swiglu",
                name=f"tilelang.fused.swiglu.{target.architecture}",
                factory=make_swiglu_kernel,
                schedule_digest=schedule_identity(BASELINE_ELEMENTWISE),
                invoke=partial(
                    _invoke_swiglu,
                    schedule=BASELINE_ELEMENTWISE,
                    target=target_config,
                ),
                eligibility=_target_eligibility(target, "bfloat16"),
                priority=80,
            )
        )
        registry.register(
            registration(
                operation="diffusion.modulated_residual",
                name=f"tilelang.diffusion.modulated_residual.{target.architecture}",
                factory=make_modulated_residual_kernel,
                schedule_digest=schedule_identity(BASELINE_DIFFUSION_EPILOGUE),
                invoke=partial(
                    _invoke_modulated_residual,
                    schedule=BASELINE_DIFFUSION_EPILOGUE,
                    target=target_config,
                ),
                eligibility=_target_eligibility(target, "bfloat16"),
                priority=80,
            )
        )
        if target.kind == "cuda":
            registry.register(
                registration(
                    operation="moe.grouped_gemm",
                    name=f"tilelang.moe.grouped_gemm.{target.architecture}",
                    factory=make_grouped_gemm_kernel,
                    schedule_digest=schedule_identity(BASELINE_GROUPED_GEMM),
                    invoke=partial(
                        _invoke_grouped_gemm,
                        schedule=BASELINE_GROUPED_GEMM,
                        target=target_config,
                    ),
                    eligibility=_target_eligibility(target, "bfloat16"),
                    priority=80,
                )
            )
