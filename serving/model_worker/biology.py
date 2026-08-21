# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Bounded biology workload dimensions; scientific interpretation stays model-owned."""

from dataclasses import dataclass


@dataclass(frozen=True, slots=True)
class BiologyDimensions:
    residues: int
    atoms: int
    templates: int = 0
    alignments: int = 0

    def __post_init__(self) -> None:
        values = (self.residues, self.atoms, self.templates, self.alignments)
        if any(
            isinstance(value, bool) or not isinstance(value, int) or value < 0 for value in values
        ):
            raise ValueError("biology dimensions must be non-negative integers")
        if self.residues > 1_000_000 or self.atoms > 100_000_000:
            raise ValueError("biology dimensions exceed hard bounds")
        if self.templates > 100_000 or self.alignments > 10_000_000:
            raise ValueError("biology context exceeds hard bounds")
