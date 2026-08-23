# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Strict reference-manifest parsing and hydration boundary."""

from __future__ import annotations

import datetime as dt
import json
from collections.abc import Mapping
from typing import Any

from data.manifest import ArtifactRef

from .index import ReferenceIndex
from .snapshot import ReferenceSnapshot
from .source import ReferenceSource

_ROOT_KEYS = {
    "schema_version",
    "reference_id",
    "version",
    "sources",
    "indexes",
    "compatible_search_tools",
    "generated_at",
    "build_provenance_digest",
}
_SOURCE_KEYS = {"name", "release", "snapshot_digest", "uri", "cutoff", "license_ref"}
_INDEX_KEYS = {
    "kind",
    "format_version",
    "tool",
    "tool_version",
    "parameters_digest",
    "artifacts",
}
_ARTIFACT_KEYS = {"digest", "size_bytes", "media_type", "logical_kind", "schema_version"}
_MAX_MANIFEST_BYTES = 256 * 1024 * 1024


def parse_manifest_document(
    document: str, *, maximum_bytes: int = 16 * 1024 * 1024
) -> Mapping[str, Any]:
    if (
        isinstance(maximum_bytes, bool)
        or not isinstance(maximum_bytes, int)
        or not 1 <= maximum_bytes <= _MAX_MANIFEST_BYTES
        or not isinstance(document, str)
        or not 1 <= len(document.encode()) <= maximum_bytes
    ):
        raise ValueError("reference manifest document is outside bounds")
    try:
        value = json.loads(
            document,
            object_pairs_hook=_strict_object,
            parse_constant=_reject_constant,
        )
    except (json.JSONDecodeError, RecursionError) as error:
        raise ValueError("reference manifest is not valid JSON") from error
    if not isinstance(value, dict) or value.get("schema_version") != 1:
        raise ValueError("reference manifest schema version is unsupported")
    return value


def parse_reference_snapshot(
    document: str, *, maximum_bytes: int = 16 * 1024 * 1024
) -> ReferenceSnapshot:
    """Parse one canonical document without accepting unknown or partial fields."""

    root = dict(parse_manifest_document(document, maximum_bytes=maximum_bytes))
    _require_keys(root, _ROOT_KEYS, "root")
    raw_sources = _require_list(root.get("sources"), "sources", maximum=64)
    raw_indexes = _require_list(root.get("indexes"), "indexes", maximum=100_000)
    raw_tools = _require_list(root.get("compatible_search_tools"), "tools", maximum=1024)

    sources: list[ReferenceSource] = []
    for raw in raw_sources:
        value = _require_mapping(raw, "source")
        _require_keys(value, _SOURCE_KEYS, "source")
        sources.append(
            ReferenceSource(
                _string(value["name"], "source name"),
                _string(value["release"], "source release"),
                _string(value["snapshot_digest"], "source digest"),
                _string(value["uri"], "source URI"),
                _timestamp(value["cutoff"], "source cutoff"),
                _string(value["license_ref"], "source license"),
            )
        )

    indexes: list[ReferenceIndex] = []
    for raw in raw_indexes:
        value = _require_mapping(raw, "index")
        _require_keys(value, _INDEX_KEYS, "index")
        raw_artifacts = _require_list(value["artifacts"], "artifacts", maximum=100_000)
        artifacts: list[ArtifactRef] = []
        for raw_artifact in raw_artifacts:
            artifact = _require_mapping(raw_artifact, "artifact")
            _require_keys(artifact, _ARTIFACT_KEYS, "artifact")
            artifacts.append(
                ArtifactRef(
                    _string(artifact["digest"], "artifact digest"),
                    _integer(artifact["size_bytes"], "artifact size"),
                    _string(artifact["media_type"], "artifact media type"),
                    _string(artifact["logical_kind"], "artifact logical kind"),
                    # uint32 on the wire (mindclade.common.v1.ArtifactRef.schema_version).
                    # Parsing it as a string accepted documents that could never be encoded.
                    _integer(artifact["schema_version"], "artifact schema version"),
                )
            )
        indexes.append(
            ReferenceIndex(
                _string(value["kind"], "index kind"),
                _string(value["format_version"], "index format version"),
                _string(value["tool"], "index tool"),
                _string(value["tool_version"], "index tool version"),
                _string(value["parameters_digest"], "index parameters digest"),
                tuple(artifacts),
            )
        )

    return ReferenceSnapshot(
        _string(root["reference_id"], "reference ID"),
        _string(root["version"], "reference version"),
        tuple(sources),
        tuple(indexes),
        tuple(_string(tool, "compatible tool") for tool in raw_tools),
        _timestamp(root["generated_at"], "generated_at"),
        _string(root["build_provenance_digest"], "build provenance digest"),
    )


def _require_mapping(value: Any, label: str) -> dict[str, Any]:
    if not isinstance(value, dict) or any(not isinstance(key, str) for key in value):
        raise ValueError(f"reference manifest {label} must be an object")
    return value


def _strict_object(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    value: dict[str, Any] = {}
    for key, item in pairs:
        if key in value:
            raise ValueError(f"reference manifest contains duplicate field {key!r}")
        value[key] = item
    return value


def _reject_constant(value: str) -> None:
    raise ValueError(f"reference manifest contains non-finite number {value}")


def _require_list(value: Any, label: str, *, maximum: int) -> list[Any]:
    if not isinstance(value, list) or not value or len(value) > maximum:
        raise ValueError(f"reference manifest {label} must be a bounded non-empty list")
    return value


def _require_keys(value: Mapping[str, Any], expected: set[str], label: str) -> None:
    if set(value) != expected:
        raise ValueError(f"reference manifest {label} fields do not match schema")


def _string(value: Any, label: str) -> str:
    if not isinstance(value, str):
        raise ValueError(f"reference manifest {label} must be a string")
    return value


def _integer(value: Any, label: str) -> int:
    if isinstance(value, bool) or not isinstance(value, int):
        raise ValueError(f"reference manifest {label} must be an integer")
    return value


def _timestamp(value: Any, label: str) -> dt.datetime:
    if not isinstance(value, str):
        raise ValueError(f"reference manifest {label} must be a timestamp")
    try:
        parsed = dt.datetime.fromisoformat(value)
    except ValueError as error:
        raise ValueError(f"reference manifest {label} is invalid") from error
    if parsed.tzinfo is None or parsed.utcoffset() is None:
        raise ValueError(f"reference manifest {label} must be timezone-aware")
    return parsed
