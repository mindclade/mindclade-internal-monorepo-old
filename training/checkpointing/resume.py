# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Save and restore the bounded single-process reference training state."""

from __future__ import annotations

from collections.abc import Mapping
from dataclasses import dataclass
from pathlib import Path

import torch
from torch import nn

from libs.python.errors import FailedPrecondition, InvalidArgument, ResourceExhausted
from libs.python.identifiers import ArtifactRef, Digest
from training.contracts import TrainingState

from .adamw import validate_adamw_steps
from .atomic_commit import (
    MANIFEST_PATH,
    commit_checkpoint_directory,
    read_checkpoint_member,
    validate_committed_root,
)
from .manifest import (
    EXPECTED_ARTIFACT_PATHS,
    MAXIMUM_MANIFEST_BYTES,
    STATE_DOCUMENT_PATH,
    STATE_TENSORS_PATH,
    CheckpointIdentity,
    CheckpointManifest,
    qualified_type,
)
from .serialization import (
    MAXIMUM_TENSOR_BYTES,
    decode_training_state,
    encode_training_state,
)

STATE_DOCUMENT_MEDIA_TYPE = "application/vnd.mindclade.training-state.v1+json"
STATE_TENSORS_MEDIA_TYPE = "application/vnd.mindclade.training-state.v1+safetensors"
MAXIMUM_STATE_DOCUMENT_BYTES = 8 << 20


@dataclass(frozen=True, slots=True)
class ResumeResult:
    manifest: CheckpointManifest
    training_state: TrainingState
    data_position: int


def save_local_checkpoint(
    destination: Path,
    *,
    model: nn.Module,
    optimizer: torch.optim.Optimizer,
    training_state: TrainingState,
    identity: CheckpointIdentity,
    data_position: int,
) -> CheckpointManifest:
    """Atomically publish a complete local checkpoint without pickle-capable formats."""

    if not isinstance(training_state, TrainingState):
        raise InvalidArgument(
            "checkpoint training_state must be TrainingState",
            reason="checkpoint_training_state",
        )
    _validate_objects(
        model,
        optimizer,
        expected_optimizer_steps=training_state.optimizer_steps,
    )
    if not isinstance(identity, CheckpointIdentity):
        raise InvalidArgument(
            "checkpoint identity must be CheckpointIdentity",
            reason="checkpoint_identity",
        )
    metadata, tensors = encode_training_state(
        model.state_dict(),
        optimizer.state_dict(),
        torch.get_rng_state(),
    )
    if len(metadata) > MAXIMUM_STATE_DOCUMENT_BYTES:
        raise ResourceExhausted(
            "checkpoint state document exceeds the local reference bound",
            reason="checkpoint_state_document_size",
        )
    artifacts = {
        STATE_DOCUMENT_PATH: _artifact(
            metadata,
            media_type=STATE_DOCUMENT_MEDIA_TYPE,
            logical_kind="training.checkpoint.state",
        ),
        STATE_TENSORS_PATH: _artifact(
            tensors,
            media_type=STATE_TENSORS_MEDIA_TYPE,
            logical_kind="training.checkpoint.tensors",
        ),
    }
    manifest = CheckpointManifest(
        identity=identity,
        training_state=training_state,
        data_position=data_position,
        model_type=qualified_type(model),
        optimizer_type=qualified_type(optimizer),
        artifacts=artifacts,
    )
    commit_checkpoint_directory(
        destination,
        {STATE_DOCUMENT_PATH: metadata, STATE_TENSORS_PATH: tensors},
        manifest.encode(),
    )
    return manifest


def restore_local_checkpoint(
    source: Path,
    *,
    model: nn.Module,
    optimizer: torch.optim.Optimizer,
    expected_identity: CheckpointIdentity,
    expected_manifest_digest: Digest | None = None,
) -> ResumeResult:
    """Verify all bytes and identities, then restore into caller-provided fresh objects."""

    _validate_objects(model, optimizer)
    if not isinstance(expected_identity, CheckpointIdentity):
        raise InvalidArgument(
            "expected identity must be CheckpointIdentity",
            reason="checkpoint_identity",
        )
    _validate_manifest_digest_argument(expected_manifest_digest)
    if optimizer.state:
        raise FailedPrecondition(
            "checkpoint restore requires a fresh optimizer with no state",
            reason="checkpoint_restore_fresh_optimizer",
        )
    root = validate_committed_root(source, set(EXPECTED_ARTIFACT_PATHS))
    manifest_bytes = read_checkpoint_member(
        root,
        MANIFEST_PATH,
        maximum_bytes=MAXIMUM_MANIFEST_BYTES,
    )
    if expected_manifest_digest is not None and not Digest.of(manifest_bytes).equals(
        expected_manifest_digest
    ):
        raise FailedPrecondition(
            "checkpoint manifest does not match the externally admitted digest",
            reason="checkpoint_manifest_digest_mismatch",
        )
    manifest = CheckpointManifest.decode(manifest_bytes)
    if manifest.identity != expected_identity:
        raise FailedPrecondition(
            "checkpoint identity does not match requested run/config/data/model/code/toolchain/topology",
            reason="checkpoint_identity_mismatch",
        )
    if manifest.model_type != qualified_type(model) or manifest.optimizer_type != qualified_type(
        optimizer
    ):
        raise FailedPrecondition(
            "checkpoint runtime types do not match fresh restore objects",
            reason="checkpoint_runtime_type_mismatch",
        )

    metadata = _verified_member(root, STATE_DOCUMENT_PATH, manifest, MAXIMUM_STATE_DOCUMENT_BYTES)
    tensor_bytes = _verified_member(
        root,
        STATE_TENSORS_PATH,
        manifest,
        MAXIMUM_TENSOR_BYTES + (100 << 20),
    )
    decoded = decode_training_state(metadata, tensor_bytes)
    _validate_decoded_optimizer_state(
        decoded.optimizer,
        optimizer,
        parameter_count=_trainable_parameter_count(model),
        expected_optimizer_steps=manifest.training_state.optimizer_steps,
    )

    # Every fallible identity, integrity, parse, and compatibility check occurs above. Model
    # and optimizer load APIs are still not transactional, hence the fresh-object requirement.
    try:
        incompatible = model.load_state_dict(decoded.model, strict=True)
        if incompatible.missing_keys or incompatible.unexpected_keys:
            raise RuntimeError("strict model state load returned incompatible keys")
        optimizer.load_state_dict(dict(decoded.optimizer))
    except (RuntimeError, ValueError, KeyError) as error:
        raise FailedPrecondition(
            "checkpoint state is incompatible; discard the partially loaded objects",
            reason="checkpoint_state_incompatible",
            cause=error,
        ) from error
    try:
        _validate_objects(
            model,
            optimizer,
            expected_optimizer_steps=manifest.training_state.optimizer_steps,
        )
    except (FloatingPointError, InvalidArgument) as error:
        raise FailedPrecondition(
            "restored checkpoint state is invalid; discard the partially loaded objects",
            reason="checkpoint_state_incompatible",
            cause=error,
        ) from error
    torch.set_rng_state(decoded.torch_rng)
    return ResumeResult(manifest, manifest.training_state, manifest.data_position)


def _artifact(value: bytes, *, media_type: str, logical_kind: str) -> ArtifactRef:
    return ArtifactRef(
        digest=Digest.of(value),
        size_bytes=len(value),
        media_type=media_type,
        logical_kind=logical_kind,
        schema_version=1,
    )


def _verified_member(
    root: Path,
    path: str,
    manifest: CheckpointManifest,
    maximum_bytes: int,
) -> bytes:
    reference = manifest.artifacts[path]
    if reference.size_bytes <= 0 or reference.size_bytes > maximum_bytes:
        raise InvalidArgument(
            f"checkpoint member declared size is outside bounds: {path}",
            reason="checkpoint_member_size",
        )
    value = read_checkpoint_member(root, path, maximum_bytes=maximum_bytes)
    actual = Digest.of(value)
    if len(value) != reference.size_bytes or not actual.equals(reference.digest):
        raise InvalidArgument(
            f"checkpoint member failed content verification: {path}",
            reason="checkpoint_member_digest",
        )
    return value


def _validate_objects(
    model: object,
    optimizer: object,
    *,
    expected_optimizer_steps: int | None = None,
) -> None:
    if not isinstance(model, nn.Module) or not isinstance(optimizer, torch.optim.Optimizer):
        raise InvalidArgument(
            "checkpoint requires an nn.Module and torch optimizer",
            reason="checkpoint_objects",
        )
    trainable = {id(parameter) for parameter in model.parameters() if parameter.requires_grad}
    optimized: list[nn.Parameter] = []
    for group in optimizer.param_groups:
        optimized.extend(group.get("params", ()))
    identities = {id(parameter) for parameter in optimized if isinstance(parameter, nn.Parameter)}
    if len(identities) != len(optimized) or identities != trainable:
        raise InvalidArgument(
            "checkpoint optimizer must own every trainable model parameter exactly once",
            reason="checkpoint_optimizer_ownership",
        )
    if isinstance(optimizer, torch.optim.AdamW) and expected_optimizer_steps is not None:
        validate_adamw_steps(
            optimizer.state,
            expected_parameter_count=len(trainable),
            expected_optimizer_steps=expected_optimizer_steps,
            allowed_device_types=frozenset({"cpu"}),
            reason="checkpoint_adamw_step",
            description="local-v1 AdamW",
        )
    for _, tensor in (*model.named_parameters(), *model.named_buffers()):
        if tensor.device.type != "cpu" or (
            tensor.is_floating_point() and tensor.dtype is not torch.float32
        ):
            raise InvalidArgument(
                "local reference checkpoint supports CPU float32 model state only",
                reason="checkpoint_parameter_placement",
            )
        if tensor.is_floating_point() and not bool(torch.isfinite(tensor.detach()).all().item()):
            raise FloatingPointError("checkpoint model state is not finite")


def _trainable_parameter_count(model: nn.Module) -> int:
    return sum(parameter.requires_grad for parameter in model.parameters())


def _validate_decoded_optimizer_state(
    state_dict: Mapping[str, object],
    optimizer: torch.optim.Optimizer,
    *,
    parameter_count: int,
    expected_optimizer_steps: int,
) -> None:
    if not isinstance(optimizer, torch.optim.AdamW):
        return
    states = state_dict.get("state")
    validate_adamw_steps(
        states,
        expected_parameter_count=parameter_count,
        expected_optimizer_steps=expected_optimizer_steps,
        allowed_device_types=frozenset({"cpu"}),
        reason="checkpoint_adamw_step",
        description="decoded local-v1 AdamW",
    )


def _validate_manifest_digest_argument(value: Digest | None) -> None:
    if value is not None and not isinstance(value, Digest):
        raise InvalidArgument(
            "expected checkpoint manifest digest must be a Digest",
            reason="checkpoint_manifest_digest",
        )
