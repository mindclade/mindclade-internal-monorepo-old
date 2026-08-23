# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from threading import Event, Semaphore, Thread

import pytest

from libs.python.errors import Code, MindcladeError, code_of
from libs.python.worker_runtime import (
    ExecutionContext,
    StageEnvelope,
    StageKind,
    StageResult,
    WorkerState,
    WorkloadEnvelope,
)
from services.workers.curation import build_worker, worker_limits

DIGEST = "sha256:" + "1" * 64
# No Python stage worker claims this kind, so it is a stable stand-in for a workload that the
# control plane routed to the wrong adapter.
FOREIGN_KIND = StageKind.ARTIFACT_TRANSFER


def resource(kind: str, suffix: int) -> str:
    return f"{kind}_019c00000000700080000000000000{suffix:02x}"


class Engine:
    def execute(self, stage: StageEnvelope, context: ExecutionContext) -> StageResult:
        context.checkpoint(operation=stage.operation)
        return StageResult((), {"completed": 1.0})


class GatedEngine:
    """Engine that parks inside execute and reports how many callers were let in at once."""

    def __init__(self) -> None:
        self.admitted = Semaphore(0)
        self.release = Event()

    def execute(self, stage: StageEnvelope, context: ExecutionContext) -> StageResult:
        self.admitted.release()
        self.release.wait(5)
        return StageResult(())


def workload(kind: StageKind = StageKind.CURATE) -> WorkloadEnvelope:
    stage = StageEnvelope(
        stage_id=resource("stage", 1),
        kind=kind,
        operation="execute",
        inputs=(),
        output_namespace="tenant/a",
        resolved_config_digest=DIGEST,
        reference_snapshot_digest=None,
        attempt=1,
        fencing_token=7,
        deadline_unix_millis=9_000_000_000_000,
    )
    return WorkloadEnvelope(
        workload_id=resource("workload", 2),
        run_id=resource("run", 3),
        job_id=resource("job", 4),
        tenant_id=resource("tenant", 5),
        workspace_id=resource("workspace", 6),
        execution_ticket_id=resource("ticket", 7),
        stage=stage,
        resource_class="cpu",
        created_unix_millis=1_000,
    )


def test_curation_adapter_composes_shared_bounded_runtime() -> None:
    worker = build_worker(
        Engine(),
        worker_limits(maximum_concurrent_executions=2, drain_timeout_millis=1_000),
    )
    assert worker.lifecycle.state is WorkerState.READY
    assert worker.execute(workload()).metrics == {"completed": 1.0}
    assert worker.drain_and_stop()
    assert worker.lifecycle.state is WorkerState.STOPPED


def test_curation_adapter_binds_the_limits_it_is_given() -> None:
    # The declared bound is two, deliberately not the WorkerLimits default of one. A
    # build_worker that dropped its limits argument would fall back to that default and shed
    # the *second* caller, so the two admissions below are what distinguishes limits that were
    # plumbed through from limits that were merely constructed.
    engine = GatedEngine()
    worker = build_worker(
        engine,
        worker_limits(maximum_concurrent_executions=2, drain_timeout_millis=1_000),
    )
    threads = [Thread(target=worker.execute, args=(workload(),)) for _ in range(2)]
    for thread in threads:
        thread.start()
    try:
        assert engine.admitted.acquire(timeout=5)
        assert engine.admitted.acquire(timeout=5)
        assert worker.lifecycle.active_executions == 2
        with pytest.raises(MindcladeError) as caught:
            worker.execute(workload())
        assert code_of(caught.value) is Code.RESOURCE_EXHAUSTED
    finally:
        engine.release.set()
        for thread in threads:
            thread.join(5)


def test_curation_adapter_refuses_a_foreign_stage_kind() -> None:
    # These adapters are near-identical copies, so a build_executor that names the wrong
    # StageKind is the plausible regression. Only a workload of another kind catches it.
    worker = build_worker(Engine())
    with pytest.raises(MindcladeError) as caught:
        worker.execute(workload(FOREIGN_KIND))
    assert code_of(caught.value) is Code.FAILED_PRECONDITION


def test_curation_adapter_refuses_work_once_draining() -> None:
    worker = build_worker(Engine())
    worker.lifecycle.drain()
    with pytest.raises(MindcladeError) as caught:
        worker.execute(workload())
    assert code_of(caught.value) is Code.FAILED_PRECONDITION
