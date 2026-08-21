# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""The single authoritative progress state for local training."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Final, Self

from libs.python.errors import InvalidArgument, ResourceExhausted

MAXIMUM_PROGRESS_COUNTER: Final = (1 << 63) - 1


@dataclass(frozen=True, slots=True)
class TrainingState:
    """Committed optimizer progress.

    Counters advance together only after an optimizer (and optional scheduler)
    step succeeds. Evaluation and failed or canceled accumulation groups do not
    mutate this value.
    """

    microbatches: int = 0
    optimizer_steps: int = 0
    samples: int = 0

    def __post_init__(self) -> None:
        for name, value in (
            ("microbatches", self.microbatches),
            ("optimizer_steps", self.optimizer_steps),
            ("samples", self.samples),
        ):
            if (
                isinstance(value, bool)
                or not isinstance(value, int)
                or not 0 <= value <= MAXIMUM_PROGRESS_COUNTER
            ):
                raise InvalidArgument(
                    f"training state {name} is outside bounds",
                    reason="training_state_counter",
                    fields={"field": name},
                )
        if self.optimizer_steps > self.microbatches:
            raise InvalidArgument(
                "optimizer steps cannot exceed completed microbatches",
                reason="training_state_order",
            )
        if self.microbatches == 0 and (self.optimizer_steps != 0 or self.samples != 0):
            raise InvalidArgument(
                "empty training state cannot contain progress",
                reason="training_state_empty",
            )
        if self.microbatches > 0 and (
            self.optimizer_steps == 0 or self.samples < self.microbatches
        ):
            raise InvalidArgument(
                "training state counters are inconsistent",
                reason="training_state_consistency",
            )

    def after_optimizer_step(self, *, microbatches: int, samples: int) -> Self:
        """Return the next committed state for one accumulation group."""

        _positive_increment(microbatches, name="microbatches")
        _positive_increment(samples, name="samples")
        if samples < microbatches:
            raise InvalidArgument(
                "completed samples cannot be fewer than completed microbatches",
                reason="training_state_increment",
            )
        values = (
            self.microbatches + microbatches,
            self.optimizer_steps + 1,
            self.samples + samples,
        )
        if any(value > MAXIMUM_PROGRESS_COUNTER for value in values):
            raise ResourceExhausted(
                "training progress counter exhausted",
                reason="training_state_overflow",
            )
        return type(self)(*values)


def _positive_increment(value: object, *, name: str) -> int:
    if (
        isinstance(value, bool)
        or not isinstance(value, int)
        or not 1 <= value <= MAXIMUM_PROGRESS_COUNTER
    ):
        raise InvalidArgument(
            f"training state {name} increment is outside bounds",
            reason="training_state_increment",
            fields={"field": name},
        )
    return value
