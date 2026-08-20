# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Validated rendezvous configuration and generation membership."""

from __future__ import annotations

import re
from dataclasses import dataclass
from typing import Final
from urllib.parse import urlsplit

from libs.python.errors import InvalidArgument

from .environment import MAXIMUM_WORLD_SIZE, _integer

MAXIMUM_RUN_ID_LENGTH: Final = 128
MAXIMUM_ENDPOINT_LENGTH: Final = 512
MAXIMUM_RENDEZVOUS_TIMEOUT_SECONDS: Final = 86400
_RUN_ID: Final = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$")


@dataclass(frozen=True, slots=True)
class RendezvousConfig:
    run_id: str
    endpoint: str
    min_nodes: int
    max_nodes: int
    timeout_seconds: int = 900

    def __post_init__(self) -> None:
        if not isinstance(self.run_id, str) or _RUN_ID.fullmatch(self.run_id) is None:
            raise InvalidArgument(
                "rendezvous run_id must be a bounded canonical name",
                reason="distributed_rendezvous_run_id",
            )
        if (
            not isinstance(self.endpoint, str)
            or not self.endpoint
            or len(self.endpoint) > MAXIMUM_ENDPOINT_LENGTH
        ):
            raise InvalidArgument(
                "rendezvous endpoint must be bounded text",
                reason="distributed_rendezvous_endpoint",
            )
        try:
            parsed = urlsplit(f"//{self.endpoint}")
            port = parsed.port
        except ValueError as error:
            raise InvalidArgument(
                "rendezvous endpoint must be host:port",
                reason="distributed_rendezvous_endpoint",
                cause=error,
            ) from error
        if (
            not parsed.hostname
            or port is None
            or not 1 <= port <= 65535
            or parsed.username is not None
            or parsed.password is not None
            or parsed.path not in ("", "/")
            or parsed.query
            or parsed.fragment
            or any(character.isspace() or ord(character) < 0x20 for character in self.endpoint)
        ):
            raise InvalidArgument(
                "rendezvous endpoint must be host:port without credentials or paths",
                reason="distributed_rendezvous_endpoint",
            )
        min_nodes = _integer(
            self.min_nodes, name="min_nodes", minimum=1, maximum=MAXIMUM_WORLD_SIZE
        )
        max_nodes = _integer(
            self.max_nodes, name="max_nodes", minimum=1, maximum=MAXIMUM_WORLD_SIZE
        )
        if min_nodes > max_nodes:
            raise InvalidArgument(
                "rendezvous min_nodes cannot exceed max_nodes",
                reason="distributed_rendezvous_nodes",
            )
        timeout = _integer(
            self.timeout_seconds,
            name="timeout_seconds",
            minimum=1,
            maximum=MAXIMUM_RENDEZVOUS_TIMEOUT_SECONDS,
        )
        object.__setattr__(self, "min_nodes", min_nodes)
        object.__setattr__(self, "max_nodes", max_nodes)
        object.__setattr__(self, "timeout_seconds", timeout)


@dataclass(frozen=True, slots=True)
class RendezvousState:
    generation: int
    world_size: int
    participants: tuple[int, ...]

    def __post_init__(self) -> None:
        generation = _integer(
            self.generation,
            name="generation",
            minimum=0,
            maximum=(1 << 64) - 1,
        )
        world_size = _integer(
            self.world_size,
            name="world_size",
            minimum=1,
            maximum=MAXIMUM_WORLD_SIZE,
        )
        try:
            participants = tuple(self.participants)
        except TypeError as error:
            raise InvalidArgument(
                "rendezvous participants must be iterable ranks",
                reason="distributed_rendezvous_participants",
                cause=error,
            ) from error
        normalized_participants = tuple(
            _integer(
                participant,
                name="participant rank",
                minimum=0,
                maximum=world_size - 1,
            )
            for participant in participants
        )
        if len(normalized_participants) != world_size or set(normalized_participants) != set(
            range(world_size)
        ):
            raise InvalidArgument(
                "rendezvous participants must contain every world rank exactly once",
                reason="distributed_rendezvous_participants",
            )
        object.__setattr__(self, "generation", generation)
        object.__setattr__(self, "world_size", world_size)
        object.__setattr__(self, "participants", tuple(range(world_size)))
