# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Content-bound per-shard resume cursor."""

from __future__ import annotations

import re
from dataclasses import dataclass

_DIGEST = re.compile(r"sha256:[0-9a-f]{64}")


@dataclass(frozen=True, slots=True)
class ShardCursor:
    manifest_digest: str
    shard_id: str
    next_record: int

    def __post_init__(self) -> None:
        if not _DIGEST.fullmatch(self.manifest_digest):
            raise ValueError("shard cursor manifest digest is invalid")
        if not self.shard_id or len(self.shard_id) > 256:
            raise ValueError("shard cursor identity is invalid")
        if (
            isinstance(self.next_record, bool)
            or not isinstance(self.next_record, int)
            or self.next_record < 0
        ):
            raise ValueError("shard cursor record position is invalid")

    def advance(self, count: int) -> ShardCursor:
        if isinstance(count, bool) or not isinstance(count, int) or count < 0:
            raise ValueError("shard cursor advance must be non-negative")
        return ShardCursor(self.manifest_digest, self.shard_id, self.next_record + count)
