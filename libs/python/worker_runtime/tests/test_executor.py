# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

import math

import pytest

from libs.python.errors import Code, MindcladeError, code_of
from libs.python.identifiers import Digest
from libs.python.worker_runtime import (
    ArtifactRef,
    CancellationToken,
    ExecutionContext,
    StageEnvelope,
    StageExecutor,
    StageKind,
    StageResult,
)

DIGEST_TEXT = "sha256:" + "1" * 64
STAGE_ID = "stage_01890f2c7b7a70008000000000000000"


def artifact() -> ArtifactRef:
    return ArtifactRef(
        Digest.parse(DIGEST_TEXT),
        4,
        "application/octet-stream",
        "features",
        1,
    )


def stage(*, kind: StageKind = StageKind.PREPROCESS, deadline: int = 5_000) -> StageEnvelope:
    return StageEnvelope(
        stage_id=STAGE_ID,
        kind=kind,
        operation="features",
        inputs=(),
        output_namespace="tenant/a",
        resolved_config_digest=DIGEST_TEXT,
        reference_snapshot_digest=None,
        attempt=1,
        fencing_token=7,
        deadline_unix_millis=deadline,
    )


class Engine:
    def execute(self, stage: StageEnvelope, context: ExecutionContext) -> StageResult:
        context.checkpoint(operation=stage.operation)
        return StageResult(outputs=(artifact(),), metrics={"items": 1.0})


def test_stage_executor_enforces_contract_and_delegates() -> None:
    result = StageExecutor(
        StageKind.PREPROCESS,
        Engine(),
        now_millis=lambda: 1_000,
    ).execute(stage())
    assert result.outputs[0].logical_kind == "features"


def test_stage_executor_configuration_is_immutable() -> None:
    executor = StageExecutor(StageKind.PREPROCESS, Engine(), now_millis=lambda: 1_000)
    with pytest.raises(AttributeError):
        executor.kind = StageKind.TRAINING  # type: ignore[misc]


def test_stage_executor_rejects_kind_mismatch_before_engine_execution() -> None:
    with pytest.raises(ValueError) as caught:
        StageExecutor(StageKind.TRAINING, Engine(), now_millis=lambda: 1_000).execute(stage())
    assert code_of(caught.value) is Code.FAILED_PRECONDITION


def test_stage_executor_rejects_an_expired_deadline() -> None:
    with pytest.raises(MindcladeError) as caught:
        StageExecutor(StageKind.PREPROCESS, Engine(), now_millis=lambda: 5_000).execute(stage())
    assert code_of(caught.value) is Code.DEADLINE_EXCEEDED


def test_stage_executor_rejects_success_after_deadline_passes_during_execution() -> None:
    now = [1_000]

    class SlowEngine:
        def execute(self, stage: StageEnvelope, context: ExecutionContext) -> StageResult:
            now[0] = stage.deadline_unix_millis
            return StageResult(outputs=(artifact(),))

    executor = StageExecutor(StageKind.PREPROCESS, SlowEngine(), now_millis=lambda: now[0])
    with pytest.raises(MindcladeError) as caught:
        executor.execute(stage())
    assert code_of(caught.value) is Code.DEADLINE_EXCEEDED


def test_engine_can_cooperatively_observe_cancellation() -> None:
    token = CancellationToken()

    class CancelingEngine:
        def execute(self, stage: StageEnvelope, context: ExecutionContext) -> StageResult:
            token.cancel()
            context.checkpoint(operation=stage.operation)
            raise AssertionError("checkpoint must interrupt execution")

    executor = StageExecutor(StageKind.PREPROCESS, CancelingEngine(), now_millis=lambda: 1_000)
    with pytest.raises(MindcladeError) as caught:
        executor.execute(stage(), cancellation=token)
    assert code_of(caught.value) is Code.CANCELED


def test_stage_metadata_and_result_metrics_are_immutable_snapshots() -> None:
    metadata = {"trace": "abc"}
    envelope = StageEnvelope(
        stage_id=STAGE_ID,
        kind=StageKind.PREPROCESS,
        operation="features",
        inputs=(),
        output_namespace="tenant/a",
        resolved_config_digest=DIGEST_TEXT,
        reference_snapshot_digest=None,
        attempt=1,
        fencing_token=7,
        deadline_unix_millis=5_000,
        metadata=metadata,
    )
    metadata["trace"] = "changed"
    assert envelope.metadata["trace"] == "abc"
    with pytest.raises(TypeError):
        envelope.metadata["trace"] = "changed"  # type: ignore[index]

    metrics = {"items": 1.0}
    result = StageResult((), metrics)
    metrics["items"] = 2.0
    assert result.metrics["items"] == 1.0


@pytest.mark.parametrize("metric", [math.nan, math.inf, -math.inf, True])
def test_stage_result_rejects_non_finite_or_boolean_metrics(metric: float) -> None:
    with pytest.raises(ValueError, match="finite") as caught:
        StageResult((), {"loss": metric})
    assert code_of(caught.value) is Code.INVALID_ARGUMENT


def test_stage_envelope_rejects_wrong_id_kind_and_boolean_counters() -> None:
    with pytest.raises(ValueError, match="resource kind"):
        StageEnvelope(
            stage_id="run_01890f2c7b7a70008000000000000000",
            kind=StageKind.PREPROCESS,
            operation="features",
            inputs=(),
            output_namespace="tenant/a",
            resolved_config_digest=DIGEST_TEXT,
            reference_snapshot_digest=None,
            attempt=True,
            fencing_token=7,
            deadline_unix_millis=5_000,
        )
    with pytest.raises(ValueError, match="attempt"):
        StageEnvelope(
            stage_id=STAGE_ID,
            kind=StageKind.PREPROCESS,
            operation="features",
            inputs=(),
            output_namespace="tenant/a",
            resolved_config_digest=DIGEST_TEXT,
            reference_snapshot_digest=None,
            attempt=True,
            fencing_token=7,
            deadline_unix_millis=5_000,
        )


def test_worker_runtime_wraps_invalid_iterables_and_stage_types() -> None:
    with pytest.raises(ValueError, match="stage inputs") as caught:
        StageEnvelope(
            stage_id=STAGE_ID,
            kind=StageKind.PREPROCESS,
            operation="features",
            inputs=1,  # type: ignore[arg-type]
            output_namespace="tenant/a",
            resolved_config_digest=DIGEST_TEXT,
            reference_snapshot_digest=None,
            attempt=1,
            fencing_token=7,
            deadline_unix_millis=5_000,
        )
    assert code_of(caught.value) is Code.INVALID_ARGUMENT

    with pytest.raises(ValueError, match="stage outputs"):
        StageResult(1)  # type: ignore[arg-type]

    executor = StageExecutor(StageKind.PREPROCESS, Engine(), now_millis=lambda: 1_000)
    with pytest.raises(ValueError, match="StageEnvelope"):
        executor.execute(object())  # type: ignore[arg-type]
