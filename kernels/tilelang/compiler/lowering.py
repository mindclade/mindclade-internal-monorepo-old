# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Generated-source assertions used as qualification evidence, not runtime dispatch."""

from __future__ import annotations

from kernels.tilelang.compiler.ir import KernelSourceArtifact


def require_codegen_tokens(
    artifact: KernelSourceArtifact, *, required: tuple[str, ...], forbidden: tuple[str, ...] = ()
) -> None:
    missing = [token for token in required if token not in artifact.source]
    present = [token for token in forbidden if token in artifact.source]
    if missing or present:
        raise ValueError(
            f"generated source contract failed: missing={missing!r}, forbidden_present={present!r}"
        )
