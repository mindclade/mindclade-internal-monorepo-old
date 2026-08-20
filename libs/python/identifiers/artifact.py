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

import re
from collections.abc import Mapping
from dataclasses import dataclass
from typing import Final

from libs.python.errors import InvalidArgument

from .digest import Digest, is_canonical_digest

MAXIMUM_MEDIA_TYPE_LENGTH: Final = 255
MAXIMUM_LOGICAL_KIND_LENGTH: Final = 128
MAXIMUM_ARTIFACT_SIZE: Final = (1 << 64) - 1
MAXIMUM_SCHEMA_VERSION: Final = (1 << 32) - 1

_MEDIA_TYPE: Final = re.compile(r"^[a-z0-9!#$&^_.+-]+/[a-z0-9!#$&^_.+-]+$")
_LOGICAL_KIND: Final = re.compile(r"^[a-z][a-z0-9._-]{0,127}$")

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
        if not isinstance(self.digest, Digest):
            raise InvalidArgument(
                "artifact digest must be a Digest",
                reason="artifact_digest",
            )
        if (
            isinstance(self.size_bytes, bool)
            or not isinstance(self.size_bytes, int)
            or not 0 <= self.size_bytes <= MAXIMUM_ARTIFACT_SIZE
        ):
            raise InvalidArgument(
                "artifact size must be an unsigned 64-bit integer",
                reason="artifact_size",
            )
        if (
            not isinstance(self.media_type, str)
            or len(self.media_type) > MAXIMUM_MEDIA_TYPE_LENGTH
            or _MEDIA_TYPE.fullmatch(self.media_type) is None
        ):
            raise InvalidArgument(
                "artifact media type must be a bounded type/subtype string",
                reason="artifact_media_type",
            )
        if (
            not isinstance(self.logical_kind, str)
            or len(self.logical_kind) > MAXIMUM_LOGICAL_KIND_LENGTH
            or _LOGICAL_KIND.fullmatch(self.logical_kind) is None
        ):
            raise InvalidArgument(
                "artifact logical kind must be a bounded canonical name",
                reason="artifact_logical_kind",
            )
        # bool is a subclass of int, so `True` would otherwise pass as schema version 1.
        if (
            isinstance(self.schema_version, bool)
            or not isinstance(self.schema_version, int)
            or not 1 <= self.schema_version <= MAXIMUM_SCHEMA_VERSION
        ):
            raise InvalidArgument(
                "artifact schema version must be an unsigned 32-bit integer starting at 1",
                reason="artifact_schema_version",
            )

    @classmethod
    def from_document(cls, document: Mapping[str, object]) -> ArtifactRef:
        """Build a reference from its canonical document form.

        Rejects unknown and missing keys rather than ignoring them: an extra key
        is either a field this build does not understand or a location leaking
        back in, and both are worth failing on.
        """
        if not isinstance(document, Mapping) or any(not isinstance(key, str) for key in document):
            raise InvalidArgument(
                "artifact reference must be a string-keyed mapping",
                reason="artifact_document",
            )
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
        size_bytes = document["size_bytes"]
        media_type = document["media_type"]
        logical_kind = document["logical_kind"]
        schema_version = document["schema_version"]
        if isinstance(size_bytes, bool) or not isinstance(size_bytes, int):
            raise InvalidArgument(
                "artifact size must be an unsigned 64-bit integer",
                reason="artifact_size",
            )
        if not isinstance(media_type, str):
            raise InvalidArgument(
                "artifact media type must be text",
                reason="artifact_media_type",
            )
        if not isinstance(logical_kind, str):
            raise InvalidArgument(
                "artifact logical kind must be text",
                reason="artifact_logical_kind",
            )
        if isinstance(schema_version, bool) or not isinstance(schema_version, int):
            raise InvalidArgument(
                "artifact schema version must be an integer",
                reason="artifact_schema_version",
            )
        return cls(
            digest=Digest.parse(digest),
            size_bytes=size_bytes,
            media_type=media_type,
            logical_kind=logical_kind,
            schema_version=schema_version,
        )

    def to_document(self) -> dict[str, str | int]:
        """The canonical document form, for canonical serialization and manifests."""
        return {
            "digest": self.digest.text,
            "size_bytes": self.size_bytes,
            "media_type": self.media_type,
            "logical_kind": self.logical_kind,
            "schema_version": self.schema_version,
        }
