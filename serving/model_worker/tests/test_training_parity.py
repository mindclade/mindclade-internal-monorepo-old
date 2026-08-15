"""Parity contract placeholder: model-family parity is qualified by model adapters."""

from serving.model_worker import WorkerLimits


def test_worker_limits_are_model_neutral() -> None:
    limits = WorkerLimits()
    limits.validate()
    assert limits.maximum_batch_size <= limits.maximum_active_requests
