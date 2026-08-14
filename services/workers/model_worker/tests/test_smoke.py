from serving.contracts import (
    BatchPlan,
    CompatibilityKey,
    InferenceRequest,
    InferenceResult,
    InputDescriptor,
)
from services.workers.model_worker import ModelWorker, ModelWorkerConfig


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
                "sha256:" + "b" * 64,
                16,
                request.model_bundle_digest,
            )
            for request in batch.requests
        )


def test_model_worker_keeps_final_batching_in_python() -> None:
    digest = "sha256:" + "a" * 64
    descriptor = InputDescriptor("segment", digest, "/tmp/segment", 4, 1, 10_000)
    request = InferenceRequest(
        "request-1",
        digest,
        b"key",
        (descriptor,),
        ("structure",),
        4,
        4,
        9_000,
    )
    worker = ModelWorker(ModelWorkerConfig(), Planner(), Engine())
    results = worker.execute((request,), now_unix_millis=1_000)
    assert [result.request_id for result in results] == ["request-1"]
    worker.lifecycle.drain()
