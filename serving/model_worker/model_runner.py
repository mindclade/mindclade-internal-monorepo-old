"""Process-local model worker with explicit lifecycle and bounded batching."""
from __future__ import annotations

from collections.abc import Callable
from threading import Lock
from typing import Protocol

from .batching.planner import BatchPlanner
from .batching.tensor_batch import TensorBatch
from .config import WorkerLimits
from .model_loader import ModelRegistry
from .protocol import ModelRequest, ModelResponse, WorkerPhase


class ModelEngine(Protocol):
    def execute(self, model: object, batch: TensorBatch) -> tuple[ModelResponse, ...]: ...


class ModelWorker:
    def __init__(
        self,
        limits: WorkerLimits,
        registry: ModelRegistry,
        engine: ModelEngine,
        *,
        cancellation_probe: Callable[[str], bool] | None = None,
    ) -> None:
        limits.validate()
        self._limits = limits
        self._registry = registry
        self._engine = engine
        self._planner = BatchPlanner(limits)
        self._cancellation_probe = cancellation_probe or (lambda _request_id: False)
        self._phase = WorkerPhase.STARTING
        self._lock = Lock()
        self._active: set[str] = set()

    @property
    def phase(self) -> WorkerPhase:
        with self._lock:
            return self._phase

    def ready(self) -> None:
        with self._lock:
            if self._phase is not WorkerPhase.STARTING:
                raise RuntimeError("worker can become ready only from starting")
            self._phase = WorkerPhase.READY

    def execute(self, requests: tuple[ModelRequest, ...]) -> tuple[ModelResponse, ...]:
        with self._lock:
            if self._phase is not WorkerPhase.READY:
                raise RuntimeError("model worker is not accepting new work")
            ids = {request.request_id for request in requests}
            if len(ids) != len(requests) or ids & self._active:
                raise ValueError("request ids must be unique and not already active")
            if len(self._active) + len(ids) > self._limits.maximum_active_requests:
                raise RuntimeError("model worker active-request limit reached")
            self._active.update(ids)
        try:
            batches = self._planner.plan(requests)
            responses: list[ModelResponse] = []
            for batch in batches:
                if any(self._cancellation_probe(request.request_id) for request in batch.requests):
                    raise RuntimeError("model batch contains a cancelled request")
                model = self._registry.get_or_load(batch.key.model_bundle_digest)
                batch_responses = self._engine.execute(model.implementation, batch)
                expected = {request.request_id for request in batch.requests}
                actual = {response.request_id for response in batch_responses}
                if actual != expected or len(actual) != len(batch_responses):
                    raise RuntimeError("model engine response set does not match batch request set")
                for response in batch_responses:
                    response.validate()
                responses.extend(batch_responses)
            return tuple(responses)
        finally:
            with self._lock:
                self._active.difference_update(request.request_id for request in requests)

    def drain(self) -> None:
        with self._lock:
            if self._phase is WorkerPhase.STOPPED:
                return
            self._phase = WorkerPhase.DRAINING

    def stop(self) -> None:
        with self._lock:
            if self._active:
                raise RuntimeError("cannot stop model worker while requests are active")
            self._phase = WorkerPhase.STOPPED
