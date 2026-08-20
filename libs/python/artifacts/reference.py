# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Construct canonical artifact references from bounded in-process bytes."""

from __future__ import annotations

from typing import Final

from libs.python.errors import InvalidArgument, ResourceExhausted
from libs.python.identifiers import MAXIMUM_ARTIFACT_SIZE, ArtifactRef, Digest

MAXIMUM_IN_MEMORY_ARTIFACT_BYTES: Final = 64 << 20


def reference_bytes(
    data: bytes | bytearray | memoryview,
    *,
    media_type: str,
    logical_kind: str,
    schema_version: int = 1,
    maximum_bytes: int = MAXIMUM_IN_MEMORY_ARTIFACT_BYTES,
) -> ArtifactRef:
    """Return the location-independent identity of a bounded byte buffer."""
    if not isinstance(data, bytes | bytearray | memoryview):
        raise InvalidArgument("artifact content must be bytes-like", reason="artifact_content_type")
    if (
        isinstance(maximum_bytes, bool)
        or not isinstance(maximum_bytes, int)
        or not 0 <= maximum_bytes <= MAXIMUM_ARTIFACT_SIZE
    ):
        raise InvalidArgument(
            "artifact byte bound must be an unsigned 64-bit integer",
            reason="artifact_content_bound",
        )
    if len(data) > maximum_bytes:
        raise ResourceExhausted(
            f"artifact content exceeds the {maximum_bytes}-byte in-memory bound",
            reason="artifact_content_size",
        )
    content = bytes(data)
    return ArtifactRef(
        digest=Digest.of(content),
        size_bytes=len(content),
        media_type=media_type,
        logical_kind=logical_kind,
        schema_version=schema_version,
    )
