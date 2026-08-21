# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Validated resource ceilings for durable batch inference."""

from __future__ import annotations

from dataclasses import dataclass


@dataclass(frozen=True, slots=True)
class BatchLimits:
    maximum_queued_jobs: int = 256
    maximum_requests_per_job: int = 4096
    maximum_requests_per_batch: int = 128
    maximum_units_per_batch: int = 65_536
    maximum_estimated_bytes_per_batch: int = 80 * 1024**3
    maximum_loaded_models: int = 4
    maximum_attempts: int = 3
    base_retry_delay_millis: int = 250
    maximum_retry_delay_millis: int = 30_000

    def __post_init__(self) -> None:
        values = (
            self.maximum_queued_jobs,
            self.maximum_requests_per_job,
            self.maximum_requests_per_batch,
            self.maximum_units_per_batch,
            self.maximum_estimated_bytes_per_batch,
            self.maximum_loaded_models,
            self.maximum_attempts,
            self.base_retry_delay_millis,
            self.maximum_retry_delay_millis,
        )
        if any(
            isinstance(value, bool) or not isinstance(value, int) or value <= 0 for value in values
        ):
            raise ValueError("batch limits must be positive integers")
        if self.maximum_queued_jobs > 100_000:
            raise ValueError("batch queue limit exceeds the supported bound")
        if self.maximum_requests_per_batch > self.maximum_requests_per_job:
            raise ValueError("batch request limit exceeds job request limit")
        if self.maximum_attempts > 32:
            raise ValueError("batch attempt limit exceeds the supported bound")
        if self.base_retry_delay_millis > self.maximum_retry_delay_millis:
            raise ValueError("base retry delay exceeds maximum retry delay")
