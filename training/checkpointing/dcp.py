# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Pickle-free, manifest-last checkpointing for replicated DDP state.

This is the bounded ``distributed-v1`` filesystem adapter.  It deliberately does
not use PyTorch DCP's pickle-backed ``.metadata`` file. Canonical model and AdamW
optimizer FQNs come from ``get_state_dict``/``set_state_dict``; tensor payloads
are safetensors and every non-tensor node is bounded canonical JSON.
"""

from __future__ import annotations

import json
import os
import re
import shutil
import stat
from collections.abc import Mapping
from dataclasses import dataclass, field
from pathlib import Path
from types import MappingProxyType
from typing import Any, Final, Self, cast

import torch
from torch import nn
from torch.distributed.checkpoint.state_dict import (
    StateDictOptions,
    get_state_dict,
    set_state_dict,
)
from torch.nn.parallel import DistributedDataParallel

from libs.python.errors import FailedPrecondition, InvalidArgument, ResourceExhausted
from libs.python.identifiers import ArtifactRef, Digest
from libs.python.serialization import canonical_json_bytes
from training.contracts import TrainingState

from .adamw import validate_adamw_steps
from .atomic_commit import MANIFEST_PATH, read_checkpoint_member, validate_committed_root
from .manifest import (
    MAXIMUM_DATA_POSITION,
    MAXIMUM_MANIFEST_BYTES,
    CheckpointIdentity,
    qualified_type,
)
from .serialization import (
    MAXIMUM_TENSOR_BYTES,
    decode_rank_rng_state,
    decode_state_component,
    encode_rank_rng_state,
    encode_state_component,
)

DCP_SCHEMA_VERSION: Final = 1
SUPPORTED_DCP_WORLD_SIZES: Final = frozenset({1, 2, 8})
MAXIMUM_DCP_FILES: Final = 4 + (2 * max(SUPPORTED_DCP_WORLD_SIZES))
MAXIMUM_DCP_TOTAL_BYTES: Final = 2 << 30
MAXIMUM_DCP_DOCUMENT_BYTES: Final = 8 << 20
MAXIMUM_DCP_TENSOR_ARCHIVE_BYTES: Final = MAXIMUM_TENSOR_BYTES + (100 << 20)

MODEL_DOCUMENT_PATH: Final = "model.json"
MODEL_TENSORS_PATH: Final = "model.safetensors"
OPTIMIZER_DOCUMENT_PATH: Final = "optimizer.json"
OPTIMIZER_TENSORS_PATH: Final = "optimizer.safetensors"
DCP_MODEL_DOCUMENT_MEDIA_TYPE: Final = "application/vnd.mindclade.dcp-model.v1+json"
DCP_MODEL_TENSORS_MEDIA_TYPE: Final = "application/vnd.mindclade.dcp-model.v1+safetensors"
DCP_OPTIMIZER_DOCUMENT_MEDIA_TYPE: Final = "application/vnd.mindclade.dcp-optimizer.v1+json"
DCP_OPTIMIZER_TENSORS_MEDIA_TYPE: Final = "application/vnd.mindclade.dcp-optimizer.v1+safetensors"
DCP_RNG_DOCUMENT_MEDIA_TYPE: Final = "application/vnd.mindclade.dcp-rng.v1+json"
DCP_RNG_TENSORS_MEDIA_TYPE: Final = "application/vnd.mindclade.dcp-rng.v1+safetensors"
_RNG_REFERENCE_DESCRIPTOR_BYTES: Final = 2 * (32 + 8)

_FQN = re.compile(r"[^\x00-\x20]{1,1024}")
_RANK_MEMBER = re.compile(r"rank-[0-9]{5}\.rng\.(?:json|safetensors)")


def _rng_document_path(rank: int) -> str:
    return f"rank-{rank:05d}.rng.json"


def _rng_tensors_path(rank: int) -> str:
    return f"rank-{rank:05d}.rng.safetensors"


def expected_dcp_artifact_paths(world_size: int) -> frozenset[str]:
    _validate_world_size(world_size)
    result = {
        MODEL_DOCUMENT_PATH,
        MODEL_TENSORS_PATH,
        OPTIMIZER_DOCUMENT_PATH,
        OPTIMIZER_TENSORS_PATH,
    }
    for rank in range(world_size):
        result.add(_rng_document_path(rank))
        result.add(_rng_tensors_path(rank))
    return frozenset(result)


@dataclass(frozen=True, slots=True)
class DCPManifest:
    """Canonical local adapter manifest for distributed checkpoint components."""

    identity: CheckpointIdentity
    training_state: TrainingState
    data_position: int
    model_type: str
    optimizer_type: str
    world_size: int
    model_fqns: tuple[str, ...]
    optimizer_fqns: tuple[str, ...]
    artifacts: Mapping[str, ArtifactRef] = field(default_factory=dict)
    schema_version: int = DCP_SCHEMA_VERSION

    def __post_init__(self) -> None:
        if not isinstance(self.identity, CheckpointIdentity):
            raise InvalidArgument(
                "DCP manifest identity is invalid",
                reason="dcp_manifest_identity",
            )
        if not isinstance(self.training_state, TrainingState):
            raise InvalidArgument(
                "DCP manifest training state is invalid",
                reason="dcp_manifest_training_state",
            )
        if (
            isinstance(self.data_position, bool)
            or not isinstance(self.data_position, int)
            or not 0 <= self.data_position <= MAXIMUM_DATA_POSITION
        ):
            raise InvalidArgument(
                "DCP data position is outside bounds",
                reason="dcp_data_position",
            )
        if type(self.schema_version) is not int or self.schema_version != DCP_SCHEMA_VERSION:
            raise InvalidArgument(
                "DCP schema version is unsupported",
                reason="dcp_manifest_version",
            )
        _validate_world_size(self.world_size)
        for label, value in (
            ("model_type", self.model_type),
            ("optimizer_type", self.optimizer_type),
        ):
            if not isinstance(value, str) or _FQN.fullmatch(value) is None:
                raise InvalidArgument(
                    f"DCP {label} is not a bounded qualified type",
                    reason="dcp_runtime_type",
                )
        _validate_fqns(self.model_fqns, name="model_fqns", require_nonempty=True)
        _validate_fqns(self.optimizer_fqns, name="optimizer_fqns", require_nonempty=True)
        expected = expected_dcp_artifact_paths(self.world_size)
        if not isinstance(self.artifacts, Mapping) or set(self.artifacts) != expected:
            raise InvalidArgument(
                "DCP artifacts do not match the declared world",
                reason="dcp_manifest_artifacts",
            )
        frozen: dict[str, ArtifactRef] = {}
        total = 0
        for path, reference in self.artifacts.items():
            if not isinstance(path, str) or not isinstance(reference, ArtifactRef):
                raise InvalidArgument(
                    "DCP artifact mapping is invalid",
                    reason="dcp_manifest_artifacts",
                )
            if reference.size_bytes <= 0:
                raise InvalidArgument(
                    "DCP artifact size must be positive",
                    reason="dcp_manifest_artifacts",
                )
            total += reference.size_bytes
            frozen[path] = reference
        if total > MAXIMUM_DCP_TOTAL_BYTES:
            raise ResourceExhausted(
                "DCP artifact bytes exceed the reference bound",
                reason="dcp_total_size",
            )
        object.__setattr__(self, "artifacts", MappingProxyType(frozen))

    @property
    def digest(self) -> str:
        return Digest.of(self.encode()).text

    def to_document(self) -> dict[str, object]:
        return {
            "schema_version": self.schema_version,
            "format": "mindclade-distributed-checkpoint-v1",
            "identity": self.identity.to_document(),
            "training_state": {
                "microbatches": self.training_state.microbatches,
                "optimizer_steps": self.training_state.optimizer_steps,
                "samples": self.training_state.samples,
            },
            "data_position": self.data_position,
            "model_type": self.model_type,
            "optimizer_type": self.optimizer_type,
            "world_size": self.world_size,
            "model_fqns": list(self.model_fqns),
            "optimizer_fqns": list(self.optimizer_fqns),
            "artifacts": {
                path: reference.to_document() for path, reference in sorted(self.artifacts.items())
            },
        }

    def encode(self) -> bytes:
        result = canonical_json_bytes(self.to_document())
        if len(result) > MAXIMUM_MANIFEST_BYTES:
            raise ResourceExhausted(
                "DCP manifest exceeds its byte bound",
                reason="dcp_manifest_size",
            )
        return result

    @classmethod
    def decode(cls, value: bytes) -> Self:
        if not isinstance(value, bytes) or not value or len(value) > MAXIMUM_MANIFEST_BYTES:
            raise InvalidArgument(
                "DCP manifest bytes are outside bounds",
                reason="dcp_manifest_size",
            )
        try:
            document = json.loads(value, object_pairs_hook=_unique_object)
        except (UnicodeDecodeError, json.JSONDecodeError, RecursionError, ValueError) as error:
            raise InvalidArgument(
                "DCP manifest is not unique-key UTF-8 JSON",
                reason="dcp_manifest_json",
                cause=error,
            ) from error
        expected = {
            "schema_version",
            "format",
            "identity",
            "training_state",
            "data_position",
            "model_type",
            "optimizer_type",
            "world_size",
            "model_fqns",
            "optimizer_fqns",
            "artifacts",
        }
        if not isinstance(document, dict) or set(document) != expected:
            raise InvalidArgument(
                "DCP manifest fields do not match schema v1",
                reason="dcp_manifest_fields",
            )
        if document["format"] != "mindclade-distributed-checkpoint-v1":
            raise InvalidArgument(
                "DCP manifest format is unsupported",
                reason="dcp_manifest_version",
            )
        state = document["training_state"]
        if not isinstance(state, dict) or set(state) != {
            "microbatches",
            "optimizer_steps",
            "samples",
        }:
            raise InvalidArgument(
                "DCP training state fields are invalid",
                reason="dcp_manifest_training_state",
            )
        artifacts = document["artifacts"]
        model_fqns = document["model_fqns"]
        optimizer_fqns = document["optimizer_fqns"]
        if (
            not isinstance(artifacts, dict)
            or not isinstance(model_fqns, list)
            or not isinstance(optimizer_fqns, list)
        ):
            raise InvalidArgument(
                "DCP manifest collections are invalid",
                reason="dcp_manifest_fields",
            )
        manifest = cls(
            identity=CheckpointIdentity.from_document(document["identity"]),
            training_state=TrainingState(**state),
            data_position=document["data_position"],
            model_type=document["model_type"],
            optimizer_type=document["optimizer_type"],
            world_size=document["world_size"],
            model_fqns=tuple(model_fqns),
            optimizer_fqns=tuple(optimizer_fqns),
            artifacts={path: ArtifactRef.from_document(item) for path, item in artifacts.items()},
            schema_version=document["schema_version"],
        )
        if manifest.encode() != value:
            raise InvalidArgument(
                "DCP manifest must use its exact canonical JSON encoding",
                reason="dcp_manifest_canonical",
            )
        return manifest


@dataclass(frozen=True, slots=True)
class DCPResumeResult:
    manifest: DCPManifest
    training_state: TrainingState
    data_position: int
    exact_resume: bool
    source_rank: int


@dataclass(frozen=True, slots=True)
class _PreparedSave:
    destination: Path
    staging: Path
    replica_document: bytes
    model_document: bytes
    model_tensors: bytes
    optimizer_document: bytes
    optimizer_tensors: bytes
    rng_document: bytes
    rng_tensors: bytes
    model_fqns: tuple[str, ...]
    optimizer_fqns: tuple[str, ...]
    shared_artifacts: Mapping[str, ArtifactRef]
    rng_reference_descriptor: torch.Tensor
    rng_reference_gather: list[torch.Tensor]


def save_distributed_checkpoint(
    destination: Path,
    *,
    model: nn.Module,
    optimizer: torch.optim.Optimizer,
    training_state: TrainingState,
    identity: CheckpointIdentity,
    data_position: int,
) -> DCPManifest:
    """Collectively publish replicated DDP state and rank-local RNG components."""

    rank, world_size, device = _runtime_identity(model)

    prepared: _PreparedSave | None = None
    preparation_error: Exception | None = None
    try:
        prepared = _prepare_save_state(
            destination,
            model=model,
            optimizer=optimizer,
            training_state=training_state,
            identity=identity,
            data_position=data_position,
            world_size=world_size,
            device=device,
        )
    except Exception as error:
        preparation_error = error
    _finish_collective_stage(
        preparation_error,
        rank=rank,
        world_size=world_size,
        device=device,
        staging=destination,
        operation="prepare state components",
        cleanup=False,
    )
    if prepared is None:  # pragma: no cover - collective success proves preparation succeeded
        raise FailedPrecondition(
            "DCP preparation completed without state",
            reason="dcp_collective_stage",
        )

    equality_error: Exception | None = None
    try:
        _require_replicated_bytes(
            prepared.replica_document,
            device=device,
            world_size=world_size,
            description="control identity, model, and optimizer state",
        )
    except Exception as error:
        equality_error = error
    _finish_collective_stage(
        equality_error,
        rank=rank,
        world_size=world_size,
        device=device,
        staging=prepared.staging,
        operation="verify replicated state",
        cleanup=False,
    )

    artifacts: dict[str, ArtifactRef] | None = None
    artifact_error: Exception | None = None
    try:
        artifacts = _gather_expected_artifacts(
            prepared,
            world_size=world_size,
        )
    except Exception as error:
        artifact_error = error
    _finish_collective_stage(
        artifact_error,
        rank=rank,
        world_size=world_size,
        device=device,
        staging=prepared.staging,
        operation="gather expected artifact references",
        cleanup=False,
    )
    if artifacts is None:  # pragma: no cover - collective success proves gathering succeeded
        raise FailedPrecondition(
            "DCP artifact gathering completed without references",
            reason="dcp_collective_stage",
        )

    expected_manifest: DCPManifest | None = None
    expected_manifest_bytes: bytes | None = None
    manifest_error: Exception | None = None
    try:
        expected_manifest = DCPManifest(
            identity=identity,
            training_state=training_state,
            data_position=data_position,
            model_type=qualified_type(_base_model(model)),
            optimizer_type=qualified_type(optimizer),
            world_size=world_size,
            model_fqns=prepared.model_fqns,
            optimizer_fqns=prepared.optimizer_fqns,
            artifacts=artifacts,
        )
        expected_manifest_bytes = expected_manifest.encode()
    except Exception as error:
        manifest_error = error
    _finish_collective_stage(
        manifest_error,
        rank=rank,
        world_size=world_size,
        device=device,
        staging=prepared.staging,
        operation="materialize expected manifest",
        cleanup=False,
    )
    if expected_manifest is None or expected_manifest_bytes is None:  # pragma: no cover
        raise FailedPrecondition(
            "DCP manifest materialization completed without a manifest",
            reason="dcp_collective_stage",
        )

    setup_error: Exception | None = None
    if rank == 0:
        try:
            if os.path.lexists(prepared.destination) or os.path.lexists(prepared.staging):
                raise FailedPrecondition(
                    "DCP destination or staging directory already exists",
                    reason="dcp_destination_exists",
                )
            prepared.staging.mkdir(mode=0o700)
        except Exception as error:  # synchronized below before any rank writes
            setup_error = error
    _finish_collective_stage(
        setup_error,
        rank=rank,
        world_size=world_size,
        device=device,
        staging=prepared.staging,
        operation="create staging directory",
        cleanup=False,
    )

    write_error: Exception | None = None
    try:
        if rank == 0:
            _write_durable(prepared.staging / MODEL_DOCUMENT_PATH, prepared.model_document)
            _write_durable(prepared.staging / MODEL_TENSORS_PATH, prepared.model_tensors)
            _write_durable(
                prepared.staging / OPTIMIZER_DOCUMENT_PATH,
                prepared.optimizer_document,
            )
            _write_durable(
                prepared.staging / OPTIMIZER_TENSORS_PATH,
                prepared.optimizer_tensors,
            )
        _write_durable(
            prepared.staging / _rng_document_path(rank),
            prepared.rng_document,
        )
        _write_durable(
            prepared.staging / _rng_tensors_path(rank),
            prepared.rng_tensors,
        )
    except Exception as error:  # all ranks report before publication can continue
        write_error = error
    _finish_collective_stage(
        write_error,
        rank=rank,
        world_size=world_size,
        device=device,
        staging=prepared.staging,
        operation="write state components",
    )

    readback_error: Exception | None = None
    try:
        _verify_staged_writes(
            prepared.staging,
            artifacts=expected_manifest.artifacts,
            rank=rank,
        )
    except Exception as error:
        readback_error = error
    _finish_collective_stage(
        readback_error,
        rank=rank,
        world_size=world_size,
        device=device,
        staging=prepared.staging,
        operation="verify staged state components",
    )

    commit_error: Exception | None = None
    if rank == 0:
        try:
            _write_durable(prepared.staging / MANIFEST_PATH, expected_manifest_bytes)
            _fsync_directory(prepared.staging)
            os.replace(prepared.staging, prepared.destination)
            _fsync_directory(prepared.destination.parent)
        except Exception as error:  # peers must observe one collective outcome
            commit_error = error
    _finish_collective_stage(
        commit_error,
        rank=rank,
        world_size=world_size,
        device=device,
        staging=prepared.staging,
        operation="commit manifest",
        cleanup=False,
    )
    committed_manifest: DCPManifest | None = None
    verification_error: Exception | None = None
    try:
        committed_manifest = _verify_committed_save(
            prepared.destination,
            expected_manifest=expected_manifest,
            expected_manifest_bytes=expected_manifest_bytes,
            rank=rank,
            device=device,
        )
    except Exception as error:
        verification_error = error
    _finish_collective_stage(
        verification_error,
        rank=rank,
        world_size=world_size,
        device=device,
        staging=prepared.staging,
        operation="verify committed manifest",
        cleanup=False,
    )
    if committed_manifest is None:  # pragma: no cover - collective success proves decode succeeded
        raise FailedPrecondition(
            "DCP commit verification completed without a manifest",
            reason="dcp_commit_identity",
        )
    return committed_manifest


def _prepare_save_state(
    destination: Path,
    *,
    model: nn.Module,
    optimizer: torch.optim.Optimizer,
    training_state: TrainingState,
    identity: CheckpointIdentity,
    data_position: int,
    world_size: int,
    device: torch.device,
) -> _PreparedSave:
    """Perform every fallible rank-local save operation before any equality collective."""

    _validate_save_objects(model, optimizer, training_state, identity, data_position)
    resolved_destination, staging = _resolve_destination(destination)
    control_document = canonical_json_bytes(
        {
            "identity": identity.to_document(),
            "training_state": {
                "microbatches": training_state.microbatches,
                "optimizer_steps": training_state.optimizer_steps,
                "samples": training_state.samples,
            },
            "data_position": data_position,
            "model_type": qualified_type(_base_model(model)),
            "optimizer_type": qualified_type(optimizer),
            "world_size": world_size,
        }
    )
    model_state, optimizer_state = _canonical_state_dicts(model, optimizer)
    model_fqns = tuple(sorted(model_state))
    optimizer_fqns = _optimizer_state_fqns(optimizer_state)
    _validate_serialized_adamw_steps(
        optimizer_state,
        expected_parameter_count=len(optimizer_fqns),
        expected_optimizer_steps=training_state.optimizer_steps,
    )
    model_document, model_tensors = encode_state_component(model_state, component="model")
    optimizer_document, optimizer_tensors = encode_state_component(
        optimizer_state,
        component="optimizer",
    )
    cuda_rng = torch.cuda.get_rng_state(device) if device.type == "cuda" else None
    rng_document, rng_tensors = encode_rank_rng_state(torch.get_rng_state(), cuda_rng)
    shared_artifacts = MappingProxyType(
        {
            MODEL_DOCUMENT_PATH: _artifact_reference(MODEL_DOCUMENT_PATH, model_document),
            MODEL_TENSORS_PATH: _artifact_reference(MODEL_TENSORS_PATH, model_tensors),
            OPTIMIZER_DOCUMENT_PATH: _artifact_reference(
                OPTIMIZER_DOCUMENT_PATH,
                optimizer_document,
            ),
            OPTIMIZER_TENSORS_PATH: _artifact_reference(
                OPTIMIZER_TENSORS_PATH,
                optimizer_tensors,
            ),
        }
    )
    rng_document_reference = _artifact_reference(_rng_document_path(0), rng_document)
    rng_tensors_reference = _artifact_reference(_rng_tensors_path(0), rng_tensors)
    rng_reference_bytes = _encode_rng_reference_descriptor(
        rng_document_reference,
        rng_tensors_reference,
    )
    rng_reference_descriptor = torch.tensor(
        tuple(rng_reference_bytes),
        dtype=torch.uint8,
        device=device,
    )
    rng_reference_gather = [torch.empty_like(rng_reference_descriptor) for _ in range(world_size)]
    replica_document = canonical_json_bytes(
        {
            "control": Digest.of(control_document).text,
            "model_document": Digest.of(model_document).text,
            "model_tensors": Digest.of(model_tensors).text,
            "optimizer_document": Digest.of(optimizer_document).text,
            "optimizer_tensors": Digest.of(optimizer_tensors).text,
        }
    )
    return _PreparedSave(
        resolved_destination,
        staging,
        replica_document,
        model_document,
        model_tensors,
        optimizer_document,
        optimizer_tensors,
        rng_document,
        rng_tensors,
        model_fqns,
        optimizer_fqns,
        shared_artifacts,
        rng_reference_descriptor,
        rng_reference_gather,
    )


@dataclass(frozen=True, slots=True)
class _PreparedRestore:
    manifest: DCPManifest
    restore_control: bytes
    model_state: Mapping[str, object]
    optimizer_state: Mapping[str, object]
    torch_rng: torch.Tensor
    cuda_rng: torch.Tensor | None
    same_world: bool
    source_rank: int


def restore_distributed_checkpoint(
    source: Path,
    *,
    model: nn.Module,
    optimizer: torch.optim.Optimizer,
    expected_identity: CheckpointIdentity,
    allow_replicated_world_size_change: bool = False,
    expected_manifest_digest: Digest | None = None,
) -> DCPResumeResult:
    """Restore verified state into fresh objects, optionally changing DDP world size.

    Same-world restore includes rank-local CPU/CUDA RNG and is exact within the
    pinned runtime. A CPU/Gloo 1↔2 restore loads replicated model, AdamW,
    counters, and cursor only; RNG is deliberately left caller-seeded and
    ``exact_resume`` is false. This is not sharded-state resharding.
    """

    rank, world_size, device = _runtime_identity(model)
    prepared: _PreparedRestore | None = None
    preparation_error: Exception | None = None
    try:
        prepared = _prepare_restore_state(
            source,
            model=model,
            optimizer=optimizer,
            expected_identity=expected_identity,
            allow_replicated_world_size_change=allow_replicated_world_size_change,
            expected_manifest_digest=expected_manifest_digest,
            rank=rank,
            world_size=world_size,
            device=device,
        )
    except Exception as error:
        preparation_error = error
    _finish_collective_stage(
        preparation_error,
        rank=rank,
        world_size=world_size,
        device=device,
        staging=source,
        operation="prepare restore state",
        cleanup=False,
    )
    if prepared is None:  # pragma: no cover - collective success proves preparation succeeded
        raise FailedPrecondition(
            "DCP restore preparation completed without state",
            reason="dcp_collective_stage",
        )

    equality_error: Exception | None = None
    try:
        _require_replicated_bytes(
            prepared.restore_control,
            device=device,
            world_size=world_size,
            description="restore control identity",
        )
    except Exception as error:
        equality_error = error
    _finish_collective_stage(
        equality_error,
        rank=rank,
        world_size=world_size,
        device=device,
        staging=source,
        operation="verify replicated restore control",
        cleanup=False,
    )

    load_error: Exception | None = None
    try:
        incompatible = set_state_dict(
            model,
            optimizer,
            # The decoder has already enforced the bounded state-tree grammar;
            # PyTorch's recursive aliases cannot express that runtime proof.
            model_state_dict=cast(dict[str, Any], dict(prepared.model_state)),
            optim_state_dict=cast(dict[str, Any], dict(prepared.optimizer_state)),
            options=_load_state_dict_options(),
        )
        if incompatible.missing_keys or incompatible.unexpected_keys:
            raise RuntimeError("strict distributed state load returned incompatible keys")
    except (RuntimeError, ValueError, KeyError) as error:
        load_error = FailedPrecondition(
            "DCP state is incompatible; discard the partially loaded objects",
            reason="dcp_state_incompatible",
            cause=error,
        )
    _finish_collective_stage(
        load_error,
        rank=rank,
        world_size=world_size,
        device=device,
        staging=source,
        operation="load restore state",
        cleanup=False,
    )

    validation_error: Exception | None = None
    try:
        _validate_runtime_objects(
            model,
            optimizer,
            expected_optimizer_steps=prepared.manifest.training_state.optimizer_steps,
        )
    except (FloatingPointError, InvalidArgument) as error:
        validation_error = FailedPrecondition(
            "restored DCP state is invalid; discard the partially loaded objects",
            reason="dcp_state_incompatible",
            cause=error,
        )
    _finish_collective_stage(
        validation_error,
        rank=rank,
        world_size=world_size,
        device=device,
        staging=source,
        operation="validate restored state",
        cleanup=False,
    )

    rng_error: Exception | None = None
    try:
        if prepared.same_world:
            torch.set_rng_state(prepared.torch_rng)
            if prepared.cuda_rng is not None:
                torch.cuda.set_rng_state(prepared.cuda_rng, device=device)
    except (RuntimeError, ValueError) as error:
        rng_error = FailedPrecondition(
            "DCP RNG state is incompatible; discard the partially loaded objects",
            reason="dcp_rng_state",
            cause=error,
        )
    _finish_collective_stage(
        rng_error,
        rank=rank,
        world_size=world_size,
        device=device,
        staging=source,
        operation="restore rank RNG state",
        cleanup=False,
    )
    return DCPResumeResult(
        prepared.manifest,
        prepared.manifest.training_state,
        prepared.manifest.data_position,
        prepared.same_world,
        prepared.source_rank,
    )


def _prepare_restore_state(
    source: Path,
    *,
    model: nn.Module,
    optimizer: torch.optim.Optimizer,
    expected_identity: CheckpointIdentity,
    allow_replicated_world_size_change: bool,
    expected_manifest_digest: Digest | None,
    rank: int,
    world_size: int,
    device: torch.device,
) -> _PreparedRestore:
    """Verify and decode all rank-local restore inputs before any collective or mutation."""

    if not isinstance(allow_replicated_world_size_change, bool):
        raise InvalidArgument(
            "world-size portability flag must be boolean",
            reason="dcp_world_size_policy",
        )
    _validate_manifest_digest_argument(expected_manifest_digest)
    _validate_restore_objects(model, optimizer, expected_identity)
    root = _prevalidate_checkpoint_root(source)
    manifest = DCPManifest.decode(
        read_checkpoint_member(root, MANIFEST_PATH, maximum_bytes=MAXIMUM_MANIFEST_BYTES)
    )
    root = validate_committed_root(root, set(expected_dcp_artifact_paths(manifest.world_size)))
    if expected_manifest_digest is not None and not Digest.parse(manifest.digest).equals(
        expected_manifest_digest
    ):
        raise FailedPrecondition(
            "DCP manifest does not match the externally admitted digest",
            reason="dcp_manifest_digest_mismatch",
        )
    if manifest.identity != expected_identity:
        raise FailedPrecondition(
            "DCP identity does not match the requested source checkpoint",
            reason="dcp_identity_mismatch",
        )
    if manifest.model_type != qualified_type(
        _base_model(model)
    ) or manifest.optimizer_type != qualified_type(optimizer):
        raise FailedPrecondition(
            "DCP runtime types do not match fresh restore objects",
            reason="dcp_runtime_type_mismatch",
        )
    same_world = world_size == manifest.world_size
    if not same_world and {world_size, manifest.world_size} != {1, 2}:
        raise FailedPrecondition(
            "DCP world-size portability is qualified only between replicated worlds 1 and 2",
            reason="dcp_world_size_portability",
        )
    if not same_world and not allow_replicated_world_size_change:
        raise FailedPrecondition(
            "DCP world size differs; replicated portability must be requested explicitly",
            reason="dcp_world_size_mismatch",
        )
    source_rank = rank if same_world else 0

    verified: dict[str, bytes] = {}
    for path, reference in manifest.artifacts.items():
        maximum = (
            MAXIMUM_DCP_DOCUMENT_BYTES
            if path.endswith(".json")
            else MAXIMUM_DCP_TENSOR_ARCHIVE_BYTES
        )
        verified[path] = _verified_member(root, path, reference, maximum)
    decoded_model = decode_state_component(
        verified[MODEL_DOCUMENT_PATH],
        verified[MODEL_TENSORS_PATH],
        component="model",
    )
    decoded_optimizer = decode_state_component(
        verified[OPTIMIZER_DOCUMENT_PATH],
        verified[OPTIMIZER_TENSORS_PATH],
        component="optimizer",
    )
    rng = decode_rank_rng_state(
        verified[_rng_document_path(source_rank)],
        verified[_rng_tensors_path(source_rank)],
    )
    _validate_cross_world_device_portability(
        same_world=same_world,
        source_world_size=manifest.world_size,
        target_world_size=world_size,
        target_device_type=device.type,
        source_has_cuda_rng=rng.cuda_rng is not None,
    )
    if tuple(sorted(decoded_model.state)) != manifest.model_fqns:
        raise InvalidArgument(
            "DCP model FQNs do not match the committed manifest",
            reason="dcp_model_fqns",
        )
    if _optimizer_state_fqns(decoded_optimizer.state) != manifest.optimizer_fqns:
        raise InvalidArgument(
            "DCP optimizer FQNs do not match the committed manifest",
            reason="dcp_optimizer_fqns",
        )
    _validate_serialized_adamw_steps(
        decoded_optimizer.state,
        expected_parameter_count=len(manifest.optimizer_fqns),
        expected_optimizer_steps=manifest.training_state.optimizer_steps,
    )
    if same_world and ((device.type == "cuda") != (rng.cuda_rng is not None)):
        raise FailedPrecondition(
            "DCP RNG device contract does not match the restore topology",
            reason="dcp_rng_device",
        )
    restore_control = canonical_json_bytes(
        {
            "manifest_digest": manifest.digest,
            "expected_manifest_digest": (
                expected_manifest_digest.text if expected_manifest_digest is not None else None
            ),
            "expected_identity": expected_identity.to_document(),
            "allow_replicated_world_size_change": allow_replicated_world_size_change,
            "runtime_world_size": world_size,
            "model_type": qualified_type(_base_model(model)),
            "optimizer_type": qualified_type(optimizer),
        }
    )
    return _PreparedRestore(
        manifest,
        restore_control,
        decoded_model.state,
        decoded_optimizer.state,
        rng.torch_rng,
        rng.cuda_rng,
        same_world,
        source_rank,
    )


def _save_state_dict_options() -> StateDictOptions:
    # DDP state is already fully replicated. Requesting PyTorch's
    # ``full_state_dict`` path offloads output only on rank zero in the pinned
    # release, which is unsuitable for cross-rank equality verification.
    return StateDictOptions(
        full_state_dict=False,
        cpu_offload=False,
        strict=True,
        broadcast_from_rank0=False,
        flatten_optimizer_state_dict=False,
    )


def _load_state_dict_options() -> StateDictOptions:
    # The payload is already the canonical full state. Loading it as a local
    # DDP state also avoids PyTorch's full-state redistribution path, which in
    # the pinned release cannot infer a device for scalar-only optimizer state.
    return StateDictOptions(
        full_state_dict=False,
        cpu_offload=False,
        strict=True,
        broadcast_from_rank0=False,
        flatten_optimizer_state_dict=False,
    )


def _canonical_state_dicts(
    model: nn.Module,
    optimizer: torch.optim.Optimizer,
) -> tuple[dict[str, object], dict[str, object]]:
    model_state, optimizer_state = get_state_dict(
        model,
        optimizer,
        options=_save_state_dict_options(),
    )
    if (
        not isinstance(model_state, Mapping)
        or not model_state
        or any(not isinstance(key, str) for key in model_state)
        or not isinstance(optimizer_state, Mapping)
    ):
        raise FailedPrecondition(
            "PyTorch returned non-canonical distributed state",
            reason="dcp_canonical_state",
        )
    return dict(model_state), dict(optimizer_state)


def _optimizer_state_fqns(optimizer_state: Mapping[str, object]) -> tuple[str, ...]:
    state = optimizer_state.get("state")
    if (
        not isinstance(state, Mapping)
        or not state
        or any(not isinstance(key, str) for key in state)
    ):
        raise FailedPrecondition(
            "DCP v1 requires initialized optimizer state keyed by canonical FQN",
            reason="dcp_optimizer_state_fqns",
        )
    result = tuple(sorted(cast(str, key) for key in state))
    _validate_fqns(result, name="optimizer_fqns", require_nonempty=True)
    return result


def _runtime_identity(model: nn.Module) -> tuple[int, int, torch.device]:
    if not isinstance(model, nn.Module):
        raise InvalidArgument("DCP model must be an nn.Module", reason="dcp_objects")
    initialized = torch.distributed.is_available() and torch.distributed.is_initialized()
    rank = torch.distributed.get_rank() if initialized else 0
    world_size = torch.distributed.get_world_size() if initialized else 1
    _validate_world_size(world_size)
    if (world_size > 1) != isinstance(model, DistributedDataParallel):
        raise FailedPrecondition(
            "multi-rank DCP requires a DistributedDataParallel model",
            reason="dcp_distributed_model",
        )
    state = (*model.named_parameters(), *model.named_buffers())
    if not state:
        raise InvalidArgument("DCP model state is empty", reason="dcp_objects")
    device = state[0][1].device
    if device.type not in {"cpu", "cuda"}:
        raise InvalidArgument("DCP device must be CPU or CUDA", reason="dcp_device")
    supported_worlds = {1, 2} if device.type == "cpu" else {1, 8}
    if world_size not in supported_worlds:
        raise FailedPrecondition(
            "DCP runtime device and world size are outside the approved support matrix",
            reason="dcp_runtime_topology",
        )
    if initialized:
        expected_backend = "gloo" if device.type == "cpu" else "nccl"
        if torch.distributed.get_backend() != expected_backend:
            raise FailedPrecondition(
                "DCP process-group backend does not match the approved device topology",
                reason="dcp_runtime_topology",
            )
    return rank, world_size, device


def _validate_cross_world_device_portability(
    *,
    same_world: bool,
    source_world_size: int,
    target_world_size: int,
    target_device_type: str,
    source_has_cuda_rng: bool,
) -> None:
    """Restrict the qualified 1↔2 projection to CPU/Gloo checkpoints.

    Distributed-v1 always stores CUDA RNG state for a CUDA source and never
    stores it for a CPU source, so the decoded rank-zero RNG component is the
    bounded source-device discriminator for this local format.
    """

    if not same_world and (target_device_type != "cpu" or source_has_cuda_rng):
        raise FailedPrecondition(
            "DCP world-size portability requires CPU/Gloo source and target topologies",
            reason="dcp_world_size_portability_device",
            fields={
                "source_world_size": str(source_world_size),
                "target_world_size": str(target_world_size),
            },
        )


def _validate_save_objects(
    model: nn.Module,
    optimizer: torch.optim.Optimizer,
    training_state: TrainingState,
    identity: CheckpointIdentity,
    data_position: int,
) -> None:
    if not isinstance(training_state, TrainingState):
        raise InvalidArgument(
            "DCP training state must be TrainingState",
            reason="dcp_training_state",
        )
    _validate_runtime_objects(
        model,
        optimizer,
        expected_optimizer_steps=training_state.optimizer_steps,
    )
    if not optimizer.state:
        raise FailedPrecondition(
            "DCP v1 requires initialized AdamW-compatible optimizer tensor state",
            reason="dcp_optimizer_state_empty",
        )
    if training_state.optimizer_steps == 0:
        raise InvalidArgument(
            "DCP training state must contain committed optimizer progress",
            reason="dcp_training_state",
        )
    if not isinstance(identity, CheckpointIdentity):
        raise InvalidArgument("DCP identity is invalid", reason="dcp_identity")
    if (
        isinstance(data_position, bool)
        or not isinstance(data_position, int)
        or not 0 <= data_position <= MAXIMUM_DATA_POSITION
    ):
        raise InvalidArgument("DCP data position is outside bounds", reason="dcp_data_position")


def _validate_restore_objects(
    model: nn.Module,
    optimizer: torch.optim.Optimizer,
    expected_identity: CheckpointIdentity,
) -> None:
    _validate_runtime_objects(model, optimizer)
    if optimizer.state:
        raise FailedPrecondition(
            "DCP restore requires a fresh optimizer with no state",
            reason="dcp_restore_fresh_optimizer",
        )
    if any(parameter.grad is not None for parameter in model.parameters()):
        raise FailedPrecondition(
            "DCP restore requires a fresh model with no gradients",
            reason="dcp_restore_fresh_model",
        )
    if not isinstance(expected_identity, CheckpointIdentity):
        raise InvalidArgument("expected DCP identity is invalid", reason="dcp_identity")


def _validate_runtime_objects(
    model: object,
    optimizer: object,
    *,
    expected_optimizer_steps: int | None = None,
) -> None:
    if not isinstance(model, nn.Module) or not isinstance(optimizer, torch.optim.Optimizer):
        raise InvalidArgument(
            "DCP requires an nn.Module and torch optimizer",
            reason="dcp_objects",
        )
    if not isinstance(optimizer, torch.optim.AdamW):
        raise InvalidArgument(
            "DCP v1 supports AdamW optimizer state only",
            reason="dcp_optimizer_type",
        )
    for group in optimizer.param_groups:
        if (
            group.get("foreach") is not False
            or group.get("fused") not in {None, False}
            or group.get("capturable") is not False
            or group.get("differentiable") is not False
            or group.get("amsgrad") is not False
            or group.get("maximize") is not False
            or group.get("decoupled_weight_decay") is not True
        ):
            raise InvalidArgument(
                "DCP v1 requires the bounded non-foreach, non-fused AdamW execution mode",
                reason="dcp_optimizer_mode",
            )
    trainable = {id(parameter) for parameter in model.parameters() if parameter.requires_grad}
    optimized = [
        parameter for group in optimizer.param_groups for parameter in group.get("params", ())
    ]
    identities = {id(parameter) for parameter in optimized if isinstance(parameter, nn.Parameter)}
    if not trainable or len(identities) != len(optimized) or identities != trainable:
        raise InvalidArgument(
            "DCP optimizer must own every trainable parameter exactly once",
            reason="dcp_optimizer_ownership",
        )
    device: torch.device | None = None
    for name, tensor in (*model.named_parameters(), *model.named_buffers()):
        if tensor.device.type not in {"cpu", "cuda"}:
            raise InvalidArgument("DCP model device is unsupported", reason="dcp_device")
        if device is None:
            device = tensor.device
        elif tensor.device != device:
            raise InvalidArgument("DCP model state spans devices", reason="dcp_device")
        if tensor.is_floating_point() and tensor.dtype is not torch.float32:
            raise InvalidArgument(
                "DCP v1 requires float32 floating model state",
                reason="dcp_model_dtype",
                fields={"tensor": name},
            )
        if tensor.is_floating_point() and not bool(torch.isfinite(tensor.detach()).all().item()):
            raise FloatingPointError("DCP model state is not finite")
    _validate_adamw_state(
        optimizer,
        identities,
        expected_optimizer_steps=expected_optimizer_steps,
    )


def _validate_adamw_state(
    optimizer: torch.optim.AdamW,
    parameter_ids: set[int],
    *,
    expected_optimizer_steps: int | None,
) -> None:
    if not optimizer.state:
        return
    if len(optimizer.state) != len(parameter_ids):
        raise InvalidArgument(
            "DCP AdamW state must cover every optimized parameter exactly once",
            reason="dcp_optimizer_state",
        )
    if expected_optimizer_steps is not None:
        device_types = {
            parameter.device.type
            for parameter in optimizer.state
            if isinstance(parameter, nn.Parameter)
        }
        validate_adamw_steps(
            optimizer.state,
            expected_parameter_count=len(parameter_ids),
            expected_optimizer_steps=expected_optimizer_steps,
            allowed_device_types=frozenset({"cpu", *device_types}),
            reason="dcp_optimizer_step",
            description="distributed-v1 AdamW",
        )
    for parameter, state in optimizer.state.items():
        if (
            not isinstance(parameter, nn.Parameter)
            or id(parameter) not in parameter_ids
            or not isinstance(state, Mapping)
            or set(state) != {"step", "exp_avg", "exp_avg_sq"}
        ):
            raise InvalidArgument(
                "DCP AdamW state does not match the bounded tensor schema",
                reason="dcp_optimizer_state",
            )
        step = state["step"]
        first_moment = state["exp_avg"]
        second_moment = state["exp_avg_sq"]
        if (
            not isinstance(step, torch.Tensor)
            or step.ndim != 0
            or step.dtype is not torch.float32
            or step.device.type not in {"cpu", parameter.device.type}
            or not bool(torch.isfinite(step.detach()).item())
            or float(step.detach().item()) < 0.0
        ):
            raise InvalidArgument(
                "DCP AdamW step state is invalid",
                reason="dcp_optimizer_state",
            )
        for moment in (first_moment, second_moment):
            if (
                not isinstance(moment, torch.Tensor)
                or moment.layout is not torch.strided
                or moment.device != parameter.device
                or moment.dtype is not torch.float32
                or moment.shape != parameter.shape
                or not bool(torch.isfinite(moment.detach()).all().item())
            ):
                raise InvalidArgument(
                    "DCP AdamW moment state is invalid",
                    reason="dcp_optimizer_state",
                )


def _base_model(model: nn.Module) -> nn.Module:
    return model.module if isinstance(model, DistributedDataParallel) else model


def _require_replicated_bytes(
    value: bytes,
    *,
    device: torch.device,
    world_size: int,
    description: str,
) -> None:
    if world_size == 1:
        return
    digest = bytes.fromhex(Digest.of(value).hex)
    local = torch.tensor(tuple(digest), dtype=torch.uint8, device=device)
    minimum = local.clone()
    maximum = local.clone()
    torch.distributed.all_reduce(minimum, op=torch.distributed.ReduceOp.MIN)
    torch.distributed.all_reduce(maximum, op=torch.distributed.ReduceOp.MAX)
    if not torch.equal(minimum, maximum):
        raise FailedPrecondition(
            f"replicated DDP {description} differs across ranks",
            reason="dcp_replicated_state_mismatch",
        )


def _finish_collective_stage(
    error: Exception | None,
    *,
    rank: int,
    world_size: int,
    device: torch.device,
    staging: Path,
    operation: str,
    cleanup: bool = True,
) -> None:
    successful = error is None
    if world_size > 1:
        flag = torch.tensor(int(successful), dtype=torch.int32, device=device)
        torch.distributed.all_reduce(flag, op=torch.distributed.ReduceOp.MIN)
        successful = bool(flag.to(device="cpu").item())
    if successful:
        return

    cleanup_error: OSError | None = None
    if cleanup and rank == 0:
        try:
            if staging.is_dir() and not staging.is_symlink():
                shutil.rmtree(staging)
        except OSError as candidate:
            cleanup_error = candidate
    if world_size > 1:
        cleanup_flag = torch.tensor(int(cleanup_error is None), dtype=torch.int32, device=device)
        torch.distributed.all_reduce(cleanup_flag, op=torch.distributed.ReduceOp.MIN)
        cleanup_succeeded = bool(cleanup_flag.to(device="cpu").item())
    else:
        cleanup_succeeded = cleanup_error is None
    if not cleanup_succeeded:
        raise FailedPrecondition(
            f"DCP failed to clean staging after {operation}",
            reason="dcp_collective_cleanup",
            cause=cleanup_error,
        )
    if error is not None:
        raise error
    raise FailedPrecondition(
        f"another rank failed to {operation}",
        reason="dcp_collective_stage",
    )


def _validate_serialized_adamw_steps(
    optimizer_state: Mapping[str, object],
    *,
    expected_parameter_count: int,
    expected_optimizer_steps: int,
) -> None:
    validate_adamw_steps(
        optimizer_state.get("state"),
        expected_parameter_count=expected_parameter_count,
        expected_optimizer_steps=expected_optimizer_steps,
        allowed_device_types=frozenset({"cpu"}),
        reason="dcp_optimizer_step",
        description="decoded distributed-v1 AdamW",
    )


def _validate_manifest_digest_argument(value: Digest | None) -> None:
    if value is not None and not isinstance(value, Digest):
        raise InvalidArgument(
            "expected DCP manifest digest must be a Digest",
            reason="dcp_manifest_digest",
        )


def _artifact_identity(path: str) -> tuple[str, str]:
    if path == MODEL_DOCUMENT_PATH:
        return DCP_MODEL_DOCUMENT_MEDIA_TYPE, "training.checkpoint.dcp.model-metadata"
    if path == MODEL_TENSORS_PATH:
        return DCP_MODEL_TENSORS_MEDIA_TYPE, "training.checkpoint.dcp.model"
    if path == OPTIMIZER_DOCUMENT_PATH:
        return DCP_OPTIMIZER_DOCUMENT_MEDIA_TYPE, "training.checkpoint.dcp.optimizer-metadata"
    if path == OPTIMIZER_TENSORS_PATH:
        return DCP_OPTIMIZER_TENSORS_MEDIA_TYPE, "training.checkpoint.dcp.optimizer"
    if _RANK_MEMBER.fullmatch(path) is not None and path.endswith(".json"):
        return DCP_RNG_DOCUMENT_MEDIA_TYPE, "training.checkpoint.dcp.rng"
    if _RANK_MEMBER.fullmatch(path) is not None and path.endswith(".safetensors"):
        return DCP_RNG_TENSORS_MEDIA_TYPE, "training.checkpoint.dcp.rng-tensors"
    raise InvalidArgument("DCP artifact path is unsupported", reason="dcp_artifact_path")


def _artifact_reference(path: str, value: bytes) -> ArtifactRef:
    media_type, kind = _artifact_identity(path)
    return ArtifactRef(Digest.of(value), len(value), media_type, kind, 1)


def _encode_rng_reference_descriptor(
    document: ArtifactRef,
    tensors: ArtifactRef,
) -> bytes:
    result = b"".join(
        (
            document.digest.raw,
            document.size_bytes.to_bytes(8, byteorder="big", signed=False),
            tensors.digest.raw,
            tensors.size_bytes.to_bytes(8, byteorder="big", signed=False),
        )
    )
    if len(result) != _RNG_REFERENCE_DESCRIPTOR_BYTES:  # pragma: no cover - fixed-width proof
        raise FailedPrecondition(
            "DCP RNG artifact descriptor width is invalid",
            reason="dcp_artifact_descriptor",
        )
    return result


def _decode_rng_reference_descriptor(value: bytes, *, rank: int) -> dict[str, ArtifactRef]:
    if len(value) != _RNG_REFERENCE_DESCRIPTOR_BYTES:
        raise InvalidArgument(
            "DCP RNG artifact descriptor width is invalid",
            reason="dcp_artifact_descriptor",
        )
    document_digest = Digest(value[0:32])
    document_size = int.from_bytes(value[32:40], byteorder="big", signed=False)
    tensors_digest = Digest(value[40:72])
    tensors_size = int.from_bytes(value[72:80], byteorder="big", signed=False)
    document_path = _rng_document_path(rank)
    tensors_path = _rng_tensors_path(rank)
    document_media_type, document_kind = _artifact_identity(document_path)
    tensors_media_type, tensors_kind = _artifact_identity(tensors_path)
    return {
        document_path: ArtifactRef(
            document_digest,
            document_size,
            document_media_type,
            document_kind,
            1,
        ),
        tensors_path: ArtifactRef(
            tensors_digest,
            tensors_size,
            tensors_media_type,
            tensors_kind,
            1,
        ),
    }


def _tensor_descriptor_bytes(value: torch.Tensor) -> bytes:
    if (
        value.dtype is not torch.uint8
        or value.ndim != 1
        or value.numel() != _RNG_REFERENCE_DESCRIPTOR_BYTES
    ):
        raise InvalidArgument(
            "DCP gathered RNG artifact descriptor is invalid",
            reason="dcp_artifact_descriptor",
        )
    return bytes(value.detach().to(device="cpu").tolist())


def _gather_expected_artifacts(
    prepared: _PreparedSave,
    *,
    world_size: int,
) -> dict[str, ArtifactRef]:
    if world_size > 1:
        torch.distributed.all_gather(
            prepared.rng_reference_gather,
            prepared.rng_reference_descriptor,
        )
        descriptors = prepared.rng_reference_gather
    else:
        descriptors = [prepared.rng_reference_descriptor]
    result = dict(prepared.shared_artifacts)
    for rank, descriptor in enumerate(descriptors):
        result.update(
            _decode_rng_reference_descriptor(
                _tensor_descriptor_bytes(descriptor),
                rank=rank,
            )
        )
    return result


def _maximum_member_bytes(path: str) -> int:
    return (
        MAXIMUM_DCP_DOCUMENT_BYTES if path.endswith(".json") else MAXIMUM_DCP_TENSOR_ARCHIVE_BYTES
    )


def _verify_staged_writes(
    root: Path,
    *,
    artifacts: Mapping[str, ArtifactRef],
    rank: int,
) -> None:
    paths = {_rng_document_path(rank), _rng_tensors_path(rank)}
    if rank == 0:
        paths.update(
            {
                MODEL_DOCUMENT_PATH,
                MODEL_TENSORS_PATH,
                OPTIMIZER_DOCUMENT_PATH,
                OPTIMIZER_TENSORS_PATH,
            }
        )
    for path in sorted(paths):
        _verified_member(root, path, artifacts[path], _maximum_member_bytes(path))


def _verify_committed_save(
    root: Path,
    *,
    expected_manifest: DCPManifest,
    expected_manifest_bytes: bytes,
    rank: int,
    device: torch.device,
) -> DCPManifest:
    root = validate_committed_root(
        root,
        set(expected_dcp_artifact_paths(expected_manifest.world_size)),
    )
    manifest_bytes = read_checkpoint_member(
        root,
        MANIFEST_PATH,
        maximum_bytes=MAXIMUM_MANIFEST_BYTES,
    )
    manifest = DCPManifest.decode(manifest_bytes)
    if manifest_bytes != expected_manifest_bytes or manifest != expected_manifest:
        raise FailedPrecondition(
            "committed DCP manifest differs from prepared state",
            reason="dcp_commit_identity",
        )
    model_document = _verified_member(
        root,
        MODEL_DOCUMENT_PATH,
        manifest.artifacts[MODEL_DOCUMENT_PATH],
        MAXIMUM_DCP_DOCUMENT_BYTES,
    )
    model_tensors = _verified_member(
        root,
        MODEL_TENSORS_PATH,
        manifest.artifacts[MODEL_TENSORS_PATH],
        MAXIMUM_DCP_TENSOR_ARCHIVE_BYTES,
    )
    optimizer_document = _verified_member(
        root,
        OPTIMIZER_DOCUMENT_PATH,
        manifest.artifacts[OPTIMIZER_DOCUMENT_PATH],
        MAXIMUM_DCP_DOCUMENT_BYTES,
    )
    optimizer_tensors = _verified_member(
        root,
        OPTIMIZER_TENSORS_PATH,
        manifest.artifacts[OPTIMIZER_TENSORS_PATH],
        MAXIMUM_DCP_TENSOR_ARCHIVE_BYTES,
    )
    rng_document_path = _rng_document_path(rank)
    rng_tensors_path = _rng_tensors_path(rank)
    rng_document = _verified_member(
        root,
        rng_document_path,
        manifest.artifacts[rng_document_path],
        MAXIMUM_DCP_DOCUMENT_BYTES,
    )
    rng_tensors = _verified_member(
        root,
        rng_tensors_path,
        manifest.artifacts[rng_tensors_path],
        MAXIMUM_DCP_TENSOR_ARCHIVE_BYTES,
    )
    decoded_model = decode_state_component(model_document, model_tensors, component="model")
    decoded_optimizer = decode_state_component(
        optimizer_document,
        optimizer_tensors,
        component="optimizer",
    )
    rng = decode_rank_rng_state(rng_document, rng_tensors)
    if tuple(sorted(decoded_model.state)) != manifest.model_fqns:
        raise InvalidArgument(
            "committed DCP model FQNs differ from the manifest",
            reason="dcp_model_fqns",
        )
    if _optimizer_state_fqns(decoded_optimizer.state) != manifest.optimizer_fqns:
        raise InvalidArgument(
            "committed DCP optimizer FQNs differ from the manifest",
            reason="dcp_optimizer_fqns",
        )
    _validate_serialized_adamw_steps(
        decoded_optimizer.state,
        expected_parameter_count=len(manifest.optimizer_fqns),
        expected_optimizer_steps=manifest.training_state.optimizer_steps,
    )
    if (device.type == "cuda") != (rng.cuda_rng is not None):
        raise FailedPrecondition(
            "committed DCP RNG device contract differs from the save topology",
            reason="dcp_rng_device",
        )
    return manifest


def _verified_member(
    root: Path,
    path: str,
    reference: ArtifactRef,
    maximum_bytes: int,
) -> bytes:
    if reference.size_bytes <= 0 or reference.size_bytes > maximum_bytes:
        raise InvalidArgument(
            f"DCP member declared size is outside bounds: {path}",
            reason="dcp_member_size",
        )
    value = read_checkpoint_member(root, path, maximum_bytes=maximum_bytes)
    if len(value) != reference.size_bytes or not Digest.of(value).equals(reference.digest):
        raise InvalidArgument(
            f"DCP member failed content verification: {path}",
            reason="dcp_member_digest",
        )
    return value


def _resolve_destination(destination: Path) -> tuple[Path, Path]:
    if not isinstance(destination, Path) or not destination.name or destination.name in {".", ".."}:
        raise InvalidArgument("DCP destination must be a named Path", reason="dcp_destination")
    parent = destination.parent
    try:
        parent_stat = parent.lstat()
    except FileNotFoundError as error:
        raise InvalidArgument(
            "DCP destination parent does not exist",
            reason="dcp_destination_parent",
            cause=error,
        ) from error
    if stat.S_ISLNK(parent_stat.st_mode) or not stat.S_ISDIR(parent_stat.st_mode):
        raise InvalidArgument(
            "DCP destination parent must be a real directory",
            reason="dcp_destination_parent",
        )
    resolved_parent = parent.resolve()
    resolved = resolved_parent / destination.name
    return resolved, resolved_parent / f".{destination.name}.dcp-staging"


def _prevalidate_checkpoint_root(source: Path) -> Path:
    if not isinstance(source, Path):
        raise InvalidArgument("DCP source must be a Path", reason="dcp_root")
    try:
        root_stat = source.lstat()
    except FileNotFoundError as error:
        raise InvalidArgument(
            "DCP source does not exist", reason="dcp_root", cause=error
        ) from error
    if stat.S_ISLNK(root_stat.st_mode) or not stat.S_ISDIR(root_stat.st_mode):
        raise InvalidArgument("DCP source must be a real directory", reason="dcp_root")
    root = source.resolve()
    names = {path.name for path in root.iterdir()}
    if not names or len(names) > MAXIMUM_DCP_FILES + 1 or MANIFEST_PATH not in names:
        raise InvalidArgument("DCP source member set is invalid", reason="dcp_root_members")
    allowed = {
        MANIFEST_PATH,
        MODEL_DOCUMENT_PATH,
        MODEL_TENSORS_PATH,
        OPTIMIZER_DOCUMENT_PATH,
        OPTIMIZER_TENSORS_PATH,
    }
    if any(name not in allowed and _RANK_MEMBER.fullmatch(name) is None for name in names):
        raise InvalidArgument("DCP source contains an invalid member", reason="dcp_root_members")
    return root


def _write_durable(path: Path, value: bytes) -> None:
    if not value:
        raise InvalidArgument("DCP member bytes must be nonempty", reason="dcp_member_bytes")
    with path.open("xb") as handle:
        handle.write(value)
        handle.flush()
        os.fsync(handle.fileno())
    path.chmod(0o600)


def _fsync_directory(path: Path) -> None:
    descriptor = os.open(path, os.O_RDONLY | getattr(os, "O_DIRECTORY", 0))
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


def _validate_world_size(world_size: object) -> int:
    if (
        isinstance(world_size, bool)
        or not isinstance(world_size, int)
        or world_size not in SUPPORTED_DCP_WORLD_SIZES
    ):
        raise InvalidArgument("DCP world size is outside bounds", reason="dcp_world_size")
    return world_size


def _validate_fqns(values: object, *, name: str, require_nonempty: bool) -> None:
    if not isinstance(values, tuple) or (require_nonempty and not values):
        raise InvalidArgument(f"DCP {name} is invalid", reason="dcp_fqns")
    if tuple(sorted(set(values))) != values or any(
        not isinstance(value, str) or _FQN.fullmatch(value) is None for value in values
    ):
        raise InvalidArgument(f"DCP {name} must be sorted unique bounded names", reason="dcp_fqns")


def _unique_object(pairs: list[tuple[str, object]]) -> dict[str, object]:
    result: dict[str, object] = {}
    for key, value in pairs:
        if key in result:
            raise ValueError(f"duplicate JSON key {key!r}")
        result[key] = value
    return result


__all__ = [
    "DCPManifest",
    "DCPResumeResult",
    "expected_dcp_artifact_paths",
    "restore_distributed_checkpoint",
    "save_distributed_checkpoint",
]
