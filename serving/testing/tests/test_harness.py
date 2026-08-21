# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

import pytest

from libs.python.worker_runtime import CancellationToken
from serving.batch import BatchSlice
from serving.contracts import InferenceResult
from serving.testing import (
    FakeGateway,
    FakeModel,
    assert_golden,
    canonical_json,
    inference_request,
    run_load,
)


def test_fake_model_is_deterministic_and_preserves_order() -> None:
    requests = (inference_request("a"), inference_request("b"))
    batch = BatchSlice(0, requests, 16, 32)
    model = FakeModel()
    first = model.execute(batch, CancellationToken())
    second = model.execute(batch, CancellationToken())
    assert first == second
    assert [item.request_id for item in first] == ["a", "b"]
    assert len(model.calls) == 2


def test_gateway_bounds_history_and_validates_handler_identity() -> None:
    request = inference_request()

    def handler(value):
        return InferenceResult(value.request_id, "sha256:" + "c" * 64, 4, value.model_bundle_digest)

    gateway = FakeGateway(handler, maximum_calls=1)
    assert gateway.infer(request, now_unix_millis=1_000).request_id == request.request_id
    with pytest.raises(RuntimeError, match="history is full"):
        gateway.infer(request, now_unix_millis=1_000)


def test_goldens_are_canonical_and_never_rewritten_implicitly() -> None:
    expected = b'{"a":1,"b":2}\n'
    assert canonical_json({"b": 2, "a": 1}) == expected
    assert_golden({"a": 1, "b": 2}, expected)
    with pytest.raises(AssertionError, match="mismatch"):
        assert_golden({"a": 2}, expected)


def test_load_harness_has_exact_accounting_and_bounded_percentiles() -> None:
    result = run_load(
        lambda index: (_ for _ in ()).throw(ValueError()) if index == 3 else index,
        operations=10,
        concurrency=2,
    )
    assert result.operations == result.succeeded + result.failed == 10
    assert result.failed == 1
    assert result.percentile(99) >= 0


def test_load_harness_rejects_unbounded_concurrency() -> None:
    with pytest.raises(ValueError, match="concurrency"):
        run_load(lambda _: None, operations=1, concurrency=2)
