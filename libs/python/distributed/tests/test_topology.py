# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

import pytest

from libs.python.distributed import ProcessTopology


def test_topology_coordinates_groups_and_fingerprint_are_deterministic() -> None:
    topology = ProcessTopology(
        world_size=8,
        local_world_size=4,
        data_parallel=2,
        tensor_parallel=2,
        pipeline_parallel=2,
    )
    assert topology.coordinates(7) == (1, 1, 1)
    assert topology.rank((1, 1, 1)) == 7
    assert topology.groups("tensor") == ((0, 2), (1, 3), (4, 6), (5, 7))
    assert topology.fingerprint == ProcessTopology(8, 4, 2, 2, 2).fingerprint


def test_topology_rejects_impossible_products_and_coordinates() -> None:
    with pytest.raises(ValueError, match="multiply"):
        ProcessTopology(8, 4, 2, 2, 1)
    with pytest.raises(ValueError, match="coordinate"):
        ProcessTopology(8, 4, 2, 2, 2).rank((2, 0, 0))
