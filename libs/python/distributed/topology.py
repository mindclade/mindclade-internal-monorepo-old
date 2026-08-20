# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Deterministic rank layout for independent parallel dimensions."""

from __future__ import annotations

from dataclasses import dataclass

from libs.python.errors import InvalidArgument
from libs.python.identifiers import Digest
from libs.python.serialization import canonical_json_bytes

from .environment import MAXIMUM_WORLD_SIZE, _integer

_DIMENSIONS = ("data", "tensor", "pipeline")


@dataclass(frozen=True, slots=True)
class ProcessTopology:
    world_size: int
    local_world_size: int
    data_parallel: int = 1
    tensor_parallel: int = 1
    pipeline_parallel: int = 1

    def __post_init__(self) -> None:
        world_size = _integer(
            self.world_size,
            name="world_size",
            minimum=1,
            maximum=MAXIMUM_WORLD_SIZE,
        )
        local_world_size = _integer(
            self.local_world_size,
            name="local_world_size",
            minimum=1,
            maximum=MAXIMUM_WORLD_SIZE,
        )
        dimensions = tuple(
            _integer(value, name=f"{name}_parallel", minimum=1, maximum=MAXIMUM_WORLD_SIZE)
            for name, value in zip(
                _DIMENSIONS,
                (self.data_parallel, self.tensor_parallel, self.pipeline_parallel),
                strict=True,
            )
        )
        if world_size % local_world_size != 0:
            raise InvalidArgument(
                "local_world_size must divide world_size",
                reason="distributed_topology_local_world",
            )
        if dimensions[0] * dimensions[1] * dimensions[2] != world_size:
            raise InvalidArgument(
                "parallel dimensions must multiply to world_size",
                reason="distributed_topology_product",
            )
        object.__setattr__(self, "world_size", world_size)
        object.__setattr__(self, "local_world_size", local_world_size)
        object.__setattr__(self, "data_parallel", dimensions[0])
        object.__setattr__(self, "tensor_parallel", dimensions[1])
        object.__setattr__(self, "pipeline_parallel", dimensions[2])

    def coordinates(self, rank: int) -> tuple[int, int, int]:
        rank = _integer(rank, name="rank", minimum=0, maximum=self.world_size - 1)
        pipeline = rank % self.pipeline_parallel
        rank //= self.pipeline_parallel
        tensor = rank % self.tensor_parallel
        data = rank // self.tensor_parallel
        return data, tensor, pipeline

    def rank(self, coordinates: tuple[int, int, int]) -> int:
        if not isinstance(coordinates, tuple) or len(coordinates) != 3:
            raise InvalidArgument(
                "topology coordinates must be a three-item tuple",
                reason="distributed_topology_coordinates",
            )
        data = _integer(
            coordinates[0], name="data coordinate", minimum=0, maximum=self.data_parallel - 1
        )
        tensor = _integer(
            coordinates[1],
            name="tensor coordinate",
            minimum=0,
            maximum=self.tensor_parallel - 1,
        )
        pipeline = _integer(
            coordinates[2],
            name="pipeline coordinate",
            minimum=0,
            maximum=self.pipeline_parallel - 1,
        )
        return (data * self.tensor_parallel + tensor) * self.pipeline_parallel + pipeline

    def groups(self, dimension: str) -> tuple[tuple[int, ...], ...]:
        if dimension not in _DIMENSIONS:
            raise InvalidArgument(
                f"topology dimension must be one of {_DIMENSIONS}",
                reason="distributed_topology_dimension",
            )
        dimension_index = _DIMENSIONS.index(dimension)
        grouped: dict[tuple[int, int], list[int]] = {}
        for rank in range(self.world_size):
            coordinates = self.coordinates(rank)
            remaining = tuple(
                coordinate
                for index, coordinate in enumerate(coordinates)
                if index != dimension_index
            )
            key = (remaining[0], remaining[1])
            grouped.setdefault(key, []).append(rank)
        return tuple(tuple(grouped[key]) for key in sorted(grouped))

    @property
    def fingerprint(self) -> Digest:
        return Digest.of(
            canonical_json_bytes(
                {
                    "world_size": self.world_size,
                    "local_world_size": self.local_world_size,
                    "data_parallel": self.data_parallel,
                    "tensor_parallel": self.tensor_parallel,
                    "pipeline_parallel": self.pipeline_parallel,
                }
            )
        )
