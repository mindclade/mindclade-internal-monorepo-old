# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Train-to-serving parity for the concrete reference-affine contract."""

from __future__ import annotations

import hashlib
import time
from dataclasses import replace
from pathlib import Path
from threading import Event

import numpy as np
import pytest
import torch

from models.reference import (
    REFERENCE_AFFINE_MODEL_NAME,
    REFERENCE_AFFINE_OPERATION,
    ReferenceAffine,
    ReferenceAffineConfig,
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


def _digest(payload: bytes) -> str:
    return "sha256:" + hashlib.sha256(payload).hexdigest()


def test_safetensors_bundle_matches_verified_serving_runtime(tmp_path: Path) -> None:
    model = ReferenceAffine(ReferenceAffineConfig(scale=3.0, bias=-1.0, maximum_input_elements=3))

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
    engine_config = ReferenceEngineConfig(
        model_bundle_root=bundle.resolve(),
        expected_bundle_digest=str(manifest["digest"]),
        output_root=output_root.resolve(),
        allowed_input_roots=(input_root.resolve(),),
        device="cpu",
        chunk_elements=2,
        iterations=1,
    )
    engine = ReferenceEngine(engine_config)
    with pytest.raises(ValueError, match="exactly one iteration"):
        ReferenceEngine(replace(engine_config, iterations=2))
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

    too_large = torch.ones((4,), dtype=torch.float32)
    oversized_payload = too_large.numpy().tobytes()
    oversized_path = (input_root / "oversized.f32").resolve()
    oversized_path.write_bytes(oversized_payload)
    oversized_request = ReferenceRequest(
        request_id="training-serving-bound-parity",
        model_bundle_digest=str(manifest["digest"]),
        operation=REFERENCE_AFFINE_OPERATION,
        deadline_unix_millis=deadline,
        maximum_input_bytes=len(oversized_payload),
        maximum_output_bytes=len(oversized_payload),
        input=ReferenceInput(
            segment_id="oversized-input",
            generation=1,
            path=oversized_path,
            offset_bytes=0,
            length_bytes=len(oversized_payload),
            element_type="f32",
            shape=tuple(too_large.shape),
            content_digest=_digest(oversized_payload),
            lease_expires_unix_millis=deadline,
        ),
    )
    with pytest.raises(ValueError, match="bundle input budget"):
        engine.execute(oversized_request, Event())

    overflowing = torch.tensor([torch.finfo(torch.float32).max], dtype=torch.float32)
    overflowing_payload = overflowing.numpy().tobytes()
    overflowing_path = (input_root / "overflowing.f32").resolve()
    overflowing_path.write_bytes(overflowing_payload)
    overflowing_request = ReferenceRequest(
        request_id="training-serving-overflow-parity",
        model_bundle_digest=str(manifest["digest"]),
        operation=REFERENCE_AFFINE_OPERATION,
        deadline_unix_millis=deadline,
        maximum_input_bytes=len(overflowing_payload),
        maximum_output_bytes=len(overflowing_payload),
        input=ReferenceInput(
            segment_id="overflowing-input",
            generation=1,
            path=overflowing_path,
            offset_bytes=0,
            length_bytes=len(overflowing_payload),
            element_type="f32",
            shape=tuple(overflowing.shape),
            content_digest=_digest(overflowing_payload),
            lease_expires_unix_millis=deadline,
        ),
    )
    with pytest.raises(FloatingPointError, match="non-finite output"):
        engine.execute(overflowing_request, Event())
