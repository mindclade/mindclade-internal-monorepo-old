# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Readiness projection for the batch composition root."""

from __future__ import annotations

from dataclasses import dataclass


@dataclass(frozen=True, slots=True)
class BatchHealth:
    ready: bool
    draining: bool
    queued_jobs: int
    maximum_queued_jobs: int

    @property
    def saturated(self) -> bool:
        return self.queued_jobs >= self.maximum_queued_jobs
