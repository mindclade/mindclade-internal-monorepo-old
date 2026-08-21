# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Policy-evidence gate; screening engines remain separately qualified."""

from __future__ import annotations

from .pipeline import CuratedRecord


def require_approved_screening(record: CuratedRecord) -> CuratedRecord | None:
    metadata = dict(record.metadata)
    return record if metadata.get("safety_decision") == "approved" else None
