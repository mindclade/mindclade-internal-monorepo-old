# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Deterministic, model-neutral scientific curation pipeline.

This module owns orchestration of curation transforms, not domain-specific
biology policy. Individual filtering, normalization, licensing, contamination,
and safety stages remain in their owning modules and are injected here as
pure transforms.
"""

from __future__ import annotations

import hashlib
import json
from collections.abc import Callable, Iterable
from dataclasses import dataclass, field
from typing import Protocol

MAX_RECORDS = 10_000_000
MAX_METADATA_ENTRIES = 256
MAX_KEY_BYTES = 1024
MAX_PAYLOAD_BYTES = 64 * 1024 * 1024


@dataclass(frozen=True, slots=True)
class CuratedRecord:
    key: str
    payload: bytes
    metadata: tuple[tuple[str, str], ...] = field(default_factory=tuple)

    def validate(self) -> None:
        if not self.key or len(self.key.encode("utf-8")) > MAX_KEY_BYTES:
            raise ValueError("curation record key is outside bounds")
        if not self.payload or len(self.payload) > MAX_PAYLOAD_BYTES:
            raise ValueError("curation record payload is outside bounds")
        if len(self.metadata) > MAX_METADATA_ENTRIES:
            raise ValueError("curation record metadata exceeds bounds")
        previous = ""
        for key, value in self.metadata:
            if not key or key <= previous or not value:
                raise ValueError("curation metadata must be sorted, unique, and non-empty")
            previous = key

    @property
    def digest(self) -> str:
        self.validate()
        digest = hashlib.sha256()
        digest.update(self.key.encode("utf-8"))
        digest.update(b"\0")
        digest.update(self.payload)
        for key, value in self.metadata:
            digest.update(b"\0")
            digest.update(key.encode("utf-8"))
            digest.update(b"=")
            digest.update(value.encode("utf-8"))
        return "sha256:" + digest.hexdigest()


class Transform(Protocol):
    def __call__(self, record: CuratedRecord) -> CuratedRecord | None: ...


@dataclass(frozen=True, slots=True)
class PipelineResult:
    records: tuple[CuratedRecord, ...]
    input_records: int
    dropped_records: int
    manifest_digest: str


class CurationPipeline:
    """Apply bounded deterministic transforms and deduplicate by canonical key."""

    def __init__(self, stages: Iterable[Transform]) -> None:
        self._stages = tuple(stages)
        if not self._stages:
            raise ValueError("curation pipeline requires at least one stage")

    def run(self, records: Iterable[CuratedRecord]) -> PipelineResult:
        output: dict[str, CuratedRecord] = {}
        input_records = 0
        dropped_records = 0

        for record in records:
            input_records += 1
            if input_records > MAX_RECORDS:
                raise ValueError("curation input exceeds record bound")
            record.validate()
            current: CuratedRecord | None = record
            for stage in self._stages:
                if current is None:
                    break
                current = stage(current)
                if current is not None:
                    current.validate()
            if current is None:
                dropped_records += 1
                continue
            previous = output.get(current.key)
            if previous is not None and previous.digest != current.digest:
                raise ValueError(f"conflicting curated records for canonical key {current.key!r}")
            output[current.key] = current

        ordered = tuple(output[key] for key in sorted(output))
        manifest = {
            "schema_version": 1,
            "input_records": input_records,
            "dropped_records": dropped_records,
            "records": [{"key": record.key, "digest": record.digest} for record in ordered],
        }
        canonical = json.dumps(
            manifest,
            sort_keys=True,
            separators=(",", ":"),
            ensure_ascii=True,
        ).encode("ascii")
        return PipelineResult(
            records=ordered,
            input_records=input_records,
            dropped_records=dropped_records,
            manifest_digest="sha256:" + hashlib.sha256(canonical).hexdigest(),
        )


def identity(record: CuratedRecord) -> CuratedRecord:
    """Reference transform used by smoke tests and pipeline composition."""

    return record


def transform(function: Callable[[CuratedRecord], CuratedRecord | None]) -> Transform:
    """Type-preserving helper for composing function-based stages."""

    return function
