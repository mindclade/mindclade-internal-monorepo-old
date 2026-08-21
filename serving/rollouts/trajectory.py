# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Immutable bounded rollout trajectory and provenance contract."""

from __future__ import annotations

import math
from dataclasses import dataclass

MAXIMUM_STEPS = 1_000_000
MAXIMUM_PAYLOAD_BYTES = 16 * 1024 * 1024


@dataclass(frozen=True, slots=True)
class TrajectoryStep:
    observation_digest: str
    action: bytes
    reward: float
    terminal: bool

    def validate(self) -> None:
        if not self.observation_digest.startswith("sha256:") or len(self.observation_digest) != 71:
            raise ValueError("trajectory observation digest is invalid")
        if not isinstance(self.action, bytes) or len(self.action) > MAXIMUM_PAYLOAD_BYTES:
            raise ValueError("trajectory action is outside bounds")
        if isinstance(self.reward, bool) or not math.isfinite(self.reward):
            raise ValueError("trajectory reward must be finite")
        if not isinstance(self.terminal, bool):
            raise ValueError("trajectory terminal flag is invalid")


@dataclass(frozen=True, slots=True)
class Trajectory:
    trajectory_id: str
    policy_digest: str
    environment_digest: str
    seed: int
    steps: tuple[TrajectoryStep, ...]

    def validate(self) -> None:
        if not self.trajectory_id or len(self.trajectory_id) > 256:
            raise ValueError("trajectory id is invalid")
        for digest in (self.policy_digest, self.environment_digest):
            if not digest.startswith("sha256:") or len(digest) != 71:
                raise ValueError("trajectory provenance digest is invalid")
        if isinstance(self.seed, bool) or not 0 <= self.seed < 2**64:
            raise ValueError("trajectory seed is invalid")
        if not self.steps or len(self.steps) > MAXIMUM_STEPS:
            raise ValueError("trajectory step count is outside bounds")
        if any(step.terminal for step in self.steps[:-1]):
            raise ValueError("trajectory contains steps after a terminal step")
        for step in self.steps:
            step.validate()
