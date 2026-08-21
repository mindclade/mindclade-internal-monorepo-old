# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Explicit precision compatibility; no implicit numerical downgrade."""

from dataclasses import dataclass
from enum import StrEnum


class Precision(StrEnum):
    FP32 = "fp32"
    TF32 = "tf32"
    BF16 = "bf16"
    FP16 = "fp16"
    FP8 = "fp8"
    INT8 = "int8"


@dataclass(frozen=True, slots=True)
class PrecisionPolicy:
    allowed: tuple[Precision, ...]
    require_exact: bool = True

    def __post_init__(self) -> None:
        if not self.allowed or len(self.allowed) != len(set(self.allowed)):
            raise ValueError("precision policy must contain unique allowed modes")

    def select(self, requested: Precision, supported: tuple[Precision, ...]) -> Precision:
        if requested in self.allowed and requested in supported:
            return requested
        if self.require_exact:
            raise ValueError("requested precision is unavailable")
        for candidate in self.allowed:
            if candidate in supported:
                return candidate
        raise ValueError("no allowed precision is supported")
