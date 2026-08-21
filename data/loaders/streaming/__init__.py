# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Finite, resumable, jointly sharded streaming datasets."""

from .backpressure import BackpressurePolicy
from .checkpoint import StreamCheckpoint
from .iterator import ShardedSampleStream
from .prefetch import PrefetchPolicy
from .reader import SampleReader
from .shuffle import buffered_shuffle

__all__ = [
    "BackpressurePolicy",
    "PrefetchPolicy",
    "SampleReader",
    "ShardedSampleStream",
    "StreamCheckpoint",
    "buffered_shuffle",
]

"""Mindclade scaffold package for data/loaders/streaming."""
