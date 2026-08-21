# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Content-addressed evaluation evidence without raw evaluation examples."""

from __future__ import annotations

import json
import re
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Final

from libs.python.errors import InvalidArgument
from libs.python.identifiers import Digest

from .thresholds import MAXIMUM_METRICS, ThresholdOutcome

SCHEMA_VERSION: Final = "mindclade.dev/evaluation-evidence/v1"
MAXIMUM_EXECUTION_FAILURES: Final = 1024
_ID: Final = re.compile(r"^[a-z][a-z0-9-]{1,62}$")
_REVISION: Final = re.compile(r"^[0-9a-f]{40}$")
_MLFLOW_ID: Final = re.compile(r"^[A-Za-z0-9_-]{1,256}$")


@dataclass(frozen=True, slots=True)
class EvaluationEvidence:
    """A deterministic release input; MLflow stores only a mirror of this record."""

    evaluation_id: str
    candidate_digest: Digest
    plan_digest: Digest
    scorer_digest: Digest
    runtime_image_digest: Digest
    source_revision: str
    dataset_digests: tuple[Digest, ...]
    outcomes: tuple[ThresholdOutcome, ...]
    execution_failures: int
    missing_outputs: int
    started_at: datetime
    finished_at: datetime
    mlflow_run_id: str | None = None

    def __post_init__(self) -> None:
        if not isinstance(self.evaluation_id, str) or _ID.fullmatch(self.evaluation_id) is None:
            raise _invalid("evaluation id must be canonical", "evaluation_id")
        for value, reason in (
            (self.candidate_digest, "candidate_digest"),
            (self.plan_digest, "plan_digest"),
            (self.scorer_digest, "scorer_digest"),
            (self.runtime_image_digest, "runtime_image_digest"),
        ):
            if not isinstance(value, Digest):
                raise _invalid("evidence digest is invalid", reason)
        if (
            not isinstance(self.source_revision, str)
            or _REVISION.fullmatch(self.source_revision) is None
        ):
            raise _invalid("source revision must be an exact commit", "source_revision")
        if not 1 <= len(self.dataset_digests) <= MAXIMUM_METRICS:
            raise _invalid("dataset digest count is outside bounds", "dataset_count")
        if any(not isinstance(item, Digest) for item in self.dataset_digests):
            raise _invalid("dataset digest is invalid", "dataset_digest")
        if len(set(self.dataset_digests)) != len(self.dataset_digests):
            raise _invalid("dataset digests must be unique", "dataset_duplicate")
        if not 1 <= len(self.outcomes) <= MAXIMUM_METRICS:
            raise _invalid("outcome count is outside bounds", "outcome_count")
        names = [item.name for item in self.outcomes]
        if len(set(names)) != len(names):
            raise _invalid("outcome names must be unique", "outcome_duplicate")
        for value, reason in (
            (self.execution_failures, "execution_failures"),
            (self.missing_outputs, "missing_outputs"),
        ):
            if (
                isinstance(value, bool)
                or not isinstance(value, int)
                or not 0 <= value <= MAXIMUM_EXECUTION_FAILURES
            ):
                raise _invalid("failure count is outside bounds", reason)
        _timestamp(self.started_at, "started_at")
        _timestamp(self.finished_at, "finished_at")
        if self.finished_at < self.started_at:
            raise _invalid("evaluation finish precedes start", "evaluation_time_order")
        if self.mlflow_run_id is not None and (
            not isinstance(self.mlflow_run_id, str)
            or _MLFLOW_ID.fullmatch(self.mlflow_run_id) is None
        ):
            raise _invalid("MLflow run id is invalid", "mlflow_run_id")

    @property
    def passed(self) -> bool:
        return (
            self.execution_failures == 0
            and self.missing_outputs == 0
            and all(item.passed for item in self.outcomes)
        )

    def canonical_document(self) -> bytes:
        """Return stable UTF-8 JSON suitable for hashing and artifact publication."""

        document = {
            "schema_version": SCHEMA_VERSION,
            "evaluation_id": self.evaluation_id,
            "candidate_digest": self.candidate_digest.text,
            "plan_digest": self.plan_digest.text,
            "scorer_digest": self.scorer_digest.text,
            "runtime_image_digest": self.runtime_image_digest.text,
            "source_revision": self.source_revision,
            "dataset_digests": sorted(item.text for item in self.dataset_digests),
            "outcomes": [
                {
                    "name": item.name,
                    "category": item.category.value,
                    "passed": item.passed,
                    "actual": item.actual,
                    "threshold": item.threshold,
                    "sample_count": item.sample_count,
                    "reason": item.reason,
                    "dataset_digest": item.dataset_digest.text,
                }
                for item in sorted(self.outcomes, key=lambda value: value.name)
            ],
            "execution_failures": self.execution_failures,
            "missing_outputs": self.missing_outputs,
            "passed": self.passed,
            "started_at": _render_time(self.started_at),
            "finished_at": _render_time(self.finished_at),
            "mlflow_run_id": self.mlflow_run_id,
            "authority": "mindclade-release-evidence",
        }
        return (json.dumps(document, sort_keys=True, separators=(",", ":")) + "\n").encode()

    def digest(self) -> Digest:
        return Digest.of(self.canonical_document())

    def mlflow_projection(self) -> tuple[dict[str, float], dict[str, str]]:
        """Return bounded aggregates and immutable references, never raw examples."""

        metrics = {
            f"qualification.{item.name}": float(item.actual)
            for item in self.outcomes
            if item.actual is not None
        }
        metrics["qualification.passed"] = 1.0 if self.passed else 0.0
        tags = {
            "mindclade.evaluation_id": self.evaluation_id,
            "mindclade.evaluation_evidence_digest": self.digest().text,
            "mindclade.plan_digest": self.plan_digest.text,
            "mindclade.scorer_digest": self.scorer_digest.text,
            "mindclade.runtime_image_digest": self.runtime_image_digest.text,
            "mindclade.source_revision": self.source_revision,
            "mindclade.authority": "mirror",
        }
        return metrics, tags


def _timestamp(value: object, label: str) -> None:
    if not isinstance(value, datetime) or value.tzinfo is None or value.utcoffset() is None:
        raise _invalid(f"{label} must be timezone-aware", "evaluation_timestamp")


def _render_time(value: datetime) -> str:
    return value.astimezone(UTC).isoformat(timespec="milliseconds").replace("+00:00", "Z")


def _invalid(message: str, reason: str) -> InvalidArgument:
    return InvalidArgument(message, reason=reason, operation="evaluation.qualification")
