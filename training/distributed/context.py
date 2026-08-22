# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Validated torchrun environment and active process-group identity."""

from __future__ import annotations

from collections.abc import Mapping
from dataclasses import dataclass
from typing import Final, Literal

import torch

from libs.python.errors import FailedPrecondition, InvalidArgument

MAXIMUM_TIMEOUT_SECONDS: Final = 3_600
SUPPORTED_DISTRIBUTED_WORLD_SIZES: Final = frozenset({2, 8})

Backend = Literal["gloo", "nccl"]


@dataclass(frozen=True, slots=True)
class TorchrunEnvironment:
    """Rank assignment supplied by ``torchrun``; application code never invents it."""

    rank: int
    local_rank: int
    world_size: int
    local_world_size: int

    def __post_init__(self) -> None:
        for name, value in (
            ("rank", self.rank),
            ("local_rank", self.local_rank),
            ("world_size", self.world_size),
            ("local_world_size", self.local_world_size),
        ):
            if isinstance(value, bool) or not isinstance(value, int):
                raise InvalidArgument(
                    f"torchrun {name} must be an integer",
                    reason="distributed_environment",
                    fields={"field": name},
                )
        if self.world_size not in SUPPORTED_DISTRIBUTED_WORLD_SIZES:
            raise InvalidArgument(
                "torchrun world size must be an approved value (2 or 8)",
                reason="distributed_world_size",
            )
        if self.local_world_size != self.world_size:
            raise InvalidArgument(
                "torchrun local world size must equal world size; only single-node training is supported",
                reason="distributed_local_world_size",
            )
        if not 0 <= self.rank < self.world_size or not 0 <= self.local_rank < self.local_world_size:
            raise InvalidArgument(
                "torchrun rank is outside its declared world",
                reason="distributed_rank",
            )
        if self.rank != self.local_rank:
            raise InvalidArgument(
                "torchrun global and local rank must match for a single-node world",
                reason="distributed_rank",
            )

    @classmethod
    def from_environ(cls, environ: Mapping[str, str]) -> TorchrunEnvironment:
        if not isinstance(environ, Mapping):
            raise InvalidArgument(
                "torchrun environment must be a mapping",
                reason="distributed_environment",
            )
        values: dict[str, int] = {}
        for field, variable in (
            ("rank", "RANK"),
            ("local_rank", "LOCAL_RANK"),
            ("world_size", "WORLD_SIZE"),
            ("local_world_size", "LOCAL_WORLD_SIZE"),
        ):
            raw = environ.get(variable)
            if not isinstance(raw, str) or not raw or not raw.isascii() or not raw.isdecimal():
                raise InvalidArgument(
                    f"torchrun environment variable {variable} is missing or invalid",
                    reason="distributed_environment",
                    fields={"field": variable},
                )
            values[field] = int(raw)
        return cls(**values)


@dataclass(frozen=True, slots=True)
class DistributedConfig:
    """Bounded process-group initialization policy."""

    backend: Backend = "gloo"
    timeout_seconds: int = 300

    def __post_init__(self) -> None:
        if self.backend not in {"gloo", "nccl"}:
            raise InvalidArgument(
                "distributed backend must be gloo or nccl",
                reason="distributed_backend",
            )
        if (
            isinstance(self.timeout_seconds, bool)
            or not isinstance(self.timeout_seconds, int)
            or not 1 <= self.timeout_seconds <= MAXIMUM_TIMEOUT_SECONDS
        ):
            raise InvalidArgument(
                "distributed timeout is outside bounds",
                reason="distributed_timeout",
            )


@dataclass(frozen=True, slots=True)
class DistributedContext:
    """Identity of the process group initialized by this package."""

    environment: TorchrunEnvironment
    backend: Backend
    device: torch.device

    def __post_init__(self) -> None:
        if not isinstance(self.environment, TorchrunEnvironment):
            raise InvalidArgument(
                "distributed context environment is invalid",
                reason="distributed_context",
            )
        expected_type = "cpu" if self.backend == "gloo" else "cuda"
        if self.device.type != expected_type:
            raise InvalidArgument(
                "distributed backend and device are incompatible",
                reason="distributed_device",
            )
        expected_world_size = 2 if self.backend == "gloo" else 8
        if self.environment.world_size != expected_world_size:
            raise InvalidArgument(
                "distributed backend does not match the approved world size",
                reason="distributed_backend_world_size",
            )
        if self.device.type == "cuda" and self.device.index != self.environment.local_rank:
            raise InvalidArgument(
                "CUDA device index must equal torchrun local rank",
                reason="distributed_device",
            )

    @property
    def rank(self) -> int:
        return self.environment.rank

    @property
    def local_rank(self) -> int:
        return self.environment.local_rank

    @property
    def world_size(self) -> int:
        return self.environment.world_size

    def validate_active(self) -> None:
        if not torch.distributed.is_available() or not torch.distributed.is_initialized():
            raise FailedPrecondition(
                "distributed process group is not initialized",
                reason="distributed_not_initialized",
            )
        if (
            torch.distributed.get_rank() != self.rank
            or torch.distributed.get_world_size() != self.world_size
            or torch.distributed.get_backend() != self.backend
        ):
            raise FailedPrecondition(
                "active process group does not match the validated context",
                reason="distributed_context_mismatch",
            )


__all__ = [
    "Backend",
    "DistributedConfig",
    "DistributedContext",
    "TorchrunEnvironment",
]
