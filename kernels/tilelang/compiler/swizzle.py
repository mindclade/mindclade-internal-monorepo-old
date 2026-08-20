# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Shared-memory and CTA rasterization swizzle specifications."""

from __future__ import annotations

from dataclasses import dataclass

from kernels.tilelang.compiler.layouts import SharedTile


@dataclass(frozen=True, slots=True)
class SharedSwizzle:
    width_bytes: int
    phase: int = 1

    def __post_init__(self) -> None:
        if self.width_bytes not in {32, 64, 128} or self.phase not in {1, 2, 4, 8}:
            raise ValueError("unsupported shared-memory swizzle")

    def validate(self, tile: SharedTile) -> None:
        row_bytes = tile.stride * tile.element_bytes
        if row_bytes < self.width_bytes or row_bytes % 16:
            raise ValueError("swizzled rows must span the swizzle and remain 16-byte aligned")


@dataclass(frozen=True, slots=True)
class CTARasterization:
    panel_size: int = 8
    order: str = "column"

    def __post_init__(self) -> None:
        if self.panel_size <= 0 or self.order not in {"row", "column"}:
            raise ValueError("invalid CTA rasterization")
