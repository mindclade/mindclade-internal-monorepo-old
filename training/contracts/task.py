# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Task boundary between model semantics and the authoritative trainer."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Protocol, runtime_checkable

import torch
from torch import nn

from libs.python.errors import InvalidArgument

from .batch import SupervisedBatch
from .state import MAXIMUM_PROGRESS_COUNTER


@dataclass(frozen=True, slots=True)
class TaskResult:
    """A differentiable scalar loss sum and its exact reduction denominator."""

    loss_sum: torch.Tensor
    denominator: int

    def __post_init__(self) -> None:
        if not isinstance(self.loss_sum, torch.Tensor):
            raise InvalidArgument(
                "task loss_sum must be a torch.Tensor",
                reason="training_task_loss",
            )
        if (
            self.loss_sum.ndim != 0
            or self.loss_sum.device.type not in {"cpu", "cuda"}
            or self.loss_sum.dtype is not torch.float32
        ):
            raise InvalidArgument(
                "task loss_sum must be a scalar CPU or CUDA float32 tensor",
                reason="training_task_loss_contract",
            )
        if not bool(torch.isfinite(self.loss_sum.detach()).item()):
            raise FloatingPointError("task loss_sum is not finite")
        if (
            isinstance(self.denominator, bool)
            or not isinstance(self.denominator, int)
            or not 1 <= self.denominator <= MAXIMUM_PROGRESS_COUNTER
        ):
            raise InvalidArgument(
                "task loss denominator is outside bounds",
                reason="training_task_denominator",
            )


@runtime_checkable
class Task(Protocol):
    """Interpret a batch and compute a model-dependent objective."""

    def compute(self, model: nn.Module, batch: SupervisedBatch) -> TaskResult: ...
