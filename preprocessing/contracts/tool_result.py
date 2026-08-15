# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Result of an externally supervised scientific search/tool stage."""

from __future__ import annotations

from dataclasses import dataclass

from .stage import ArtifactRef


@dataclass(frozen=True)
class ToolResult:
    tool: str
    version: str
    command_digest: str
    stdout: ArtifactRef | None
    stderr: ArtifactRef | None
    outputs: tuple[ArtifactRef, ...]
    exit_code: int
    duration_millis: int

    @property
    def succeeded(self) -> bool:
        return self.exit_code == 0
