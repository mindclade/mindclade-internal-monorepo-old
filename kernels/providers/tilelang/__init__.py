# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Qualification-gated TileLang provider."""

from kernels.providers.tilelang.adapter import CompiledKernelCache
from kernels.providers.tilelang.capabilities import detect_capabilities
from kernels.providers.tilelang.manifest import TILELANG_VERSION

__all__ = ["TILELANG_VERSION", "CompiledKernelCache", "detect_capabilities"]
