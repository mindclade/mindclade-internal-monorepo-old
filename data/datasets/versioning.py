# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Immutable dataset release manifest and split policy."""

from __future__ import annotations

import datetime as dt
import hashlib
import json
import re
from dataclasses import dataclass, field

from data.contracts import Sensitivity
from data.manifest import ArtifactRef

_IDENTIFIER = re.compile(r"[a-z][a-z0-9-]{1,62}")
_SEMVER = re.compile(r"[0-9]+\.[0-9]+\.[0-9]+")
_DIGEST = re.compile(r"sha256:[0-9a-f]{64}")
_VERSION = re.compile(r"[A-Za-z0-9][A-Za-z0-9._+-]{0,127}")


@dataclass(frozen=True, slots=True)
class SplitPolicy:
    algorithm: str
    seed: int
    grouping_keys: tuple[str, ...]
    stratification_keys: tuple[str, ...] = ()
    temporal_cutoff: dt.datetime | None = None

    def __post_init__(self) -> None:
        if not isinstance(self.algorithm, str) or not _VERSION.fullmatch(self.algorithm):
            raise ValueError("split algorithm is invalid")
        if isinstance(self.seed, bool) or not isinstance(self.seed, int) or self.seed < 0:
            raise ValueError("split seed must be a non-negative integer")
        for values, name, limit in (
            (tuple(self.grouping_keys), "grouping", 16),
            (tuple(self.stratification_keys), "stratification", 16),
        ):
            if len(values) > limit or any(
                not isinstance(value, str) or not value or len(value) > 128 for value in values
            ):
                raise ValueError(f"split {name} keys are invalid")
            if len(set(values)) != len(values):
                raise ValueError(f"split {name} keys must be unique")
            object.__setattr__(self, f"{name}_keys", values)
        if not self.grouping_keys:
            raise ValueError("split policy requires at least one leakage grouping key")
        if self.temporal_cutoff is not None and (
            not isinstance(self.temporal_cutoff, dt.datetime)
            or self.temporal_cutoff.tzinfo is None
            or self.temporal_cutoff.utcoffset() is None
        ):
            raise ValueError("split temporal cutoff must be timezone-aware")

    def as_dict(self) -> dict[str, object]:
        return {
            "algorithm": self.algorithm,
            "seed": self.seed,
            "grouping_keys": list(self.grouping_keys),
            "stratification_keys": list(self.stratification_keys),
            "temporal_cutoff": self.temporal_cutoff.astimezone(dt.UTC).isoformat()
            if self.temporal_cutoff is not None
            else None,
        }


@dataclass(frozen=True, slots=True)
class DatasetVersionManifest:
    dataset_id: str
    version: str
    owner: str
    intended_uses: tuple[str, ...]
    prohibited_uses: tuple[str, ...]
    source_snapshot_digests: tuple[str, ...]
    artifacts: tuple[ArtifactRef, ...]
    schema_digest: str
    canonicalization_version: str
    curation_version: str
    split_policy: SplitPolicy
    quality_report_digest: str
    lineage_graph_digest: str
    build_provenance_digest: str
    generated_at: dt.datetime
    classification: Sensitivity
    tokenizer_version: str | None = None
    featurization_version: str | None = None
    evidence_digests: tuple[str, ...] = field(default_factory=tuple)
    supersedes_manifest_digest: str | None = None

    def __post_init__(self) -> None:
        for value, name in ((self.dataset_id, "dataset_id"), (self.owner, "owner")):
            if not isinstance(value, str) or not _IDENTIFIER.fullmatch(value):
                raise ValueError(f"dataset manifest {name} is invalid")
        if not isinstance(self.version, str) or not _SEMVER.fullmatch(self.version):
            raise ValueError("dataset manifest version must be semantic X.Y.Z")
        for values, name, maximum in (
            (tuple(self.intended_uses), "intended uses", 64),
            (tuple(self.prohibited_uses), "prohibited uses", 64),
        ):
            if (
                not values
                or len(values) > maximum
                or any(
                    not isinstance(value, str) or not value.strip() or len(value) > 512
                    for value in values
                )
            ):
                raise ValueError(f"dataset manifest {name} are invalid")
            if len(set(values)) != len(values):
                raise ValueError(f"dataset manifest {name} must be unique")
        source_digests = tuple(self.source_snapshot_digests)
        artifacts = tuple(self.artifacts)
        evidence = tuple(self.evidence_digests)
        if not source_digests or len(source_digests) > 4096:
            raise ValueError("dataset manifest requires bounded source snapshots")
        if (
            not artifacts
            or len(artifacts) > 100_000
            or any(not isinstance(item, ArtifactRef) for item in artifacts)
        ):
            raise ValueError("dataset manifest requires bounded artifact references")
        if len({item.digest for item in artifacts}) != len(artifacts):
            raise ValueError("dataset artifact digests must be unique")
        for value, name in (
            *((value, "source snapshot") for value in source_digests),
            (self.schema_digest, "schema"),
            (self.quality_report_digest, "quality report"),
            (self.lineage_graph_digest, "lineage graph"),
            (self.build_provenance_digest, "build provenance"),
            *((value, "evidence") for value in evidence),
        ):
            if not isinstance(value, str) or not _DIGEST.fullmatch(value):
                raise ValueError(f"dataset {name} digest is invalid")
        if len(source_digests) != len(set(source_digests)) or len(evidence) != len(set(evidence)):
            raise ValueError("dataset source and evidence digests must be unique")
        for version_value, name, required in (
            (self.canonicalization_version, "canonicalization", True),
            (self.curation_version, "curation", True),
            (self.tokenizer_version, "tokenizer", False),
            (self.featurization_version, "featurization", False),
        ):
            if required and version_value is None:
                raise ValueError(f"dataset {name} version is required")
            if version_value is not None and (
                not isinstance(version_value, str) or not _VERSION.fullmatch(version_value)
            ):
                raise ValueError(f"dataset {name} version is invalid")
        if not isinstance(self.split_policy, SplitPolicy):
            raise ValueError("dataset split policy is invalid")
        if (
            not isinstance(self.generated_at, dt.datetime)
            or self.generated_at.tzinfo is None
            or self.generated_at.utcoffset() is None
        ):
            raise ValueError("dataset generated_at must be timezone-aware")
        if not isinstance(self.classification, Sensitivity):
            raise ValueError("dataset classification is invalid")
        if self.supersedes_manifest_digest is not None and not _DIGEST.fullmatch(
            self.supersedes_manifest_digest
        ):
            raise ValueError("superseded manifest digest is invalid")
        object.__setattr__(self, "intended_uses", tuple(self.intended_uses))
        object.__setattr__(self, "prohibited_uses", tuple(self.prohibited_uses))
        object.__setattr__(self, "source_snapshot_digests", source_digests)
        object.__setattr__(self, "artifacts", artifacts)
        object.__setattr__(self, "evidence_digests", evidence)

    def canonical_document(self) -> str:
        value = {
            "schema_version": 1,
            "dataset_id": self.dataset_id,
            "version": self.version,
            "owner": self.owner,
            "intended_uses": list(self.intended_uses),
            "prohibited_uses": list(self.prohibited_uses),
            "source_snapshot_digests": list(self.source_snapshot_digests),
            "artifacts": [
                {
                    "digest": item.digest,
                    "size_bytes": item.size_bytes,
                    "media_type": item.media_type,
                    "logical_kind": item.logical_kind,
                    "schema_version": item.schema_version,
                }
                for item in self.artifacts
            ],
            "schema_digest": self.schema_digest,
            "canonicalization_version": self.canonicalization_version,
            "curation_version": self.curation_version,
            "tokenizer_version": self.tokenizer_version,
            "featurization_version": self.featurization_version,
            "split_policy": self.split_policy.as_dict(),
            "quality_report_digest": self.quality_report_digest,
            "lineage_graph_digest": self.lineage_graph_digest,
            "build_provenance_digest": self.build_provenance_digest,
            "evidence_digests": list(self.evidence_digests),
            "generated_at": self.generated_at.astimezone(dt.UTC).isoformat(),
            "classification": self.classification.value,
            "supersedes_manifest_digest": self.supersedes_manifest_digest,
        }
        return json.dumps(value, sort_keys=True, separators=(",", ":")) + "\n"

    @property
    def manifest_digest(self) -> str:
        return "sha256:" + hashlib.sha256(self.canonical_document().encode()).hexdigest()
