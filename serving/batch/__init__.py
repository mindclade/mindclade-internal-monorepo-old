# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Bounded reusable mechanics for durable batch inference."""

from .artifacts import ResultManifest, build_manifest
from .batching import BatchSlice, partition
from .cancellation import CancellationRegistry
from .config import BatchLimits
from .executor import BatchEngine, BatchExecutor
from .job import BatchJob
from .queue import JobQueue
from .result import BatchResult
from .worker import BatchWorker

__all__ = [
    "BatchEngine",
    "BatchExecutor",
    "BatchJob",
    "BatchLimits",
    "BatchResult",
    "BatchSlice",
    "BatchWorker",
    "CancellationRegistry",
    "JobQueue",
    "ResultManifest",
    "build_manifest",
    "partition",
]
