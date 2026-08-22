# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Content-addressed compiler output used by tuning and qualification."""

from __future__ import annotations

import hashlib
from dataclasses import dataclass

MAXIMUM_GENERATED_SOURCE_BYTES = 4 * 1024 * 1024


@dataclass(frozen=True, slots=True)
class KernelSourceArtifact:
    source: str
    target: str
    compiler_version: str

    def __post_init__(self) -> None:
        if not isinstance(self.source, str):
            raise TypeError("generated source must be text")
        if not self.source:
            raise ValueError("generated source cannot be empty")
        try:
            source_size = len(self.source.encode("utf-8"))
        except UnicodeEncodeError as exc:
            raise ValueError("generated source must be valid UTF-8 text") from exc
        if source_size > MAXIMUM_GENERATED_SOURCE_BYTES:
            raise ValueError(
                "generated source exceeds the "
                f"{MAXIMUM_GENERATED_SOURCE_BYTES}-byte inspection limit"
            )
        for name, value in (("target", self.target), ("compiler_version", self.compiler_version)):
            if not isinstance(value, str):
                raise TypeError(f"{name} must be text")
            if not value:
                raise ValueError(f"{name} cannot be empty")

    @property
    def source_digest(self) -> str:
        return hashlib.sha256(self.source.encode("utf-8")).hexdigest()

    @property
    def identity_digest(self) -> str:
        payload = f"{self.target}\0{self.compiler_version}\0{self.source_digest}"
        return hashlib.sha256(payload.encode("utf-8")).hexdigest()
