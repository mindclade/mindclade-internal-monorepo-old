# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Runtime version checks isolated from import-time package behavior."""

from __future__ import annotations

from dataclasses import dataclass

from kernels.providers.tilelang.attention.attention import _tilelang


@dataclass(frozen=True, slots=True)
class RuntimeIdentity:
    tilelang_version: str
    target: str
    architecture: str


def require_runtime(target: str, architecture: str) -> RuntimeIdentity:
    tilelang, _ = _tilelang()
    return RuntimeIdentity(tilelang.__version__, target, architecture)
