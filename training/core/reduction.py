# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Reduction boundary used by the authoritative eager trainer.

The core trainer owns optimizer ordering and loss normalization.  A reducer owns
only the collectives needed to preserve those semantics across replicas.  This
keeps the local path dependency-free from ``torch.distributed`` while making the
distributed denominator and DDP averaging correction impossible to omit.
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Protocol, runtime_checkable

import torch

from libs.python.errors import InvalidArgument


@dataclass(frozen=True, slots=True)
class ReducedCounts:
    """Globally reduced counters for one collective optimizer group."""

    denominator: int
    microbatches: int
    samples: int

    def __post_init__(self) -> None:
        for name, value in (
            ("denominator", self.denominator),
            ("microbatches", self.microbatches),
            ("samples", self.samples),
        ):
            if isinstance(value, bool) or not isinstance(value, int) or value <= 0:
                raise InvalidArgument(
                    f"reduced {name} must be a positive integer",
                    reason="training_reduction_count",
                    fields={"field": name},
                )
        if self.samples < self.microbatches:
            raise InvalidArgument(
                "reduced samples cannot be fewer than reduced microbatches",
                reason="training_reduction_count",
            )


@runtime_checkable
class Reducer(Protocol):
    """Collective operations required by :class:`Trainer`.

    ``backward_scale`` compensates for the gradient averaging performed by DDP.
    For local execution both it and ``world_size`` are exactly one.
    """

    @property
    def world_size(self) -> int: ...

    @property
    def backward_scale(self) -> float: ...

    def validate_schedule(
        self,
        *,
        microbatches: int,
        accumulation_steps: int,
        device: torch.device,
    ) -> None: ...

    def reduce_counts(
        self,
        *,
        denominator: int,
        microbatches: int,
        samples: int,
        device: torch.device,
    ) -> ReducedCounts: ...

    def reduce_loss_sum(self, value: float, *, device: torch.device) -> float: ...

    def any_true(self, value: bool, *, device: torch.device) -> bool: ...


@dataclass(frozen=True, slots=True)
class LocalReducer:
    """Identity reduction for the single-process reference path."""

    @property
    def world_size(self) -> int:
        return 1

    @property
    def backward_scale(self) -> float:
        return 1.0

    def validate_schedule(
        self,
        *,
        microbatches: int,
        accumulation_steps: int,
        device: torch.device,
    ) -> None:
        del microbatches, accumulation_steps, device

    def reduce_counts(
        self,
        *,
        denominator: int,
        microbatches: int,
        samples: int,
        device: torch.device,
    ) -> ReducedCounts:
        del device
        return ReducedCounts(denominator, microbatches, samples)

    def reduce_loss_sum(self, value: float, *, device: torch.device) -> float:
        del device
        return float(value)

    def any_true(self, value: bool, *, device: torch.device) -> bool:
        del device
        return value


__all__ = ["LocalReducer", "ReducedCounts", "Reducer"]
