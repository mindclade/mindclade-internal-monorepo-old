# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Connected-device cases shared by parity and regression qualification."""

from __future__ import annotations

import hashlib
import math
from collections.abc import Iterable, Iterator
from dataclasses import dataclass, replace
from functools import cache
from typing import Any

import torch

from kernels.api.capabilities import DeviceCapabilities
from kernels.api.specs import ExecutionMode, KernelRequest, TensorLayout, TensorSpec
from kernels.defaults import default_registry
from kernels.providers.tilelang.capabilities import detect_capabilities
from kernels.qualification.workloads import WorkloadPair, production_workload_pairs
from kernels.registry import KernelImplementation, KernelRegistry
from kernels.tilelang.targets import resolve_target

PRODUCTION_CASE_SEED = 20_260_821
MAXIMUM_CASE_INPUT_ELEMENTS = 1_048_576
MAXIMUM_CASE_GENERATION_BYTES = 8 * 1_024 * 1_024

_TORCH_DTYPES: dict[str, torch.dtype] = {
    "bfloat16": torch.bfloat16,
    "bool": torch.bool,
    "float16": torch.float16,
    "float32": torch.float32,
    "float8_e4m3fn": torch.float8_e4m3fn,
    "float8_e5m2": torch.float8_e5m2,
}
_DTYPE_BYTES = {
    "bfloat16": 2,
    "bool": 1,
    "float16": 2,
    "float32": 4,
    "float8_e4m3fn": 1,
    "float8_e5m2": 1,
}
_LAYOUT_RANKS = {
    TensorLayout.BHSD: 4,
    TensorLayout.BSHD: 4,
    TensorLayout.EXPERT_MAJOR: 3,
    TensorLayout.PAIR_MAJOR: 4,
}


@dataclass(frozen=True, slots=True)
class ConnectedCase:
    name: str
    request: KernelRequest
    arguments: tuple[torch.Tensor, ...]
    reference_keywords: tuple[tuple[str, Any], ...] = ()
    paired_training_request: KernelRequest | None = None

    def __post_init__(self) -> None:
        if not isinstance(self.name, str) or not self.name.strip():
            raise ValueError("connected case name must be non-empty")
        if len(self.arguments) != len(self.request.inputs):
            raise ValueError("connected case argument count must match the request inputs")
        for index, (argument, specification) in enumerate(
            zip(self.arguments, self.request.inputs, strict=True)
        ):
            validate_tensor_argument(argument, specification, input_index=index)
        keyword_names = tuple(name for name, _ in self.reference_keywords)
        if len(keyword_names) != len(set(keyword_names)):
            raise ValueError("connected case reference keyword names must be unique")
        if self.paired_training_request is not None:
            training = self.paired_training_request
            if training.execution_mode != ExecutionMode.TRAINING:
                raise ValueError("paired request must use training execution mode")
            inference_equivalent = replace(
                training,
                execution_mode=ExecutionMode.INFERENCE,
                gradient_inputs=(),
            )
            if inference_equivalent != self.request:
                raise ValueError("paired training request must match the inference request")

    @property
    def keywords(self) -> dict[str, Any]:
        return dict(self.reference_keywords)


def _dtype(tensor: torch.Tensor) -> str:
    return str(tensor.dtype).removeprefix("torch.")


def _spec(tensor: torch.Tensor, layout: TensorLayout = TensorLayout.CONTIGUOUS) -> TensorSpec:
    return TensorSpec(tuple(tensor.shape), _dtype(tensor), layout, tensor.is_contiguous(), 16)


def torch_dtype(specification: TensorSpec) -> torch.dtype:
    """Resolve the exact dtype supported by the connected workload generator."""

    try:
        return _TORCH_DTYPES[specification.dtype]
    except KeyError as error:
        raise ValueError(
            f"connected workload dtype {specification.dtype!r} is unsupported"
        ) from error


def validate_tensor_argument(
    argument: torch.Tensor,
    specification: TensorSpec,
    *,
    input_index: int,
) -> None:
    """Validate physical tensor properties represented by a ``TensorSpec``."""

    if not isinstance(argument, torch.Tensor):
        raise TypeError(f"connected argument {input_index} must be a torch.Tensor")
    if tuple(argument.shape) != specification.shape:
        raise ValueError(f"connected argument {input_index} shape does not match its TensorSpec")
    expected_rank = _LAYOUT_RANKS.get(specification.layout)
    if expected_rank is not None and argument.ndim != expected_rank:
        raise ValueError(f"connected argument {input_index} rank does not match its logical layout")
    if argument.dtype != torch_dtype(specification):
        raise TypeError(f"connected argument {input_index} dtype does not match its TensorSpec")
    if argument.is_contiguous() != specification.contiguous:
        raise ValueError(
            f"connected argument {input_index} contiguity does not match its TensorSpec"
        )
    if argument.data_ptr() % specification.alignment != 0:
        raise ValueError(
            f"connected argument {input_index} does not satisfy its TensorSpec alignment"
        )


def _request_seed(request: KernelRequest, input_index: int) -> int:
    encoded = f"{PRODUCTION_CASE_SEED}:{request.digest}:{input_index}".encode()
    digest = hashlib.sha256(encoded).digest()
    return int.from_bytes(digest[:8], "big") & ((1 << 63) - 1)


def _storage_shape(specification: TensorSpec) -> tuple[int, ...]:
    if specification.contiguous:
        return specification.shape
    return (*specification.shape[:-1], specification.shape[-1] * 2)


def _validate_generation_budget(request: KernelRequest) -> None:
    unsupported = sorted({spec.dtype for spec in request.inputs} - _TORCH_DTYPES.keys())
    if unsupported:
        raise ValueError(f"connected workload has unsupported dtypes: {unsupported}")
    elements = sum(math.prod(_storage_shape(spec)) for spec in request.inputs)
    generation_bytes = sum(
        math.prod(_storage_shape(spec)) * max(4, _DTYPE_BYTES[spec.dtype])
        for spec in request.inputs
    )
    if elements > MAXIMUM_CASE_INPUT_ELEMENTS:
        raise ValueError(
            "connected workload exceeds MAXIMUM_CASE_INPUT_ELEMENTS: "
            f"{elements} > {MAXIMUM_CASE_INPUT_ELEMENTS}"
        )
    if generation_bytes > MAXIMUM_CASE_GENERATION_BYTES:
        raise ValueError(
            "connected workload exceeds MAXIMUM_CASE_GENERATION_BYTES: "
            f"{generation_bytes} > {MAXIMUM_CASE_GENERATION_BYTES}"
        )


def _generated_tensor(
    request: KernelRequest,
    specification: TensorSpec,
    input_index: int,
    device: torch.device,
) -> torch.Tensor:
    dtype = torch_dtype(specification)
    generator = torch.Generator(device=device)
    generator.manual_seed(_request_seed(request, input_index))
    shape = _storage_shape(specification)
    samples = torch.rand(shape, device=device, dtype=torch.float32, generator=generator)
    if dtype is torch.bool:
        tensor = samples > 0.2
        flat = tensor.reshape(-1)
        flat[0] = True
        if flat.numel() > 1:
            flat[-1] = False
    elif request.operation == "fp8.scaled_gemm" and input_index in (2, 3):
        tensor = (samples * 0.5 + 0.5).to(dtype=dtype)
    else:
        tensor = (samples * 0.5 - 0.25).to(dtype=dtype)
    if not specification.contiguous:
        tensor = tensor[..., ::2]
    validate_tensor_argument(tensor, specification, input_index=input_index)
    return tensor


def arguments_for_request(
    request: KernelRequest,
    device: torch.device,
) -> tuple[torch.Tensor, ...]:
    """Generate bounded deterministic arguments from an exact request's input specs."""

    if not isinstance(request, KernelRequest):
        raise TypeError("request must be a KernelRequest")
    if not isinstance(device, torch.device):
        raise TypeError("device must be a torch.device")
    if device.type not in {"cpu", "cuda"}:
        raise ValueError("connected workload generation supports only CPU and CUDA devices")
    _validate_generation_budget(request)
    return tuple(
        _generated_tensor(request, specification, index, device)
        for index, specification in enumerate(request.inputs)
    )


def _reference_keywords(request: KernelRequest) -> tuple[tuple[str, Any], ...]:
    semantics = dict(request.semantics)
    if request.operation == "attention.sdpa":
        causal = semantics.get("causal", "false")
        if causal not in {"false", "true"}:
            raise ValueError("attention causal semantic must be false or true")
        if semantics.get("mask", "none") != "none":
            raise ValueError("connected attention generation supports only mask=none")
        if semantics.get("scale", "default") != "default":
            raise ValueError("connected attention generation supports only scale=default")
        return (("causal", causal == "true"),)
    if request.operation == "fp8.scaled_gemm":
        activation = semantics.get("activation", "none")
        if activation not in {"none", "relu", "silu"}:
            raise ValueError("scaled GEMM activation semantic is unsupported")
        return (
            ("activation", activation),
            ("output_dtype", torch_dtype(request.outputs[0])),
        )
    if semantics:
        raise ValueError(f"connected workload has unsupported semantics for {request.operation}")
    return ()


def production_connected_cases(
    device: torch.device,
    workload_pairs: Iterable[WorkloadPair] | None = None,
) -> Iterator[ConnectedCase]:
    """Yield one bounded case per exact production inference/training pair."""

    pairs = production_workload_pairs() if workload_pairs is None else workload_pairs
    for index, pair in enumerate(pairs):
        if not isinstance(pair, WorkloadPair):
            raise TypeError("workload_pairs must contain WorkloadPair values")
        request = pair.inference
        yield ConnectedCase(
            name=f"production-{index:03d}-{request.operation}-{request.digest[:12]}",
            request=request,
            arguments=arguments_for_request(request, device),
            reference_keywords=_reference_keywords(request),
            paired_training_request=pair.training,
        )


def connected_device() -> tuple[torch.device, DeviceCapabilities]:
    if not torch.cuda.is_available():
        raise RuntimeError("a connected CUDA or ROCm accelerator is required")
    device = torch.device("cuda")
    return device, detect_capabilities(device)


def connected_cases(device: torch.device) -> tuple[ConnectedCase, ...]:
    torch.manual_seed(20260821)
    cases: list[ConnectedCase] = []

    for causal in (False, True):
        q = torch.randn(2, 4, 65, 64, device=device, dtype=torch.float16)
        k = torch.randn(2, 4, 79, 64, device=device, dtype=torch.float16)
        v = torch.randn_like(k)
        q_spec = _spec(q, TensorLayout.BHSD)
        k_spec = _spec(k, TensorLayout.BHSD)
        cases.append(
            ConnectedCase(
                f"attention-{'causal' if causal else 'dense'}",
                KernelRequest(
                    "attention.sdpa",
                    (q_spec, k_spec, k_spec),
                    (q_spec,),
                    "cuda",
                    "sm_90",
                    (
                        ("causal", str(causal).lower()),
                        ("mask", "none"),
                        ("scale", "default"),
                    ),
                ),
                (q, k, v),
                (("causal", causal),),
            )
        )

    for activation in ("none", "relu", "silu"):
        a = torch.randn(130, 48, device=device, dtype=torch.float16)
        b = torch.randn(48, 70, device=device, dtype=torch.float16)
        a_scale = torch.tensor([0.75], device=device)
        b_scale = torch.tensor([1.25], device=device)
        output = TensorSpec((130, 70), "bfloat16", alignment=16)
        cases.append(
            ConnectedCase(
                f"scaled-gemm-{activation}",
                KernelRequest(
                    "fp8.scaled_gemm",
                    (_spec(a), _spec(b), _spec(a_scale), _spec(b_scale)),
                    (output,),
                    "cuda",
                    "sm_90",
                    (("activation", activation), ("scale_granularity", "per_tensor")),
                ),
                (a, b, a_scale, b_scale),
                (("activation", activation), ("output_dtype", torch.bfloat16)),
            )
        )

    gate = torch.randn(37, 513, device=device, dtype=torch.bfloat16)
    up = torch.randn_like(gate)
    value_spec = _spec(gate)
    cases.append(
        ConnectedCase(
            "swiglu",
            KernelRequest(
                "fused.swiglu",
                (value_spec, value_spec),
                (value_spec,),
                "cuda",
                "sm_90",
            ),
            (gate, up),
        )
    )

    left = torch.randn(2, 17, 17, 16, device=device, dtype=torch.bfloat16)
    right = torch.randn_like(left)
    mask = torch.rand(2, 17, device=device) > 0.2
    pair_spec = _spec(left, TensorLayout.PAIR_MAJOR)
    mask_spec = _spec(mask)
    for orientation in ("incoming", "outgoing"):
        cases.append(
            ConnectedCase(
                f"triangle-{orientation}",
                KernelRequest(
                    f"pairformer.triangle_{orientation}",
                    (pair_spec, pair_spec, mask_spec),
                    (pair_spec,),
                    "cuda",
                    "sm_90",
                ),
                (left, right, mask),
            )
        )

    tokens = torch.randn(5, 19, 48, device=device, dtype=torch.bfloat16)
    weights = torch.randn(5, 48, 73, device=device, dtype=torch.bfloat16)
    output = TensorSpec(
        (5, 19, 73),
        "bfloat16",
        TensorLayout.EXPERT_MAJOR,
        True,
        16,
    )
    cases.append(
        ConnectedCase(
            "moe-grouped-gemm",
            KernelRequest(
                "moe.grouped_gemm",
                (
                    _spec(tokens, TensorLayout.EXPERT_MAJOR),
                    _spec(weights, TensorLayout.EXPERT_MAJOR),
                ),
                (output,),
                "cuda",
                "sm_90",
            ),
            (tokens, weights),
        )
    )

    normalized = torch.randn(3, 29, 88, device=device, dtype=torch.bfloat16)
    residual = torch.randn_like(normalized)
    modulation = tuple(torch.randn(3, 88, device=device, dtype=torch.bfloat16) for _ in range(3))
    output_spec = _spec(normalized)
    cases.append(
        ConnectedCase(
            "diffusion-modulated-residual",
            KernelRequest(
                "diffusion.modulated_residual",
                (
                    output_spec,
                    output_spec,
                    *(_spec(value) for value in modulation),
                ),
                (output_spec,),
                "cuda",
                "sm_90",
            ),
            (normalized, residual, *modulation),
        )
    )
    return tuple(cases)


def catalog_capabilities_for(request: KernelRequest) -> DeviceCapabilities:
    """Return static legality capabilities, never runtime qualification evidence."""

    target = resolve_target(request.target, request.architecture)
    if target is None:
        raise LookupError(
            f"no reviewed target catalog entry for {request.target}/{request.architecture}"
        )
    return target.capabilities


@cache
def _registry() -> KernelRegistry:
    return default_registry()


def implementations_for_request(
    request: KernelRequest,
    capabilities: DeviceCapabilities,
) -> tuple[KernelImplementation, KernelImplementation]:
    """Resolve the sole eligible TileLang candidate and independent reference."""

    registry = _registry()
    candidates = tuple(
        candidate
        for candidate in registry.candidates(request.operation)
        if candidate.identity.provider.value == "tilelang"
        and candidate.rejection_reason(request, capabilities) is None
    )
    if len(candidates) != 1:
        raise RuntimeError(
            f"{request.operation}/{request.digest} resolved "
            f"{len(candidates)} TileLang candidates instead of one"
        )
    return candidates[0], registry.reference(request.operation)


def implementations_for(
    case: ConnectedCase,
    capabilities: DeviceCapabilities,
) -> tuple[KernelImplementation, KernelImplementation]:
    return implementations_for_request(case.request, capabilities)
