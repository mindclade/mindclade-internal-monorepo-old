# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Injected actor boundary for rollout generation."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Protocol

from libs.python.worker_runtime import CancellationToken

from .sampling import SamplingConfig
from .trajectory import Trajectory


@dataclass(frozen=True, slots=True)
class RolloutRequest:
    trajectory_id: str
    policy_digest: str
    environment_digest: str
    input_digest: str
    seed: int
    deadline_unix_millis: int
    sampling: SamplingConfig

    def validate(self, *, now_unix_millis: int) -> None:
        if not self.trajectory_id or len(self.trajectory_id) > 256:
            raise ValueError("rollout trajectory id is invalid")
        for digest in (self.policy_digest, self.environment_digest, self.input_digest):
            if not digest.startswith("sha256:") or len(digest) != 71:
                raise ValueError("rollout digest is invalid")
        if isinstance(self.seed, bool) or not 0 <= self.seed < 2**64:
            raise ValueError("rollout seed is invalid")
        if self.deadline_unix_millis <= now_unix_millis:
            raise ValueError("rollout deadline has expired")


class Actor(Protocol):
    def generate(
        self,
        requests: tuple[RolloutRequest, ...],
        cancellation: CancellationToken,
    ) -> tuple[Trajectory, ...]: ...
