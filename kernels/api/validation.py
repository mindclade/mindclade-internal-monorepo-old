# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Input validation shared by reference and accelerator providers."""

from __future__ import annotations

from collections.abc import Sequence
from typing import Protocol

from kernels.api.errors import KernelValidationError


class TensorLike(Protocol):
    @property
    def shape(self) -> Sequence[int]: ...

    @property
    def dtype(self) -> object: ...

    @property
    def device(self) -> object: ...

    def is_contiguous(self) -> bool: ...


def require_rank(tensor: TensorLike, rank: int, name: str) -> None:
    if len(tensor.shape) != rank:
        raise KernelValidationError(
            f"{name} must have rank {rank}",
            details={"actual_rank": len(tensor.shape), "argument": name},
        )


def require_same_device(*named_tensors: tuple[str, TensorLike]) -> None:
    if not named_tensors:
        return
    expected = str(named_tensors[0][1].device)
    mismatches = [name for name, tensor in named_tensors if str(tensor.device) != expected]
    if mismatches:
        raise KernelValidationError(
            "all tensors must be on the same device",
            details={"expected": expected, "mismatches": mismatches},
        )


def require_same_dtype(*named_tensors: tuple[str, TensorLike]) -> None:
    if not named_tensors:
        return
    expected = str(named_tensors[0][1].dtype)
    mismatches = [name for name, tensor in named_tensors if str(tensor.dtype) != expected]
    if mismatches:
        raise KernelValidationError(
            "all tensors must have the same dtype",
            details={"expected": expected, "mismatches": mismatches},
        )


def require_contiguous(*named_tensors: tuple[str, TensorLike]) -> None:
    noncontiguous = [name for name, tensor in named_tensors if not tensor.is_contiguous()]
    if noncontiguous:
        raise KernelValidationError(
            "optimized kernels require contiguous tensors",
            details={"arguments": noncontiguous},
        )


def require_positive_dimensions(tensor: TensorLike, name: str) -> None:
    if any(int(dim) <= 0 for dim in tensor.shape):
        raise KernelValidationError(
            f"{name} must not contain empty dimensions",
            details={"argument": name, "shape": tuple(int(dim) for dim in tensor.shape)},
        )
