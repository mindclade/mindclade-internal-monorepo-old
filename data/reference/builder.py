# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Pure candidate builder; durable execution and publication remain Go-owned."""

from __future__ import annotations

import datetime as dt

from .index import ReferenceIndex
from .snapshot import ReferenceSnapshot
from .source import ReferenceSource


def build_snapshot_candidate(
    reference_id: str,
    version: str,
    sources: tuple[ReferenceSource, ...],
    indexes: tuple[ReferenceIndex, ...],
    compatible_search_tools: tuple[str, ...],
    *,
    generated_at: dt.datetime,
    build_provenance_digest: str,
) -> ReferenceSnapshot:
    return ReferenceSnapshot(
        reference_id,
        version,
        sources,
        indexes,
        compatible_search_tools,
        generated_at,
        build_provenance_digest,
    )
