# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Bounded rollout trajectory with policy and environment provenance."""

from __future__ import annotations

import hashlib
import json
import math
from dataclasses import dataclass

from .policy_version import PolicyVersion


@dataclass(frozen=True, slots=True)
class ExperienceStep:
    observation_digest: str
    action: int
    reward: float
    terminated: bool

    def __post_init__(self) -> None:
        if (
            len(self.observation_digest) != 71
            or not self.observation_digest.startswith("sha256:")
            or any(character not in "0123456789abcdef" for character in self.observation_digest[7:])
        ):
            raise ValueError("experience observation digest is invalid")
        if isinstance(self.action, bool) or not isinstance(self.action, int) or self.action < 0:
            raise ValueError("experience action is invalid")
        if isinstance(self.reward, bool) or not math.isfinite(self.reward):
            raise ValueError("experience reward must be finite")
        if not isinstance(self.terminated, bool):
            raise ValueError("experience termination flag must be boolean")


@dataclass(frozen=True, slots=True)
class Trajectory:
    trajectory_id: str
    environment_digest: str
    policy: PolicyVersion
    steps: tuple[ExperienceStep, ...]

    def __post_init__(self) -> None:
        if not self.trajectory_id or len(self.trajectory_id) > 256:
            raise ValueError("trajectory identity is invalid")
        if len(self.environment_digest) != 71 or not self.environment_digest.startswith("sha256:"):
            raise ValueError("trajectory environment digest is invalid")
        if not isinstance(self.policy, PolicyVersion):
            raise ValueError("trajectory policy version is invalid")
        steps = tuple(self.steps)
        if not 1 <= len(steps) <= 1_000_000 or any(
            not isinstance(step, ExperienceStep) for step in steps
        ):
            raise ValueError("trajectory steps are outside bounds")
        if any(step.terminated for step in steps[:-1]):
            raise ValueError("trajectory may terminate only at its final step")
        object.__setattr__(self, "steps", steps)

    @property
    def digest(self) -> str:
        value = {
            "trajectory_id": self.trajectory_id,
            "environment_digest": self.environment_digest,
            "policy": {
                "model_digest": self.policy.model_digest,
                "runtime_digest": self.policy.runtime_digest,
                "configuration_digest": self.policy.configuration_digest,
            },
            "steps": [
                {
                    "observation_digest": step.observation_digest,
                    "action": step.action,
                    "reward": step.reward,
                    "terminated": step.terminated,
                }
                for step in self.steps
            ],
        }
        canonical = json.dumps(value, sort_keys=True, separators=(",", ":")).encode()
        return "sha256:" + hashlib.sha256(canonical).hexdigest()
