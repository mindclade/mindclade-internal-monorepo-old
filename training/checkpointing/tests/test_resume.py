# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Exact-resume and fail-closed tests for the local reference checkpoint."""

from __future__ import annotations

import json
from dataclasses import replace
from pathlib import Path

import pytest
import torch

from libs.python.errors import FailedPrecondition, InvalidArgument
from libs.python.identifiers import ArtifactRef, Digest, IdGenerator, ResourceId
from models.reference import ReferenceAffine
from training.checkpointing import (
    CheckpointIdentity,
    CheckpointManifest,
    restore_local_checkpoint,
    save_local_checkpoint,
    save_local_trainer_checkpoint,
)
from training.checkpointing import atomic_commit as atomic_commit_module
from training.checkpointing.serialization import decode_training_state, encode_training_state
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


def _rehashed_artifact(reference: ArtifactRef, value: bytes) -> ArtifactRef:
    return ArtifactRef(
        digest=Digest.of(value),
        size_bytes=len(value),
        media_type=reference.media_type,
        logical_kind=reference.logical_kind,
        schema_version=reference.schema_version,
    )


def _rewrite_local_state(
    destination: Path,
    manifest: CheckpointManifest,
    *,
    model_delta: float = 0.0,
    optimizer_step: float | None = None,
) -> CheckpointManifest:
    decoded = decode_training_state(
        (destination / "state.json").read_bytes(),
        (destination / "state.safetensors").read_bytes(),
    )
    model = dict(decoded.model)
    if model_delta:
        scale = model["scale"]
        assert isinstance(scale, torch.Tensor)
        model["scale"] = scale + model_delta
    optimizer = dict(decoded.optimizer)
    if optimizer_step is not None:
        states = optimizer["state"]
        assert isinstance(states, dict)
        for state in states.values():
            assert isinstance(state, dict)
            step = state["step"]
            assert isinstance(step, torch.Tensor)
            step.fill_(optimizer_step)
    metadata, tensors = encode_training_state(model, optimizer, decoded.torch_rng)
    artifacts = dict(manifest.artifacts)
    artifacts["state.json"] = _rehashed_artifact(artifacts["state.json"], metadata)
    artifacts["state.safetensors"] = _rehashed_artifact(artifacts["state.safetensors"], tensors)
    forged = replace(manifest, artifacts=artifacts)
    (destination / "state.json").write_bytes(metadata)
    (destination / "state.safetensors").write_bytes(tensors)
    (destination / "manifest.json").write_bytes(forged.encode())
    return forged


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


def test_local_state_metadata_requires_exact_canonical_json() -> None:
    model = ReferenceAffine()
    optim = _optimizer(model)
    metadata, tensors = encode_training_state(
        model.state_dict(),
        optim.state_dict(),
        torch.get_rng_state(),
    )
    document = json.loads(metadata)
    reordered = json.dumps(
        dict(reversed(tuple(document.items()))),
        ensure_ascii=False,
        separators=(",", ":"),
    ).encode("utf-8")
    noncanonical_number = metadata.replace(b'"schema_version":1', b'"schema_version":-0', 1)

    for noncanonical in (b" " + metadata, reordered, noncanonical_number):
        with pytest.raises(InvalidArgument, match="canonical JSON"):
            decode_training_state(noncanonical, tensors)

    boolean_version = metadata.replace(b'"schema_version":1', b'"schema_version":true', 1)
    with pytest.raises(InvalidArgument, match="schema version"):
        decode_training_state(boolean_version, tensors)


def test_local_state_metadata_rejects_excessive_json_nesting() -> None:
    deeply_nested = (b"[" * 10_000) + b"0" + (b"]" * 10_000)
    with pytest.raises(InvalidArgument, match="UTF-8 JSON"):
        decode_training_state(deeply_nested, b"not-read")


def test_local_state_metadata_does_not_count_quoted_json_syntax_as_nesting() -> None:
    quoted_syntax = '[{"quoted":"\\\\value"}]' * 128
    metadata, tensors = encode_training_state(
        {"quoted_syntax": quoted_syntax},
        {},
        torch.get_rng_state(),
    )

    decoded = decode_training_state(metadata, tensors)

    assert decoded.model["quoted_syntax"] == quoted_syntax


def test_local_commit_rejects_silent_member_write_tamper(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    original_write = atomic_commit_module._write_durable

    def tamper_write(path: Path, value: bytes) -> None:
        if path.name == "state.json":
            value = bytes((value[0] ^ 1,)) + value[1:]
        original_write(path, value)

    monkeypatch.setattr(atomic_commit_module, "_write_durable", tamper_write)
    model = ReferenceAffine()
    destination = tmp_path / "tampered-write"
    with pytest.raises(InvalidArgument, match="write verification"):
        save_local_checkpoint(
            destination,
            model=model,
            optimizer=_optimizer(model),
            training_state=TrainingState(),
            identity=_identity(),
            data_position=0,
        )

    assert not destination.exists()
    assert not tuple(tmp_path.glob(".tampered-write.staging-*"))


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

    zero_position = replace(manifest, data_position=0).encode()
    document = json.loads(zero_position)
    reordered = json.dumps(
        dict(reversed(tuple(document.items()))),
        ensure_ascii=False,
        separators=(",", ":"),
    ).encode("utf-8")
    noncanonical_number = zero_position.replace(b'"data_position":0', b'"data_position":-0', 1)
    for noncanonical in (b" " + zero_position, reordered, noncanonical_number):
        with pytest.raises(InvalidArgument, match="canonical JSON"):
            CheckpointManifest.decode(noncanonical)


def test_scheduler_backed_trainer_is_rejected_before_local_staging(tmp_path: Path) -> None:
    model = ReferenceAffine()
    optimizer = _optimizer(model)
    scheduled = Trainer(
        model,
        SupervisedMSETask(),
        optimizer,
        scheduler=torch.optim.lr_scheduler.StepLR(optimizer, step_size=1),
    )
    _train_steps(scheduled, 1)
    destination = tmp_path / "scheduled"

    with pytest.raises(FailedPrecondition, match="scheduler"):
        save_local_trainer_checkpoint(
            destination,
            trainer=scheduled,
            identity=_identity(),
            data_position=1,
        )

    assert not destination.exists()


@pytest.mark.parametrize("step", [float("nan"), 1.5, 2.0, float(1 << 63)])
def test_local_save_rejects_invalid_or_mismatched_adamw_steps(
    tmp_path: Path,
    step: float,
) -> None:
    model = ReferenceAffine()
    optimizer = _optimizer(model)
    trained = _trainer(model, optimizer)
    _train_steps(trained, 1)
    first_state = next(iter(optimizer.state.values()))
    first_step = first_state["step"]
    assert isinstance(first_step, torch.Tensor)
    first_step.fill_(step)

    with pytest.raises(InvalidArgument, match="step"):
        save_local_checkpoint(
            tmp_path / "invalid-step",
            model=model,
            optimizer=optimizer,
            training_state=trained.state,
            identity=_identity(),
            data_position=1,
        )

    assert not (tmp_path / "invalid-step").exists()


def test_local_restore_rejects_recomputed_optimizer_counter_before_mutation(
    tmp_path: Path,
) -> None:
    model = ReferenceAffine()
    optimizer = _optimizer(model)
    trained = _trainer(model, optimizer)
    _train_steps(trained, 1)
    destination = tmp_path / "checkpoint"
    identity = _identity()
    manifest = save_local_checkpoint(
        destination,
        model=model,
        optimizer=optimizer,
        training_state=trained.state,
        identity=identity,
        data_position=1,
    )
    _rewrite_local_state(destination, manifest, optimizer_step=2.0)

    fresh = ReferenceAffine()
    fresh_optimizer = _optimizer(fresh)
    before = {name: tensor.clone() for name, tensor in fresh.state_dict().items()}
    with pytest.raises(InvalidArgument, match="TrainingState optimizer_steps"):
        restore_local_checkpoint(
            destination,
            model=fresh,
            optimizer=fresh_optimizer,
            expected_identity=identity,
        )

    _assert_tree_equal(fresh.state_dict(), before)
    assert not fresh_optimizer.state


def test_external_manifest_anchor_rejects_whole_tree_recomputation(tmp_path: Path) -> None:
    model = ReferenceAffine()
    optimizer = _optimizer(model)
    trained = _trainer(model, optimizer)
    _train_steps(trained, 1)
    destination = tmp_path / "checkpoint"
    manifest = save_local_checkpoint(
        destination,
        model=model,
        optimizer=optimizer,
        training_state=trained.state,
        identity=_identity(),
        data_position=1,
    )
    admitted = Digest.parse(manifest.digest)
    forged = _rewrite_local_state(destination, manifest, model_delta=10.0)
    assert forged.digest != manifest.digest

    fresh = ReferenceAffine()
    fresh_optimizer = _optimizer(fresh)
    before = {name: tensor.clone() for name, tensor in fresh.state_dict().items()}
    with pytest.raises(FailedPrecondition, match="externally admitted digest"):
        restore_local_checkpoint(
            destination,
            model=fresh,
            optimizer=fresh_optimizer,
            expected_identity=_identity(),
            expected_manifest_digest=admitted,
        )

    _assert_tree_equal(fresh.state_dict(), before)
    assert not fresh_optimizer.state


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
