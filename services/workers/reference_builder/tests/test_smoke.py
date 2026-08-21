# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from libs.python.worker_runtime import (
    ExecutionContext,
    StageEnvelope,
    StageKind,
    StageResult,
    WorkerState,
    WorkloadEnvelope,
)
from services.workers.reference_builder import build_worker, worker_limits

DIGEST = "sha256:" + "1" * 64


def resource(kind: str, suffix: int) -> str:
    return f"{kind}_019c00000000700080000000000000{suffix:02x}"


class Engine:
    def execute(self, stage: StageEnvelope, context: ExecutionContext) -> StageResult:
        context.checkpoint(operation=stage.operation)
        return StageResult((), {"completed": 1.0})


def workload() -> WorkloadEnvelope:
    stage = StageEnvelope(
        stage_id=resource("stage", 1),
        kind=StageKind.REFERENCE_BUILD,
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


def test_reference_builder_adapter_composes_shared_bounded_runtime() -> None:
    worker = build_worker(
        Engine(),
        worker_limits(maximum_concurrent_executions=2, drain_timeout_millis=1_000),
    )
    assert worker.lifecycle.state is WorkerState.READY
    assert worker.execute(workload()).metrics == {"completed": 1.0}
    assert worker.drain_and_stop()
    assert worker.lifecycle.state is WorkerState.STOPPED
