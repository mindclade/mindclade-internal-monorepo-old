# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Pure, exact eligibility predicates for TileLang source candidates."""

from __future__ import annotations

from collections.abc import Callable

from kernels.api.capabilities import DeviceCapabilities
from kernels.api.specs import KernelRequest, TensorLayout, TensorSpec

ShapeCheck = Callable[[KernelRequest], str | None]


def exact_eligibility(
    *,
    targets: frozenset[str],
    architectures: frozenset[str],
    dtypes: frozenset[str],
    shape_check: ShapeCheck,
) -> Callable[[KernelRequest, DeviceCapabilities], str | None]:
    def check(request: KernelRequest, capabilities: DeviceCapabilities) -> str | None:
        if request.target not in targets or capabilities.target != request.target:
            return "target"
        if (
            request.architecture not in architectures
            or capabilities.architecture != request.architecture
        ):
            return "architecture"
        specs = (*request.inputs, *request.outputs)
        if any(spec.dtype not in dtypes for spec in specs):
            return "dtype"
        if any(not spec.contiguous for spec in specs):
            return "layout_contiguity"
        return shape_check(request)

    return check


def _semantic_value(
    request: KernelRequest,
    name: str,
    default: str,
    *,
    allowed_keys: frozenset[str],
) -> tuple[str | None, str | None]:
    semantics = dict(request.semantics)
    if not set(semantics).issubset(allowed_keys):
        return None, "semantics"
    return semantics.get(name, default), None


def _one_output(request: KernelRequest) -> tuple[TensorSpec | None, str | None]:
    if len(request.outputs) != 1:
        return None, "output_arity"
    return request.outputs[0], None


def attention_contract(
    *,
    causal: bool,
    head_dimensions: frozenset[int] = frozenset({32, 64, 128, 256}),
) -> ShapeCheck:
    def check(request: KernelRequest) -> str | None:
        if len(request.inputs) != 3 or any(len(spec.shape) != 4 for spec in request.inputs):
            return "attention_rank"
        output, reason = _one_output(request)
        if reason is not None:
            return reason
        q, k, v = request.inputs
        if k.shape != v.shape or q.shape[:2] != k.shape[:2] or q.shape[-1] != k.shape[-1]:
            return "attention_shape"
        if q.dtype != k.dtype or q.dtype != v.dtype or output is None or output.dtype != q.dtype:
            return "attention_dtype"
        if output.shape != q.shape:
            return "output_shape"
        if any(
            spec.layout not in {TensorLayout.BHSD, TensorLayout.CONTIGUOUS}
            for spec in (q, k, v, output)
        ):
            return "attention_layout"
        if q.shape[-1] not in head_dimensions:
            return "head_dimension"
        semantics = dict(request.semantics)
        if not set(semantics).issubset({"causal", "mask", "scale"}):
            return "semantics"
        if semantics.get("mask", "none") != "none":
            return "attention_mask"
        if semantics.get("scale", "default") != "default":
            return "attention_scale"
        if semantics.get("causal", "false") != str(causal).lower():
            return "attention_causal_variant"
        return None

    return check


def scaled_gemm_contract(
    *,
    activation: str,
    input_dtype: str,
    output_dtype: str,
) -> ShapeCheck:
    def check(request: KernelRequest) -> str | None:
        if len(request.inputs) != 4:
            return "input_arity"
        a, b, a_scale, b_scale = request.inputs
        if len(a.shape) != 2 or len(b.shape) != 2:
            return "gemm_rank"
        if a.shape[1] != b.shape[0]:
            return "reduction_dimension"
        if a.dtype != b.dtype:
            return "gemm_dtype"
        if a.dtype != input_dtype:
            return "input_dtype"
        if a_scale.shape != (1,) or b_scale.shape != (1,):
            return "scale_shape"
        if a_scale.dtype != "float32" or b_scale.dtype != "float32":
            return "scale_dtype"
        output, reason = _one_output(request)
        if reason is not None:
            return reason
        if output is None or output.shape != (a.shape[0], b.shape[1]):
            return "output_shape"
        if output.dtype != output_dtype:
            return "output_dtype"
        if any(spec.layout != TensorLayout.CONTIGUOUS for spec in (*request.inputs, output)):
            return "gemm_layout"
        semantics = dict(request.semantics)
        if not set(semantics).issubset({"activation", "scale_granularity"}):
            return "semantics"
        if semantics.get("activation", "none") != activation:
            return "activation_variant"
        if semantics.get("scale_granularity", "per_tensor") != "per_tensor":
            return "scale_granularity"
        alignment = 32 if input_dtype.startswith("float8") else 16
        return None if a.shape[1] % alignment == 0 else "reduction_alignment"

    return check


def swiglu_contract(request: KernelRequest) -> str | None:
    if len(request.inputs) != 2:
        return "input_arity"
    gate, up = request.inputs
    output, reason = _one_output(request)
    if reason is not None:
        return reason
    if gate.shape != up.shape or output is None or output.shape != gate.shape:
        return "swiglu_shape"
    if gate.dtype != up.dtype or output.dtype != gate.dtype:
        return "swiglu_dtype"
    if any(spec.layout != TensorLayout.CONTIGUOUS for spec in (gate, up, output)):
        return "swiglu_layout"
    return None if not request.semantics else "semantics"


def triangle_contract(request: KernelRequest) -> str | None:
    if len(request.inputs) != 3:
        return "input_arity"
    left, right, mask = request.inputs
    if len(left.shape) != 4 or left.shape != right.shape:
        return "triangle_shape"
    batch, rows, columns, _ = left.shape
    if rows != columns or mask.shape != (batch, rows):
        return "triangle_shape"
    if left.dtype != right.dtype or mask.dtype != "bool":
        return "triangle_dtype"
    output, reason = _one_output(request)
    if reason is not None:
        return reason
    if output is None or output.shape != left.shape or output.dtype != left.dtype:
        return "output_shape"
    pair_layouts = {TensorLayout.PAIR_MAJOR, TensorLayout.CONTIGUOUS}
    if left.layout not in pair_layouts or right.layout not in pair_layouts:
        return "triangle_layout"
    if mask.layout != TensorLayout.CONTIGUOUS or output.layout not in pair_layouts:
        return "triangle_layout"
    return None if not request.semantics else "semantics"


def grouped_gemm_contract(request: KernelRequest) -> str | None:
    if len(request.inputs) != 2:
        return "input_arity"
    tokens, weights = request.inputs
    if len(tokens.shape) != 3 or len(weights.shape) != 3:
        return "grouped_gemm_rank"
    if tokens.shape[0] != weights.shape[0] or tokens.shape[2] != weights.shape[1]:
        return "grouped_gemm_shape"
    if tokens.dtype != weights.dtype:
        return "grouped_gemm_dtype"
    output, reason = _one_output(request)
    expected = (tokens.shape[0], tokens.shape[1], weights.shape[2])
    if reason is not None:
        return reason
    if output is None or output.shape != expected or output.dtype != tokens.dtype:
        return "output_shape"
    layouts = {TensorLayout.EXPERT_MAJOR, TensorLayout.CONTIGUOUS}
    if any(spec.layout not in layouts for spec in (tokens, weights, output)):
        return "grouped_gemm_layout"
    return None if not request.semantics else "semantics"


def modulated_residual_contract(request: KernelRequest) -> str | None:
    if len(request.inputs) != 5:
        return "input_arity"
    normalized, residual, scale, shift, gate = request.inputs
    if len(normalized.shape) != 3 or normalized.shape != residual.shape:
        return "diffusion_shape"
    expected = (normalized.shape[0], normalized.shape[2])
    if any(spec.shape != expected for spec in (scale, shift, gate)):
        return "diffusion_shape"
    if len({spec.dtype for spec in request.inputs}) != 1:
        return "diffusion_dtype"
    output, reason = _one_output(request)
    if reason is not None:
        return reason
    if output is None or output.shape != normalized.shape or output.dtype != normalized.dtype:
        return "output_shape"
    if any(spec.layout != TensorLayout.CONTIGUOUS for spec in (*request.inputs, output)):
        return "diffusion_layout"
    return None if not request.semantics else "semantics"
