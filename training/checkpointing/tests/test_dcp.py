# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Exact local behavior and fail-closed coverage for distributed-v1."""

from __future__ import annotations

import json
import shutil
from dataclasses import replace
from pathlib import Path

import pytest
import torch

from libs.python.errors import FailedPrecondition, InvalidArgument
from libs.python.identifiers import ArtifactRef, Digest, IdGenerator, ResourceId
from libs.python.serialization import canonical_json_bytes
from models.reference import ReferenceAffine
from training.checkpointing import (
    CheckpointIdentity,
    DCPManifest,
    restore_distributed_checkpoint,
    save_distributed_checkpoint,
    save_distributed_trainer_checkpoint,
)
from training.checkpointing import dcp as dcp_module
from training.checkpointing.serialization import (
    decode_rank_rng_state,
    decode_state_component,
    encode_state_component,
)
from training.contracts import SupervisedBatch, TrainingState
from training.core import Scheduler, Trainer
from training.tasks import SupervisedMSETask


def dcp_identity(topology: str = "cpu-world-size-1") -> CheckpointIdentity:
    generator = IdGenerator()
    stamp = 1_700_000_100_000
    return CheckpointIdentity(
        checkpoint_id=ResourceId("checkpoint", generator.raw_at(stamp)).text,
        run_id=ResourceId("run", generator.raw_at(stamp + 1)).text,
        resolved_config_digest=Digest.of_text("distributed config").text,
        dataset_digest=Digest.of_text("deterministic affine samples").text,
        model_digest=Digest.of_text("reference-affine-v1").text,
        code_digest=Digest.of_text("source tree").text,
        toolchain_digest=Digest.of_text("pinned torch toolchain").text,
        topology_digest=Digest.of_text(topology).text,
    )


def optimizer(model: ReferenceAffine) -> torch.optim.AdamW:
    return torch.optim.AdamW(model.parameters(), lr=0.025, weight_decay=0.01, foreach=False)


def batches(count: int) -> tuple[SupervisedBatch, ...]:
    inputs = torch.tensor([[-2.0], [-0.5], [1.0], [3.0]], dtype=torch.float32)
    targets = (inputs * 3.0) - 1.0
    return (SupervisedBatch(inputs, targets),) * count


def trainer(
    model: ReferenceAffine,
    optim: torch.optim.Optimizer,
    *,
    scheduler: Scheduler | None = None,
    state: TrainingState | None = None,
) -> Trainer:
    return Trainer(
        model,
        SupervisedMSETask(),
        optim,
        scheduler=scheduler,
        state=state,
    )


def assert_tree_equal(actual: object, expected: object) -> None:
    if isinstance(expected, torch.Tensor):
        assert isinstance(actual, torch.Tensor)
        torch.testing.assert_close(actual, expected, rtol=0.0, atol=0.0)
    elif isinstance(expected, dict):
        assert isinstance(actual, dict)
        assert actual.keys() == expected.keys()
        for key in expected:
            assert_tree_equal(actual[key], expected[key])
    elif isinstance(expected, (list, tuple)):
        assert isinstance(actual, type(expected))
        assert len(actual) == len(expected)
        for actual_item, expected_item in zip(actual, expected, strict=True):
            assert_tree_equal(actual_item, expected_item)
    else:
        assert actual == expected


def rehashed_artifact(reference: ArtifactRef, value: bytes) -> ArtifactRef:
    return ArtifactRef(
        digest=Digest.of(value),
        size_bytes=len(value),
        media_type=reference.media_type,
        logical_kind=reference.logical_kind,
        schema_version=reference.schema_version,
    )


def rewrite_dcp_optimizer_step(
    destination: Path,
    manifest: DCPManifest,
    *,
    step_value: float,
) -> DCPManifest:
    decoded = decode_state_component(
        (destination / "optimizer.json").read_bytes(),
        (destination / "optimizer.safetensors").read_bytes(),
        component="optimizer",
    )
    optimizer_state = dict(decoded.state)
    states = optimizer_state["state"]
    assert isinstance(states, dict)
    for state in states.values():
        assert isinstance(state, dict)
        step = state["step"]
        assert isinstance(step, torch.Tensor)
        step.fill_(step_value)
    metadata, tensors = encode_state_component(optimizer_state, component="optimizer")
    artifacts = dict(manifest.artifacts)
    artifacts["optimizer.json"] = rehashed_artifact(artifacts["optimizer.json"], metadata)
    artifacts["optimizer.safetensors"] = rehashed_artifact(
        artifacts["optimizer.safetensors"], tensors
    )
    forged = replace(manifest, artifacts=artifacts)
    (destination / "optimizer.json").write_bytes(metadata)
    (destination / "optimizer.safetensors").write_bytes(tensors)
    (destination / "manifest.json").write_bytes(forged.encode())
    return forged


def test_dcp_exact_resume_uses_canonical_fqns_and_pickle_free_members(tmp_path: Path) -> None:
    uninterrupted = ReferenceAffine()
    uninterrupted_optimizer = optimizer(uninterrupted)
    uninterrupted_trainer = trainer(uninterrupted, uninterrupted_optimizer)
    uninterrupted_trainer.train(batches(4))

    interrupted = ReferenceAffine()
    interrupted_optimizer = optimizer(interrupted)
    interrupted_trainer = trainer(interrupted, interrupted_optimizer)
    interrupted_trainer.train(batches(2))
    torch.manual_seed(123456)
    expected_rng = torch.get_rng_state().clone()

    destination = tmp_path / "dcp"
    identity = dcp_identity()
    manifest = save_distributed_checkpoint(
        destination,
        model=interrupted,
        optimizer=interrupted_optimizer,
        training_state=interrupted_trainer.state,
        identity=identity,
        data_position=8,
    )

    assert manifest == DCPManifest.decode((destination / "manifest.json").read_bytes())
    assert manifest.model_fqns == ("bias", "scale")
    assert manifest.optimizer_fqns == ("bias", "scale")
    names = {path.name for path in destination.iterdir()}
    assert names == {
        "manifest.json",
        "model.json",
        "model.safetensors",
        "optimizer.json",
        "optimizer.safetensors",
        "rank-00000.rng.json",
        "rank-00000.rng.safetensors",
    }
    assert not any(name.endswith((".metadata", ".pt", ".pth", ".pkl")) for name in names)

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
            DCPManifest.decode(noncanonical)

    torch.manual_seed(1)
    resumed = ReferenceAffine()
    resumed_optimizer = optimizer(resumed)
    result = restore_distributed_checkpoint(
        destination,
        model=resumed,
        optimizer=resumed_optimizer,
        expected_identity=identity,
    )

    assert result.exact_resume
    assert result.source_rank == 0
    assert result.data_position == 8
    torch.testing.assert_close(torch.get_rng_state(), expected_rng, rtol=0.0, atol=0.0)
    assert_tree_equal(resumed.state_dict(), interrupted.state_dict())
    assert_tree_equal(resumed_optimizer.state_dict(), interrupted_optimizer.state_dict())

    resumed_trainer = trainer(
        resumed,
        resumed_optimizer,
        state=result.training_state,
    )
    resumed_trainer.train(batches(2))
    assert resumed_trainer.state == uninterrupted_trainer.state
    assert_tree_equal(resumed.state_dict(), uninterrupted.state_dict())
    assert_tree_equal(resumed_optimizer.state_dict(), uninterrupted_optimizer.state_dict())


def test_dcp_component_metadata_requires_exact_canonical_json(tmp_path: Path) -> None:
    model = ReferenceAffine()
    optim = optimizer(model)
    trained = trainer(model, optim)
    trained.train(batches(1))
    destination = tmp_path / "dcp"
    save_distributed_checkpoint(
        destination,
        model=model,
        optimizer=optim,
        training_state=trained.state,
        identity=dcp_identity(),
        data_position=4,
    )
    metadata = (destination / "model.json").read_bytes()
    tensors = (destination / "model.safetensors").read_bytes()
    document = json.loads(metadata)
    reordered = json.dumps(
        dict(reversed(tuple(document.items()))),
        ensure_ascii=False,
        separators=(",", ":"),
    ).encode("utf-8")
    noncanonical_number = metadata.replace(b'"schema_version":1', b'"schema_version":-0', 1)
    for noncanonical in (b" " + metadata, reordered, noncanonical_number):
        with pytest.raises(InvalidArgument, match="canonical JSON"):
            decode_state_component(noncanonical, tensors, component="model")

    boolean_version = metadata.replace(b'"schema_version":1', b'"schema_version":true', 1)
    with pytest.raises(InvalidArgument, match="schema v1"):
        decode_state_component(boolean_version, tensors, component="model")

    rng_metadata = (destination / "rank-00000.rng.json").read_bytes()
    rng_tensors = (destination / "rank-00000.rng.safetensors").read_bytes()
    boolean_rng_version = rng_metadata.replace(
        b'"schema_version":1',
        b'"schema_version":true',
        1,
    )
    with pytest.raises(InvalidArgument, match="version"):
        decode_rank_rng_state(boolean_rng_version, rng_tensors)


def test_dcp_component_metadata_rejects_excessive_json_nesting() -> None:
    deeply_nested = (b"[" * 10_000) + b"0" + (b"]" * 10_000)
    with pytest.raises(InvalidArgument, match="UTF-8 JSON"):
        decode_state_component(deeply_nested, b"not-read", component="model")


def test_dcp_requires_initialized_optimizer_and_fresh_restore_objects(tmp_path: Path) -> None:
    model = ReferenceAffine()
    optim = optimizer(model)
    identity = dcp_identity()

    with pytest.raises(FailedPrecondition, match="initialized"):
        save_distributed_checkpoint(
            tmp_path / "empty",
            model=model,
            optimizer=optim,
            training_state=trainer(model, optim).state,
            identity=identity,
            data_position=0,
        )

    trained = trainer(model, optim)
    trained.train(batches(1))
    destination = tmp_path / "complete"
    save_distributed_checkpoint(
        destination,
        model=model,
        optimizer=optim,
        training_state=trained.state,
        identity=identity,
        data_position=4,
    )
    fresh_model = ReferenceAffine()
    nonfresh_optimizer = optimizer(fresh_model)
    trainer(fresh_model, nonfresh_optimizer).train(batches(1))
    with pytest.raises(FailedPrecondition, match="fresh optimizer"):
        restore_distributed_checkpoint(
            destination,
            model=fresh_model,
            optimizer=nonfresh_optimizer,
            expected_identity=identity,
        )


def test_scheduler_backed_trainer_is_rejected_before_dcp_staging(tmp_path: Path) -> None:
    model = ReferenceAffine()
    optim = optimizer(model)
    scheduled = trainer(
        model,
        optim,
        scheduler=torch.optim.lr_scheduler.StepLR(optim, step_size=1),
    )
    scheduled.train(batches(1))
    destination = tmp_path / "scheduled"

    with pytest.raises(FailedPrecondition, match="scheduler"):
        save_distributed_trainer_checkpoint(
            destination,
            trainer=scheduled,
            identity=dcp_identity(),
            data_position=4,
        )

    assert not destination.exists()


def test_dcp_save_rejects_adamw_step_counter_mismatch(tmp_path: Path) -> None:
    model = ReferenceAffine()
    optim = optimizer(model)
    trained = trainer(model, optim)
    trained.train(batches(1))

    with pytest.raises(InvalidArgument, match="step"):
        save_distributed_checkpoint(
            tmp_path / "mismatch",
            model=model,
            optimizer=optim,
            training_state=TrainingState(microbatches=2, optimizer_steps=2, samples=8),
            identity=dcp_identity(),
            data_position=8,
        )

    assert not (tmp_path / "mismatch").exists()


def test_dcp_save_does_not_remove_preexisting_staging_directory(tmp_path: Path) -> None:
    model = ReferenceAffine()
    optim = optimizer(model)
    trained = trainer(model, optim)
    trained.train(batches(1))
    staging = tmp_path / ".checkpoint.dcp-staging"
    staging.mkdir()
    sentinel = staging / "owned-by-another-writer"
    sentinel.write_bytes(b"retain")

    with pytest.raises(FailedPrecondition, match="already exists"):
        save_distributed_checkpoint(
            tmp_path / "checkpoint",
            model=model,
            optimizer=optim,
            training_state=trained.state,
            identity=dcp_identity(),
            data_position=4,
        )

    assert sentinel.read_bytes() == b"retain"


def test_dcp_restore_rejects_recomputed_optimizer_counter_before_mutation(
    tmp_path: Path,
) -> None:
    model = ReferenceAffine()
    optim = optimizer(model)
    trained = trainer(model, optim)
    trained.train(batches(1))
    destination = tmp_path / "dcp"
    identity = dcp_identity()
    manifest = save_distributed_checkpoint(
        destination,
        model=model,
        optimizer=optim,
        training_state=trained.state,
        identity=identity,
        data_position=4,
    )
    rewrite_dcp_optimizer_step(destination, manifest, step_value=2.0)

    fresh = ReferenceAffine()
    fresh_optimizer = optimizer(fresh)
    before = {name: tensor.clone() for name, tensor in fresh.state_dict().items()}
    with pytest.raises(InvalidArgument, match="TrainingState optimizer_steps"):
        restore_distributed_checkpoint(
            destination,
            model=fresh,
            optimizer=fresh_optimizer,
            expected_identity=identity,
        )

    assert_tree_equal(fresh.state_dict(), before)
    assert not fresh_optimizer.state


def test_dcp_external_manifest_anchor_rejects_recomputed_manifest(tmp_path: Path) -> None:
    model = ReferenceAffine()
    optim = optimizer(model)
    trained = trainer(model, optim)
    trained.train(batches(1))
    destination = tmp_path / "dcp"
    identity = dcp_identity()
    manifest = save_distributed_checkpoint(
        destination,
        model=model,
        optimizer=optim,
        training_state=trained.state,
        identity=identity,
        data_position=4,
    )
    forged = replace(manifest, data_position=5)
    (destination / "manifest.json").write_bytes(forged.encode())

    fresh = ReferenceAffine()
    fresh_optimizer = optimizer(fresh)
    before = {name: tensor.clone() for name, tensor in fresh.state_dict().items()}
    with pytest.raises(FailedPrecondition, match="externally admitted digest"):
        restore_distributed_checkpoint(
            destination,
            model=fresh,
            optimizer=fresh_optimizer,
            expected_identity=identity,
            expected_manifest_digest=Digest.parse(manifest.digest),
        )

    assert_tree_equal(fresh.state_dict(), before)
    assert not fresh_optimizer.state


def test_collective_cleanup_failure_is_synchronized_without_barrier(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    staging = tmp_path / "staging"
    staging.mkdir()
    collective_calls = 0

    def fail_remote_stage_or_cleanup(flag: torch.Tensor, *, op: object) -> None:
        nonlocal collective_calls
        del op
        collective_calls += 1
        flag.zero_()

    def fail_cleanup(path: Path) -> None:
        raise OSError(f"cannot remove {path.name}")

    monkeypatch.setattr(torch.distributed, "all_reduce", fail_remote_stage_or_cleanup)
    monkeypatch.setattr(shutil, "rmtree", fail_cleanup)

    with pytest.raises(FailedPrecondition, match="clean staging"):
        dcp_module._finish_collective_stage(
            None,
            rank=0,
            world_size=2,
            device=torch.device("cpu"),
            staging=staging,
            operation="write state components",
        )

    assert collective_calls == 2
    assert staging.exists()


def test_dcp_rejects_unsupported_optimizer_algorithm_and_execution_mode(tmp_path: Path) -> None:
    sgd_model = ReferenceAffine()
    sgd = torch.optim.SGD(sgd_model.parameters(), lr=0.025, momentum=0.9, foreach=False)
    sgd_trainer = trainer(sgd_model, sgd)
    sgd_trainer.train(batches(1))
    with pytest.raises(InvalidArgument, match="AdamW optimizer state only"):
        save_distributed_checkpoint(
            tmp_path / "sgd",
            model=sgd_model,
            optimizer=sgd,
            training_state=sgd_trainer.state,
            identity=dcp_identity(),
            data_position=4,
        )

    foreach_model = ReferenceAffine()
    foreach_adamw = torch.optim.AdamW(
        foreach_model.parameters(),
        lr=0.025,
        foreach=True,
    )
    foreach_trainer = trainer(foreach_model, foreach_adamw)
    foreach_trainer.train(batches(1))
    with pytest.raises(InvalidArgument, match="bounded non-foreach"):
        save_distributed_checkpoint(
            tmp_path / "foreach",
            model=foreach_model,
            optimizer=foreach_adamw,
            training_state=foreach_trainer.state,
            identity=dcp_identity(),
            data_position=4,
        )

    invalid_state_model = ReferenceAffine()
    invalid_state_optimizer = optimizer(invalid_state_model)
    invalid_state_trainer = trainer(invalid_state_model, invalid_state_optimizer)
    invalid_state_trainer.train(batches(1))
    next(iter(invalid_state_optimizer.state.values()))["unexpected"] = torch.tensor(1.0)
    with pytest.raises(InvalidArgument, match="tensor schema"):
        save_distributed_checkpoint(
            tmp_path / "invalid-state",
            model=invalid_state_model,
            optimizer=invalid_state_optimizer,
            training_state=invalid_state_trainer.state,
            identity=dcp_identity(),
            data_position=4,
        )


def test_dcp_rejects_corruption_before_mutating_objects(tmp_path: Path) -> None:
    model = ReferenceAffine()
    optim = optimizer(model)
    trained = trainer(model, optim)
    trained.train(batches(1))
    destination = tmp_path / "dcp"
    identity = dcp_identity()
    save_distributed_checkpoint(
        destination,
        model=model,
        optimizer=optim,
        training_state=trained.state,
        identity=identity,
        data_position=4,
    )
    tensor_path = destination / "optimizer.safetensors"
    tensor_path.write_bytes(tensor_path.read_bytes() + b"corrupt")

    fresh = ReferenceAffine()
    before = {name: tensor.clone() for name, tensor in fresh.state_dict().items()}
    with pytest.raises(InvalidArgument, match="verification"):
        restore_distributed_checkpoint(
            destination,
            model=fresh,
            optimizer=optimizer(fresh),
            expected_identity=identity,
        )
    assert_tree_equal(fresh.state_dict(), before)


def test_dcp_world_size_policy_is_explicit(tmp_path: Path) -> None:
    model = ReferenceAffine()
    optim = optimizer(model)
    trained = trainer(model, optim)
    trained.train(batches(1))
    destination = tmp_path / "dcp"
    identity = dcp_identity()
    manifest = save_distributed_checkpoint(
        destination,
        model=model,
        optimizer=optim,
        training_state=trained.state,
        identity=identity,
        data_position=4,
    )
    document = manifest.to_document()
    document["world_size"] = 2
    with pytest.raises(InvalidArgument, match="artifacts"):
        DCPManifest.decode(canonical_json_bytes(document))
    document["world_size"] = 3
    with pytest.raises(InvalidArgument, match="world size"):
        DCPManifest.decode(canonical_json_bytes(document))


@pytest.mark.parametrize(
    ("source_world_size", "target_world_size", "target_device_type", "source_has_cuda_rng"),
    [
        pytest.param(2, 1, "cuda", False, id="cpu2-to-cuda1"),
        pytest.param(1, 2, "cpu", True, id="cuda1-to-cpu2"),
    ],
)
def test_dcp_cross_world_portability_rejects_cuda_endpoint(
    source_world_size: int,
    target_world_size: int,
    target_device_type: str,
    source_has_cuda_rng: bool,
) -> None:
    with pytest.raises(FailedPrecondition, match="CPU/Gloo"):
        dcp_module._validate_cross_world_device_portability(
            same_world=False,
            source_world_size=source_world_size,
            target_world_size=target_world_size,
            target_device_type=target_device_type,
            source_has_cuda_rng=source_has_cuda_rng,
        )

    dcp_module._validate_cross_world_device_portability(
        same_world=True,
        source_world_size=source_world_size,
        target_world_size=source_world_size,
        target_device_type=target_device_type,
        source_has_cuda_rng=source_has_cuda_rng,
    )
