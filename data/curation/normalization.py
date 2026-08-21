# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Pure payload normalization adapter with version evidence."""

from __future__ import annotations

from collections.abc import Callable
from dataclasses import dataclass

from .pipeline import CuratedRecord
from .provenance import with_metadata


@dataclass(frozen=True, slots=True)
class NormalizePayload:
    function: Callable[[bytes], bytes]
    version: str

    def __post_init__(self) -> None:
        if not callable(self.function) or not self.version or len(self.version) > 128:
            raise ValueError("payload normalization stage is invalid")

    def __call__(self, record: CuratedRecord) -> CuratedRecord:
        payload = self.function(record.payload)
        if not isinstance(payload, bytes):
            raise TypeError("payload normalizer must return bytes")
        return with_metadata(
            CuratedRecord(record.key, payload, record.metadata),
            "normalization_version",
            self.version,
        )
