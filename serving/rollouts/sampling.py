# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Deterministic sampling configuration and per-trajectory seed derivation."""

from __future__ import annotations

import hashlib
import math
import random
from dataclasses import dataclass


@dataclass(frozen=True, slots=True)
class SamplingConfig:
    temperature: float = 1.0
    top_p: float = 1.0
    maximum_tokens: int = 1024

    def __post_init__(self) -> None:
        if not math.isfinite(self.temperature) or not 0 < self.temperature <= 10:
            raise ValueError("sampling temperature is outside bounds")
        if not math.isfinite(self.top_p) or not 0 < self.top_p <= 1:
            raise ValueError("sampling top_p is outside bounds")
        if isinstance(self.maximum_tokens, bool) or not 1 <= self.maximum_tokens <= 1_000_000:
            raise ValueError("sampling token limit is outside bounds")


def derive_seed(run_seed: int, trajectory_id: str, policy_digest: str) -> int:
    if isinstance(run_seed, bool) or not 0 <= run_seed < 2**64:
        raise ValueError("run seed must be an unsigned 64-bit integer")
    if not trajectory_id or len(trajectory_id) > 256:
        raise ValueError("trajectory id is invalid")
    if not policy_digest.startswith("sha256:") or len(policy_digest) != 71:
        raise ValueError("policy digest is invalid")
    payload = f"{run_seed}\0{trajectory_id}\0{policy_digest}".encode()
    return int.from_bytes(hashlib.sha256(payload).digest()[:8], "big")


def categorical(weights: tuple[float, ...], *, seed: int) -> int:
    if not weights or len(weights) > 1_000_000:
        raise ValueError("sampling weights are outside bounds")
    if any(not math.isfinite(weight) or weight < 0 for weight in weights):
        raise ValueError("sampling weights must be finite and non-negative")
    total = sum(weights)
    if total <= 0:
        raise ValueError("sampling weights must have positive mass")
    threshold = random.Random(seed).random() * total
    cumulative = 0.0
    for index, weight in enumerate(weights):
        cumulative += weight
        if threshold < cumulative:
            return index
    return len(weights) - 1
