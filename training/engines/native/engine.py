# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Thin eager adapter over the one authoritative trainer."""

from __future__ import annotations

from collections.abc import Sequence

from libs.python.errors import InvalidArgument
from training.contracts import StepResult, SupervisedBatch, TrainingState
from training.core import CancellationCheck, Trainer


class NativeEngine:
    """Expose the CPU eager path without duplicating optimizer-loop semantics."""

    def __init__(self, trainer: Trainer) -> None:
        if not isinstance(trainer, Trainer):
            raise InvalidArgument(
                "native engine requires the authoritative Trainer",
                reason="training_native_trainer",
            )
        self._trainer = trainer

    @property
    def trainer(self) -> Trainer:
        return self._trainer

    @property
    def state(self) -> TrainingState:
        return self._trainer.state

    def train(
        self,
        batches: Sequence[SupervisedBatch],
        *,
        cancellation_check: CancellationCheck | None = None,
    ) -> tuple[StepResult, ...]:
        return self._trainer.train(batches, cancellation_check=cancellation_check)

    def evaluate(
        self,
        batches: Sequence[SupervisedBatch],
        *,
        cancellation_check: CancellationCheck | None = None,
    ) -> StepResult:
        return self._trainer.evaluate(batches, cancellation_check=cancellation_check)
