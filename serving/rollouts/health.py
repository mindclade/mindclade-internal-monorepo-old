# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Rollout readiness projection."""

from dataclasses import dataclass


@dataclass(frozen=True, slots=True)
class RolloutHealth:
    ready: bool
    draining: bool
    active_policy_digest: str | None
