# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from threading import Event, Thread

import pytest

from libs.python.errors import Code, MindcladeError, code_of
from libs.python.worker_runtime import (
    ExecutionContext,
    StageEnvelope,
    StageExecutor,
    StageKind,
    StageResult,
    StageWorker,
    WorkerLifecycle,
    WorkerLimits,
    WorkerState,
    WorkloadEnvelope,
)

DIGEST = "sha256:" + "1" * 64


def _resource(kind: str, suffix: int) -> str:
    return f"{kind}_019c00000000700080000000000000{suffix:02x}"


def _workload() -> WorkloadEnvelope:
    stage = StageEnvelope(
        stage_id=_resource("stage", 1),
        kind=StageKind.EVALUATION,
        operation="score",
        inputs=(),
        output_namespace="tenant/a",
        resolved_config_digest=DIGEST,
        reference_snapshot_digest=None,
        attempt=1,
        fencing_token=9,
        deadline_unix_millis=5_000,
    )
    return WorkloadEnvelope(
        workload_id=_resource("workload", 2),
        run_id=_resource("run", 3),
        job_id=_resource("job", 4),
        tenant_id=_resource("tenant", 5),
        workspace_id=_resource("workspace", 6),
        execution_ticket_id=_resource("ticket", 7),
        stage=stage,
        resource_class="cpu",
        created_unix_millis=1_000,
    )


class _Engine:
    def execute(self, stage: StageEnvelope, context: ExecutionContext) -> StageResult:
        context.checkpoint(operation=stage.operation)
        return StageResult((), {"completed": 1.0})


def test_worker_requires_readiness_and_drains_deterministically() -> None:
    worker = StageWorker(StageExecutor(StageKind.EVALUATION, _Engine(), now_millis=lambda: 1_000))
    with pytest.raises(MindcladeError) as caught:
        worker.execute(_workload())
    assert code_of(caught.value) is Code.FAILED_PRECONDITION

    worker.ready()
    assert worker.execute(_workload()).metrics == {"completed": 1.0}
    assert worker.drain_and_stop()
    assert worker.lifecycle.state is WorkerState.STOPPED


def test_worker_rejects_excess_concurrency_without_queueing() -> None:
    entered = Event()
    release = Event()

    class BlockingEngine:
        def execute(self, stage: StageEnvelope, context: ExecutionContext) -> StageResult:
            entered.set()
            release.wait(1)
            return StageResult(())

    worker = StageWorker(
        StageExecutor(StageKind.EVALUATION, BlockingEngine(), now_millis=lambda: 1_000),
        WorkerLimits(maximum_concurrent_executions=1),
    )
    worker.ready()
    thread = Thread(target=worker.execute, args=(_workload(),))
    thread.start()
    assert entered.wait(1)
    try:
        with pytest.raises(MindcladeError) as caught:
            worker.execute(_workload())
        assert code_of(caught.value) is Code.RESOURCE_EXHAUSTED
    finally:
        release.set()
        thread.join(1)


def test_lifecycle_cannot_stop_active_work() -> None:
    lifecycle = WorkerLifecycle()
    lifecycle.ready()
    lifecycle.begin()
    lifecycle.drain()
    assert not lifecycle.wait_drained(0)
    with pytest.raises(MindcladeError, match="active executions"):
        lifecycle.stop()
    lifecycle.finish()
    lifecycle.stop()


@pytest.mark.parametrize(
    "limits",
    [
        WorkerLimits(maximum_concurrent_executions=1, drain_timeout_millis=1),
        WorkerLimits(maximum_concurrent_executions=1024, drain_timeout_millis=300_000),
    ],
)
def test_limit_boundaries_are_accepted(limits: WorkerLimits) -> None:
    assert limits.maximum_concurrent_executions >= 1


@pytest.mark.parametrize(
    "kwargs",
    [
        {"maximum_concurrent_executions": 0},
        {"maximum_concurrent_executions": True},
        {"drain_timeout_millis": 0},
        {"drain_timeout_millis": 300_001},
    ],
)
def test_invalid_limits_fail_closed(kwargs: dict[str, object]) -> None:
    with pytest.raises(ValueError):
        WorkerLimits(**kwargs)  # type: ignore[arg-type]
