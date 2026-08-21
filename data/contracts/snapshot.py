# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Reproducible dataset publication snapshots."""

from __future__ import annotations

import datetime as dt
import re
from dataclasses import dataclass

from .shard import ShardManifest

_DIGEST = re.compile(r"sha256:[0-9a-f]{64}")
_IDENTIFIER = re.compile(r"[a-z][a-z0-9-]{1,62}")
_SEMVER = re.compile(r"[0-9]+\.[0-9]+\.[0-9]+")


@dataclass(frozen=True)
class DatasetSnapshot:
    dataset_id: str
    contract_version: str
    contract_digest: str
    transform_digest: str
    generated_at: dt.datetime
    split: str
    seed: int
    shards: tuple[ShardManifest, ...]
    record_count: int

    def __post_init__(self) -> None:
        if not isinstance(self.dataset_id, str) or not _IDENTIFIER.fullmatch(self.dataset_id):
            raise ValueError("dataset_id must be a bounded lowercase identifier")
        if not isinstance(self.contract_version, str) or not _SEMVER.fullmatch(
            self.contract_version
        ):
            raise ValueError("contract_version must be semantic X.Y.Z")
        for value, label in (
            (self.contract_digest, "contract_digest"),
            (self.transform_digest, "transform_digest"),
        ):
            if not _DIGEST.fullmatch(value):
                raise ValueError(f"{label} must be canonical SHA-256")
        if (
            not isinstance(self.generated_at, dt.datetime)
            or self.generated_at.tzinfo is None
            or self.generated_at.utcoffset() is None
        ):
            raise ValueError("generated_at must be timezone-aware")
        if self.split not in {"train", "validation", "test", "holdout", "serving"}:
            raise ValueError("split must be one declared lifecycle partition")
        if isinstance(self.seed, bool) or not isinstance(self.seed, int) or self.seed < 0:
            raise ValueError("seed must be a non-negative integer")
        shards = tuple(self.shards)
        if (
            not shards
            or len(shards) > 100_000
            or any(not isinstance(item, ShardManifest) for item in shards)
        ):
            raise ValueError("snapshot requires 1..100000 shards")
        if len({item.uri for item in shards}) != len(shards):
            raise ValueError("snapshot shard URIs must be unique")
        if (
            isinstance(self.record_count, bool)
            or not isinstance(self.record_count, int)
            or self.record_count < 0
        ):
            raise ValueError("snapshot record_count must be a non-negative integer")
        if self.record_count != sum(item.record_count for item in shards):
            raise ValueError("snapshot record_count must equal the shard total")
        object.__setattr__(self, "shards", shards)
