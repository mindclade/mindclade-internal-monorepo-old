# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Content identity and location contracts for immutable data artifacts.

Identity is deliberately independent from storage location. Replication or a
provider migration may add a location, but cannot change an :class:`ArtifactRef`.
"""

from __future__ import annotations

import re
from dataclasses import dataclass
from urllib.parse import urlsplit

_DIGEST = re.compile(r"sha256:[0-9a-f]{64}")
_TOKEN = re.compile(r"[a-z][a-z0-9._-]{0,127}")
_MAX_SIZE = 2**63 - 1


@dataclass(frozen=True, slots=True, order=True)
class ArtifactRef:
    """Provider-neutral identity for one immutable blob."""

    digest: str
    size_bytes: int
    media_type: str
    logical_kind: str
    schema_version: str

    def __post_init__(self) -> None:
        if not isinstance(self.digest, str) or not _DIGEST.fullmatch(self.digest):
            raise ValueError("artifact digest must be canonical SHA-256")
        if (
            isinstance(self.size_bytes, bool)
            or not isinstance(self.size_bytes, int)
            or not 0 <= self.size_bytes <= _MAX_SIZE
        ):
            raise ValueError("artifact size must be a bounded non-negative integer")
        if (
            not isinstance(self.media_type, str)
            or "/" not in self.media_type
            or len(self.media_type) > 256
            or any(ord(character) < 0x20 for character in self.media_type)
        ):
            raise ValueError("artifact media type is invalid")
        for value, label in (
            (self.logical_kind, "logical kind"),
            (self.schema_version, "schema version"),
        ):
            if not isinstance(value, str) or not _TOKEN.fullmatch(value):
                raise ValueError(f"artifact {label} is invalid")


@dataclass(frozen=True, slots=True, order=True)
class ArtifactLocation:
    """Credential-free physical location for an exact artifact generation."""

    artifact_digest: str
    provider: str
    uri: str
    generation: str
    region: str | None = None

    def __post_init__(self) -> None:
        if not isinstance(self.artifact_digest, str) or not _DIGEST.fullmatch(self.artifact_digest):
            raise ValueError("location artifact digest must be canonical SHA-256")
        if not isinstance(self.provider, str) or not _TOKEN.fullmatch(self.provider):
            raise ValueError("location provider is invalid")
        parsed = urlsplit(self.uri) if isinstance(self.uri, str) else None
        if (
            parsed is None
            or parsed.scheme not in {"file", "gs", "s3", "https"}
            or (parsed.scheme != "file" and not parsed.hostname)
            or not parsed.path.strip("/")
            or parsed.username is not None
            or parsed.password is not None
            or parsed.query
            or parsed.fragment
        ):
            raise ValueError("artifact location must be an unsigned absolute URI")
        if (
            not isinstance(self.generation, str)
            or not self.generation
            or len(self.generation) > 256
            or any(ord(character) < 0x20 for character in self.generation)
        ):
            raise ValueError("artifact generation is invalid")
        if self.region is not None and (
            not isinstance(self.region, str) or not _TOKEN.fullmatch(self.region)
        ):
            raise ValueError("artifact region is invalid")

    def binds(self, artifact: ArtifactRef) -> bool:
        return self.artifact_digest == artifact.digest
