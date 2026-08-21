# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Versioned FP8 format names shared by references and kernels."""

from __future__ import annotations

from enum import StrEnum

import torch


class FP8Format(StrEnum):
    E4M3FN = "float8_e4m3fn"
    E5M2 = "float8_e5m2"

    @property
    def torch_dtype(self) -> torch.dtype:
        return {
            FP8Format.E4M3FN: torch.float8_e4m3fn,
            FP8Format.E5M2: torch.float8_e5m2,
        }[self]

    @property
    def max_finite(self) -> float:
        return float(torch.finfo(self.torch_dtype).max)
