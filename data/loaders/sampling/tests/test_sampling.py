# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import pytest

from data.loaders.sampling import (
    eligible_indices,
    mixture_schedule,
    sample_indices,
    stable_order,
    temperature_weights,
)


def test_sampling_is_local_seeded_and_reproducible() -> None:
    identities = ("a", "b", "c", "d")
    assert stable_order(identities, seed=3, epoch=2) == stable_order(
        reversed(identities), seed=3, epoch=2
    )
    assert sample_indices(10, 4, seed=9) == sample_indices(10, 4, seed=9)
    assert mixture_schedule((0.2, 0.8), 20, seed=7) == mixture_schedule((0.2, 0.8), 20, seed=7)


def test_temperature_and_curriculum_validate_domains() -> None:
    assert sum(temperature_weights((1.0, 2.0), 1.0)) == pytest.approx(1.0)
    assert eligible_indices((0.1, 0.5, 0.2), maximum=0.2) == (0, 2)
    with pytest.raises(ValueError, match="positive"):
        temperature_weights((1.0, 0.0), 1.0)
