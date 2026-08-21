# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Validated local limits for the batch-inference adapter."""

from __future__ import annotations

from libs.python.worker_runtime import WorkerLimits


def worker_limits(
    *, maximum_concurrent_executions: int = 1, drain_timeout_millis: int = 30_000
) -> WorkerLimits:
    return WorkerLimits(maximum_concurrent_executions, drain_timeout_millis)
