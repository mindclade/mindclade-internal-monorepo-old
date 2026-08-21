# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Exact-resume and fail-closed tests for the local reference checkpoint."""

from __future__ import annotations

from dataclasses import replace
from pathlib import Path

import pytest
import torch

from libs.python.errors import FailedPrecondition, InvalidArgument
from libs.python.identifiers import Digest, IdGenerator, ResourceId
from models.reference import ReferenceAffine
from training.checkpointing import (
    CheckpointIdentity,
    CheckpointManifest,
    restore_local_checkpoint,
    save_local_checkpoint,
)
from training.checkpointing.serialization import encode_training_state
from training.contracts import SupervisedBatch, TrainingState
from training.core import Trainer
from training.tasks import SupervisedMSETask


def _identity() -> CheckpointIdentity:
    generator = IdGenerator()
    stamp = 1_700_000_000_000
    return CheckpointIdentity(
        checkpoint_id=ResourceId("checkpoint", generator.raw_at(stamp)).text,
        run_id=ResourceId("run", generator.raw_at(stamp + 1)).text,
        resolved_config_digest=Digest.of_text("resolved config").text,
        dataset_digest=Digest.of_text("dataset snapshot").text,
        model_digest=Digest.of_text("reference-affine-v1").text,
        code_digest=Digest.of_text("source tree").text,
        toolchain_digest=Digest.of_text("pinned toolchain").text,
        topology_digest=Digest.of_text("cpu-world-size-1").text,
    )


def _optimizer(model: ReferenceAffine) -> torch.optim.AdamW:
    return torch.optim.AdamW(
        model.parameters(),
        lr=0.05,
        weight_decay=0.01,
        foreach=False,
    )


def _step(model: ReferenceAffine, optimizer: torch.optim.Optimizer) -> None:
    inputs = torch.tensor([[-2.0], [-0.5], [1.0], [3.0]], dtype=torch.float32)
    targets = (inputs * 3.0) - 1.0
    optimizer.zero_grad(set_to_none=True)
    loss = torch.nn.functional.mse_loss(model(inputs), targets)
    torch.autograd.backward(loss)
    optimizer.step()


def _trainer(model: ReferenceAffine, optimizer: torch.optim.Optimizer) -> Trainer:
    return Trainer(model, SupervisedMSETask(), optimizer)


def _train_steps(trainer: Trainer, steps: int) -> None:
    inputs = torch.tensor([[-2.0], [-0.5], [1.0], [3.0]], dtype=torch.float32)
    targets = (inputs * 3.0) - 1.0
    trainer.train((SupervisedBatch(inputs, targets),) * steps)


def _assert_tree_equal(actual: object, expected: object) -> None:
    if isinstance(expected, torch.Tensor):
        assert isinstance(actual, torch.Tensor)
        torch.testing.assert_close(actual, expected, rtol=0.0, atol=0.0)
        return
    if isinstance(expected, dict):
        assert isinstance(actual, dict)
        assert actual.keys() == expected.keys()
        for key in expected:
            _assert_tree_equal(actual[key], expected[key])
        return
    if isinstance(expected, (list, tuple)):
        assert isinstance(actual, type(expected))
        assert len(actual) == len(expected)
        for actual_item, expected_item in zip(actual, expected, strict=True):
            _assert_tree_equal(actual_item, expected_item)
        return
    assert actual == expected


def test_interrupted_resume_matches_uninterrupted_training_exactly(tmp_path: Path) -> None:
    uninterrupted = ReferenceAffine()
    uninterrupted_optimizer = _optimizer(uninterrupted)
    uninterrupted_trainer = _trainer(uninterrupted, uninterrupted_optimizer)
    _train_steps(uninterrupted_trainer, 4)

    interrupted = ReferenceAffine()
    interrupted_optimizer = _optimizer(interrupted)
    interrupted_trainer = _trainer(interrupted, interrupted_optimizer)
    _train_steps(interrupted_trainer, 2)

    torch.manual_seed(8675309)
    expected_rng = torch.get_rng_state().clone()
    identity = _identity()
    training_state = interrupted_trainer.state
    destination = tmp_path / "checkpoint"
    manifest = save_local_checkpoint(
        destination,
        model=interrupted,
        optimizer=interrupted_optimizer,
        training_state=training_state,
        identity=identity,
        data_position=2,
    )
    assert manifest == CheckpointManifest.decode((destination / "manifest.json").read_bytes())

    torch.manual_seed(1)
    resumed = ReferenceAffine()
    resumed_optimizer = _optimizer(resumed)
    result = restore_local_checkpoint(
        destination,
        model=resumed,
        optimizer=resumed_optimizer,
        expected_identity=identity,
    )

    assert result.training_state == training_state
    assert result.data_position == 2
    torch.testing.assert_close(torch.get_rng_state(), expected_rng, rtol=0.0, atol=0.0)
    _assert_tree_equal(resumed.state_dict(), interrupted.state_dict())
    _assert_tree_equal(resumed_optimizer.state_dict(), interrupted_optimizer.state_dict())

    resumed_trainer = Trainer(
        resumed,
        SupervisedMSETask(),
        resumed_optimizer,
        state=result.training_state,
    )
    _train_steps(resumed_trainer, 2)
    assert resumed_trainer.state == uninterrupted_trainer.state
    _assert_tree_equal(resumed.state_dict(), uninterrupted.state_dict())
    _assert_tree_equal(resumed_optimizer.state_dict(), uninterrupted_optimizer.state_dict())


def test_manifest_is_canonical_and_artifacts_are_immutable(tmp_path: Path) -> None:
    model = ReferenceAffine()
    optimizer = _optimizer(model)
    _step(model, optimizer)
    destination = tmp_path / "checkpoint"
    manifest = save_local_checkpoint(
        destination,
        model=model,
        optimizer=optimizer,
        training_state=TrainingState(microbatches=1, optimizer_steps=1, samples=4),
        identity=_identity(),
        data_position=1,
    )

    encoded = manifest.encode()
    assert encoded == (destination / "manifest.json").read_bytes()
    assert CheckpointManifest.decode(encoded).encode() == encoded
    with pytest.raises(TypeError):
        manifest.artifacts["extra"] = manifest.artifacts["state.json"]  # type: ignore[index]


@pytest.mark.parametrize(
    "field",
    [
        "checkpoint_id",
        "run_id",
        "resolved_config_digest",
        "dataset_digest",
        "model_digest",
        "code_digest",
        "toolchain_digest",
        "topology_digest",
    ],
)
def test_identity_mismatch_fails_before_object_mutation(tmp_path: Path, field: str) -> None:
    source_model = ReferenceAffine()
    source_optimizer = _optimizer(source_model)
    _step(source_model, source_optimizer)
    identity = _identity()
    destination = tmp_path / "checkpoint"
    save_local_checkpoint(
        destination,
        model=source_model,
        optimizer=source_optimizer,
        training_state=TrainingState(microbatches=1, optimizer_steps=1, samples=4),
        identity=identity,
        data_position=1,
    )

    target_model = ReferenceAffine()
    target_optimizer = _optimizer(target_model)
    before = {key: value.clone() for key, value in target_model.state_dict().items()}
    if field == "checkpoint_id":
        replacement = ResourceId("checkpoint", IdGenerator().raw_at(1_700_000_000_100)).text
    elif field == "run_id":
        replacement = ResourceId("run", IdGenerator().raw_at(1_700_000_000_100)).text
    else:
        replacement = Digest.of_text(f"different {field}").text

    with pytest.raises(FailedPrecondition, match="identity"):
        restore_local_checkpoint(
            destination,
            model=target_model,
            optimizer=target_optimizer,
            expected_identity=replace(identity, **{field: replacement}),
        )

    _assert_tree_equal(target_model.state_dict(), before)
    assert target_optimizer.state_dict()["state"] == {}


def test_corrupt_artifact_and_incomplete_directory_are_not_restorable(tmp_path: Path) -> None:
    model = ReferenceAffine()
    optimizer = _optimizer(model)
    destination = tmp_path / "checkpoint"
    identity = _identity()
    save_local_checkpoint(
        destination,
        model=model,
        optimizer=optimizer,
        training_state=TrainingState(),
        identity=identity,
        data_position=0,
    )
    state_path = destination / "state.safetensors"
    state_path.write_bytes(state_path.read_bytes() + b"corrupt")

    fresh = ReferenceAffine()
    with pytest.raises(InvalidArgument, match="verification"):
        restore_local_checkpoint(
            destination,
            model=fresh,
            optimizer=_optimizer(fresh),
            expected_identity=identity,
        )

    incomplete = tmp_path / "incomplete"
    incomplete.mkdir()
    (incomplete / "state.json").write_bytes(b"{}")
    fresh = ReferenceAffine()
    with pytest.raises(InvalidArgument, match="incomplete"):
        restore_local_checkpoint(
            incomplete,
            model=fresh,
            optimizer=_optimizer(fresh),
            expected_identity=identity,
        )


def test_existing_destination_and_wrong_optimizer_type_fail_closed(tmp_path: Path) -> None:
    model = ReferenceAffine()
    optimizer = _optimizer(model)
    identity = _identity()
    destination = tmp_path / "checkpoint"
    save_local_checkpoint(
        destination,
        model=model,
        optimizer=optimizer,
        training_state=TrainingState(),
        identity=identity,
        data_position=0,
    )
    with pytest.raises(FailedPrecondition, match="already exists"):
        save_local_checkpoint(
            destination,
            model=model,
            optimizer=optimizer,
            training_state=TrainingState(),
            identity=identity,
            data_position=0,
        )

    fresh = ReferenceAffine()
    wrong_optimizer = torch.optim.SGD(fresh.parameters(), lr=0.1, foreach=False)
    with pytest.raises(FailedPrecondition, match="runtime types"):
        restore_local_checkpoint(
            destination,
            model=fresh,
            optimizer=wrong_optimizer,
            expected_identity=identity,
        )


def test_invalid_rng_state_cannot_create_a_checkpoint_payload() -> None:
    model = ReferenceAffine()
    optimizer = _optimizer(model)

    with pytest.raises(InvalidArgument, match="RNG state"):
        encode_training_state(
            model.state_dict(),
            optimizer.state_dict(),
            torch.zeros(4, dtype=torch.uint8),
        )


def test_non_float32_model_buffer_is_rejected_before_commit(tmp_path: Path) -> None:
    class BufferedAffine(ReferenceAffine):
        def __init__(self) -> None:
            super().__init__()
            self.register_buffer("statistics", torch.ones(1, dtype=torch.float64))

    model = BufferedAffine()

    with pytest.raises(InvalidArgument, match="model state"):
        save_local_checkpoint(
            tmp_path / "checkpoint",
            model=model,
            optimizer=_optimizer(model),
            training_state=TrainingState(),
            identity=_identity(),
            data_position=0,
        )

    assert not (tmp_path / "checkpoint").exists()
