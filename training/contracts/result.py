# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Detached results from authoritative training and evaluation steps."""

from __future__ import annotations

import math
from collections.abc import Mapping
from dataclasses import dataclass, field
from types import MappingProxyType
from typing import Final

from libs.python.errors import InvalidArgument

from .state import MAXIMUM_PROGRESS_COUNTER, TrainingState

MAXIMUM_METRICS: Final = 256
MAXIMUM_METRIC_NAME_LENGTH: Final = 128


@dataclass(frozen=True, slots=True)
class StepResult:
    """A finite, immutable summary of an optimizer group or evaluation pass."""

    state: TrainingState
    loss_sum: float
    denominator: int
    microbatches: int
    samples: int
    optimizer_step: bool
    metrics: Mapping[str, float] = field(default_factory=dict)

    def __post_init__(self) -> None:
        if not isinstance(self.state, TrainingState):
            raise InvalidArgument(
                "step result state must be TrainingState",
                reason="training_result_state",
            )
        loss_sum = _finite_number(self.loss_sum, name="loss_sum")
        object.__setattr__(self, "loss_sum", loss_sum)
        for counter_name, counter_value in (
            ("denominator", self.denominator),
            ("microbatches", self.microbatches),
            ("samples", self.samples),
        ):
            if (
                isinstance(counter_value, bool)
                or not isinstance(counter_value, int)
                or not 1 <= counter_value <= MAXIMUM_PROGRESS_COUNTER
            ):
                raise InvalidArgument(
                    f"step result {counter_name} is outside bounds",
                    reason="training_result_counter",
                    fields={"field": counter_name},
                )
        if self.samples < self.microbatches:
            raise InvalidArgument(
                "step result samples cannot be fewer than microbatches",
                reason="training_result_consistency",
            )
        if not isinstance(self.optimizer_step, bool):
            raise InvalidArgument(
                "step result optimizer_step must be boolean",
                reason="training_result_step_kind",
            )
        if not isinstance(self.metrics, Mapping) or len(self.metrics) > MAXIMUM_METRICS:
            raise InvalidArgument(
                "step result metrics are outside bounds",
                reason="training_result_metrics",
            )
        normalized: dict[str, float] = {}
        for metric_name, metric_value in self.metrics.items():
            if (
                not isinstance(metric_name, str)
                or not metric_name
                or len(metric_name) > MAXIMUM_METRIC_NAME_LENGTH
                or any(ord(character) < 0x20 for character in metric_name)
            ):
                raise InvalidArgument(
                    "step result metric name is invalid",
                    reason="training_result_metric_name",
                )
            normalized[metric_name] = _finite_number(metric_value, name=f"metric {metric_name}")
        object.__setattr__(self, "metrics", MappingProxyType(normalized))

    @property
    def mean_loss(self) -> float:
        return self.loss_sum / self.denominator


def _finite_number(value: object, *, name: str) -> float:
    if isinstance(value, bool) or not isinstance(value, int | float) or not math.isfinite(value):
        raise InvalidArgument(
            f"step result {name} must be a finite number",
            reason="training_result_nonfinite",
        )
    return float(value)
