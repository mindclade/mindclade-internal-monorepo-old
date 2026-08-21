# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

import itertools
from collections.abc import Callable, Mapping, Sequence

from kernels.tilelang.autotune.candidate import Candidate

ConfigValue = int | float | str | bool
Legality = Callable[[Mapping[str, ConfigValue]], str | None]


def bounded_candidates(
    dimensions: Mapping[str, Sequence[ConfigValue]],
    *,
    legality: Legality,
    source_digest: str,
    environment_digest: str,
    maximum: int,
) -> tuple[Candidate, ...]:
    if maximum <= 0 or not dimensions:
        raise ValueError("autotune dimensions and a positive maximum are required")
    names = sorted(dimensions)
    if any(not dimensions[name] for name in names):
        raise ValueError("every autotune dimension must have at least one value")
    candidates: list[Candidate] = []
    for values in itertools.product(*(dimensions[name] for name in names)):
        config = dict(zip(names, values, strict=True))
        if legality(config) is not None:
            continue
        candidates.append(
            Candidate(tuple(sorted(config.items())), source_digest, environment_digest)
        )
        if len(candidates) >= maximum:
            break
    if not candidates:
        raise ValueError("the constrained autotune space contains no legal candidates")
    return tuple(candidates)
