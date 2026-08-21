# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Stable content-keyed sampling order."""

from __future__ import annotations

import hashlib
from collections.abc import Iterable


def stable_order(identities: Iterable[str], *, seed: int, epoch: int) -> tuple[str, ...]:
    values = tuple(identities)
    if len(set(values)) != len(values) or any(not value for value in values):
        raise ValueError("sampling identities must be unique non-empty strings")
    if any(
        isinstance(value, bool) or not isinstance(value, int) or value < 0
        for value in (seed, epoch)
    ):
        raise ValueError("sampling seed and epoch must be non-negative integers")

    def key(identity: str) -> tuple[bytes, str]:
        payload = f"{seed}:{epoch}:{identity}".encode()
        return hashlib.sha256(payload).digest(), identity

    return tuple(sorted(values, key=key))
