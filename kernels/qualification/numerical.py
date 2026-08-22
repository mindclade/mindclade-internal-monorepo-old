# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import hashlib
import json
from dataclasses import asdict, dataclass


@dataclass(frozen=True, slots=True)
class NumericalEvidence:
    cases: int
    seeds: tuple[int, ...]
    rtol: float
    atol: float
    max_absolute_error: float
    max_relative_error: float
    forward_passed: bool
    gradient_required: bool
    gradient_passed: bool
    determinism_passed: bool
    sanitizer_passed: bool

    def __post_init__(self) -> None:
        if self.cases <= 0 or not self.seeds:
            raise ValueError("numerical evidence requires cases and recorded seeds")
        if min(self.rtol, self.atol, self.max_absolute_error, self.max_relative_error) < 0:
            raise ValueError("numerical tolerances and errors must be non-negative")
        # Failed evidence remains serializable so rejected qualification attempts
        # are auditable. Promotion checks ``passed`` and rejects this record.

    @property
    def passed(self) -> bool:
        return (
            self.forward_passed
            and (not self.gradient_required or self.gradient_passed)
            and self.determinism_passed
            and self.sanitizer_passed
        )

    @property
    def digest(self) -> str:
        payload = json.dumps(asdict(self), sort_keys=True, separators=(",", ":"))
        return hashlib.sha256(payload.encode()).hexdigest()
