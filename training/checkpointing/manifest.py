# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Immutable manifest for the committed single-process reference checkpoint."""

from __future__ import annotations

import json
import re
from collections.abc import Mapping
from dataclasses import dataclass, field
from types import MappingProxyType
from typing import Final, Self

from libs.python.errors import InvalidArgument, ResourceExhausted
from libs.python.identifiers import ArtifactRef, Digest, ResourceId
from libs.python.serialization import canonical_json_bytes
from training.contracts import TrainingState

CHECKPOINT_SCHEMA_VERSION: Final = 1
MAXIMUM_MANIFEST_BYTES: Final = 1 << 20
MAXIMUM_DATA_POSITION: Final = (1 << 63) - 1
STATE_DOCUMENT_PATH: Final = "state.json"
STATE_TENSORS_PATH: Final = "state.safetensors"
EXPECTED_ARTIFACT_PATHS: Final = frozenset({STATE_DOCUMENT_PATH, STATE_TENSORS_PATH})

_TYPE_NAME = re.compile(r"[A-Za-z_][A-Za-z0-9_.]{0,510}[A-Za-z0-9_]$|[A-Za-z_]$")


@dataclass(frozen=True, slots=True)
class CheckpointIdentity:
    """Immutable identities a restore must match before touching live objects."""

    checkpoint_id: str
    run_id: str
    resolved_config_digest: str
    dataset_digest: str
    model_digest: str
    code_digest: str
    toolchain_digest: str
    topology_digest: str

    def __post_init__(self) -> None:
        checkpoint = ResourceId.parse(self.checkpoint_id)
        run = ResourceId.parse(self.run_id)
        if checkpoint.kind != "checkpoint" or run.kind != "run":
            raise InvalidArgument(
                "checkpoint and run identifiers have incorrect kinds",
                reason="checkpoint_identity_kind",
            )
        for name in (
            "resolved_config_digest",
            "dataset_digest",
            "model_digest",
            "code_digest",
            "toolchain_digest",
            "topology_digest",
        ):
            Digest.parse(getattr(self, name))

    def to_document(self) -> dict[str, str]:
        return {
            "checkpoint_id": self.checkpoint_id,
            "run_id": self.run_id,
            "resolved_config_digest": self.resolved_config_digest,
            "dataset_digest": self.dataset_digest,
            "model_digest": self.model_digest,
            "code_digest": self.code_digest,
            "toolchain_digest": self.toolchain_digest,
            "topology_digest": self.topology_digest,
        }

    @classmethod
    def from_document(cls, value: object) -> Self:
        expected = {
            "checkpoint_id",
            "run_id",
            "resolved_config_digest",
            "dataset_digest",
            "model_digest",
            "code_digest",
            "toolchain_digest",
            "topology_digest",
        }
        if not isinstance(value, dict) or set(value) != expected:
            raise InvalidArgument(
                "checkpoint identity fields do not match schema v1",
                reason="checkpoint_identity_fields",
            )
        if any(not isinstance(item, str) for item in value.values()):
            raise InvalidArgument(
                "checkpoint identity values must be strings",
                reason="checkpoint_identity_values",
            )
        return cls(**value)


@dataclass(frozen=True, slots=True)
class CheckpointManifest:
    """The complete record whose presence makes a local checkpoint restorable."""

    identity: CheckpointIdentity
    training_state: TrainingState
    data_position: int
    model_type: str
    optimizer_type: str
    artifacts: Mapping[str, ArtifactRef] = field(default_factory=dict)
    schema_version: int = CHECKPOINT_SCHEMA_VERSION
    world_size: int = 1

    def __post_init__(self) -> None:
        if not isinstance(self.identity, CheckpointIdentity):
            raise InvalidArgument(
                "checkpoint manifest identity is invalid",
                reason="checkpoint_manifest_identity",
            )
        if not isinstance(self.training_state, TrainingState):
            raise InvalidArgument(
                "checkpoint manifest training state is invalid",
                reason="checkpoint_manifest_training_state",
            )
        if (
            isinstance(self.data_position, bool)
            or not isinstance(self.data_position, int)
            or not 0 <= self.data_position <= MAXIMUM_DATA_POSITION
        ):
            raise InvalidArgument(
                "checkpoint data position is outside bounds",
                reason="checkpoint_data_position",
            )
        for value, label in (
            (self.model_type, "model_type"),
            (self.optimizer_type, "optimizer_type"),
        ):
            if not isinstance(value, str) or _TYPE_NAME.fullmatch(value) is None:
                raise InvalidArgument(
                    f"checkpoint {label} is not a bounded qualified type",
                    reason="checkpoint_runtime_type",
                )
        if (
            type(self.schema_version) is not int
            or self.schema_version != CHECKPOINT_SCHEMA_VERSION
            or type(self.world_size) is not int
            or self.world_size != 1
        ):
            raise InvalidArgument(
                "reference checkpoint requires schema version 1 and world size 1",
                reason="checkpoint_manifest_version",
            )
        if (
            not isinstance(self.artifacts, Mapping)
            or set(self.artifacts) != EXPECTED_ARTIFACT_PATHS
        ):
            raise InvalidArgument(
                "checkpoint artifacts do not match the reference schema",
                reason="checkpoint_manifest_artifacts",
            )
        frozen: dict[str, ArtifactRef] = {}
        for path, reference in self.artifacts.items():
            if not isinstance(path, str) or not isinstance(reference, ArtifactRef):
                raise InvalidArgument(
                    "checkpoint artifact mapping is invalid",
                    reason="checkpoint_manifest_artifacts",
                )
            frozen[path] = reference
        object.__setattr__(self, "artifacts", MappingProxyType(frozen))

    @property
    def digest(self) -> str:
        return Digest.of(self.encode()).text

    def encode(self) -> bytes:
        value = canonical_json_bytes(self.to_document())
        if len(value) > MAXIMUM_MANIFEST_BYTES:
            raise ResourceExhausted(
                "checkpoint manifest exceeds its byte bound",
                reason="checkpoint_manifest_size",
            )
        return value

    def to_document(self) -> dict[str, object]:
        return {
            "schema_version": self.schema_version,
            "world_size": self.world_size,
            "identity": self.identity.to_document(),
            "training_state": {
                "microbatches": self.training_state.microbatches,
                "optimizer_steps": self.training_state.optimizer_steps,
                "samples": self.training_state.samples,
            },
            "data_position": self.data_position,
            "model_type": self.model_type,
            "optimizer_type": self.optimizer_type,
            "artifacts": {
                path: reference.to_document() for path, reference in sorted(self.artifacts.items())
            },
        }

    @classmethod
    def decode(cls, value: bytes) -> Self:
        if not isinstance(value, bytes) or not value or len(value) > MAXIMUM_MANIFEST_BYTES:
            raise InvalidArgument(
                "checkpoint manifest bytes are outside bounds",
                reason="checkpoint_manifest_size",
            )
        try:
            document = json.loads(value, object_pairs_hook=_unique_object)
        except (UnicodeDecodeError, json.JSONDecodeError, ValueError) as error:
            raise InvalidArgument(
                "checkpoint manifest is not unique-key UTF-8 JSON",
                reason="checkpoint_manifest_json",
                cause=error,
            ) from error
        expected = {
            "schema_version",
            "world_size",
            "identity",
            "training_state",
            "data_position",
            "model_type",
            "optimizer_type",
            "artifacts",
        }
        if not isinstance(document, dict) or set(document) != expected:
            raise InvalidArgument(
                "checkpoint manifest fields do not match schema v1",
                reason="checkpoint_manifest_fields",
            )
        state = document["training_state"]
        if not isinstance(state, dict) or set(state) != {
            "microbatches",
            "optimizer_steps",
            "samples",
        }:
            raise InvalidArgument(
                "checkpoint training state fields do not match schema v1",
                reason="checkpoint_manifest_training_state",
            )
        artifacts = document["artifacts"]
        if not isinstance(artifacts, dict):
            raise InvalidArgument(
                "checkpoint artifact references must be an object",
                reason="checkpoint_manifest_artifacts",
            )
        return cls(
            identity=CheckpointIdentity.from_document(document["identity"]),
            training_state=TrainingState(**state),
            data_position=document["data_position"],
            model_type=document["model_type"],
            optimizer_type=document["optimizer_type"],
            artifacts={path: ArtifactRef.from_document(item) for path, item in artifacts.items()},
            schema_version=document["schema_version"],
            world_size=document["world_size"],
        )


def qualified_type(value: object) -> str:
    kind = type(value)
    result = f"{kind.__module__}.{kind.__qualname__}"
    if _TYPE_NAME.fullmatch(result) is None:
        raise InvalidArgument(
            "checkpoint object type is not a bounded qualified name",
            reason="checkpoint_runtime_type",
        )
    return result


def _unique_object(pairs: list[tuple[str, object]]) -> dict[str, object]:
    result: dict[str, object] = {}
    for key, item in pairs:
        if key in result:
            raise ValueError(f"duplicate JSON key {key!r}")
        result[key] = item
    return result
