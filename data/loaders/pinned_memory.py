# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Pin-memory policy validated against an asynchronous transfer path."""

from __future__ import annotations

from dataclasses import dataclass


@dataclass(frozen=True, slots=True)
class PinMemoryPolicy:
    enabled: bool
    asynchronous_device_transfer: bool

    def __post_init__(self) -> None:
        if not isinstance(self.enabled, bool) or not isinstance(
            self.asynchronous_device_transfer, bool
        ):
            raise ValueError("pin-memory policy values must be boolean")
        if self.enabled and not self.asynchronous_device_transfer:
            raise ValueError("pin memory requires a measured asynchronous transfer path")
