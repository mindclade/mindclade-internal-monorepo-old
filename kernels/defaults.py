# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Composition root for the built-in kernel providers."""

from kernels.providers.pytorch.registry import register_references
from kernels.providers.tilelang.registry import register_tilelang_candidates
from kernels.registry import KernelRegistry


def default_registry() -> KernelRegistry:
    registry = KernelRegistry()
    register_references(registry)
    register_tilelang_candidates(registry)
    return registry
