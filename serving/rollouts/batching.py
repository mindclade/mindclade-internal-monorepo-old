# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Stable policy-compatible rollout batching."""

from __future__ import annotations

from .actor import RolloutRequest

SamplingConfigKey = tuple[float, float, int]


def group_requests(
    requests: tuple[RolloutRequest, ...], maximum_batch_size: int
) -> tuple[tuple[RolloutRequest, ...], ...]:
    if isinstance(maximum_batch_size, bool) or not 1 <= maximum_batch_size <= 4096:
        raise ValueError("rollout batch size is outside bounds")
    groups: list[list[RolloutRequest]] = []
    open_groups: dict[tuple[str, SamplingConfigKey], int] = {}
    for request in requests:
        key = (request.policy_digest, _sampling_key(request))
        index = open_groups.get(key)
        if index is None:
            groups.append([])
            index = len(groups) - 1
            open_groups[key] = index
        groups[index].append(request)
        if len(groups[index]) == maximum_batch_size:
            del open_groups[key]
    return tuple(tuple(group) for group in groups)


def _sampling_key(request: RolloutRequest) -> SamplingConfigKey:
    value = request.sampling
    return (value.temperature, value.top_p, value.maximum_tokens)
