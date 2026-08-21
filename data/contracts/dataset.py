# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Dataset boundary contracts used before ingestion and training."""

from __future__ import annotations

import re
from dataclasses import dataclass

from .record import FieldContract, FieldType, Sensitivity
from .source import SourceSnapshot

_IDENTIFIER = re.compile(r"[a-z][a-z0-9-]{1,62}")
_SEMVER = re.compile(r"[0-9]+\.[0-9]+\.[0-9]+")
_DIGEST = re.compile(r"sha256:[0-9a-f]{64}")
_SENSITIVITY_RANK = {
    Sensitivity.PUBLIC: 0,
    Sensitivity.INTERNAL: 1,
    Sensitivity.PROPRIETARY_INTERNAL: 2,
    Sensitivity.RESTRICTED: 3,
}


@dataclass(frozen=True)
class DatasetContract:
    """A fail-closed contract for a versioned record collection."""

    dataset_id: str
    version: str
    owner: str
    fields: tuple[FieldContract, ...]
    primary_keys: tuple[str, ...]
    event_time_field: str
    ingestion_time_field: str
    schema_digest: str
    sources: tuple[SourceSnapshot, ...]
    classification: Sensitivity = Sensitivity.PROPRIETARY_INTERNAL
    allowed_lateness_seconds: int = 0
    freshness_seconds: int = 86_400
    retention_days: int = 365
    deletion_policy: str = "owner-approved-tombstone"
    unknown_field_policy: str = "reject"

    def __post_init__(self) -> None:
        if (
            not isinstance(self.dataset_id, str)
            or not _IDENTIFIER.fullmatch(self.dataset_id)
            or not isinstance(self.owner, str)
            or not _IDENTIFIER.fullmatch(self.owner)
        ):
            raise ValueError("dataset_id and owner must be bounded lowercase identifiers")
        if not isinstance(self.version, str) or not _SEMVER.fullmatch(self.version):
            raise ValueError("dataset contract version must be semantic X.Y.Z")
        if not isinstance(self.schema_digest, str) or not _DIGEST.fullmatch(self.schema_digest):
            raise ValueError("schema_digest must be canonical SHA-256")
        fields = tuple(self.fields)
        if (
            not fields
            or len(fields) > 1024
            or any(not isinstance(item, FieldContract) for item in fields)
        ):
            raise ValueError("dataset contract requires 1..1024 field contracts")
        names = [item.name for item in fields]
        if len(set(names)) != len(names):
            raise ValueError("dataset field names must be unique")
        primary_keys = tuple(self.primary_keys)
        if (
            not primary_keys
            or len(primary_keys) > 16
            or any(not isinstance(item, str) for item in primary_keys)
            or len(set(primary_keys)) != len(primary_keys)
        ):
            raise ValueError("dataset requires 1..16 unique primary keys")
        if not isinstance(self.event_time_field, str) or not isinstance(
            self.ingestion_time_field, str
        ):
            raise ValueError("dataset time fields must be strings")
        if self.event_time_field == self.ingestion_time_field:
            raise ValueError("event and ingestion time fields must be distinct")
        missing = set(primary_keys) | {self.event_time_field, self.ingestion_time_field}
        missing -= set(names)
        if missing:
            raise ValueError(f"dataset key/time fields are undeclared: {sorted(missing)}")
        by_name = {item.name: item for item in fields}
        for name in (self.event_time_field, self.ingestion_time_field):
            if by_name[name].data_type is not FieldType.TIMESTAMP or by_name[name].nullable:
                raise ValueError("event and ingestion time fields must be non-null timestamps")
        if any(by_name[name].nullable for name in primary_keys):
            raise ValueError("primary key fields must be non-null")
        sources = tuple(self.sources)
        if (
            not sources
            or len(sources) > 64
            or any(not isinstance(item, SourceSnapshot) for item in sources)
        ):
            raise ValueError("dataset contract requires 1..64 immutable source snapshots")
        if not isinstance(self.classification, Sensitivity):
            raise ValueError("classification must be a Sensitivity")
        source_and_field_sensitivity = [source.classification for source in sources] + [
            field.sensitivity for field in fields
        ]
        if any(
            _SENSITIVITY_RANK[item] > _SENSITIVITY_RANK[self.classification]
            for item in source_and_field_sensitivity
        ):
            raise ValueError("dataset classification may not downgrade a source or field")
        source_identities = [(source.uri, source.digest) for source in sources]
        if len(set(source_identities)) != len(source_identities):
            raise ValueError("dataset source snapshots must be unique")
        for value, label, maximum in (
            (self.allowed_lateness_seconds, "allowed_lateness_seconds", 31_536_000),
            (self.freshness_seconds, "freshness_seconds", 31_536_000),
            (self.retention_days, "retention_days", 36_500),
        ):
            if (
                isinstance(value, bool)
                or not isinstance(value, int)
                or value < 0
                or value > maximum
            ):
                raise ValueError(f"{label} is outside its bounded range")
        if self.freshness_seconds == 0:
            raise ValueError("freshness_seconds must be positive")
        if self.unknown_field_policy != "reject":
            raise ValueError("unknown fields must be rejected")
        if self.deletion_policy not in {"owner-approved-tombstone", "retention-expiry"}:
            raise ValueError("deletion_policy must be an approved deterministic policy")
        object.__setattr__(self, "fields", fields)
        object.__setattr__(self, "primary_keys", primary_keys)
        object.__setattr__(self, "sources", sources)
