# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

import hashlib
import json
from dataclasses import asdict, dataclass


@dataclass(frozen=True, slots=True)
class TuningEnvironment:
    target: str
    architecture: str
    device_name: str
    tilelang_version: str
    torch_version: str
    driver_version: str
    source_revision: str

    def __post_init__(self) -> None:
        if not all(asdict(self).values()):
            raise ValueError("tuning environment identity must be complete")

    @property
    def digest(self) -> str:
        payload = json.dumps(asdict(self), sort_keys=True, separators=(",", ":"))
        return hashlib.sha256(payload.encode()).hexdigest()
