# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Logical and shared-memory layout contracts."""

from __future__ import annotations

from dataclasses import dataclass
from math import prod


@dataclass(frozen=True, slots=True)
class StridedLayout:
    shape: tuple[int, ...]
    strides: tuple[int, ...]
    element_bytes: int
    alignment: int

    def __post_init__(self) -> None:
        if not self.shape or len(self.shape) != len(self.strides):
            raise ValueError("shape and strides must be non-empty and have equal rank")
        if any(dim <= 0 for dim in self.shape) or any(stride <= 0 for stride in self.strides):
            raise ValueError("shape dimensions and strides must be positive")
        if self.element_bytes not in {1, 2, 4, 8} or self.alignment <= 0:
            raise ValueError("unsupported element width or alignment")

    @classmethod
    def contiguous(
        cls, shape: tuple[int, ...], *, element_bytes: int, alignment: int
    ) -> StridedLayout:
        strides: list[int] = []
        for index in range(len(shape)):
            strides.append(prod(shape[index + 1 :]))
        return cls(shape, tuple(strides), element_bytes, alignment)

    @property
    def nbytes(self) -> int:
        return prod(self.shape) * self.element_bytes

    def vector_width(self, maximum_bytes: int = 16) -> int:
        if self.strides[-1] != 1:
            return 1
        width = min(maximum_bytes, self.alignment) // self.element_bytes
        return max(1, min(width, self.shape[-1]))


@dataclass(frozen=True, slots=True)
class SharedTile:
    rows: int
    columns: int
    element_bytes: int
    padding: int = 0

    @property
    def stride(self) -> int:
        return self.columns + self.padding

    @property
    def nbytes(self) -> int:
        return self.rows * self.stride * self.element_bytes

    def bank_period(self, banks: int = 32, bank_bytes: int = 4) -> int:
        if banks <= 0 or bank_bytes <= 0:
            raise ValueError("bank geometry must be positive")
        return (self.stride * self.element_bytes // bank_bytes) % banks
