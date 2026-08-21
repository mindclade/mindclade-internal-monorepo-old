# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Replay sampling cursor bound to a frozen buffer manifest."""

from __future__ import annotations

import re
from dataclasses import dataclass

_DIGEST = re.compile(r"sha256:[0-9a-f]{64}")


@dataclass(frozen=True, slots=True)
class ReplayCursor:
    manifest_digest: str
    samples_drawn: int
    seed: int

    def __post_init__(self) -> None:
        if not _DIGEST.fullmatch(self.manifest_digest):
            raise ValueError("replay cursor manifest digest is invalid")
        if any(
            isinstance(value, bool) or not isinstance(value, int) or value < 0
            for value in (self.samples_drawn, self.seed)
        ):
            raise ValueError("replay cursor position/seed is invalid")
