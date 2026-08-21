# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Content fingerprints for exact deduplication evidence."""

from __future__ import annotations

import hashlib

from .pipeline import CuratedRecord


def payload_fingerprint(record: CuratedRecord) -> str:
    record.validate()
    return "sha256:" + hashlib.sha256(record.payload).hexdigest()
