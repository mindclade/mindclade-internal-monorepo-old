# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Scientific canonicalization and validation semantics for ingestion."""

from .pipeline import IngestionPipeline
from .publication import IngestionResult
from .raw import RawRecord
from .record import CanonicalRecord
from .stages import StageKind, StageSpec

__all__ = [
    "CanonicalRecord",
    "IngestionPipeline",
    "IngestionResult",
    "RawRecord",
    "StageKind",
    "StageSpec",
]
