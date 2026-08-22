# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Small FakeTensor helpers shared by graph-safe custom operators."""

from __future__ import annotations

import torch


def output_like(
    reference: torch.Tensor,
    shape: tuple[int, ...],
    *,
    dtype: torch.dtype | None = None,
) -> torch.Tensor:
    """Allocate metadata-only output while preserving device and layout semantics."""

    if not shape or any(dimension <= 0 for dimension in shape):
        raise ValueError("fake output shapes must contain positive dimensions")
    return reference.new_empty(shape, dtype=reference.dtype if dtype is None else dtype)
