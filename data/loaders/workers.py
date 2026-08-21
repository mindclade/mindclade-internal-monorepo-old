# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Importable worker initialization with explicit derived randomness."""

from __future__ import annotations

import random

import numpy as np
import torch


def seed_worker(worker_id: int) -> None:
    """Derive Python and NumPy seeds from PyTorch's worker-local seed."""

    if isinstance(worker_id, bool) or not isinstance(worker_id, int) or worker_id < 0:
        raise ValueError("worker id must be a non-negative integer")
    worker_seed = torch.initial_seed() % 2**32
    random.seed(worker_seed)
    np.random.seed(worker_seed)
