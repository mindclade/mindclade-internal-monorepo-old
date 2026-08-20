# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

import pytest

from libs.python.distributed import RankHealth, evaluate_health


def test_health_reports_missing_stale_and_unhealthy_ranks() -> None:
    health = evaluate_health(
        (RankHealth(0, 900, True), RankHealth(1, 700, False)),
        world_size=3,
        now_unix_millis=1_000,
        stale_after_millis=200,
    )
    assert health.missing_ranks == (2,)
    assert health.stale_ranks == (1,)
    assert health.unhealthy_ranks == (1,)
    assert not health.ready


def test_health_rejects_duplicate_or_future_observations() -> None:
    with pytest.raises(ValueError, match="duplicate"):
        evaluate_health(
            (RankHealth(0, 100, True), RankHealth(0, 100, True)),
            world_size=2,
            now_unix_millis=100,
            stale_after_millis=10,
        )
    with pytest.raises(ValueError, match="future"):
        evaluate_health(
            (RankHealth(0, 101, True),),
            world_size=1,
            now_unix_millis=100,
            stale_after_millis=10,
        )
