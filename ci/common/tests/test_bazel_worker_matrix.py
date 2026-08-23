# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

import pytest

from ci.common.bazel_worker_matrix import (
    PRESUBMIT_WORKER,
    UNSHARDED_FULL_WORKER,
    WorkerMatrixError,
    select,
)


@pytest.mark.parametrize("remote_cache_enabled", [False, True])
def test_pull_request_stays_unsharded_and_obeys_selector_activation(
    remote_cache_enabled: bool,
) -> None:
    matrix = select(
        lane="presubmit",
        event="pull_request",
        remote_cache_enabled=remote_cache_enabled,
        shard_count=4,
    )
    assert matrix.workers == (PRESUBMIT_WORKER,)
    assert matrix.mode == "presubmit-auto"
    assert matrix.shard_count == 4


@pytest.mark.parametrize(
    "lane,event",
    [
        ("presubmit", "merge_group"),
        ("presubmit", "push"),
        ("nightly", "schedule"),
        ("nightly", "workflow_dispatch"),
    ],
)
def test_complete_gate_uses_unsharded_fallback_without_remote_cache(lane: str, event: str) -> None:
    matrix = select(
        lane=lane,
        event=event,
        remote_cache_enabled=False,
        shard_count=4,
    )
    assert matrix.workers == (UNSHARDED_FULL_WORKER,)
    assert matrix.mode == "full-unsharded"
    assert matrix.shard_count == 4


@pytest.mark.parametrize(
    "lane,event",
    [
        ("presubmit", "merge_group"),
        ("presubmit", "push"),
        ("nightly", "schedule"),
        ("nightly", "workflow_dispatch"),
    ],
)
def test_complete_gate_fans_out_exact_contract_shards_with_remote_cache(
    lane: str, event: str
) -> None:
    matrix = select(
        lane=lane,
        event=event,
        remote_cache_enabled=True,
        shard_count=4,
    )
    assert matrix.workers == (0, 1, 2, 3)
    assert matrix.mode == "full-sharded"
    assert matrix.shard_count == 4
    assert matrix.encoded_workers() == "[0,1,2,3]"


@pytest.mark.parametrize(
    "lane,event",
    [("presubmit", "schedule"), ("nightly", "pull_request"), ("unknown", "push")],
)
def test_unknown_lane_or_event_fails_closed(lane: str, event: str) -> None:
    with pytest.raises(WorkerMatrixError):
        select(
            lane=lane,
            event=event,
            remote_cache_enabled=True,
            shard_count=4,
        )
