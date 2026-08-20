# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

import pytest

from libs.python.distributed import DistributedEnvironment, RendezvousConfig, RendezvousState


def test_environment_round_trips_and_computes_node_rank() -> None:
    values = {
        "RANK": "5",
        "WORLD_SIZE": "8",
        "LOCAL_RANK": "1",
        "LOCAL_WORLD_SIZE": "4",
        "MASTER_ADDR": "trainer-0.internal",
        "MASTER_PORT": "29400",
    }
    environment = DistributedEnvironment.from_environ(values)
    assert environment.node_rank == 1
    assert environment.to_environ() == values


def test_environment_rejects_missing_or_inconsistent_layout() -> None:
    with pytest.raises(ValueError, match="missing"):
        DistributedEnvironment.from_environ({})
    with pytest.raises(ValueError, match="evenly partition"):
        DistributedEnvironment(5, 8, 0, 4, "host", 29400)
    invalid = {
        "RANK": "0_0",
        "WORLD_SIZE": "1",
        "LOCAL_RANK": "0",
        "LOCAL_WORLD_SIZE": "1",
        "MASTER_ADDR": "host",
        "MASTER_PORT": "29400",
    }
    with pytest.raises(ValueError, match="decimal digits"):
        DistributedEnvironment.from_environ(invalid)


def test_rendezvous_contract_rejects_credentials_and_incomplete_membership() -> None:
    assert RendezvousConfig("run-1", "[::1]:29400", 1, 4).max_nodes == 4
    with pytest.raises(ValueError, match="without credentials"):
        RendezvousConfig("run-1", "user:secret@host:29400", 1, 4)
    with pytest.raises(ValueError, match="every world rank"):
        RendezvousState(1, 2, (0,))
    with pytest.raises(ValueError, match="integer"):
        RendezvousState(1, 2, (False, 1))
    assert RendezvousState(1, 2, (1, 0)).participants == (0, 1)
