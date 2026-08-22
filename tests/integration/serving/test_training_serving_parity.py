# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""End-to-end train-to-serving parity for the concrete reference-affine contract."""

from __future__ import annotations

import hashlib
import time
from pathlib import Path
from threading import Event

import numpy as np
import torch

from models.reference import (
    REFERENCE_AFFINE_MODEL_NAME,
    REFERENCE_AFFINE_OPERATION,
    ReferenceAffine,
    reference_affine_config_bytes,
    save_reference_affine,
)
from serving.model_worker.reference import (
    ReferenceEngine,
    ReferenceEngineConfig,
    ReferenceInput,
    ReferenceRequest,
)
from tools.release.build_model_bundle import build
from training.contracts import SupervisedBatch
from training.core import Trainer
from training.optim import SGDConfig, build_optimizer
from training.tasks import SupervisedMSETask


def _digest(payload: bytes) -> str:
    return "sha256:" + hashlib.sha256(payload).hexdigest()


def test_trained_safetensors_bundle_matches_verified_serving_runtime(tmp_path: Path) -> None:
    torch.manual_seed(7)
    model = ReferenceAffine()
    optimizer = build_optimizer(model.parameters(), SGDConfig(learning_rate=0.05))
    trainer = Trainer(model, SupervisedMSETask(), optimizer)
    inputs = torch.tensor([[-2.0], [-1.0], [0.0], [1.0], [2.0]], dtype=torch.float32)
    targets = (inputs * 3.0) - 1.0
    batch = SupervisedBatch(inputs, targets)
    for _ in range(20):
        trainer.train((batch,))

    checkpoint = tmp_path / "checkpoint"
    checkpoint.mkdir()
    save_reference_affine(model, checkpoint / "model.safetensors")
    (checkpoint / "config.json").write_bytes(reference_affine_config_bytes(model.config))
    bundle = tmp_path / "bundle"
    manifest = build(checkpoint, bundle, REFERENCE_AFFINE_MODEL_NAME, schema_version=1)

    input_root = tmp_path / "inputs"
    output_root = tmp_path / "outputs"
    input_root.mkdir()
    output_root.mkdir()
    parity_input = torch.tensor([[-3.0, 0.25, 4.0]], dtype=torch.float32)
    payload = parity_input.numpy().tobytes()
    input_path = (input_root / "input.f32").resolve()
    input_path.write_bytes(payload)
    deadline = int(time.time() * 1000) + 60_000
    engine = ReferenceEngine(
        ReferenceEngineConfig(
            model_bundle_root=bundle.resolve(),
            expected_bundle_digest=str(manifest["digest"]),
            output_root=output_root.resolve(),
            allowed_input_roots=(input_root.resolve(),),
            device="cpu",
            chunk_elements=2,
            iterations=1,
        )
    )
    request = ReferenceRequest(
        request_id="training-serving-parity",
        model_bundle_digest=str(manifest["digest"]),
        operation=REFERENCE_AFFINE_OPERATION,
        deadline_unix_millis=deadline,
        maximum_input_bytes=len(payload),
        maximum_output_bytes=len(payload),
        input=ReferenceInput(
            segment_id="parity-input",
            generation=1,
            path=input_path,
            offset_bytes=0,
            length_bytes=len(payload),
            element_type="f32",
            shape=tuple(parity_input.shape),
            content_digest=_digest(payload),
            lease_expires_unix_millis=deadline,
        ),
    )

    output = engine.execute(request, Event())
    served = torch.from_numpy(np.frombuffer(output.path.read_bytes(), dtype=np.float32).copy())
    served = served.reshape(parity_input.shape)
    with torch.inference_mode():
        eager = model(parity_input)

    torch.testing.assert_close(served, eager, rtol=0.0, atol=0.0)
    assert output.content_digest == _digest(output.path.read_bytes())
