# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from threading import Event, Semaphore, Thread

import pytest

from libs.python.errors import Code, MindcladeError, code_of
from libs.python.worker_runtime import WorkerLimits, WorkerState
from services.workers.model_worker import ModelWorker, ModelWorkerConfig
from serving.contracts import (
    BatchPlan,
    CompatibilityKey,
    InferenceRequest,
    InferenceResult,
    InputDescriptor,
)

DIGEST = "sha256:" + "a" * 64
RESULT_DIGEST = "sha256:" + "b" * 64


class Planner:
    def plan(self, requests: tuple[InferenceRequest, ...]) -> tuple[BatchPlan, ...]:
        digest = requests[0].model_bundle_digest
        return (
            BatchPlan(
                CompatibilityKey(digest, "structure", "bf16", "small"),
                requests,
                1024,
                "small-bf16",
            ),
        )


class Engine:
    def execute(self, batch: BatchPlan) -> tuple[InferenceResult, ...]:
        return tuple(
            InferenceResult(
                request.request_id,
                RESULT_DIGEST,
                16,
                request.model_bundle_digest,
            )
            for request in batch.requests
        )


def request(request_id: str = "request-1") -> InferenceRequest:
    descriptor = InputDescriptor("segment", DIGEST, "/tmp/segment", 4, 1, 10_000)
    return InferenceRequest(
        request_id,
        DIGEST,
        b"key",
        (descriptor,),
        ("structure",),
        4,
        4,
        9_000,
    )


def test_model_worker_keeps_final_batching_in_python() -> None:
    worker = ModelWorker(ModelWorkerConfig(), Planner(), Engine())
    assert worker.lifecycle.state is WorkerState.READY
    results = worker.execute((request(),), now_unix_millis=1_000)
    assert [result.request_id for result in results] == ["request-1"]
    assert worker.drain_and_stop()
    assert worker.lifecycle.state is WorkerState.STOPPED


class GatedEngine(Engine):
    """Engine that reports how many callers the adapter let in at once."""

    def __init__(self) -> None:
        self.admitted = Semaphore(0)
        self.release = Event()

    def execute(self, batch: BatchPlan) -> tuple[InferenceResult, ...]:
        self.admitted.release()
        self.release.wait(5)
        return super().execute(batch)


def test_model_worker_rejects_excess_concurrency_without_queueing() -> None:
    # The declared bound is two, not the WorkerLimits default of one, so this fails both for a
    # worker with no bound at all and for one that quietly fell back to the default.
    engine = GatedEngine()
    worker = ModelWorker(
        ModelWorkerConfig(),
        Planner(),
        engine,
        limits=WorkerLimits(maximum_concurrent_executions=2),
    )
    threads = [
        Thread(
            target=worker.execute,
            args=((request(f"request-{index}"),),),
            kwargs={"now_unix_millis": 1_000},
        )
        for index in range(2)
    ]
    for thread in threads:
        thread.start()
    try:
        assert engine.admitted.acquire(timeout=5)
        assert engine.admitted.acquire(timeout=5)
        assert worker.lifecycle.active_executions == 2
        with pytest.raises(MindcladeError) as caught:
            worker.execute((request("request-3"),), now_unix_millis=1_000)
        assert code_of(caught.value) is Code.RESOURCE_EXHAUSTED
    finally:
        engine.release.set()
        for thread in threads:
            thread.join(5)


def test_model_worker_cannot_stop_while_an_execution_is_in_flight() -> None:
    entered = Event()
    release = Event()

    class BlockingEngine(Engine):
        def execute(self, batch: BatchPlan) -> tuple[InferenceResult, ...]:
            entered.set()
            release.wait(5)
            return super().execute(batch)

    worker = ModelWorker(ModelWorkerConfig(), Planner(), BlockingEngine())
    thread = Thread(
        target=worker.execute,
        args=((request(),),),
        kwargs={"now_unix_millis": 1_000},
    )
    thread.start()
    try:
        assert entered.wait(5)
        assert worker.lifecycle.active_executions == 1
        # A worker that let the supervisor observe STOPPED here would invite the same work to
        # be reissued elsewhere while this engine is still about to publish its results.
        worker.lifecycle.drain()
        with pytest.raises(MindcladeError, match="active executions") as caught:
            worker.lifecycle.stop()
        assert code_of(caught.value) is Code.FAILED_PRECONDITION
        assert not worker.lifecycle.wait_drained(0)
    finally:
        release.set()
        thread.join(5)
    assert worker.lifecycle.wait_drained(1_000)
    worker.lifecycle.stop()


def test_model_worker_refuses_work_once_draining() -> None:
    worker = ModelWorker(ModelWorkerConfig(), Planner(), Engine())
    worker.lifecycle.drain()
    with pytest.raises(MindcladeError) as caught:
        worker.execute((request(),), now_unix_millis=1_000)
    assert code_of(caught.value) is Code.FAILED_PRECONDITION


def test_model_worker_rejects_unadmitted_requests_before_the_engine_runs() -> None:
    executed: list[BatchPlan] = []

    class RecordingEngine(Engine):
        def execute(self, batch: BatchPlan) -> tuple[InferenceResult, ...]:
            executed.append(batch)
            return super().execute(batch)

    class SmugglingPlanner:
        def plan(self, requests: tuple[InferenceRequest, ...]) -> tuple[BatchPlan, ...]:
            key = CompatibilityKey(DIGEST, "structure", "bf16", "small")
            return (
                BatchPlan(key, (request("smuggled"),), 1024, "small-bf16"),
                BatchPlan(key, requests, 1024, "small-bf16"),
            )

    worker = ModelWorker(ModelWorkerConfig(), SmugglingPlanner(), RecordingEngine())
    with pytest.raises(MindcladeError) as caught:
        worker.execute((request(),), now_unix_millis=1_000)
    assert code_of(caught.value) is Code.FAILED_PRECONDITION
    assert executed == []


def test_model_worker_rejects_a_substituted_request_under_an_admitted_id() -> None:
    executed: list[BatchPlan] = []

    class RecordingEngine(Engine):
        def execute(self, batch: BatchPlan) -> tuple[InferenceResult, ...]:
            executed.append(batch)
            return super().execute(batch)

    class SwappingPlanner:
        def plan(self, requests: tuple[InferenceRequest, ...]) -> tuple[BatchPlan, ...]:
            # Same id, different payload: an expired lease pointing somewhere else. BatchPlan
            # validation checks ids and digests, so nothing downstream re-validates this.
            forged = InferenceRequest(
                requests[0].request_id,
                DIGEST,
                b"key",
                (InputDescriptor("segment", DIGEST, "/etc/shadow", 4, 1, 1),),
                ("structure",),
                4,
                4,
                9_000,
            )
            key = CompatibilityKey(DIGEST, "structure", "bf16", "small")
            return (BatchPlan(key, (forged,), 1024, "small-bf16"),)

    worker = ModelWorker(ModelWorkerConfig(), SwappingPlanner(), RecordingEngine())
    with pytest.raises(MindcladeError) as caught:
        worker.execute((request(),), now_unix_millis=1_000)
    assert code_of(caught.value) is Code.FAILED_PRECONDITION
    assert executed == []


def test_model_worker_rejects_cross_batch_result_attribution() -> None:
    class SplittingPlanner:
        def plan(self, requests: tuple[InferenceRequest, ...]) -> tuple[BatchPlan, ...]:
            key = CompatibilityKey(DIGEST, "structure", "bf16", "small")
            return tuple(BatchPlan(key, (single,), 1024, "small-bf16") for single in requests)

    class MisattributingEngine:
        def __init__(self) -> None:
            self._answers = ["request-2", "request-1"]

        def execute(self, batch: BatchPlan) -> tuple[InferenceResult, ...]:
            # Right set overall, wrong batch: each caller would receive an output computed
            # from the other request's inputs.
            return (InferenceResult(self._answers.pop(0), RESULT_DIGEST, 16, DIGEST),)

    worker = ModelWorker(ModelWorkerConfig(), SplittingPlanner(), MisattributingEngine())
    with pytest.raises(MindcladeError) as caught:
        worker.execute(
            (request("request-1"), request("request-2")),
            now_unix_millis=1_000,
        )
    assert code_of(caught.value) is Code.FAILED_PRECONDITION


def test_model_worker_translates_contract_rejections_into_the_shared_error_type() -> None:
    # serving.contracts validates with bare ValueError. An expired input lease is the most
    # ordinary rejection there is, and it must not cross the supervision boundary untyped.
    expired = InferenceRequest(
        "request-1",
        DIGEST,
        b"key",
        (InputDescriptor("segment", DIGEST, "/tmp/segment", 4, 1, 500),),
        ("structure",),
        4,
        4,
        9_000,
    )
    worker = ModelWorker(ModelWorkerConfig(), Planner(), Engine())
    with pytest.raises(MindcladeError) as caught:
        worker.execute((expired,), now_unix_millis=1_000)
    assert code_of(caught.value) is Code.INVALID_ARGUMENT
    assert isinstance(caught.value.__cause__, ValueError)


def test_model_worker_rejects_duplicate_request_ids() -> None:
    worker = ModelWorker(ModelWorkerConfig(), Planner(), Engine())
    with pytest.raises(MindcladeError) as caught:
        worker.execute((request(), request()), now_unix_millis=1_000)
    assert code_of(caught.value) is Code.INVALID_ARGUMENT


def test_model_worker_rejects_duplicate_engine_results() -> None:
    class DoublingEngine(Engine):
        def execute(self, batch: BatchPlan) -> tuple[InferenceResult, ...]:
            results = super().execute(batch)
            return results + results

    worker = ModelWorker(ModelWorkerConfig(), Planner(), DoublingEngine())
    with pytest.raises(MindcladeError) as caught:
        worker.execute((request(),), now_unix_millis=1_000)
    assert code_of(caught.value) is Code.FAILED_PRECONDITION


def test_model_worker_enforces_batch_and_gpu_budgets() -> None:
    class WideBatchPlanner:
        def plan(self, requests: tuple[InferenceRequest, ...]) -> tuple[BatchPlan, ...]:
            key = CompatibilityKey(DIGEST, "structure", "bf16", "small")
            return (BatchPlan(key, requests, 1024, "small-bf16"),)

    config = ModelWorkerConfig(maximum_pending_requests=4, maximum_batch_requests=1)
    worker = ModelWorker(config, WideBatchPlanner(), Engine())
    with pytest.raises(MindcladeError) as caught:
        worker.execute(
            (request("request-1"), request("request-2")),
            now_unix_millis=1_000,
        )
    assert code_of(caught.value) is Code.RESOURCE_EXHAUSTED

    budget = ModelWorkerConfig(maximum_gpu_bytes_per_batch=512)
    budgeted = ModelWorker(budget, Planner(), Engine())
    with pytest.raises(MindcladeError) as gpu_caught:
        budgeted.execute((request(),), now_unix_millis=1_000)
    assert code_of(gpu_caught.value) is Code.RESOURCE_EXHAUSTED


def test_model_worker_rejects_out_of_bounds_request_counts() -> None:
    config = ModelWorkerConfig(maximum_pending_requests=1, maximum_batch_requests=1)
    worker = ModelWorker(config, Planner(), Engine())
    with pytest.raises(MindcladeError) as caught:
        worker.execute((), now_unix_millis=1_000)
    assert code_of(caught.value) is Code.INVALID_ARGUMENT
    with pytest.raises(MindcladeError) as excess:
        worker.execute(
            (request("request-1"), request("request-2")),
            now_unix_millis=1_000,
        )
    assert code_of(excess.value) is Code.INVALID_ARGUMENT
