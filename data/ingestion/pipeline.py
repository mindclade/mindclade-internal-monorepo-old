# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Bounded deterministic canonicalization and contract-validation pipeline."""

from __future__ import annotations

from collections.abc import Callable, Iterable, Mapping
from typing import Any

from data.contracts import DatasetContract

from .canonical import canonical_json
from .publication import IngestionResult
from .raw import RawRecord
from .record import CanonicalRecord
from .stages import StageSpec
from .validation import RejectedRecord, validate_canonical

Canonicalizer = Callable[[RawRecord], Mapping[str, Any]]
MAX_RECORDS = 10_000_000


class IngestionPipeline:
    """Canonicalize framed records without owning retrieval or durable publication."""

    def __init__(
        self,
        contract: DatasetContract,
        stage: StageSpec,
        canonicalize: Canonicalizer,
        *,
        reject_invalid: bool = True,
    ) -> None:
        if not isinstance(contract, DatasetContract):
            raise TypeError("contract must be a DatasetContract")
        if not isinstance(stage, StageSpec) or not stage.replay_safe:
            raise ValueError("canonical ingestion requires a replay-safe stage")
        if stage.output_schema_digest != contract.schema_digest:
            raise ValueError("stage output schema does not match dataset contract")
        if not callable(canonicalize):
            raise TypeError("canonicalize must be callable")
        if not isinstance(reject_invalid, bool):
            raise TypeError("reject_invalid must be boolean")
        self._contract = contract
        self._stage = stage
        self._canonicalize = canonicalize
        self._reject_invalid = reject_invalid

    def run(self, records: Iterable[RawRecord]) -> IngestionResult:
        accepted: dict[bytes, CanonicalRecord] = {}
        rejected: list[RejectedRecord] = []
        input_records = 0
        duplicates = 0

        for raw in records:
            input_records += 1
            if input_records > MAX_RECORDS:
                raise ValueError("ingestion input exceeds record bound")
            if not isinstance(raw, RawRecord):
                raise TypeError("ingestion input must contain RawRecord values")
            try:
                canonical = CanonicalRecord(raw.digest, self._canonicalize(raw))
            except (TypeError, ValueError) as error:
                if not self._reject_invalid:
                    raise
                from data.contracts import ValidationIssue

                rejected.append(
                    RejectedRecord(
                        raw.digest,
                        (ValidationIssue("<record>", "canonicalize", str(error)[:1024]),),
                    )
                )
                continue
            issues = validate_canonical(canonical, self._contract)
            if issues:
                if not self._reject_invalid:
                    rendered = ", ".join(f"{issue.field}:{issue.code}" for issue in issues)
                    raise ValueError(f"canonical record violates contract: {rendered}")
                rejected.append(RejectedRecord(raw.digest, issues))
                continue
            identity = canonical_json(
                [canonical.values[name] for name in self._contract.primary_keys]
            )
            previous = accepted.get(identity)
            if previous is not None:
                if previous.digest != canonical.digest:
                    raise ValueError("conflicting records share a dataset primary key")
                duplicates += 1
                continue
            accepted[identity] = canonical

        ordered = tuple(accepted[key] for key in sorted(accepted))
        return IngestionResult(
            records=ordered,
            rejected=tuple(sorted(rejected, key=lambda item: item.source_record_digest)),
            input_records=input_records,
            duplicate_records=duplicates,
            stage_idempotency_key=self._stage.idempotency_key,
        )
