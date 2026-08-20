# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

from dataclasses import dataclass


@dataclass(frozen=True, slots=True)
class FallbackEvent:
    operation: str
    request_digest: str
    rejected_implementation: str
    reason: str

    def __post_init__(self) -> None:
        if not all((self.operation, self.request_digest, self.rejected_implementation, self.reason)):
            raise ValueError("fallback telemetry fields must be non-empty")
