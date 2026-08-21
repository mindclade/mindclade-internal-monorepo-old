# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Content-addressed dataset shard manifests."""

from __future__ import annotations

import re
from dataclasses import dataclass

_DIGEST = re.compile(r"sha256:[0-9a-f]{64}")
_URI = re.compile(r"(?:gs|s3)://[^\s?#]+")
_MAXIMUM_COUNT = 2**63 - 1


@dataclass(frozen=True)
class ShardManifest:
    uri: str
    digest: str
    record_count: int
    size_bytes: int

    def __post_init__(self) -> None:
        if not _URI.fullmatch(self.uri):
            raise ValueError("shard uri must be an unsigned object-store URI")
        if not _DIGEST.fullmatch(self.digest):
            raise ValueError("shard digest must be canonical SHA-256")
        for value, label in ((self.record_count, "record_count"), (self.size_bytes, "size_bytes")):
            if (
                isinstance(value, bool)
                or not isinstance(value, int)
                or not 0 <= value <= _MAXIMUM_COUNT
            ):
                raise ValueError(f"{label} must be a bounded non-negative integer")
        if self.record_count > 0 and self.size_bytes == 0:
            raise ValueError("a non-empty shard may not have zero bytes")
