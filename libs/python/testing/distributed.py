# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Deterministic environment matrices for multi-rank tests."""

from __future__ import annotations

from collections.abc import Mapping

from libs.python.distributed import DistributedEnvironment
from libs.python.errors import InvalidArgument


def rank_environments(
    world_size: int,
    *,
    local_world_size: int | None = None,
    master_addr: str = "127.0.0.1",
    master_port: int = 29_500,
) -> tuple[Mapping[str, str], ...]:
    """Build one validated environment mapping for every rank."""
    if isinstance(world_size, bool) or not isinstance(world_size, int) or world_size < 1:
        raise InvalidArgument("world_size must be a positive integer", reason="testing_world_size")
    local_size = world_size if local_world_size is None else local_world_size
    if (
        isinstance(local_size, bool)
        or not isinstance(local_size, int)
        or local_size < 1
        or world_size % local_size != 0
    ):
        raise InvalidArgument(
            "local_world_size must be a positive divisor of world_size",
            reason="testing_local_world_size",
        )
    return tuple(
        DistributedEnvironment(
            rank=rank,
            world_size=world_size,
            local_rank=rank % local_size,
            local_world_size=local_size,
            master_addr=master_addr,
            master_port=master_port,
        ).to_environ()
        for rank in range(world_size)
    )
