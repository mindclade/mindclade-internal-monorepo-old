# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Content-addressed TileLang candidates with exact runtime contracts."""

from __future__ import annotations

import hashlib
import inspect
import json
from collections.abc import Callable
from dataclasses import asdict, is_dataclass
from functools import partial
from typing import Literal

from kernels.api.specs import ImplementationIdentity, Provider
from kernels.providers.tilelang.adapter import CompiledKernelCache
from kernels.providers.tilelang.attention import baseline_schedule as attention_schedule
from kernels.providers.tilelang.attention import make_flash_attention_kernel
from kernels.providers.tilelang.attention.attention import _tilelang
from kernels.providers.tilelang.attention.schedules import FlashAttentionSchedule
from kernels.providers.tilelang.diffusion import (
    BASELINE_DIFFUSION_EPILOGUE,
    make_modulated_residual_kernel,
)
from kernels.providers.tilelang.diffusion.schedules import DiffusionEpilogueSchedule
from kernels.providers.tilelang.fp8 import baseline_schedule as gemm_schedule
from kernels.providers.tilelang.fp8 import make_scaled_gemm_kernel
from kernels.providers.tilelang.fp8.schedules import GemmSchedule
from kernels.providers.tilelang.fused import (
    BASELINE_ELEMENTWISE,
    BASELINE_TRIANGLE,
    make_swiglu_kernel,
    make_triangle_multiplication_kernel,
)
from kernels.providers.tilelang.fused.schedules import ElementwiseSchedule, TriangleSchedule
from kernels.providers.tilelang.manifest import (
    PROVIDER_SCHEMA_VERSION,
    TILELANG_VERSION,
    TVM_FFI_RANGE,
)
from kernels.providers.tilelang.moe import (
    BASELINE_GROUPED_GEMM,
    GroupedGemmSchedule,
    make_grouped_gemm_kernel,
)
from kernels.providers.tilelang.policy import (
    ShapeCheck,
    attention_contract,
    exact_eligibility,
    grouped_gemm_contract,
    modulated_residual_contract,
    scaled_gemm_contract,
    swiglu_contract,
    triangle_contract,
)
from kernels.registry import KernelImplementation, KernelRegistry
from kernels.tilelang.targets import TARGETS, TargetSpec

RUNTIME_TARGET_KEYS = frozenset({("cuda", "sm_90")})
_COMPILED_KERNELS = CompiledKernelCache(max_entries=128)


def schedule_identity(schedule: object) -> str:
    payload = asdict(schedule) if is_dataclass(schedule) else repr(schedule)  # type: ignore[arg-type]
    return hashlib.sha256(
        json.dumps(payload, sort_keys=True, separators=(",", ":")).encode()
    ).hexdigest()


def _callable_source(function: Callable[..., object]) -> str:
    source_callable = function.func if isinstance(function, partial) else function
    binding = repr((function.args, function.keywords)) if isinstance(function, partial) else ""
    closure = inspect.getclosurevars(source_callable)
    captured = repr(sorted(closure.nonlocals.items()))
    return inspect.getsource(source_callable) + binding + captured


def source_closure_digest(
    *,
    operation: str,
    factory: Callable[..., object],
    invoke: Callable[..., object],
    contract: ShapeCheck,
) -> str:
    """Hash every reviewed source boundary that can change generated behavior."""

    payload = {
        "contract": _callable_source(contract),
        "factory": _callable_source(factory),
        "invoke": _callable_source(invoke),
        "operation": operation,
        "provider_schema": PROVIDER_SCHEMA_VERSION,
        "runtime_loader": _callable_source(_tilelang),
        "tilelang_version": TILELANG_VERSION,
        "tvm_ffi_range": TVM_FFI_RANGE,
    }
    encoded = json.dumps(payload, sort_keys=True, separators=(",", ":")).encode()
    return hashlib.sha256(encoded).hexdigest()


def implementation_identity(
    name: str,
    operation: str,
    factory: Callable[..., object],
    invoke: Callable[..., object],
    contract: ShapeCheck,
    schedule_digest: str,
) -> ImplementationIdentity:
    return ImplementationIdentity(
        provider=Provider.TILELANG,
        name=name,
        source_digest=source_closure_digest(
            operation=operation,
            factory=factory,
            invoke=invoke,
            contract=contract,
        ),
        compiler="tilelang",
        compiler_version=TILELANG_VERSION,
        schedule_digest=schedule_digest,
    )


def registration(
    *,
    operation: str,
    name: str,
    factory: Callable[..., object],
    schedule_digest: str,
    invoke: Callable[..., object],
    target: TargetSpec,
    dtypes: frozenset[str],
    contract: ShapeCheck,
    priority: int,
) -> KernelImplementation:
    return KernelImplementation(
        operation=operation,
        identity=implementation_identity(
            name,
            operation,
            factory,
            invoke,
            contract,
            schedule_digest,
        ),
        invoke=invoke,
        eligibility=exact_eligibility(
            targets=frozenset({target.kind}),
            architectures=frozenset({target.architecture}),
            dtypes=dtypes,
            shape_check=contract,
        ),
        priority=priority,
    )


def _target_key(target: str | dict[str, str] | None) -> str:
    return json.dumps(target, sort_keys=True, separators=(",", ":"))


def _compiled(
    operation: str,
    schedule: object,
    target: str | dict[str, str] | None,
    variant: str,
    compile_kernel: Callable[[], object],
) -> object:
    key = (
        operation,
        schedule_identity(schedule),
        _target_key(target),
        variant,
        TILELANG_VERSION,
        TVM_FFI_RANGE,
    )
    return _COMPILED_KERNELS.get_or_compile(key, compile_kernel)


def _invoke_triangle(
    left: object,
    right: object,
    mask: object,
    *,
    schedule: TriangleSchedule,
    orientation: Literal["incoming", "outgoing"],
    target: str | dict[str, str] | None,
) -> object:
    kernel = _compiled(
        f"pairformer.triangle_{orientation}",
        schedule,
        target,
        orientation,
        lambda: make_triangle_multiplication_kernel(
            schedule,
            orientation=orientation,
            target=target,
        ),
    )
    return kernel(left, right, mask)  # type: ignore[operator]


def _invoke_attention(
    q: object,
    k: object,
    v: object,
    *,
    schedule: FlashAttentionSchedule,
    causal: bool,
    target: str | dict[str, str] | None,
) -> object:
    variant = f"causal={str(causal).lower()};scale=default"
    kernel = _compiled(
        "attention.sdpa",
        schedule,
        target,
        variant,
        lambda: make_flash_attention_kernel(schedule, causal=causal, target=target),
    )
    return kernel(q, k, v)  # type: ignore[operator]


def _invoke_scaled_gemm(
    a: object,
    b: object,
    a_scale: object,
    b_scale: object,
    *,
    schedule: GemmSchedule,
    activation: str,
    target: str | dict[str, str] | None,
) -> object:
    kernel = _compiled(
        "fp8.scaled_gemm",
        schedule,
        target,
        f"activation={activation}",
        lambda: make_scaled_gemm_kernel(
            schedule,
            target=target,
            activation=activation,
        ),
    )
    return kernel(a, b, a_scale, b_scale)  # type: ignore[operator]


def _invoke_swiglu(
    gate: object,
    up: object,
    *,
    schedule: ElementwiseSchedule,
    target: str | dict[str, str] | None,
) -> object:
    kernel = _compiled(
        "fused.swiglu",
        schedule,
        target,
        "dtype=bfloat16",
        lambda: make_swiglu_kernel(schedule, dtype="bfloat16", target=target),
    )
    return kernel(gate, up)  # type: ignore[operator]


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
    kernel = _compiled(
        "diffusion.modulated_residual",
        schedule,
        target,
        "dtype=bfloat16",
        lambda: make_modulated_residual_kernel(
            schedule,
            dtype="bfloat16",
            target=target,
        ),
    )
    return kernel(normalized, residual, scale, shift, gate)  # type: ignore[operator]


def _invoke_grouped_gemm(
    tokens: object,
    weights: object,
    *,
    schedule: GroupedGemmSchedule,
    target: str | dict[str, str] | None,
) -> object:
    kernel = _compiled(
        "moe.grouped_gemm",
        schedule,
        target,
        "padded=true",
        lambda: make_grouped_gemm_kernel(schedule, target=target),
    )
    return kernel(tokens, weights)  # type: ignore[operator]


def register_tilelang_candidates(registry: KernelRegistry) -> None:
    """Register only runtime-reviewed targets; all dispatch still requires evidence."""

    for target_key in sorted(RUNTIME_TARGET_KEYS):
        target = TARGETS[target_key]
        target_config = target.tilelang_target
        for dtype in ("float16", "bfloat16"):
            attention = attention_schedule(target.architecture, dtype)
            if attention.rejection_reason(target, 64) is None:
                head_dimensions = frozenset(
                    head_dimension
                    for head_dimension in (32, 64, 128, 256)
                    if attention.rejection_reason(target, head_dimension) is None
                )
                for causal in (False, True):
                    contract = attention_contract(
                        causal=causal,
                        head_dimensions=head_dimensions,
                    )
                    variant = "causal" if causal else "dense"
                    invoke = partial(
                        _invoke_attention,
                        schedule=attention,
                        causal=causal,
                        target=target_config,
                    )
                    registry.register(
                        registration(
                            operation="attention.sdpa",
                            name=(f"tilelang.attention.{target.architecture}.{dtype}.{variant}"),
                            factory=make_flash_attention_kernel,
                            schedule_digest=attention.digest,
                            invoke=invoke,
                            target=target,
                            dtypes=frozenset({dtype}),
                            contract=contract,
                            priority=100,
                        )
                    )

            triangle = TriangleSchedule(
                BASELINE_TRIANGLE.block_m,
                BASELINE_TRIANGLE.block_n,
                BASELINE_TRIANGLE.block_k,
                BASELINE_TRIANGLE.threads,
                BASELINE_TRIANGLE.num_stages,
                dtype,
            )
            if triangle.rejection_reason(target, 64) is None:
                for orientation in ("incoming", "outgoing"):
                    operation = f"pairformer.triangle_{orientation}"
                    invoke = partial(
                        _invoke_triangle,
                        schedule=triangle,
                        orientation=orientation,
                        target=target_config,
                    )
                    registry.register(
                        registration(
                            operation=operation,
                            name=(
                                f"tilelang.pairformer.{orientation}.{target.architecture}.{dtype}"
                            ),
                            factory=make_triangle_multiplication_kernel,
                            schedule_digest=triangle.digest,
                            invoke=invoke,
                            target=target,
                            dtypes=frozenset({dtype, "bool"}),
                            contract=triangle_contract,
                            priority=90,
                        )
                    )

        for dtype in ("float16", "bfloat16", "float8_e4m3fn"):
            if not target.capabilities.supports_dtype(dtype):
                continue
            gemm = gemm_schedule(target.architecture, dtype)
            if gemm.rejection_reason(target, gemm.block_k) is not None:
                continue
            for activation in ("none", "relu", "silu"):
                contract = scaled_gemm_contract(
                    activation=activation,
                    input_dtype=gemm.input_dtype,
                    output_dtype=gemm.output_dtype,
                )
                invoke = partial(
                    _invoke_scaled_gemm,
                    schedule=gemm,
                    activation=activation,
                    target=target_config,
                )
                registry.register(
                    registration(
                        operation="fp8.scaled_gemm",
                        name=(f"tilelang.scaled_gemm.{target.architecture}.{dtype}.{activation}"),
                        factory=make_scaled_gemm_kernel,
                        schedule_digest=gemm.digest,
                        invoke=invoke,
                        target=target,
                        dtypes=frozenset({dtype, "float32", gemm.output_dtype}),
                        contract=contract,
                        priority=100,
                    )
                )

        for operation, name, factory, schedule, invoke, dtypes, contract in (
            (
                "fused.swiglu",
                f"tilelang.fused.swiglu.{target.architecture}",
                make_swiglu_kernel,
                BASELINE_ELEMENTWISE,
                partial(
                    _invoke_swiglu,
                    schedule=BASELINE_ELEMENTWISE,
                    target=target_config,
                ),
                frozenset({"bfloat16"}),
                swiglu_contract,
            ),
            (
                "diffusion.modulated_residual",
                f"tilelang.diffusion.modulated_residual.{target.architecture}",
                make_modulated_residual_kernel,
                BASELINE_DIFFUSION_EPILOGUE,
                partial(
                    _invoke_modulated_residual,
                    schedule=BASELINE_DIFFUSION_EPILOGUE,
                    target=target_config,
                ),
                frozenset({"bfloat16"}),
                modulated_residual_contract,
            ),
            (
                "moe.grouped_gemm",
                f"tilelang.moe.grouped_gemm.{target.architecture}",
                make_grouped_gemm_kernel,
                BASELINE_GROUPED_GEMM,
                partial(
                    _invoke_grouped_gemm,
                    schedule=BASELINE_GROUPED_GEMM,
                    target=target_config,
                ),
                frozenset({"bfloat16"}),
                grouped_gemm_contract,
            ),
        ):
            registry.register(
                registration(
                    operation=operation,
                    name=name,
                    factory=factory,
                    schedule_digest=schedule_identity(schedule),
                    invoke=invoke,
                    target=target,
                    dtypes=dtypes,
                    contract=contract,
                    priority=80,
                )
            )
