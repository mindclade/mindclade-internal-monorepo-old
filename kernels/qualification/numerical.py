# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import hashlib
import json
import math
from dataclasses import asdict, dataclass
from typing import Any


def _require_digest(value: str, name: str) -> None:
    if len(value) != 64 or any(character not in "0123456789abcdef" for character in value):
        raise ValueError(f"{name} must be a lowercase SHA-256 digest")


@dataclass(frozen=True, slots=True)
class NumericalEvidence:
    cases: int
    seeds: tuple[int, ...]
    rtol: float
    atol: float
    max_absolute_error: float
    max_relative_error: float
    forward_passed: bool
    gradient_inputs: tuple[int, ...]
    gradient_passed: bool
    determinism_passed: bool
    adversarial_passed: bool
    sanitizer_tools: tuple[str, ...]
    sanitizer_passed: bool
    raw_results_digest: str

    def __post_init__(self) -> None:
        if isinstance(self.cases, bool) or not isinstance(self.cases, int) or self.cases <= 0:
            raise ValueError("numerical evidence requires a positive integer case count")
        if (
            not self.seeds
            or any(isinstance(seed, bool) or not isinstance(seed, int) for seed in self.seeds)
            or len(set(self.seeds)) != len(self.seeds)
        ):
            raise ValueError("numerical evidence requires cases and unique recorded seeds")
        numeric_values = (
            self.rtol,
            self.atol,
            self.max_absolute_error,
            self.max_relative_error,
        )
        if any(
            isinstance(value, bool)
            or not isinstance(value, int | float)
            or not math.isfinite(value)
            or value < 0
            for value in numeric_values
        ):
            raise ValueError("numerical tolerances and errors must be non-negative")
        if (
            any(
                isinstance(index, bool) or not isinstance(index, int) or index < 0
                for index in self.gradient_inputs
            )
            or tuple(sorted(set(self.gradient_inputs))) != self.gradient_inputs
        ):
            raise ValueError("gradient inputs must be sorted and unique")
        if len(set(self.sanitizer_tools)) != len(self.sanitizer_tools):
            raise ValueError("sanitizer tools must be unique")
        flags = (
            self.forward_passed,
            self.gradient_passed,
            self.determinism_passed,
            self.adversarial_passed,
            self.sanitizer_passed,
        )
        if any(not isinstance(flag, bool) for flag in flags):
            raise TypeError("numerical result flags must be booleans")
        _require_digest(self.raw_results_digest, "raw_results_digest")

    @property
    def gradient_required(self) -> bool:
        return bool(self.gradient_inputs)

    @property
    def passed(self) -> bool:
        return (
            self.forward_passed
            and self.max_absolute_error <= self.atol
            and self.max_relative_error <= self.rtol
            and (not self.gradient_required or self.gradient_passed)
            and self.determinism_passed
            and self.adversarial_passed
            and self.sanitizer_passed
        )

    def canonical(self) -> dict[str, object]:
        return asdict(self)

    @classmethod
    def from_dict(cls, payload: dict[str, Any]) -> NumericalEvidence:
        return cls(
            cases=payload["cases"],
            seeds=tuple(payload["seeds"]),
            rtol=payload["rtol"],
            atol=payload["atol"],
            max_absolute_error=payload["max_absolute_error"],
            max_relative_error=payload["max_relative_error"],
            forward_passed=payload["forward_passed"],
            gradient_inputs=tuple(payload["gradient_inputs"]),
            gradient_passed=payload["gradient_passed"],
            determinism_passed=payload["determinism_passed"],
            adversarial_passed=payload["adversarial_passed"],
            sanitizer_tools=tuple(payload["sanitizer_tools"]),
            sanitizer_passed=payload["sanitizer_passed"],
            raw_results_digest=payload["raw_results_digest"],
        )

    @property
    def digest(self) -> str:
        payload = json.dumps(self.canonical(), sort_keys=True, separators=(",", ":"))
        return hashlib.sha256(payload.encode()).hexdigest()
