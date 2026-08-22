# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import hashlib
import json
from dataclasses import dataclass


@dataclass(frozen=True, slots=True)
class DeviceMetadata:
    name: str
    target: str
    architecture: str
    driver: str
    runtime: str

    def __post_init__(self) -> None:
        if not all((self.name, self.target, self.architecture, self.driver, self.runtime)):
            raise ValueError("complete device metadata is required for accelerator evidence")

    @property
    def digest(self) -> str:
        payload = {
            "architecture": self.architecture,
            "driver": self.driver,
            "name": self.name,
            "runtime": self.runtime,
            "target": self.target,
        }
        encoded = json.dumps(payload, sort_keys=True, separators=(",", ":")).encode()
        return hashlib.sha256(encoded).hexdigest()
