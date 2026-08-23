# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Strict hydration for immutable dataset manifest documents."""

from __future__ import annotations

import datetime as dt
import json
from typing import Any

from data.contracts import Sensitivity
from data.manifest import ArtifactRef

from .versioning import DatasetVersionManifest, SplitPolicy

_ROOT_KEYS = {
    "schema_version",
    "dataset_id",
    "version",
    "owner",
    "intended_uses",
    "prohibited_uses",
    "source_snapshot_digests",
    "artifacts",
    "schema_digest",
    "canonicalization_version",
    "curation_version",
    "tokenizer_version",
    "featurization_version",
    "split_policy",
    "quality_report_digest",
    "lineage_graph_digest",
    "build_provenance_digest",
    "evidence_digests",
    "generated_at",
    "classification",
    "supersedes_manifest_digest",
}
_ARTIFACT_KEYS = {"digest", "size_bytes", "media_type", "logical_kind", "schema_version"}
_SPLIT_KEYS = {
    "algorithm",
    "seed",
    "grouping_keys",
    "stratification_keys",
    "temporal_cutoff",
}
_MAX_MANIFEST_BYTES = 256 * 1024 * 1024


def parse_dataset_manifest(
    document: str, *, maximum_bytes: int = 16 * 1024 * 1024
) -> DatasetVersionManifest:
    """Parse one manifest, rejecting unknown, missing, oversized, or mistyped input."""

    if (
        isinstance(maximum_bytes, bool)
        or not isinstance(maximum_bytes, int)
        or not 1 <= maximum_bytes <= _MAX_MANIFEST_BYTES
        or not isinstance(document, str)
        or not 1 <= len(document.encode()) <= maximum_bytes
    ):
        raise ValueError("dataset manifest document is outside bounds")
    try:
        raw = json.loads(
            document,
            object_pairs_hook=_strict_object,
            parse_constant=_reject_constant,
        )
    except (json.JSONDecodeError, RecursionError) as error:
        raise ValueError("dataset manifest is not valid JSON") from error
    root = _mapping(raw, "root")
    if root.get("schema_version") != 1:
        raise ValueError("dataset manifest schema version is unsupported")
    _keys(root, _ROOT_KEYS, "root")

    artifacts: list[ArtifactRef] = []
    for raw_artifact in _list(root["artifacts"], "artifacts", maximum=100_000):
        artifact = _mapping(raw_artifact, "artifact")
        _keys(artifact, _ARTIFACT_KEYS, "artifact")
        artifacts.append(
            ArtifactRef(
                _string(artifact["digest"], "artifact digest"),
                _integer(artifact["size_bytes"], "artifact size"),
                _string(artifact["media_type"], "artifact media type"),
                _string(artifact["logical_kind"], "artifact logical kind"),
                # uint32 on the wire (mindclade.common.v1.ArtifactRef.schema_version). Parsing
                # it as a string accepted documents that could never be encoded.
                _integer(artifact["schema_version"], "artifact schema version"),
            )
        )

    split = _mapping(root["split_policy"], "split policy")
    _keys(split, _SPLIT_KEYS, "split policy")
    cutoff_value = split["temporal_cutoff"]
    cutoff = None if cutoff_value is None else _timestamp(cutoff_value, "temporal cutoff")

    try:
        classification = Sensitivity(_string(root["classification"], "classification"))
    except ValueError as error:
        raise ValueError("dataset manifest classification is invalid") from error

    return DatasetVersionManifest(
        dataset_id=_string(root["dataset_id"], "dataset ID"),
        version=_string(root["version"], "version"),
        owner=_string(root["owner"], "owner"),
        intended_uses=_strings(root["intended_uses"], "intended uses", maximum=64),
        prohibited_uses=_strings(root["prohibited_uses"], "prohibited uses", maximum=64),
        source_snapshot_digests=_strings(
            root["source_snapshot_digests"], "source snapshots", maximum=4096
        ),
        artifacts=tuple(artifacts),
        schema_digest=_string(root["schema_digest"], "schema digest"),
        canonicalization_version=_string(
            root["canonicalization_version"], "canonicalization version"
        ),
        curation_version=_string(root["curation_version"], "curation version"),
        tokenizer_version=_optional_string(root["tokenizer_version"], "tokenizer version"),
        featurization_version=_optional_string(
            root["featurization_version"], "featurization version"
        ),
        split_policy=SplitPolicy(
            _string(split["algorithm"], "split algorithm"),
            _integer(split["seed"], "split seed"),
            _strings(split["grouping_keys"], "grouping keys", maximum=16),
            _strings(
                split["stratification_keys"],
                "stratification keys",
                maximum=16,
                allow_empty=True,
            ),
            cutoff,
        ),
        quality_report_digest=_string(root["quality_report_digest"], "quality digest"),
        lineage_graph_digest=_string(root["lineage_graph_digest"], "lineage digest"),
        build_provenance_digest=_string(root["build_provenance_digest"], "build provenance digest"),
        evidence_digests=_strings(
            root["evidence_digests"], "evidence digests", maximum=4096, allow_empty=True
        ),
        generated_at=_timestamp(root["generated_at"], "generated_at"),
        classification=classification,
        supersedes_manifest_digest=_optional_string(
            root["supersedes_manifest_digest"], "superseded manifest digest"
        ),
    )


def _mapping(value: Any, label: str) -> dict[str, Any]:
    if not isinstance(value, dict) or any(not isinstance(key, str) for key in value):
        raise ValueError(f"dataset manifest {label} must be an object")
    return value


def _strict_object(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    value: dict[str, Any] = {}
    for key, item in pairs:
        if key in value:
            raise ValueError(f"dataset manifest contains duplicate field {key!r}")
        value[key] = item
    return value


def _reject_constant(value: str) -> None:
    raise ValueError(f"dataset manifest contains non-finite number {value}")


def _list(value: Any, label: str, *, maximum: int, allow_empty: bool = False) -> list[Any]:
    if not isinstance(value, list) or (not value and not allow_empty) or len(value) > maximum:
        raise ValueError(f"dataset manifest {label} must be a bounded list")
    return value


def _keys(value: dict[str, Any], expected: set[str], label: str) -> None:
    if set(value) != expected:
        raise ValueError(f"dataset manifest {label} fields do not match schema")


def _string(value: Any, label: str) -> str:
    if not isinstance(value, str):
        raise ValueError(f"dataset manifest {label} must be a string")
    return value


def _optional_string(value: Any, label: str) -> str | None:
    return None if value is None else _string(value, label)


def _integer(value: Any, label: str) -> int:
    if isinstance(value, bool) or not isinstance(value, int):
        raise ValueError(f"dataset manifest {label} must be an integer")
    return value


def _strings(value: Any, label: str, *, maximum: int, allow_empty: bool = False) -> tuple[str, ...]:
    return tuple(
        _string(item, label)
        for item in _list(value, label, maximum=maximum, allow_empty=allow_empty)
    )


def _timestamp(value: Any, label: str) -> dt.datetime:
    if not isinstance(value, str):
        raise ValueError(f"dataset manifest {label} must be a timestamp")
    try:
        parsed = dt.datetime.fromisoformat(value)
    except ValueError as error:
        raise ValueError(f"dataset manifest {label} is invalid") from error
    if parsed.tzinfo is None or parsed.utcoffset() is None:
        raise ValueError(f"dataset manifest {label} must be timezone-aware")
    return parsed
