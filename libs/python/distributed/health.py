# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Pure, deterministic evaluation of bounded distributed health reports."""

from __future__ import annotations

from collections.abc import Iterable
from dataclasses import dataclass
from itertools import islice
from typing import Final

from libs.python.errors import InvalidArgument, ResourceExhausted

from .environment import MAXIMUM_WORLD_SIZE, _integer

MAXIMUM_HEALTH_DETAIL_LENGTH: Final = 1024


@dataclass(frozen=True, slots=True)
class RankHealth:
    rank: int
    observed_unix_millis: int
    healthy: bool
    detail: str = ""

    def __post_init__(self) -> None:
        rank = _integer(self.rank, name="rank", minimum=0, maximum=MAXIMUM_WORLD_SIZE - 1)
        observed = _integer(
            self.observed_unix_millis,
            name="observed_unix_millis",
            minimum=0,
            maximum=(1 << 64) - 1,
        )
        if not isinstance(self.healthy, bool):
            raise InvalidArgument("healthy must be a boolean", reason="distributed_health")
        if not isinstance(self.detail, str) or len(self.detail) > MAXIMUM_HEALTH_DETAIL_LENGTH:
            raise InvalidArgument(
                "health detail must be bounded text",
                reason="distributed_health_detail",
            )
        object.__setattr__(self, "rank", rank)
        object.__setattr__(self, "observed_unix_millis", observed)


@dataclass(frozen=True, slots=True)
class ClusterHealth:
    missing_ranks: tuple[int, ...]
    stale_ranks: tuple[int, ...]
    unhealthy_ranks: tuple[int, ...]

    def __post_init__(self) -> None:
        normalized: list[tuple[int, ...]] = []
        for name, values in (
            ("missing_ranks", self.missing_ranks),
            ("stale_ranks", self.stale_ranks),
            ("unhealthy_ranks", self.unhealthy_ranks),
        ):
            try:
                ranks = tuple(values)
            except TypeError as error:
                raise InvalidArgument(
                    f"{name} must be iterable ranks",
                    reason="distributed_cluster_health",
                    cause=error,
                ) from error
            checked = tuple(
                _integer(rank, name=name, minimum=0, maximum=MAXIMUM_WORLD_SIZE - 1)
                for rank in ranks
            )
            if len(set(checked)) != len(checked):
                raise InvalidArgument(
                    f"{name} must not contain duplicate ranks",
                    reason="distributed_cluster_health",
                )
            normalized.append(tuple(sorted(checked)))
        if set(normalized[0]) & (set(normalized[1]) | set(normalized[2])):
            raise InvalidArgument(
                "missing ranks cannot also be stale or unhealthy",
                reason="distributed_cluster_health",
            )
        object.__setattr__(self, "missing_ranks", normalized[0])
        object.__setattr__(self, "stale_ranks", normalized[1])
        object.__setattr__(self, "unhealthy_ranks", normalized[2])

    @property
    def ready(self) -> bool:
        return not (self.missing_ranks or self.stale_ranks or self.unhealthy_ranks)


def evaluate_health(
    reports: Iterable[RankHealth],
    *,
    world_size: int,
    now_unix_millis: int,
    stale_after_millis: int,
) -> ClusterHealth:
    world_size = _integer(world_size, name="world_size", minimum=1, maximum=MAXIMUM_WORLD_SIZE)
    now = _integer(
        now_unix_millis,
        name="now_unix_millis",
        minimum=0,
        maximum=(1 << 64) - 1,
    )
    stale_after = _integer(
        stale_after_millis,
        name="stale_after_millis",
        minimum=1,
        maximum=(1 << 64) - 1,
    )
    try:
        iterator = iter(reports)
    except TypeError as error:
        raise InvalidArgument(
            "health reports must be iterable",
            reason="distributed_health_reports",
            cause=error,
        ) from error
    materialized = tuple(islice(iterator, world_size + 1))
    if len(materialized) > world_size:
        raise ResourceExhausted(
            "health report count exceeds world_size",
            reason="distributed_health_report_count",
        )
    by_rank: dict[int, RankHealth] = {}
    for report in materialized:
        if not isinstance(report, RankHealth) or report.rank >= world_size:
            raise InvalidArgument(
                "health reports must contain in-range RankHealth values",
                reason="distributed_health_report",
            )
        if report.rank in by_rank:
            raise InvalidArgument(
                "health reports contain a duplicate rank",
                reason="distributed_health_duplicate",
            )
        if report.observed_unix_millis > now:
            raise InvalidArgument(
                "health observation cannot be in the future",
                reason="distributed_health_time",
            )
        by_rank[report.rank] = report
    missing = tuple(rank for rank in range(world_size) if rank not in by_rank)
    stale = tuple(
        rank
        for rank, report in sorted(by_rank.items())
        if now - report.observed_unix_millis > stale_after
    )
    unhealthy = tuple(rank for rank, report in sorted(by_rank.items()) if not report.healthy)
    return ClusterHealth(missing, stale, unhealthy)
