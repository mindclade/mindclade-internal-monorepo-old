# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Content-addressed reference search index."""

from __future__ import annotations

import re
from dataclasses import dataclass

from data.manifest import ArtifactRef

_DIGEST = re.compile(r"sha256:[0-9a-f]{64}")
_TOKEN = re.compile(r"[A-Za-z0-9][A-Za-z0-9._+-]{0,127}")


@dataclass(frozen=True, slots=True, order=True)
class ReferenceIndex:
    kind: str
    format_version: str
    tool: str
    tool_version: str
    parameters_digest: str
    artifacts: tuple[ArtifactRef, ...]

    def __post_init__(self) -> None:
        if any(
            not isinstance(value, str) or not _TOKEN.fullmatch(value)
            for value in (self.kind, self.format_version, self.tool, self.tool_version)
        ):
            raise ValueError("reference index kind/format/tool is invalid")
        if not _DIGEST.fullmatch(self.parameters_digest):
            raise ValueError("reference index parameter digest is invalid")
        artifacts = tuple(self.artifacts)
        if (
            not artifacts
            or len(artifacts) > 100_000
            or len({artifact.digest for artifact in artifacts}) != len(artifacts)
        ):
            raise ValueError("reference index artifacts are invalid")
        object.__setattr__(self, "artifacts", artifacts)
