# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Content-keyed augmentation seed derivation."""

from __future__ import annotations

import hashlib

from .pipeline import CuratedRecord


def augmentation_seed(record: CuratedRecord, *, global_seed: int, epoch: int) -> int:
    if any(
        isinstance(value, bool) or not isinstance(value, int) or value < 0
        for value in (global_seed, epoch)
    ):
        raise ValueError("augmentation seed/epoch must be non-negative integers")
    payload = f"{global_seed}:{epoch}:{record.digest}".encode()
    return int.from_bytes(hashlib.sha256(payload).digest()[:8], "big")
