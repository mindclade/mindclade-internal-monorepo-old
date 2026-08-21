# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Progress-state invariants."""

import pytest

from libs.python.errors import ResourceExhausted
from training.contracts.state import MAXIMUM_PROGRESS_COUNTER, TrainingState


def test_state_advances_one_committed_optimizer_group_at_a_time() -> None:
    initial = TrainingState()

    first = initial.after_optimizer_step(microbatches=3, samples=7)
    second = first.after_optimizer_step(microbatches=1, samples=2)

    assert initial == TrainingState()
    assert first == TrainingState(microbatches=3, optimizer_steps=1, samples=7)
    assert second == TrainingState(microbatches=4, optimizer_steps=2, samples=9)


@pytest.mark.parametrize(
    "state",
    [
        {"microbatches": -1},
        {"microbatches": 1, "optimizer_steps": 2, "samples": 1},
        {"microbatches": 1, "optimizer_steps": 0, "samples": 1},
        {"microbatches": 2, "optimizer_steps": 1, "samples": 1},
        {"microbatches": True},
    ],
)
def test_state_rejects_inconsistent_counters(state: dict[str, int]) -> None:
    with pytest.raises(ValueError):
        TrainingState(**state)


@pytest.mark.parametrize(
    ("microbatches", "samples"),
    [(0, 1), (1, 0), (2, 1), (True, 1)],
)
def test_state_rejects_invalid_group_increments(microbatches: int, samples: int) -> None:
    with pytest.raises(ValueError):
        TrainingState().after_optimizer_step(microbatches=microbatches, samples=samples)


def test_state_fails_closed_on_counter_overflow() -> None:
    state = TrainingState(
        microbatches=MAXIMUM_PROGRESS_COUNTER,
        optimizer_steps=1,
        samples=MAXIMUM_PROGRESS_COUNTER,
    )

    with pytest.raises(ResourceExhausted, match="counter exhausted"):
        state.after_optimizer_step(microbatches=1, samples=1)
