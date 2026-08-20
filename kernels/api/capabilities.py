# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Explicit device capability model used by eligibility predicates."""

from __future__ import annotations

from dataclasses import dataclass


@dataclass(frozen=True, slots=True)
class DeviceCapabilities:
    target: str
    architecture: str
    warp_size: int
    max_threads_per_block: int
    shared_memory_per_block: int
    tensor_core_dtypes: frozenset[str]
    supports_async_copy: bool = False
    supports_tma: bool = False
    supports_wgmma: bool = False
    supports_tmem: bool = False

    def __post_init__(self) -> None:
        if self.warp_size <= 0 or self.max_threads_per_block <= 0:
            raise ValueError("warp and thread limits must be positive")
        if self.shared_memory_per_block <= 0:
            raise ValueError("shared-memory limit must be positive")

    def supports_dtype(self, dtype: str) -> bool:
        return dtype in self.tensor_core_dtypes


PORTABLE_CPU = DeviceCapabilities(
    target="cpu",
    architecture="generic",
    warp_size=1,
    max_threads_per_block=1,
    shared_memory_per_block=1,
    tensor_core_dtypes=frozenset(),
)
