# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Fail-closed orchestration from predeclared evaluation plan to release decision."""

from __future__ import annotations

import json
import re
from dataclasses import dataclass
from datetime import datetime
from typing import Final

from libs.python.errors import InvalidArgument
from libs.python.identifiers import Digest

from .evidence import EvaluationEvidence
from .promotion import PromotionDecision, make_promotion_decision
from .thresholds import (
    MAXIMUM_METRICS,
    MetricObservation,
    ThresholdOutcome,
    ThresholdRule,
    evaluate_threshold,
    missing_outcome,
)
from .verification import Attestation, VerificationPolicy, VerificationResult, verify_evidence

_ID: Final = re.compile(r"^[a-z][a-z0-9-]{1,62}$")
_REVISION: Final = re.compile(r"^[0-9a-f]{40}$")


@dataclass(frozen=True, slots=True)
class EvaluationPlan:
    """Immutable gate policy declared before a candidate is measured."""

    evaluation_id: str
    candidate_digest: Digest
    scorer_digest: Digest
    rules: tuple[ThresholdRule, ...]

    def __post_init__(self) -> None:
        if not isinstance(self.evaluation_id, str) or _ID.fullmatch(self.evaluation_id) is None:
            raise _invalid("evaluation id must be canonical", "evaluation_id")
        if not isinstance(self.candidate_digest, Digest) or not isinstance(
            self.scorer_digest, Digest
        ):
            raise _invalid("evaluation plan digest is invalid", "plan_digest")
        if not 1 <= len(self.rules) <= MAXIMUM_METRICS:
            raise _invalid("evaluation rule count is outside bounds", "rule_count")
        names = [item.name for item in self.rules]
        if len(set(names)) != len(names):
            raise _invalid("evaluation rule names must be unique", "rule_duplicate")

    def canonical_document(self) -> bytes:
        value = {
            "schema_version": "mindclade.dev/evaluation-plan/v1",
            "evaluation_id": self.evaluation_id,
            "candidate_digest": self.candidate_digest.text,
            "scorer_digest": self.scorer_digest.text,
            "rules": [
                {
                    "name": item.name,
                    "category": item.category.value,
                    "direction": item.direction.value,
                    "threshold": item.threshold,
                    "minimum_samples": item.minimum_samples,
                    "dataset_digest": item.dataset_digest.text,
                    "required_slices": sorted(item.required_slices),
                    "baseline": item.baseline,
                    "maximum_regression": item.maximum_regression,
                }
                for item in sorted(self.rules, key=lambda rule: rule.name)
            ],
        }
        return (json.dumps(value, sort_keys=True, separators=(",", ":")) + "\n").encode()

    def digest(self) -> Digest:
        return Digest.of(self.canonical_document())


@dataclass(frozen=True, slots=True)
class EvaluationBatch:
    candidate_digest: Digest
    scorer_digest: Digest
    runtime_image_digest: Digest
    source_revision: str
    observations: tuple[MetricObservation, ...]
    execution_failures: int
    missing_outputs: int
    started_at: datetime
    finished_at: datetime
    mlflow_run_id: str | None = None

    def __post_init__(self) -> None:
        if any(
            not isinstance(value, Digest)
            for value in (self.candidate_digest, self.scorer_digest, self.runtime_image_digest)
        ):
            raise _invalid("evaluation batch digest is invalid", "batch_digest")
        if not isinstance(self.source_revision, str) or _REVISION.fullmatch(self.source_revision) is None:
            raise _invalid("source revision must be an exact commit", "source_revision")
        if len(self.observations) > MAXIMUM_METRICS:
            raise _invalid("observation count is outside bounds", "observation_count")
        names = [item.name for item in self.observations]
        if len(set(names)) != len(names):
            raise _invalid("observation names must be unique", "observation_duplicate")


@dataclass(frozen=True, slots=True)
class GateResult:
    evidence: EvaluationEvidence
    verification: VerificationResult
    promotion: PromotionDecision


def build_evidence(plan: EvaluationPlan, batch: EvaluationBatch) -> EvaluationEvidence:
    """Evaluate every rule and preserve missing outputs as explicit failures."""

    if not plan.candidate_digest.equals(batch.candidate_digest):
        raise _invalid("batch candidate does not match plan", "candidate_mismatch")
    if not plan.scorer_digest.equals(batch.scorer_digest):
        raise _invalid("batch scorer does not match plan", "scorer_mismatch")

    observations = {item.name: item for item in batch.observations}
    outcomes: list[ThresholdOutcome] = []
    for rule in sorted(plan.rules, key=lambda item: item.name):
        observed = observations.get(rule.name)
        outcomes.append(
            missing_outcome(rule) if observed is None else evaluate_threshold(rule, observed)
        )
    undeclared = sorted(set(observations) - {item.name for item in plan.rules})
    if undeclared:
        raise _invalid("batch contains undeclared metrics", "undeclared_metric")

    dataset_digests = tuple(
        sorted({item.dataset_digest for item in plan.rules}, key=lambda item: item.text)
    )
    return EvaluationEvidence(
        evaluation_id=plan.evaluation_id,
        candidate_digest=plan.candidate_digest,
        plan_digest=plan.digest(),
        scorer_digest=plan.scorer_digest,
        runtime_image_digest=batch.runtime_image_digest,
        source_revision=batch.source_revision,
        dataset_digests=dataset_digests,
        outcomes=tuple(outcomes),
        execution_failures=batch.execution_failures,
        missing_outputs=batch.missing_outputs + sum(item.actual is None for item in outcomes),
        started_at=batch.started_at,
        finished_at=batch.finished_at,
        mlflow_run_id=batch.mlflow_run_id,
    )


def evaluate_release(
    plan: EvaluationPlan,
    batch: EvaluationBatch,
    policy: VerificationPolicy,
    attestation: Attestation | None,
) -> GateResult:
    evidence = build_evidence(plan, batch)
    verification = verify_evidence(evidence, policy, attestation)
    promotion = make_promotion_decision(plan.candidate_digest, verification)
    return GateResult(evidence=evidence, verification=verification, promotion=promotion)


def _invalid(message: str, reason: str) -> InvalidArgument:
    return InvalidArgument(message, reason=reason, operation="evaluation.qualification")
