# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Validated projection of the standard distributed process environment."""

from __future__ import annotations

import os
from collections.abc import Mapping
from dataclasses import dataclass
from typing import Final

from libs.python.errors import InvalidArgument

MAXIMUM_WORLD_SIZE: Final = 1 << 20
MAXIMUM_MASTER_ADDRESS_LENGTH: Final = 255


def _integer(value: object, *, name: str, minimum: int, maximum: int) -> int:
    if isinstance(value, bool):
        raise InvalidArgument(f"{name} must be an integer", reason="distributed_environment")
    if isinstance(value, str):
        if not value or not value.isascii() or not value.isdecimal():
            raise InvalidArgument(
                f"{name} must use unsigned decimal digits",
                reason="distributed_environment",
            )
        parsed = int(value)
    elif isinstance(value, int):
        parsed = value
    else:
        raise InvalidArgument(
            f"{name} must be an integer",
            reason="distributed_environment",
        )
    if not minimum <= parsed <= maximum:
        raise InvalidArgument(
            f"{name} must be in [{minimum}, {maximum}]",
            reason="distributed_environment",
        )
    return parsed


@dataclass(frozen=True, slots=True)
class DistributedEnvironment:
    rank: int
    world_size: int
    local_rank: int
    local_world_size: int
    master_addr: str
    master_port: int

    def __post_init__(self) -> None:
        rank = _integer(self.rank, name="rank", minimum=0, maximum=MAXIMUM_WORLD_SIZE - 1)
        world_size = _integer(
            self.world_size,
            name="world_size",
            minimum=1,
            maximum=MAXIMUM_WORLD_SIZE,
        )
        local_rank = _integer(
            self.local_rank,
            name="local_rank",
            minimum=0,
            maximum=MAXIMUM_WORLD_SIZE - 1,
        )
        local_world_size = _integer(
            self.local_world_size,
            name="local_world_size",
            minimum=1,
            maximum=MAXIMUM_WORLD_SIZE,
        )
        if rank >= world_size or local_rank >= local_world_size:
            raise InvalidArgument(
                "distributed ranks must be smaller than their world sizes",
                reason="distributed_rank_range",
            )
        if world_size % local_world_size != 0 or rank % local_world_size != local_rank:
            raise InvalidArgument(
                "local rank layout must evenly partition the global world",
                reason="distributed_local_layout",
            )
        if (
            not isinstance(self.master_addr, str)
            or not self.master_addr
            or len(self.master_addr) > MAXIMUM_MASTER_ADDRESS_LENGTH
            or any(character.isspace() or ord(character) < 0x20 for character in self.master_addr)
        ):
            raise InvalidArgument(
                "master_addr must be bounded non-whitespace text",
                reason="distributed_master_address",
            )
        master_port = _integer(self.master_port, name="master_port", minimum=1, maximum=65535)
        object.__setattr__(self, "rank", rank)
        object.__setattr__(self, "world_size", world_size)
        object.__setattr__(self, "local_rank", local_rank)
        object.__setattr__(self, "local_world_size", local_world_size)
        object.__setattr__(self, "master_port", master_port)

    @classmethod
    def from_environ(cls, environ: Mapping[str, str] | None = None) -> DistributedEnvironment:
        source = os.environ if environ is None else environ
        if not isinstance(source, Mapping):
            raise InvalidArgument(
                "distributed environment must be a mapping",
                reason="distributed_environment",
            )
        required = (
            "RANK",
            "WORLD_SIZE",
            "LOCAL_RANK",
            "LOCAL_WORLD_SIZE",
            "MASTER_ADDR",
            "MASTER_PORT",
        )
        missing = [name for name in required if name not in source]
        if missing:
            raise InvalidArgument(
                f"distributed environment is missing {', '.join(missing)}",
                reason="distributed_environment_missing",
            )
        return cls(
            rank=_integer(source["RANK"], name="RANK", minimum=0, maximum=MAXIMUM_WORLD_SIZE - 1),
            world_size=_integer(
                source["WORLD_SIZE"],
                name="WORLD_SIZE",
                minimum=1,
                maximum=MAXIMUM_WORLD_SIZE,
            ),
            local_rank=_integer(
                source["LOCAL_RANK"],
                name="LOCAL_RANK",
                minimum=0,
                maximum=MAXIMUM_WORLD_SIZE - 1,
            ),
            local_world_size=_integer(
                source["LOCAL_WORLD_SIZE"],
                name="LOCAL_WORLD_SIZE",
                minimum=1,
                maximum=MAXIMUM_WORLD_SIZE,
            ),
            master_addr=source["MASTER_ADDR"],
            master_port=_integer(
                source["MASTER_PORT"], name="MASTER_PORT", minimum=1, maximum=65535
            ),
        )

    @property
    def node_rank(self) -> int:
        return self.rank // self.local_world_size

    def to_environ(self) -> dict[str, str]:
        return {
            "RANK": str(self.rank),
            "WORLD_SIZE": str(self.world_size),
            "LOCAL_RANK": str(self.local_rank),
            "LOCAL_WORLD_SIZE": str(self.local_world_size),
            "MASTER_ADDR": self.master_addr,
            "MASTER_PORT": str(self.master_port),
        }
