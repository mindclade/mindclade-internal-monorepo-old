# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Behavioral coverage for deterministic, fail-closed release qualification."""

from datetime import UTC, datetime, timedelta

import pytest

from evaluation.qualification import (
    Attestation,
    Direction,
    EvaluationBatch,
    EvaluationPlan,
    MetricCategory,
    MetricObservation,
    SliceObservation,
    ThresholdRule,
    VerificationPolicy,
    build_evidence,
    evaluate_release,
)
from libs.python.errors import InvalidArgument
from libs.python.identifiers import Digest


def digest(value: str) -> Digest:
    return Digest.of_text(value)


def rule(
    name: str,
    category: MetricCategory,
    *,
    direction: Direction = Direction.AT_LEAST,
    threshold: float = 0.8,
    baseline: float | None = None,
    regression: float = 0.0,
    slices: tuple[str, ...] = (),
) -> ThresholdRule:
    return ThresholdRule(
        name=name,
        category=category,
        direction=direction,
        threshold=threshold,
        minimum_samples=10,
        dataset_digest=digest("dataset-" + name),
        required_slices=slices,
        baseline=baseline,
        maximum_regression=regression,
    )


def observation(item: ThresholdRule, value: float) -> MetricObservation:
    slices = tuple(
        SliceObservation(name=name, value=value, sample_count=10) for name in item.required_slices
    )
    return MetricObservation(
        name=item.name,
        value=value,
        sample_count=10,
        dataset_digest=item.dataset_digest,
        slices=slices,
    )


def plan_and_batch() -> tuple[EvaluationPlan, EvaluationBatch]:
    rules = (
        rule("accuracy", MetricCategory.QUALITY, slices=("rare-class",)),
        rule("unsafe-rate", MetricCategory.SAFETY, direction=Direction.AT_MOST, threshold=0.01),
    )
    candidate = digest("candidate")
    scorer = digest("scorer")
    plan = EvaluationPlan("release-gate", candidate, scorer, rules)
    started = datetime(2026, 8, 21, 12, tzinfo=UTC)
    batch = EvaluationBatch(
        candidate_digest=candidate,
        scorer_digest=scorer,
        runtime_image_digest=digest("runtime-image"),
        source_revision="a" * 40,
        observations=(observation(rules[1], 0.005), observation(rules[0], 0.91)),
        execution_failures=0,
        missing_outputs=0,
        started_at=started,
        finished_at=started + timedelta(minutes=5),
        mlflow_run_id="mirror-run-1",
    )
    return plan, batch


def test_evidence_is_canonical_and_mlflow_projection_is_only_aggregate_references() -> None:
    plan, batch = plan_and_batch()
    first = build_evidence(plan, batch)
    reordered = EvaluationBatch(
        candidate_digest=batch.candidate_digest,
        scorer_digest=batch.scorer_digest,
        runtime_image_digest=batch.runtime_image_digest,
        source_revision=batch.source_revision,
        observations=tuple(reversed(batch.observations)),
        execution_failures=0,
        missing_outputs=0,
        started_at=batch.started_at,
        finished_at=batch.finished_at,
        mlflow_run_id=batch.mlflow_run_id,
    )
    second = build_evidence(plan, reordered)

    assert first.digest().equals(second.digest())
    assert first.passed
    metrics, tags = first.mlflow_projection()
    assert metrics["qualification.passed"] == 1.0
    assert tags["mindclade.evaluation_evidence_digest"] == first.digest().text
    assert b"rare-class" not in first.canonical_document()
    assert b"raw" not in first.canonical_document()


def test_missing_metric_and_scorer_failure_deny_release() -> None:
    plan, batch = plan_and_batch()
    failed = EvaluationBatch(
        candidate_digest=batch.candidate_digest,
        scorer_digest=batch.scorer_digest,
        runtime_image_digest=batch.runtime_image_digest,
        source_revision=batch.source_revision,
        observations=(batch.observations[0],),
        execution_failures=1,
        missing_outputs=0,
        started_at=batch.started_at,
        finished_at=batch.finished_at,
    )
    evidence = build_evidence(plan, failed)
    policy = VerificationPolicy(digest("policy"), (MetricCategory.QUALITY, MetricCategory.SAFETY))
    result = evaluate_release(plan, failed, policy, None)

    assert not evidence.passed
    assert evidence.missing_outputs == 1
    assert not result.promotion.authorized
    assert {
        "attestation-missing",
        "execution-failure",
        "missing-output",
        "threshold-failure",
    }.issubset(result.promotion.reasons)


def test_attestation_must_bind_exact_evidence_and_policy() -> None:
    plan, batch = plan_and_batch()
    evidence = build_evidence(plan, batch)
    policy = VerificationPolicy(digest("policy"), (MetricCategory.QUALITY, MetricCategory.SAFETY))
    attestation = Attestation(
        attestor_identity="service-account:independent-qualifier",
        subject_digest=evidence.digest(),
        policy_digest=policy.policy_digest,
        signature_digest=digest("signature"),
        signed_at=batch.finished_at + timedelta(seconds=1),
    )

    result = evaluate_release(plan, batch, policy, attestation)
    assert result.verification.accepted
    assert result.promotion.authorized
    assert result.promotion.reasons == ()
    assert result.promotion.digest().text.startswith("sha256:")

    wrong = Attestation(
        attestor_identity=attestation.attestor_identity,
        subject_digest=digest("other-evidence"),
        policy_digest=policy.policy_digest,
        signature_digest=attestation.signature_digest,
        signed_at=attestation.signed_at,
    )
    denied = evaluate_release(plan, batch, policy, wrong)
    assert not denied.promotion.authorized
    assert denied.promotion.reasons == ("attestation-subject-mismatch",)


def test_slice_and_baseline_regressions_fail_independently() -> None:
    item = rule(
        "quality",
        MetricCategory.QUALITY,
        threshold=0.7,
        baseline=0.9,
        regression=0.05,
        slices=("critical",),
    )
    candidate = digest("candidate")
    scorer = digest("scorer")
    plan = EvaluationPlan("baseline-gate", candidate, scorer, (item,))
    started = datetime(2026, 8, 21, tzinfo=UTC)
    observed = MetricObservation(
        name=item.name,
        value=0.84,
        sample_count=10,
        dataset_digest=item.dataset_digest,
        slices=(SliceObservation("critical", 0.84, 10),),
    )
    batch = EvaluationBatch(
        candidate,
        scorer,
        digest("image"),
        "b" * 40,
        (observed,),
        0,
        0,
        started,
        started,
    )
    evidence = build_evidence(plan, batch)
    assert evidence.outcomes[0].reason == "slice-threshold-failed"
    assert not evidence.passed


@pytest.mark.parametrize("value", [float("nan"), float("inf"), float("-inf")])
def test_non_finite_metrics_are_rejected(value: float) -> None:
    item = rule("quality", MetricCategory.QUALITY)
    with pytest.raises(InvalidArgument):
        observation(item, value)


def test_candidate_scorer_and_undeclared_metric_mismatches_are_rejected() -> None:
    plan, batch = plan_and_batch()
    mismatched = EvaluationBatch(
        digest("wrong-candidate"),
        batch.scorer_digest,
        batch.runtime_image_digest,
        batch.source_revision,
        batch.observations,
        0,
        0,
        batch.started_at,
        batch.finished_at,
    )
    with pytest.raises(InvalidArgument, match="candidate"):
        build_evidence(plan, mismatched)

    extra_rule = rule("extra", MetricCategory.COST)
    undeclared = EvaluationBatch(
        batch.candidate_digest,
        batch.scorer_digest,
        batch.runtime_image_digest,
        batch.source_revision,
        (*batch.observations, observation(extra_rule, 1.0)),
        0,
        0,
        batch.started_at,
        batch.finished_at,
    )
    with pytest.raises(InvalidArgument, match="undeclared"):
        build_evidence(plan, undeclared)
