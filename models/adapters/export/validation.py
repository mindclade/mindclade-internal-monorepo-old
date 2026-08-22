# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Fresh-module eager-versus-export parity validation."""

from __future__ import annotations

import math
from collections.abc import Callable, Sequence
from dataclasses import dataclass

import torch
from torch import nn

from models.adapters.export.torch_export import LoadedExportBundle, _validated_inputs

MAXIMUM_PARITY_CASES = 64


@dataclass(frozen=True, slots=True)
class ParityReport:
    """Immutable summary of cases compared in the PyTorch runtime."""

    case_shapes: tuple[tuple[tuple[int, ...], ...], ...]
    rtol: float
    atol: float

    @property
    def case_count(self) -> int:
        return len(self.case_shapes)


def validate_export_parity(
    bundle: LoadedExportBundle,
    model_factory: Callable[[], nn.Module],
    cases: Sequence[Sequence[torch.Tensor]],
    *,
    rtol: float = 1e-5,
    atol: float = 1e-6,
) -> ParityReport:
    """Load artifact state into a fresh eager module and compare every case."""
    if not isinstance(bundle, LoadedExportBundle):
        raise TypeError("parity validation requires a LoadedExportBundle")
    if not callable(model_factory):
        raise TypeError("model_factory must be callable")
    rtol = _validated_tolerance(rtol, name="rtol")
    atol = _validated_tolerance(atol, name="atol")
    if not isinstance(cases, Sequence):
        raise TypeError("parity cases must be a sequence of tensor sequences")
    normalized_cases = tuple(tuple(case) for case in cases)
    if not 1 <= len(normalized_cases) <= MAXIMUM_PARITY_CASES:
        raise ValueError(f"parity case count must be in [1, {MAXIMUM_PARITY_CASES}]")

    eager_module = model_factory()
    if not isinstance(eager_module, nn.Module):
        raise TypeError("model_factory must return an nn.Module")
    eager_module.to(device=torch.device(bundle.manifest.input_contracts[0].device))
    fresh_state: dict[str, torch.Tensor] = {}
    for name, value in bundle.program.state_dict.items():
        if not isinstance(value, torch.Tensor):
            raise TypeError("exported program state must contain only tensors")
        fresh_state[name] = value.detach().clone()
    eager_state = eager_module.state_dict()
    if set(eager_state) != set(fresh_state):
        raise ValueError("fresh eager module state keys do not match the exported artifact")
    for name, artifact_value in fresh_state.items():
        eager_value = eager_state[name]
        if eager_value.shape != artifact_value.shape:
            raise ValueError(f"fresh eager state {name!r} shape does not match the artifact")
        if eager_value.dtype != artifact_value.dtype:
            raise TypeError(f"fresh eager state {name!r} dtype does not match the artifact")
        if eager_value.device != artifact_value.device:
            raise ValueError(f"fresh eager state {name!r} device does not match the artifact")
        if eager_value.layout != artifact_value.layout:
            raise ValueError(f"fresh eager state {name!r} layout does not match the artifact")
    incompatible = eager_module.load_state_dict(fresh_state, strict=True)
    if incompatible.missing_keys or incompatible.unexpected_keys:
        raise RuntimeError("strict eager state restoration reported incompatible keys")
    eager_module.eval()
    exported_module = bundle.program.module()

    shapes: list[tuple[tuple[int, ...], ...]] = []
    with torch.inference_mode():
        for case in normalized_cases:
            inputs = _validated_inputs(case, bundle.manifest.input_contracts)
            eager_output: object = eager_module(*inputs)
            exported_output: object = exported_module(*inputs)
            torch.testing.assert_close(
                exported_output,
                eager_output,
                rtol=rtol,
                atol=atol,
                check_device=True,
                check_dtype=True,
            )
            shapes.append(tuple(tuple(item.shape) for item in inputs))
    return ParityReport(case_shapes=tuple(shapes), rtol=rtol, atol=atol)


def _validated_tolerance(value: float, *, name: str) -> float:
    if isinstance(value, bool) or not isinstance(value, int | float):
        raise TypeError(f"{name} must be a real number")
    normalized = float(value)
    if normalized < 0.0 or not math.isfinite(normalized):
        raise ValueError(f"{name} must be finite and non-negative")
    return normalized
