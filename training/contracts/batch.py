# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Validated tensor batches for the eager reference trainer.

The tensors remain caller-owned. The frozen record prevents field replacement,
but deliberately does not clone tensor storage or perform an implicit device or
dtype conversion.
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Final

import torch

from libs.python.errors import InvalidArgument, ResourceExhausted

MAXIMUM_TENSOR_RANK: Final = 16
MAXIMUM_BATCH_SAMPLES: Final = 1_000_000
MAXIMUM_BATCH_ELEMENTS: Final = 100_000_000


@dataclass(frozen=True, slots=True)
class SupervisedBatch:
    """A finite CPU-or-CUDA float32 batch with a shared leading dimension.

    ``inputs`` has shape ``[B, ...]`` and is passed unchanged to the model.
    ``targets`` has shape ``[B, ...]`` and is interpreted by the owning task.
    Both tensors may be non-contiguous; ``B`` must be positive.
    """

    inputs: torch.Tensor
    targets: torch.Tensor

    def __post_init__(self) -> None:
        _validate_tensor(self.inputs, name="inputs")
        _validate_tensor(self.targets, name="targets")
        if self.inputs.device != self.targets.device:
            raise InvalidArgument(
                "supervised inputs and targets must be on the same device",
                reason="training_batch_device",
            )
        if self.inputs.shape[0] != self.targets.shape[0]:
            raise InvalidArgument(
                "supervised inputs and targets must have the same batch dimension",
                reason="training_batch_dimension",
            )

    @property
    def batch_size(self) -> int:
        return int(self.inputs.shape[0])

    @property
    def target_elements(self) -> int:
        return self.targets.numel()

    @property
    def device(self) -> torch.device:
        return self.inputs.device


def _validate_tensor(value: object, *, name: str) -> None:
    if not isinstance(value, torch.Tensor):
        raise InvalidArgument(
            f"supervised batch {name} must be a torch.Tensor",
            reason="training_batch_tensor",
            fields={"field": name},
        )
    if value.device.type not in {"cpu", "cuda"} or value.dtype is not torch.float32:
        raise InvalidArgument(
            f"supervised batch {name} must be CPU or CUDA float32",
            reason="training_batch_placement",
            fields={"field": name},
        )
    if value.ndim == 0 or value.ndim > MAXIMUM_TENSOR_RANK:
        raise InvalidArgument(
            f"supervised batch {name} rank is outside bounds",
            reason="training_batch_rank",
            fields={"field": name},
        )
    if not 1 <= value.shape[0] <= MAXIMUM_BATCH_SAMPLES:
        raise InvalidArgument(
            f"supervised batch {name} leading dimension is outside bounds",
            reason="training_batch_size",
            fields={"field": name},
        )
    if value.numel() == 0:
        raise InvalidArgument(
            f"supervised batch {name} must contain values",
            reason="training_batch_empty",
            fields={"field": name},
        )
    if value.numel() > MAXIMUM_BATCH_ELEMENTS:
        raise ResourceExhausted(
            f"supervised batch {name} exceeds the element bound",
            reason="training_batch_elements",
            fields={"field": name},
        )
    if not bool(torch.isfinite(value.detach()).all().item()):
        raise InvalidArgument(
            f"supervised batch {name} must contain only finite values",
            reason="training_batch_nonfinite",
            fields={"field": name},
        )
