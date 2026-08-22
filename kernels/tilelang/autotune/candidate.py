# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import hashlib
import json
from dataclasses import dataclass


@dataclass(frozen=True, slots=True)
class Candidate:
    configuration: tuple[tuple[str, int | float | str | bool], ...]
    source_digest: str
    environment_digest: str

    def __post_init__(self) -> None:
        keys = [key for key, _ in self.configuration]
        if keys != sorted(keys) or len(keys) != len(set(keys)):
            raise ValueError("candidate configuration keys must be unique and sorted")
        for name, digest in (
            ("source_digest", self.source_digest),
            ("environment_digest", self.environment_digest),
        ):
            if len(digest) != 64 or any(c not in "0123456789abcdef" for c in digest):
                raise ValueError(f"{name} must be a lowercase SHA-256 digest")

    @property
    def config(self) -> dict[str, int | float | str | bool]:
        return dict(self.configuration)

    @property
    def digest(self) -> str:
        payload = {
            "configuration": self.config,
            "environment_digest": self.environment_digest,
            "source_digest": self.source_digest,
        }
        encoded = json.dumps(payload, sort_keys=True, separators=(",", ":")).encode()
        return hashlib.sha256(encoded).hexdigest()
