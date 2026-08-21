# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Composable validator protocol and function adapter."""

from __future__ import annotations

from collections.abc import Callable, Sequence
from dataclasses import dataclass
from typing import Protocol

from data.sample import Sample

from .report import QualityFinding


class Validator(Protocol):
    @property
    def name(self) -> str: ...

    def validate(self, samples: Sequence[Sample]) -> tuple[QualityFinding, ...]: ...


@dataclass(frozen=True, slots=True)
class FunctionValidator:
    name: str
    function: Callable[[Sequence[Sample]], tuple[QualityFinding, ...]]

    def __post_init__(self) -> None:
        if not self.name or len(self.name) > 128 or not callable(self.function):
            raise ValueError("quality function validator is invalid")

    def validate(self, samples: Sequence[Sample]) -> tuple[QualityFinding, ...]:
        findings = tuple(self.function(samples))
        if any(not isinstance(item, QualityFinding) for item in findings):
            raise TypeError("quality validator returned an invalid finding")
        return findings
