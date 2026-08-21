# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Immutable reference-source, index, and snapshot contracts."""

from .builder import build_snapshot_candidate
from .catalog import ReferenceCatalog
from .index import ReferenceIndex
from .manifest import parse_manifest_document, parse_reference_snapshot
from .snapshot import ReferenceSnapshot
from .source import ReferenceSource
from .validation import require_compatible_tool, validate_snapshot_locations

__all__ = [
    "ReferenceCatalog",
    "ReferenceIndex",
    "ReferenceSnapshot",
    "ReferenceSource",
    "build_snapshot_candidate",
    "parse_manifest_document",
    "parse_reference_snapshot",
    "require_compatible_tool",
    "validate_snapshot_locations",
]
