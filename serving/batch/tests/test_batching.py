# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

import json

import pytest

from libs.python.worker_runtime import CancellationToken
from serving.batch import (
    BatchExecutor,
    BatchJob,
    BatchLimits,
    BatchWorker,
    build_manifest,
    partition,
)
from serving.contracts import InferenceRequest, InferenceResult, InputDescriptor

DIGEST = "sha256:" + "a" * 64
OUTPUT = "sha256:" + "b" * 64


def request(identifier: str, *, units: int = 4) -> InferenceRequest:
    descriptor = InputDescriptor("segment", DIGEST, "/buffers/input", 4, 1, 20_000)
    return InferenceRequest(
        identifier, DIGEST, identifier.encode(), (descriptor,), (), units, units, 10_000
    )


def job(*requests: InferenceRequest, identifier: str = "job-1", deadline: int = 10_000) -> BatchJob:
    return BatchJob(identifier, DIGEST, tuple(requests), 1, 7, deadline)


class Engine:
    def execute(self, batch, cancellation: CancellationToken):
        assert not cancellation.is_cancelled
        return tuple(
            InferenceResult(item.request_id, OUTPUT, 16, item.model_bundle_digest)
            for item in batch.requests
        )


def test_partition_is_stable_and_obeys_all_ceilings() -> None:
    limits = BatchLimits(
        maximum_requests_per_job=4,
        maximum_requests_per_batch=2,
        maximum_units_per_batch=16,
        maximum_estimated_bytes_per_batch=20,
    )
    value = job(request("a"), request("b"), request("c"))
    batches = partition(value, limits, estimate_bytes=lambda _: 10)
    assert [[item.request_id for item in batch.requests] for batch in batches] == [
        ["a", "b"],
        ["c"],
    ]
    assert [batch.ordinal for batch in batches] == [0, 1]


def test_partition_rejects_one_oversize_request() -> None:
    value = job(request("oversize", units=9))
    with pytest.raises(ValueError, match="unit ceiling"):
        partition(value, BatchLimits(maximum_units_per_batch=16), estimate_bytes=lambda _: 1)


def test_executor_requires_exact_order_and_cardinality() -> None:
    class BrokenEngine:
        def execute(self, batch, cancellation):
            return ()

    value = job(request("a"))
    executor = BatchExecutor(BatchLimits(), BrokenEngine(), estimate_bytes=lambda _: 1)
    with pytest.raises(RuntimeError, match="order/cardinality"):
        executor.execute(value, now_unix_millis=1_000, cancellation=CancellationToken())


def test_executor_rejects_result_from_another_model_bundle() -> None:
    class BrokenEngine:
        def execute(self, batch, cancellation):
            return (InferenceResult("a", OUTPUT, 16, "sha256:" + "c" * 64),)

    value = job(request("a"))
    executor = BatchExecutor(BatchLimits(), BrokenEngine(), estimate_bytes=lambda _: 1)
    with pytest.raises(RuntimeError, match="another model bundle"):
        executor.execute(value, now_unix_millis=1_000, cancellation=CancellationToken())


def test_worker_executes_and_builds_canonical_lineage_manifest() -> None:
    limits = BatchLimits(maximum_requests_per_batch=1)
    worker = BatchWorker(limits, BatchExecutor(limits, Engine(), estimate_bytes=lambda _: 1))
    worker.ready()
    value = job(request("a"), request("b"))
    worker.submit(value, now_unix_millis=1_000)
    result = worker.run_next(now_unix_millis=1_000)
    assert result is not None
    assert result.batch_count == 2
    manifest = build_manifest(value, result)
    document = json.loads(manifest.document)
    assert document["fencing_token"] == 7
    assert [item["request_id"] for item in document["outputs"]] == ["a", "b"]
    assert manifest.digest.startswith("sha256:")
    assert worker.telemetry.snapshot().completed == 1


def test_worker_can_cancel_a_queued_job() -> None:
    limits = BatchLimits()
    worker = BatchWorker(limits, BatchExecutor(limits, Engine(), estimate_bytes=lambda _: 1))
    worker.ready()
    worker.submit(job(request("a")), now_unix_millis=1_000)
    assert worker.cancel("job-1")
    with pytest.raises(RuntimeError, match="canceled"):
        worker.run_next(now_unix_millis=1_000)
    assert not worker.cancel("job-1")
    assert worker.telemetry.snapshot().canceled == 1


def test_job_validation_rejects_duplicate_requests_and_stale_deadline() -> None:
    duplicate = job(request("a"), request("a"))
    with pytest.raises(ValueError, match="duplicate"):
        duplicate.validate(BatchLimits(), now_unix_millis=1_000)
    with pytest.raises(ValueError, match="expired"):
        job(request("a"), deadline=1_000).validate(BatchLimits(), now_unix_millis=1_000)
