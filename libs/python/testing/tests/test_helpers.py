# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

import os

from libs.python.testing import DeviceSpec, rank_environments, select_device, temporary_environ


def test_environment_fixture_restores_original_state() -> None:
    original = os.environ.copy()
    with temporary_environ({"MINDCLADE_TEST_VALUE": "inside"}):
        assert os.environ["MINDCLADE_TEST_VALUE"] == "inside"
    assert os.environ == original


def test_device_selection_and_rank_matrix_are_deterministic() -> None:
    assert select_device(["cuda:0", "cpu"], ["cpu"]) == DeviceSpec("cpu")
    environments = rank_environments(4, local_world_size=2)
    assert [environment["RANK"] for environment in environments] == ["0", "1", "2", "3"]
    assert [environment["LOCAL_RANK"] for environment in environments] == ["0", "1", "0", "1"]
