# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

import pytest

from serving.model_worker.batching import BatchReservationLedger, ContinuousBatchQueue
from serving.model_worker.biology import BiologyDimensions
from serving.model_worker.compilation import CompilationKey
from serving.model_worker.diffusion import DiffusionSchedule
from serving.model_worker.generation import GenerationResult, StopReason
from serving.model_worker.kv_cache import KVCache
from serving.model_worker.memory import MemoryLedger
from serving.model_worker.multimodal import ModalityInput, validate_modalities
from serving.model_worker.precision import Precision, PrecisionPolicy
from serving.model_worker.protocol import ModelRequest
from serving.model_worker.sampling import SamplingParameters
from serving.model_worker.shape_buckets import ShapeBucket, ShapeBucketSelector
from serving.model_worker.telemetry import Telemetry
from serving.model_worker.warmup import WarmupCase, WarmupPlan

DIGEST = "sha256:" + "a" * 64


def request(identifier: str = "request-1") -> ModelRequest:
    return ModelRequest(identifier, "deployment", DIGEST, "bf16", "generation", 4, 4, "/buffer")


def test_precision_never_downgrades_when_exact_is_required() -> None:
    policy = PrecisionPolicy((Precision.BF16, Precision.FP32))
    assert policy.select(Precision.BF16, (Precision.BF16,)) is Precision.BF16
    with pytest.raises(ValueError, match="unavailable"):
        policy.select(Precision.FP16, (Precision.FP32,))


def test_shape_buckets_choose_smallest_fit() -> None:
    selector = ShapeBucketSelector((ShapeBucket(8, "small"), ShapeBucket(16, "large")))
    assert selector.select(9).name == "large"
    with pytest.raises(ValueError, match="every"):
        selector.select(17)


def test_memory_and_batch_reservations_are_exact_and_bounded() -> None:
    memory = MemoryLedger(10)
    assert memory.reserve(8)
    assert not memory.reserve(3)
    memory.release(8)
    assert memory.snapshot().reserved_bytes == 0
    batches = BatchReservationLedger(2, 10)
    assert batches.reserve(1, 8)
    assert not batches.reserve(2, 1)
    batches.release(1, 8)


def test_kv_cache_evicts_by_bytes_and_recency() -> None:
    cache = KVCache(10)
    assert cache.put("a", object(), 6) == ()
    assert cache.get("a") is not None
    assert cache.put("b", object(), 6) == ("a",)


def test_continuous_queue_is_bounded_and_deduplicated() -> None:
    queue = ContinuousBatchQueue(1)
    queue.put(request())
    with pytest.raises(ValueError, match="already"):
        queue.put(request())
    assert queue.take(1)[0].request_id == "request-1"


def test_statistical_contracts_record_seeds_and_validate_schedules() -> None:
    assert SamplingParameters(7).seed == 7
    assert DiffusionSchedule((1.0, 0.5, 0.0), 7).seed == 7
    with pytest.raises(ValueError, match="descending"):
        DiffusionSchedule((0.0, 1.0), 7)


def test_generation_contract_bounds_token_ids() -> None:
    result = GenerationResult((1, 2), StopReason.END_TOKEN, 7)
    result.validate(maximum_tokens=2)
    with pytest.raises(ValueError, match="exceeds"):
        result.validate(maximum_tokens=1)


def test_biology_multimodal_and_compilation_contracts_are_location_free() -> None:
    assert BiologyDimensions(10, 100).atoms == 100
    inputs = (ModalityInput("sequence", DIGEST, 10),)
    validate_modalities(inputs)
    key = CompilationKey(DIGEST, "sha256:" + "b" * 64, "h100", "bf16", "small")
    assert key.hardware_class == "h100"


def test_warmup_and_telemetry_are_bounded() -> None:
    assert WarmupPlan((WarmupCase("small", 2),)).cases[0].repetitions == 2
    telemetry = Telemetry()
    telemetry.increment("admitted")
    telemetry.increment("completed")
    assert telemetry.snapshot().completed == 1
