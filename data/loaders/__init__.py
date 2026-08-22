# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Deterministic, bounded PyTorch data-loader contracts."""

from .collate import CollatedBatch, collate_samples
from .device_prefetch import move_batch
from .diagnostics import CoverageReport, audit_coverage, measure_batches
from .loader import EpochDataLoader, LoaderConfig, SampleDataset, build_loader
from .pinned_memory import PinMemoryPolicy
from .workers import seed_worker

__all__ = [
    "CollatedBatch",
    "CoverageReport",
    "EpochDataLoader",
    "LoaderConfig",
    "PinMemoryPolicy",
    "SampleDataset",
    "audit_coverage",
    "build_loader",
    "collate_samples",
    "measure_batches",
    "move_batch",
    "seed_worker",
]
