# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Target descriptions used for legality checks, never as qualification proof."""

from __future__ import annotations

from dataclasses import dataclass

from kernels.api.capabilities import DeviceCapabilities


@dataclass(frozen=True, slots=True)
class TargetSpec:
    kind: str
    architecture: str
    capabilities: DeviceCapabilities
    tilelang_target: str | dict[str, str]
    min_tilelang_version: str = "0.1.13"

    def __post_init__(self) -> None:
        if self.capabilities.target != self.kind:
            raise ValueError("target kind and capability target must agree")
        if self.capabilities.architecture != self.architecture:
            raise ValueError("target architecture and capability architecture must agree")


@dataclass(frozen=True, slots=True)
class TargetRequirement:
    dtypes: frozenset[str] = frozenset()
    min_shared_memory: int = 0
    min_threads: int = 1
    async_copy: bool = False
    tma: bool = False
    wgmma: bool = False
    tmem: bool = False

    def rejection_reason(self, target: TargetSpec) -> str | None:
        caps = target.capabilities
        if not self.dtypes.issubset(caps.tensor_core_dtypes):
            return "dtype_capability"
        if self.min_shared_memory > caps.shared_memory_per_block:
            return "shared_memory_limit"
        if self.min_threads > caps.max_threads_per_block:
            return "thread_limit"
        for required, available, reason in (
            (self.async_copy, caps.supports_async_copy, "async_copy_capability"),
            (self.tma, caps.supports_tma, "tma_capability"),
            (self.wgmma, caps.supports_wgmma, "wgmma_capability"),
            (self.tmem, caps.supports_tmem, "tmem_capability"),
        ):
            if required and not available:
                return reason
        return None


def cuda_target(
    architecture: str,
    *,
    shared_memory: int,
    dtypes: frozenset[str],
    tma: bool = False,
    wgmma: bool = False,
    tmem: bool = False,
) -> TargetSpec:
    return TargetSpec(
        kind="cuda",
        architecture=architecture,
        capabilities=DeviceCapabilities(
            target="cuda",
            architecture=architecture,
            warp_size=32,
            max_threads_per_block=1024,
            shared_memory_per_block=shared_memory,
            tensor_core_dtypes=dtypes,
            supports_async_copy=True,
            supports_tma=tma,
            supports_wgmma=wgmma,
            supports_tmem=tmem,
        ),
        tilelang_target={"kind": "cuda", "arch": architecture},
    )
