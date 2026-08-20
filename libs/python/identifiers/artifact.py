# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""The one artifact reference, shared by every Python package that names bytes.

Per ADR-0004 an artifact is identified by what it contains, never by where it
happens to sit. The five fields here are the whole identity, and
``tests/integration/cross_language/test_manifest_roundtrip.py`` asserts both
halves of that: the field set is exactly these five, and ``uri``, ``provider``
and ``generation`` are absent. A reference that carried a location would make the
same bytes in two buckets two different artifacts.

This type replaces two divergent definitions. ``libs/python/worker_runtime`` typed
``schema_version`` as ``str`` while ``preprocessing/contracts/stage.py`` typed it
``int`` and range-checked it; the cross-language fixture carries an integer and
the round-trip test asserts ``schema_version > 0``, which raises ``TypeError``
against the string form. The integer wins because the contract says so.
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any, Final

from libs.python.errors import InvalidArgument

from .digest import Digest, is_canonical_digest

MAXIMUM_MEDIA_TYPE_LENGTH: Final = 255
MAXIMUM_LOGICAL_KIND_LENGTH: Final = 128

# The exact field set the cross-language round-trip test pins.
ARTIFACT_REF_FIELDS: Final = frozenset(
    {"digest", "size_bytes", "media_type", "logical_kind", "schema_version"}
)


@dataclass(frozen=True, slots=True)
class ArtifactRef:
    """A location-independent reference to content-addressed bytes."""

    digest: Digest
    size_bytes: int
    media_type: str
    logical_kind: str
    schema_version: int

    def __post_init__(self) -> None:
        if self.size_bytes < 0:
            raise InvalidArgument("artifact size must not be negative", reason="artifact_size")
        if "/" not in self.media_type or len(self.media_type) > MAXIMUM_MEDIA_TYPE_LENGTH:
            raise InvalidArgument(
                "artifact media type must be a bounded type/subtype string",
                reason="artifact_media_type",
            )
        if not self.logical_kind or len(self.logical_kind) > MAXIMUM_LOGICAL_KIND_LENGTH:
            raise InvalidArgument(
                "artifact logical kind is required and bounded", reason="artifact_logical_kind"
            )
        # bool is a subclass of int, so `True` would otherwise pass as schema version 1.
        if isinstance(self.schema_version, bool) or self.schema_version < 1:
            raise InvalidArgument(
                "artifact schema version is a positive integer", reason="artifact_schema_version"
            )

    @classmethod
    def from_document(cls, document: dict[str, Any]) -> ArtifactRef:
        """Build a reference from its canonical document form.

        Rejects unknown and missing keys rather than ignoring them: an extra key
        is either a field this build does not understand or a location leaking
        back in, and both are worth failing on.
        """
        keys = set(document)
        if keys != ARTIFACT_REF_FIELDS:
            raise InvalidArgument(
                "artifact reference fields must be exactly "
                f"{sorted(ARTIFACT_REF_FIELDS)}, got {sorted(keys)}",
                reason="artifact_fields",
            )
        digest = document["digest"]
        if not isinstance(digest, str) or not is_canonical_digest(digest):
            raise InvalidArgument(
                "artifact digest must be a canonical sha256 string", reason="artifact_digest"
            )
        return cls(
            digest=Digest.parse(digest),
            size_bytes=document["size_bytes"],
            media_type=document["media_type"],
            logical_kind=document["logical_kind"],
            schema_version=document["schema_version"],
        )

    def to_document(self) -> dict[str, Any]:
        """The canonical document form, for canonical serialization and manifests."""
        return {
            "digest": self.digest.text,
            "size_bytes": self.size_bytes,
            "media_type": self.media_type,
            "logical_kind": self.logical_kind,
            "schema_version": self.schema_version,
        }
