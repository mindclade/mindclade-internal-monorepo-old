# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Immutable, deterministic process-local artifact manifest."""

from __future__ import annotations

from collections.abc import Mapping, Sequence
from dataclasses import dataclass, field
from types import MappingProxyType
from typing import Final, cast

from libs.python.errors import InvalidArgument, ResourceExhausted
from libs.python.identifiers import ArtifactRef, Digest
from libs.python.serialization import canonical_json_bytes

ARTIFACT_MANIFEST_FIELDS: Final = frozenset(
    {"schema_version", "artifact", "parents", "annotations"}
)
ARTIFACT_MANIFEST_SCHEMA_VERSION: Final = 1
MAXIMUM_PARENTS: Final = 256
MAXIMUM_ANNOTATIONS: Final = 64
MAXIMUM_ANNOTATION_KEY_LENGTH: Final = 128
MAXIMUM_ANNOTATION_VALUE_LENGTH: Final = 4096


@dataclass(frozen=True, slots=True)
class ArtifactManifest:
    """An artifact identity plus direct immutable provenance edges.

    This is not a catalog record or wire contract. Locations, credentials,
    retention policy, grants, and mutable names are intentionally absent.
    """

    artifact: ArtifactRef
    parents: tuple[ArtifactRef, ...] = ()
    annotations: Mapping[str, str] = field(default_factory=dict)
    schema_version: int = ARTIFACT_MANIFEST_SCHEMA_VERSION

    def __post_init__(self) -> None:
        if not isinstance(self.artifact, ArtifactRef):
            raise InvalidArgument(
                "artifact manifest requires a canonical ArtifactRef",
                reason="artifact_manifest_ref",
            )
        try:
            parents = tuple(self.parents)
        except TypeError as error:
            raise InvalidArgument(
                "artifact manifest parents must be iterable ArtifactRef values",
                reason="artifact_manifest_parents",
                cause=error,
            ) from error
        if len(parents) > MAXIMUM_PARENTS:
            raise ResourceExhausted(
                f"artifact manifest accepts at most {MAXIMUM_PARENTS} parents",
                reason="artifact_manifest_parent_count",
            )
        if any(not isinstance(parent, ArtifactRef) for parent in parents):
            raise InvalidArgument(
                "artifact manifest parents must be ArtifactRef values",
                reason="artifact_manifest_parents",
            )
        parent_digests = [parent.digest.text for parent in parents]
        if len(set(parent_digests)) != len(parent_digests):
            raise InvalidArgument(
                "artifact manifest parents must be unique",
                reason="artifact_manifest_duplicate_parent",
            )
        if self.artifact.digest.text in parent_digests:
            raise InvalidArgument(
                "artifact manifest cannot name itself as a parent",
                reason="artifact_manifest_self_parent",
            )
        if not isinstance(self.annotations, Mapping):
            raise InvalidArgument(
                "artifact annotations must be a mapping",
                reason="artifact_manifest_annotations",
            )
        if len(self.annotations) > MAXIMUM_ANNOTATIONS:
            raise ResourceExhausted(
                f"artifact manifest accepts at most {MAXIMUM_ANNOTATIONS} annotations",
                reason="artifact_manifest_annotation_count",
            )
        annotations: dict[str, str] = {}
        for key, value in self.annotations.items():
            if (
                not isinstance(key, str)
                or not key
                or len(key) > MAXIMUM_ANNOTATION_KEY_LENGTH
                or not isinstance(value, str)
                or len(value) > MAXIMUM_ANNOTATION_VALUE_LENGTH
            ):
                raise InvalidArgument(
                    "artifact annotations must contain bounded text keys and values",
                    reason="artifact_manifest_annotation",
                )
            annotations[key] = value
        if (
            isinstance(self.schema_version, bool)
            or not isinstance(self.schema_version, int)
            or self.schema_version != ARTIFACT_MANIFEST_SCHEMA_VERSION
        ):
            raise InvalidArgument(
                f"artifact manifest schema version must be {ARTIFACT_MANIFEST_SCHEMA_VERSION}",
                reason="artifact_manifest_schema",
            )
        object.__setattr__(self, "parents", parents)
        object.__setattr__(self, "annotations", MappingProxyType(annotations))

    def to_document(self) -> dict[str, object]:
        return {
            "schema_version": self.schema_version,
            "artifact": self.artifact.to_document(),
            "parents": [parent.to_document() for parent in self.parents],
            "annotations": dict(self.annotations),
        }

    @classmethod
    def from_document(cls, document: Mapping[str, object]) -> ArtifactManifest:
        if not isinstance(document, Mapping) or set(document) != ARTIFACT_MANIFEST_FIELDS:
            raise InvalidArgument(
                "artifact manifest has unknown or missing fields",
                reason="artifact_manifest_fields",
            )
        artifact_document = document["artifact"]
        parent_documents = document["parents"]
        annotations = document["annotations"]
        schema_version = document["schema_version"]
        if not isinstance(artifact_document, Mapping):
            raise InvalidArgument(
                "artifact manifest artifact must be a mapping",
                reason="artifact_manifest_ref",
            )
        if not isinstance(parent_documents, Sequence) or isinstance(
            parent_documents, str | bytes | bytearray
        ):
            raise InvalidArgument(
                "artifact manifest parents must be a sequence",
                reason="artifact_manifest_parents",
            )
        if len(parent_documents) > MAXIMUM_PARENTS:
            raise ResourceExhausted(
                f"artifact manifest accepts at most {MAXIMUM_PARENTS} parents",
                reason="artifact_manifest_parent_count",
            )
        if not isinstance(annotations, Mapping):
            raise InvalidArgument(
                "artifact annotations must be a mapping",
                reason="artifact_manifest_annotations",
            )
        if isinstance(schema_version, bool) or not isinstance(schema_version, int):
            raise InvalidArgument(
                "artifact manifest schema version must be an integer",
                reason="artifact_manifest_schema",
            )
        parents: list[ArtifactRef] = []
        for parent in parent_documents:
            if not isinstance(parent, Mapping):
                raise InvalidArgument(
                    "artifact manifest parents must be mappings",
                    reason="artifact_manifest_parents",
                )
            parents.append(ArtifactRef.from_document(parent))
        return cls(
            artifact=ArtifactRef.from_document(artifact_document),
            parents=tuple(parents),
            annotations=cast(Mapping[str, str], annotations),
            schema_version=schema_version,
        )

    def canonical_bytes(self) -> bytes:
        return canonical_json_bytes(self.to_document())

    @property
    def digest(self) -> Digest:
        return Digest.of(self.canonical_bytes())
