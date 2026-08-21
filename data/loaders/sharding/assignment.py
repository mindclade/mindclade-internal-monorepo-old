# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Stable rendezvous assignment for immutable shard identities."""

from __future__ import annotations

import hashlib
from collections.abc import Iterable


def assign(shard_ids: Iterable[str], workers: Iterable[str]) -> dict[str, str]:
    shards = tuple(shard_ids)
    worker_ids = tuple(workers)
    if not worker_ids or len(set(worker_ids)) != len(worker_ids):
        raise ValueError("assignment workers must be unique and non-empty")
    if len(set(shards)) != len(shards) or any(not item for item in (*shards, *worker_ids)):
        raise ValueError("assignment identities must be unique and non-empty")
    result: dict[str, str] = {}
    for shard in shards:
        result[shard] = max(
            worker_ids,
            key=lambda worker: (hashlib.sha256(f"{shard}\0{worker}".encode()).digest(), worker),
        )
    return result
