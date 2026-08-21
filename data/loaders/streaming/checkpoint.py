# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Immutable manifest-bound streaming position."""

from __future__ import annotations

import re
from dataclasses import dataclass

_DIGEST = re.compile(r"sha256:[0-9a-f]{64}")


@dataclass(frozen=True, slots=True)
class StreamCheckpoint:
    manifest_digest: str
    next_index: int
    epoch: int
    shuffle_seed: int

    def __post_init__(self) -> None:
        if not _DIGEST.fullmatch(self.manifest_digest):
            raise ValueError("stream checkpoint manifest digest is invalid")
        for value, name in (
            (self.next_index, "next_index"),
            (self.epoch, "epoch"),
            (self.shuffle_seed, "shuffle_seed"),
        ):
            if isinstance(value, bool) or not isinstance(value, int) or value < 0:
                raise ValueError(f"stream checkpoint {name} is invalid")
