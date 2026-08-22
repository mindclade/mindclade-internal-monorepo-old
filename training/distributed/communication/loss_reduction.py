# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Exact-count and numerator reductions for DDP training."""

from __future__ import annotations

import math
from dataclasses import dataclass
from typing import Final

import torch

from libs.python.errors import FailedPrecondition, InvalidArgument, ResourceExhausted
from training.core import ReducedCounts
from training.distributed.context import DistributedContext

MAXIMUM_REDUCED_COUNT: Final = (1 << 63) - 1


@dataclass(frozen=True, slots=True)
class DDPReducer:
    """Reducer paired with PyTorch DDP's average-gradient semantics."""

    context: DistributedContext

    def __post_init__(self) -> None:
        if not isinstance(self.context, DistributedContext):
            raise InvalidArgument(
                "DDP reducer requires a DistributedContext",
                reason="distributed_reducer_context",
            )
        self.context.validate_active()

    @property
    def world_size(self) -> int:
        return self.context.world_size

    @property
    def backward_scale(self) -> float:
        # DDP averages each gradient bucket across replicas. Multiplying each
        # local numerator by world size before backward restores a global sum.
        return float(self.world_size)

    def validate_schedule(
        self,
        *,
        microbatches: int,
        accumulation_steps: int,
        device: torch.device,
    ) -> None:
        self._validate_device(device)
        for name, value in (
            ("microbatches", microbatches),
            ("accumulation_steps", accumulation_steps),
        ):
            if isinstance(value, bool) or not isinstance(value, int) or value <= 0:
                raise InvalidArgument(
                    f"distributed {name} must be a positive integer",
                    reason="distributed_schedule",
                    fields={"field": name},
                )
        schedule = torch.tensor(
            [microbatches, accumulation_steps],
            dtype=torch.int64,
            device=device,
        )
        minimum = schedule.clone()
        maximum = schedule.clone()
        torch.distributed.all_reduce(minimum, op=torch.distributed.ReduceOp.MIN)
        torch.distributed.all_reduce(maximum, op=torch.distributed.ReduceOp.MAX)
        if not torch.equal(minimum, maximum):
            raise FailedPrecondition(
                "all ranks must use the same microbatch and accumulation schedule",
                reason="distributed_schedule_mismatch",
            )

    def reduce_counts(
        self,
        *,
        denominator: int,
        microbatches: int,
        samples: int,
        device: torch.device,
    ) -> ReducedCounts:
        self._validate_device(device)
        maximum_local = MAXIMUM_REDUCED_COUNT // self.world_size
        for name, value in (
            ("denominator", denominator),
            ("microbatches", microbatches),
            ("samples", samples),
        ):
            if (
                isinstance(value, bool)
                or not isinstance(value, int)
                or not 1 <= value <= maximum_local
            ):
                raise ResourceExhausted(
                    f"distributed {name} is outside the safe reduction bound",
                    reason="distributed_reduction_bound",
                    fields={"field": name},
                )
        counts = torch.tensor(
            [denominator, microbatches, samples],
            dtype=torch.int64,
            device=device,
        )
        torch.distributed.all_reduce(counts, op=torch.distributed.ReduceOp.SUM)
        values = counts.to(device="cpu").tolist()
        return ReducedCounts(*(int(value) for value in values))

    def reduce_loss_sum(self, value: float, *, device: torch.device) -> float:
        self._validate_device(device)
        if (
            isinstance(value, bool)
            or not isinstance(value, int | float)
            or not math.isfinite(value)
        ):
            raise InvalidArgument(
                "distributed loss numerator must be finite",
                reason="distributed_loss_sum",
            )
        numerator = torch.tensor(float(value), dtype=torch.float64, device=device)
        torch.distributed.all_reduce(numerator, op=torch.distributed.ReduceOp.SUM)
        result = float(numerator.to(device="cpu").item())
        if not math.isfinite(result):
            raise FloatingPointError("globally reduced loss numerator is not finite")
        return result

    def any_true(self, value: bool, *, device: torch.device) -> bool:
        self._validate_device(device)
        if not isinstance(value, bool):
            raise InvalidArgument(
                "distributed boolean reduction requires a boolean",
                reason="distributed_boolean",
            )
        flag = torch.tensor(int(value), dtype=torch.int32, device=device)
        torch.distributed.all_reduce(flag, op=torch.distributed.ReduceOp.MAX)
        return bool(flag.to(device="cpu").item())

    def _validate_device(self, device: torch.device) -> None:
        self.context.validate_active()
        if not isinstance(device, torch.device) or device != self.context.device:
            raise FailedPrecondition(
                "collective tensor device does not match the distributed context",
                reason="distributed_collective_device",
            )


__all__ = ["DDPReducer"]
