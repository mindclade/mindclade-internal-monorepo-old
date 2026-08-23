# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Deferred coverage for training/distributed/communication/loss_reduction.py."""

import pytest


# SKIPPED, not passing — but NOT for the reason the other scaffolds in this directory are.
#
# `collectives.py`, `gradient_sync.py` and `metric_reduction.py` beside it really are
# fourteen-line scaffold boundaries with no functions, so "no implementation to test yet" is
# an accurate skip reason for their tests. `loss_reduction.py` is not: it is 184 lines and
# implements `DDPReducer`. This file used to claim otherwise, which sends the next reader
# looking for an implementation that is already there.
#
# What actually blocks a unit test is the fixture, not the code. Constructing a `DDPReducer`
# runs `DistributedContext.validate_active()`, which requires a live process group whose rank,
# world size and backend match — and `DistributedContext.__post_init__` admits gloo only at
# world size exactly 2. A single-process group therefore cannot build one, so every path in
# the class, including the pure input validation that runs before any collective, is
# unreachable without the two-process harness that
# `training/distributed/tests/test_distributed_smoke.py` stands up through subprocesses.
#
# So this is qualification-tier coverage, not a missing unit test. Write it against that
# harness — assert the reductions at world size 2 and the bool guards, which matter because
# `isinstance(True, int)` is true in Python and every validator here rejects `bool` explicitly
# for that reason — and lower SCAFFOLD_BASELINE in tests/integration/test_python_scaffold.py
# in the same commit.
#
# The marker stays because the ratchet counts the decorator, and an uncounted deferral is how
# a gap stops being tracked.
@pytest.mark.scaffold
def test_scaffold_contract() -> None:
    pytest.skip("deferred: DDPReducer requires the two-process gloo harness, not a unit fixture")
