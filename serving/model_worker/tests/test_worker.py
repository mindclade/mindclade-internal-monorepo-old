# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

import pytest

from serving.model_worker import (
    LoadedModel,
    ModelRegistry,
    ModelRequest,
    ModelResponse,
    ModelWorker,
    WorkerLimits,
    WorkerPhase,
)

D = "sha256:" + "2" * 64


class Loader:
    def load(self, bundle_digest: str) -> LoadedModel:
        return LoadedModel(bundle_digest, {"digest": bundle_digest})


class Engine:
    def execute(self, model: object, batch):
        assert model == {"digest": D}
        return tuple(ModelResponse(request.request_id, payload=b"ok") for request in batch.requests)


def request(request_id: str) -> ModelRequest:
    return ModelRequest(request_id, "deployment_a", D, "bf16", "gpu", 1, 1, "segment:a")


def test_worker_lifecycle_and_engine_response_contract() -> None:
    worker = ModelWorker(WorkerLimits(), ModelRegistry(Loader()), Engine())
    worker.ready()
    assert worker.execute((request("r1"), request("r2"))) == (
        ModelResponse("r1", payload=b"ok"),
        ModelResponse("r2", payload=b"ok"),
    )
    worker.drain()
    assert worker.phase is WorkerPhase.DRAINING
    with pytest.raises(RuntimeError):
        worker.execute((request("r3"),))
    worker.stop()
    assert worker.phase is WorkerPhase.STOPPED
