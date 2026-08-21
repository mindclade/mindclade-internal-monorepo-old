# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Content-addressed compiler output used by tuning and qualification."""

from __future__ import annotations

import hashlib
from dataclasses import dataclass


@dataclass(frozen=True, slots=True)
class KernelSourceArtifact:
    source: str
    target: str
    compiler_version: str

    @property
    def source_digest(self) -> str:
        return hashlib.sha256(self.source.encode()).hexdigest()

    @property
    def identity_digest(self) -> str:
        payload = f"{self.target}\0{self.compiler_version}\0{self.source_digest}"
        return hashlib.sha256(payload.encode()).hexdigest()
