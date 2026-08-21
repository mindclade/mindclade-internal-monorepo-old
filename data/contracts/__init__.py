# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Provider-neutral, versioned data contracts."""

from .dataset import DatasetContract
from .record import FieldContract, FieldType, LogPolicy, Sensitivity
from .shard import ShardManifest
from .snapshot import DatasetSnapshot
from .source import SourceSnapshot
from .validation import ValidationIssue, validate_record

__all__ = [
    "DatasetContract",
    "DatasetSnapshot",
    "FieldContract",
    "FieldType",
    "LogPolicy",
    "Sensitivity",
    "ShardManifest",
    "SourceSnapshot",
    "ValidationIssue",
    "validate_record",
]
