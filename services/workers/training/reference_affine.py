# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Concrete bounded ``reference.affine.train.v1`` stage engine.

The affine workload is a conformance fixture, not a scientific model.  This module composes
the model-owned arithmetic, authoritative eager trainer, replicated DDP/DCP adapters, canonical
bundle builder, and an injected provider-neutral artifact boundary without copying any of their
semantics into the deployable layer.
"""

from __future__ import annotations

import json
import math
import os
import re
import shutil
from collections.abc import Mapping
from contextlib import suppress
from dataclasses import dataclass
from pathlib import Path
from types import MappingProxyType
from typing import Final, Literal, cast

import torch
from safetensors.torch import load as load_safetensors
from torch import nn

from libs.python.artifacts import VerifiedArtifactClient, reference_bytes
from libs.python.errors import (
    Canceled,
    DeadlineExceeded,
    FailedPrecondition,
    InvalidArgument,
    ResourceExhausted,
)
from libs.python.identifiers import ArtifactRef, Digest
from libs.python.serialization import canonical_json_bytes, validate_json_nesting
from libs.python.worker_runtime import ExecutionContext, StageEnvelope, StageKind, StageResult
from models.reference import (
    DEFAULT_MAXIMUM_INPUT_ELEMENTS,
    REFERENCE_AFFINE_DTYPE,
    REFERENCE_AFFINE_MODEL_NAME,
    REFERENCE_AFFINE_OPERATION,
    ReferenceAffine,
    ReferenceAffineConfig,
    load_reference_affine,
    reference_affine_config_bytes,
    save_reference_affine,
)
from tools.release.build_model_bundle import MANIFEST_MEDIA_TYPE
from tools.release.build_model_bundle import build as build_model_bundle
from training.checkpointing import (
    CheckpointIdentity,
    restore_distributed_checkpoint,
    save_distributed_checkpoint,
)
from training.contracts import SupervisedBatch, TrainingState
from training.contracts.state import MAXIMUM_PROGRESS_COUNTER
from training.core import Trainer, TrainerConfig
from training.distributed import (
    DistributedConfig,
    DistributedContext,
    distributed_session,
    shard_supervised_batch,
)
from training.distributed.communication import DDPReducer
from training.distributed.parallelism import wrap_ddp
from training.optim import AdamWConfig, build_optimizer
from training.runtime.telemetry.exporters import MLflowExporter
from training.tasks import SupervisedMSETask

from .artifacts import ArtifactIO
from .checkpoint_publication import (
    CHECKPOINT_COMMIT_LOGICAL_KIND,
    CHECKPOINT_COMMIT_MEDIA_TYPE,
    CHECKPOINT_MANIFEST_LOGICAL_KIND,
    CHECKPOINT_MANIFEST_MEDIA_TYPE,
    CheckpointCommitPlan,
    CheckpointCommitRequest,
    CheckpointCommitter,
    build_checkpoint_manifest,
    checkpoint_resume_binding_from_manifest,
    validate_checkpoint_plan,
    validate_checkpoint_resume_binding,
)
from .checkpoint_publication import (
    MAXIMUM_MANIFEST_BYTES as MAXIMUM_CHECKPOINT_MANIFEST_BYTES,
)
from .telemetry import MirrorIdentity, OptionalMLflowMirror

TRAINING_OPERATION: Final = "reference.affine.train.v1"
CONFIG_LOGICAL_KIND: Final = "training.resolved-config"
CONFIG_MEDIA_TYPE: Final = "application/vnd.mindclade.training.reference-affine-config.v1+json"
DATASET_LOGICAL_KIND: Final = "training.dataset"
DATASET_MEDIA_TYPE: Final = (
    "application/vnd.mindclade.training.reference-affine-dataset.v1+safetensors"
)
CHECKPOINT_LOGICAL_KIND: Final = CHECKPOINT_MANIFEST_LOGICAL_KIND
CHECKPOINT_MEDIA_TYPE: Final = CHECKPOINT_MANIFEST_MEDIA_TYPE
RUN_EVIDENCE_LOGICAL_KIND: Final = "training.run.evidence"
RUN_EVIDENCE_MEDIA_TYPE: Final = "application/vnd.mindclade.training.run-evidence.v1+json"
MODEL_BUNDLE_LOGICAL_KIND: Final = "model.bundle"

MAXIMUM_CONFIG_BYTES: Final = 64 << 10
MAXIMUM_DATASET_BYTES: Final = 64 << 20
MAXIMUM_OPTIMIZER_STEPS: Final = 100_000
MAXIMUM_MICROBATCH_SIZE: Final = 1_000_000
MAXIMUM_SEED: Final = (1 << 63) - 1
MAXIMUM_REFERENCE_WORKING_SET_BYTES: Final = 256 << 20
_FLOAT32_BYTES: Final = 4
_INT64_BYTES: Final = 8
_ACCUMULATED_ACTIVATION_COPIES: Final = 4
_BATCH_INDEX_BUFFER_COPIES: Final = 3
_DECODED_DATASET_PAIR_COPIES: Final = 2
_RESIDENT_DATASET_PAIR_COPIES: Final = 2

ReferenceBackend = Literal["gloo", "nccl"]
ReferenceDeviceType = Literal["cpu", "cuda"]

_CONFIG_FIELDS: Final = frozenset(
    {
        "accumulation_steps",
        "allow_replicated_world_size_change",
        "dtype",
        "engine",
        "gradient_clip_norm",
        "initial_bias",
        "initial_scale",
        "learning_rate",
        "maximum_input_elements",
        "maximum_optimizer_steps",
        "microbatch_size",
        "model",
        "model_operation",
        "optimizer_steps_per_execution",
        "schema_version",
        "seed",
        "weight_decay",
    }
)
_REQUIRED_METADATA: Final = frozenset(
    {
        "backend",
        "checkpoint_id",
        "code_digest",
        "compatibility_policy_digest",
        "device_type",
        "local_world_size",
        "model_digest",
        "run_id",
        "runtime_image_digest",
        "toolchain_digest",
        "topology_digest",
        "world_size",
    }
)
_OPTIONAL_METADATA: Final = frozenset(
    {
        "classification",
        "cohort_digest",
        "resume_checkpoint_id",
        "resume_topology_digest",
        "source_revision",
    }
)
_CANONICAL_DECIMAL: Final = re.compile(r"-?(?:0|[1-9][0-9]*)(?:\.[0-9]*[1-9])?")


def _unique_object(pairs: list[tuple[str, object]]) -> dict[str, object]:
    result: dict[str, object] = {}
    for key, value in pairs:
        if key in result:
            raise ValueError(f"duplicate JSON key {key!r}")
        result[key] = value
    return result


def _reject_constant(value: str) -> object:
    raise ValueError(f"non-finite JSON number {value}")


@dataclass(frozen=True, slots=True)
class ReferenceAffineTrainingConfig:
    """Immutable numerical policy for one reference-affine run."""

    maximum_optimizer_steps: int
    optimizer_steps_per_execution: int
    accumulation_steps: int
    microbatch_size: int
    learning_rate: float
    weight_decay: float
    gradient_clip_norm: float | None
    seed: int
    initial_scale: float
    initial_bias: float
    maximum_input_elements: int
    allow_replicated_world_size_change: bool = False

    @classmethod
    def decode(cls, value: bytes) -> ReferenceAffineTrainingConfig:
        if not isinstance(value, bytes) or not value or len(value) > MAXIMUM_CONFIG_BYTES:
            raise InvalidArgument(
                "reference-affine config bytes are outside bounds",
                reason="training_reference_config_size",
            )
        try:
            validate_json_nesting(value)
            document = json.loads(
                value,
                object_pairs_hook=_unique_object,
                parse_constant=_reject_constant,
            )
        except (UnicodeDecodeError, json.JSONDecodeError, RecursionError, ValueError) as error:
            raise InvalidArgument(
                "reference-affine config must be unique-key UTF-8 JSON",
                reason="training_reference_config_json",
                cause=error,
            ) from error
        if not isinstance(document, dict) or set(document) != _CONFIG_FIELDS:
            raise InvalidArgument(
                "reference-affine config fields do not match schema v1",
                reason="training_reference_config_fields",
            )
        try:
            canonical = canonical_json_bytes(
                document,
                maximum_encoded_bytes=MAXIMUM_CONFIG_BYTES,
            )
        except RecursionError as error:
            raise InvalidArgument(
                "reference-affine config exceeds the canonical JSON nesting limit",
                reason="training_reference_config_json",
                cause=error,
            ) from error
        if canonical != value:
            raise InvalidArgument(
                "reference-affine config must use canonical JSON bytes",
                reason="training_reference_config_canonical",
            )
        if type(document["schema_version"]) is not int or document["schema_version"] != 1:
            raise InvalidArgument(
                "reference-affine config schema version is unsupported",
                reason="training_reference_config_version",
            )
        exact = {
            "engine": TRAINING_OPERATION,
            "model": REFERENCE_AFFINE_MODEL_NAME,
            "model_operation": REFERENCE_AFFINE_OPERATION,
            "dtype": REFERENCE_AFFINE_DTYPE,
        }
        if any(document[key] != expected for key, expected in exact.items()):
            raise InvalidArgument(
                "reference-affine config selects an unsupported engine or model contract",
                reason="training_reference_config_contract",
            )
        return cls(
            maximum_optimizer_steps=_integer(
                document["maximum_optimizer_steps"],
                "maximum_optimizer_steps",
                maximum=MAXIMUM_OPTIMIZER_STEPS,
            ),
            optimizer_steps_per_execution=_integer(
                document["optimizer_steps_per_execution"],
                "optimizer_steps_per_execution",
                maximum=MAXIMUM_OPTIMIZER_STEPS,
            ),
            accumulation_steps=_integer(
                document["accumulation_steps"],
                "accumulation_steps",
                maximum=1_024,
            ),
            microbatch_size=_integer(
                document["microbatch_size"],
                "microbatch_size",
                maximum=MAXIMUM_MICROBATCH_SIZE,
            ),
            learning_rate=_text_number(
                document["learning_rate"],
                "learning_rate",
                minimum=0.0,
                maximum=10.0,
                minimum_inclusive=False,
            ),
            weight_decay=_text_number(
                document["weight_decay"],
                "weight_decay",
                minimum=0.0,
                maximum=1.0,
            ),
            gradient_clip_norm=_optional_positive_number(document["gradient_clip_norm"]),
            seed=_integer(document["seed"], "seed", maximum=MAXIMUM_SEED, minimum=0),
            initial_scale=_float32_text_number(document["initial_scale"], "initial_scale"),
            initial_bias=_float32_text_number(document["initial_bias"], "initial_bias"),
            maximum_input_elements=_integer(
                document["maximum_input_elements"],
                "maximum_input_elements",
                maximum=DEFAULT_MAXIMUM_INPUT_ELEMENTS,
            ),
            allow_replicated_world_size_change=_boolean(
                document["allow_replicated_world_size_change"],
                "allow_replicated_world_size_change",
            ),
        )

    def __post_init__(self) -> None:
        _integer(
            self.maximum_optimizer_steps,
            "maximum_optimizer_steps",
            maximum=MAXIMUM_OPTIMIZER_STEPS,
        )
        _integer(
            self.optimizer_steps_per_execution,
            "optimizer_steps_per_execution",
            maximum=MAXIMUM_OPTIMIZER_STEPS,
        )
        _integer(self.accumulation_steps, "accumulation_steps", maximum=1_024)
        _integer(self.microbatch_size, "microbatch_size", maximum=MAXIMUM_MICROBATCH_SIZE)
        _integer(self.seed, "seed", maximum=MAXIMUM_SEED, minimum=0)
        if not isinstance(self.allow_replicated_world_size_change, bool):
            raise InvalidArgument(
                "allow_replicated_world_size_change must be boolean",
                reason="training_reference_config_value",
            )
        if self.optimizer_steps_per_execution > self.maximum_optimizer_steps:
            raise InvalidArgument(
                "optimizer_steps_per_execution cannot exceed maximum_optimizer_steps",
                reason="training_reference_step_limit",
            )
        # Reuse authoritative optimizer/model validation after the strict wire parser.
        AdamWConfig(self.learning_rate, weight_decay=self.weight_decay)
        TrainerConfig(
            accumulation_steps=self.accumulation_steps,
            maximum_microbatches_per_call=self.accumulation_steps,
            gradient_clip_norm=self.gradient_clip_norm,
        )
        ReferenceAffineConfig(
            scale=self.initial_scale,
            bias=self.initial_bias,
            maximum_input_elements=self.maximum_input_elements,
        )

    def parameters(self) -> Mapping[str, object]:
        return MappingProxyType(
            {
                "accumulation_steps": self.accumulation_steps,
                "dtype": REFERENCE_AFFINE_DTYPE,
                "learning_rate": self.learning_rate,
                "microbatch_size": self.microbatch_size,
                "model": REFERENCE_AFFINE_MODEL_NAME,
                "optimizer": "adamw",
                "weight_decay": self.weight_decay,
            }
        )


@dataclass(frozen=True, slots=True)
class TrainingTopology:
    backend: ReferenceBackend
    device_type: ReferenceDeviceType
    world_size: int
    local_world_size: int
    topology_digest: str

    @classmethod
    def from_metadata(cls, metadata: Mapping[str, str]) -> TrainingTopology:
        backend_value = metadata["backend"]
        device_type_value = metadata["device_type"]
        world_size = _decimal(metadata["world_size"], "world_size", maximum=64)
        local_world_size = _decimal(
            metadata["local_world_size"],
            "local_world_size",
            maximum=64,
        )
        supported = {
            ("gloo", "cpu", 1),
            ("gloo", "cpu", 2),
            ("nccl", "cuda", 1),
            ("nccl", "cuda", 8),
        }
        if (backend_value, device_type_value, world_size) not in supported:
            raise InvalidArgument(
                "training topology is outside the closed reference support matrix",
                reason="training_reference_topology",
            )
        backend = cast(ReferenceBackend, backend_value)
        device_type = cast(ReferenceDeviceType, device_type_value)
        if local_world_size != world_size:
            raise InvalidArgument(
                "reference training supports one node with local_world_size equal to world_size",
                reason="training_reference_topology",
            )
        topology_digest = metadata["topology_digest"]
        Digest.parse(topology_digest)
        expected = reference_topology_digest(
            backend=backend,
            device_type=device_type,
            world_size=world_size,
            local_world_size=local_world_size,
        )
        if topology_digest != expected:
            raise InvalidArgument(
                "topology_digest does not identify the declared logical topology",
                reason="training_reference_topology_digest",
            )
        return cls(backend, device_type, world_size, local_world_size, topology_digest)


@dataclass(frozen=True, slots=True)
class TrainingIdentity:
    run_id: str
    checkpoint_id: str
    model_digest: str
    code_digest: str
    toolchain_digest: str
    topology: TrainingTopology
    resume_checkpoint_id: str | None
    resume_topology_digest: str | None
    source_revision: str | None
    runtime_image_digest: str
    compatibility_policy_digest: str
    classification: str
    cohort_digest: str | None

    @classmethod
    def from_stage(cls, stage: StageEnvelope) -> TrainingIdentity:
        keys = set(stage.metadata)
        if not keys >= _REQUIRED_METADATA or not keys <= _REQUIRED_METADATA | _OPTIONAL_METADATA:
            raise InvalidArgument(
                "reference-affine stage metadata fields do not match schema v1",
                reason="training_reference_metadata_fields",
            )
        topology = TrainingTopology.from_metadata(stage.metadata)
        for name in ("model_digest", "code_digest", "toolchain_digest"):
            Digest.parse(stage.metadata[name])
        resume_topology = stage.metadata.get("resume_topology_digest")
        if resume_topology is not None:
            Digest.parse(resume_topology)
        runtime_image = stage.metadata["runtime_image_digest"]
        Digest.parse(runtime_image)
        compatibility_policy = stage.metadata["compatibility_policy_digest"]
        Digest.parse(compatibility_policy)
        cohort = stage.metadata.get("cohort_digest")
        if cohort is not None:
            Digest.parse(cohort)
        classification = stage.metadata.get("classification", "internal")
        if classification not in {"public", "internal", "confidential", "restricted"}:
            raise InvalidArgument(
                "reference-affine data classification is invalid",
                reason="training_reference_classification",
            )
        source_revision = stage.metadata.get("source_revision")
        if source_revision is not None and (
            len(source_revision) not in {40, 64}
            or any(character not in "0123456789abcdef" for character in source_revision)
        ):
            raise InvalidArgument(
                "source_revision must be an exact lowercase Git object id",
                reason="training_reference_source_revision",
            )
        # CheckpointIdentity performs exact resource-kind validation without duplicating it here.
        _checkpoint_identity(
            checkpoint_id=stage.metadata["checkpoint_id"],
            run_id=stage.metadata["run_id"],
            resolved_config_digest=stage.resolved_config_digest,
            dataset_digest=Digest.of(b"").text,
            model_digest=stage.metadata["model_digest"],
            code_digest=stage.metadata["code_digest"],
            toolchain_digest=stage.metadata["toolchain_digest"],
            topology_digest=topology.topology_digest,
        )
        return cls(
            run_id=stage.metadata["run_id"],
            checkpoint_id=stage.metadata["checkpoint_id"],
            model_digest=stage.metadata["model_digest"],
            code_digest=stage.metadata["code_digest"],
            toolchain_digest=stage.metadata["toolchain_digest"],
            topology=topology,
            resume_checkpoint_id=stage.metadata.get("resume_checkpoint_id"),
            resume_topology_digest=resume_topology,
            source_revision=source_revision,
            runtime_image_digest=runtime_image,
            compatibility_policy_digest=compatibility_policy,
            classification=classification,
            cohort_digest=cohort,
        )


@dataclass(frozen=True, slots=True)
class ReferenceDataset:
    inputs: torch.Tensor
    targets: torch.Tensor

    @classmethod
    def decode(cls, value: bytes, *, maximum_input_elements: int) -> ReferenceDataset:
        if not isinstance(value, bytes) or not value or len(value) > MAXIMUM_DATASET_BYTES:
            raise InvalidArgument(
                "reference-affine dataset bytes are outside bounds",
                reason="training_reference_dataset_size",
            )
        try:
            tensors = load_safetensors(value)
        except Exception as error:
            raise InvalidArgument(
                "reference-affine dataset is not valid safetensors",
                reason="training_reference_dataset_format",
                cause=error,
            ) from error
        if set(tensors) != {"inputs", "targets"}:
            raise InvalidArgument(
                "reference-affine dataset must contain exactly inputs and targets",
                reason="training_reference_dataset_fields",
            )
        inputs = tensors["inputs"]
        targets = tensors["targets"]
        if (
            inputs.device.type != "cpu"
            or targets.device.type != "cpu"
            or inputs.dtype is not torch.float32
            or targets.dtype is not torch.float32
            or inputs.ndim == 0
            or inputs.shape != targets.shape
            or inputs.numel() == 0
        ):
            raise InvalidArgument(
                "reference-affine dataset tensors must be equal-shape nonempty CPU float32",
                reason="training_reference_dataset_contract",
            )
        if inputs.numel() > maximum_input_elements or targets.numel() > maximum_input_elements:
            raise ResourceExhausted(
                "reference-affine dataset exceeds maximum_input_elements",
                reason="training_reference_dataset_elements",
            )
        if not bool(torch.isfinite(inputs).all().item()) or not bool(
            torch.isfinite(targets).all().item()
        ):
            raise InvalidArgument(
                "reference-affine dataset must contain only finite values",
                reason="training_reference_dataset_nonfinite",
            )
        # Own the immutable in-process fixture rather than retaining storage backed by parser
        # internals that an adapter could unexpectedly reuse.
        return cls(inputs.contiguous().clone(), targets.contiguous().clone())

    @property
    def samples(self) -> int:
        return int(self.inputs.shape[0])


@dataclass(slots=True)
class _InterruptionProbe:
    """Nonthrowing local observation followed by a collective classification."""

    execution: ExecutionContext
    cancellation_seen: bool = False
    deadline_seen: bool = False
    clock_error: BaseException | None = None
    observed_millis: int | None = None

    def __call__(self) -> bool:
        # This callback runs immediately before Trainer reducer collectives. It must return on
        # every rank even when the local clock is faulty; raising here can strand peers in
        # DDPReducer.any_true.
        self.cancellation_seen = self.cancellation_seen or self.execution.cancellation.is_cancelled
        try:
            now = self.execution.current_millis()
        except BaseException as error:
            self.clock_error = self.clock_error or error
        else:
            self.observed_millis = now
            self.deadline_seen = self.deadline_seen or now >= self.execution.deadline_unix_millis
        return self.cancellation_seen or self.deadline_seen or self.clock_error is not None

    def raise_if_requested(
        self,
        *,
        distributed: DistributedContext | None,
        device: torch.device,
        operation: str,
        cause: Canceled | None = None,
    ) -> None:
        local: tuple[bool, bool, bool] = (
            self.cancellation_seen,
            self.deadline_seen,
            self.clock_error is not None,
        )
        if distributed is None:
            observed = local
        else:
            flags = torch.tensor(
                tuple(int(value) for value in local), dtype=torch.int32, device=device
            )
            torch.distributed.all_reduce(flags, op=torch.distributed.ReduceOp.MAX)
            values = flags.to(device="cpu").tolist()
            observed = (bool(values[0]), bool(values[1]), bool(values[2]))
        # Match ExecutionContext precedence on every rank: explicit cancellation wins a
        # simultaneous deadline, and either user interruption wins an invalid-clock diagnostic.
        if observed[0]:
            if cause is not None:
                raise cause
            raise Canceled(
                "stage execution was canceled",
                reason="stage_canceled",
                operation=operation,
            )
        if observed[1]:
            raise DeadlineExceeded(
                "stage deadline expired during numerical execution",
                reason="stage_deadline",
                operation=operation,
                cause=cause,
            )
        if observed[2]:
            raise FailedPrecondition(
                "stage clock failed on at least one distributed rank",
                reason="stage_clock",
                operation=operation,
                cause=self.clock_error,
            )


def reference_topology_digest(
    *,
    backend: str,
    device_type: str,
    world_size: int,
    local_world_size: int,
) -> str:
    """Return the canonical fingerprint used by the reference topology contract."""

    document = {
        "backend": backend,
        "data_parallel_size": world_size,
        "device_type": device_type,
        "local_world_size": local_world_size,
        "node_count": 1,
        "pipeline_parallel_size": 1,
        "tensor_parallel_size": 1,
        "world_size": world_size,
    }
    return Digest.of(canonical_json_bytes(document)).text


class ReferenceAffineTrainingEngine:
    """Single-owner stage engine for the closed reference training contract."""

    @property
    def owns_terminal_commit(self) -> bool:
        """Declare the exact executor semantics this engine's committer requires."""

        return True

    def __init__(
        self,
        artifact_io: ArtifactIO,
        *,
        workspace_root: Path,
        checkpoint_committer: CheckpointCommitter | None = None,
        mlflow_exporter: MLflowExporter | None = None,
        distributed_timeout_seconds: int = 300,
        environ: Mapping[str, str] | None = None,
    ) -> None:
        if not isinstance(artifact_io, ArtifactIO):
            raise InvalidArgument(
                "training artifact_io does not implement the required boundary",
                reason="training_reference_artifact_io",
            )
        if not isinstance(workspace_root, Path) or not workspace_root.is_absolute():
            raise InvalidArgument(
                "training workspace_root must be an absolute Path",
                reason="training_reference_workspace",
            )
        if workspace_root.is_symlink() or not workspace_root.is_dir():
            raise InvalidArgument(
                "training workspace_root must be an existing non-symlink directory",
                reason="training_reference_workspace",
            )
        DistributedConfig(timeout_seconds=distributed_timeout_seconds)
        if environ is not None and not isinstance(environ, Mapping):
            raise InvalidArgument(
                "training distributed environment must be a mapping",
                reason="training_reference_environment",
            )
        self._artifact_io = artifact_io
        if checkpoint_committer is not None and not isinstance(
            checkpoint_committer, CheckpointCommitter
        ):
            raise InvalidArgument(
                "training checkpoint_committer does not implement the canonical boundary",
                reason="training_reference_checkpoint_committer",
            )
        if mlflow_exporter is not None and not isinstance(mlflow_exporter, MLflowExporter):
            raise InvalidArgument(
                "training mlflow_exporter must implement the bounded exporter",
                reason="training_reference_mlflow_exporter",
            )
        if mlflow_exporter is not None and mlflow_exporter.required:
            raise InvalidArgument(
                "reference training accepts only an optional MLflow mirror",
                reason="training_reference_mlflow_required",
            )
        self._checkpoint_committer = checkpoint_committer
        self._workspace_root = workspace_root
        self._mlflow_exporter = mlflow_exporter
        self._distributed_timeout_seconds = distributed_timeout_seconds
        self._environ = MappingProxyType(dict(environ)) if environ is not None else None

    def execute(self, stage: StageEnvelope, context: ExecutionContext) -> StageResult:
        if not isinstance(stage, StageEnvelope) or stage.kind is not StageKind.TRAINING:
            raise InvalidArgument(
                "reference-affine engine requires a training StageEnvelope",
                reason="training_reference_stage",
            )
        if not isinstance(context, ExecutionContext):
            raise InvalidArgument(
                "reference-affine engine requires an ExecutionContext",
                reason="training_reference_context",
            )
        if stage.operation != TRAINING_OPERATION:
            raise InvalidArgument(
                f"reference-affine engine supports only {TRAINING_OPERATION}",
                reason="training_reference_operation",
            )
        if self._checkpoint_committer is None:
            raise FailedPrecondition(
                "reference training requires a canonical checkpoint committer",
                reason="training_reference_checkpoint_committer_missing",
            )
        context.checkpoint(operation=TRAINING_OPERATION)
        config_reference, dataset_reference, resume_reference = self._validate_inputs(stage)
        config_bytes = VerifiedArtifactClient(
            self._artifact_io,
            maximum_bytes=MAXIMUM_CONFIG_BYTES,
        ).read(
            config_reference,
            cancelled=lambda: context.cancellation.is_cancelled,
        )
        if config_reference.digest.text != stage.resolved_config_digest:
            raise FailedPrecondition(
                "config artifact does not match the admitted resolved_config_digest",
                reason="training_reference_config_identity",
            )
        config = ReferenceAffineTrainingConfig.decode(config_bytes)
        del config_bytes
        dataset_bytes = VerifiedArtifactClient(
            self._artifact_io,
            maximum_bytes=MAXIMUM_DATASET_BYTES,
        ).read(
            dataset_reference,
            cancelled=lambda: context.cancellation.is_cancelled,
        )
        dataset = ReferenceDataset.decode(
            dataset_bytes,
            maximum_input_elements=config.maximum_input_elements,
        )
        _validate_training_working_set(
            config,
            dataset,
            encoded_dataset_bytes=len(dataset_bytes),
        )
        # ReferenceDataset owns contiguous clones. Do not retain the encoded artifact while
        # allocating the device copy or global batches.
        del dataset_bytes
        identity = TrainingIdentity.from_stage(stage)
        if config.microbatch_size < identity.topology.world_size:
            raise InvalidArgument(
                "microbatch_size must assign at least one sample to every rank",
                reason="training_reference_microbatch_world",
            )
        if resume_reference is None:
            if (
                identity.resume_checkpoint_id is not None
                or identity.resume_topology_digest is not None
            ):
                raise InvalidArgument(
                    "resume metadata requires a resume checkpoint input",
                    reason="training_reference_resume_binding",
                )
        elif identity.resume_checkpoint_id is None:
            raise InvalidArgument(
                "resume checkpoint input requires resume_checkpoint_id metadata",
                reason="training_reference_resume_binding",
            )
        if self._mlflow_exporter is not None and identity.source_revision is None:
            raise InvalidArgument(
                "an MLflow mirror requires source_revision metadata",
                reason="training_reference_mlflow_identity",
            )

        if identity.topology.world_size == 1:
            self._validate_local_environment()
            device = self._local_device(identity.topology.device_type)
            return self._execute_rank(
                stage,
                context,
                config,
                dataset,
                dataset_reference,
                resume_reference,
                identity,
                device=device,
                distributed=None,
            )

        with distributed_session(
            DistributedConfig(
                backend=identity.topology.backend,
                timeout_seconds=self._distributed_timeout_seconds,
            ),
            environ=self._environ,
        ) as distributed:
            if (
                distributed.world_size != identity.topology.world_size
                or distributed.environment.local_world_size != identity.topology.local_world_size
            ):
                raise FailedPrecondition(
                    "active torchrun world does not match the admitted training topology",
                    reason="training_reference_topology_runtime",
                )
            return self._execute_rank(
                stage,
                context,
                config,
                dataset,
                dataset_reference,
                resume_reference,
                identity,
                device=distributed.device,
                distributed=distributed,
            )

    def _execute_rank(
        self,
        stage: StageEnvelope,
        execution: ExecutionContext,
        config: ReferenceAffineTrainingConfig,
        dataset: ReferenceDataset,
        dataset_reference: ArtifactRef,
        resume_reference: ArtifactRef | None,
        identity: TrainingIdentity,
        *,
        device: torch.device,
        distributed: DistributedContext | None,
    ) -> StageResult:
        rank = distributed.rank if distributed is not None else 0
        workspace = self._workspace_root / (
            f"{stage.stage_id}.a{stage.attempt}.f{stage.fencing_token}"
        )
        setup_error: Exception | None = None
        if rank == 0:
            try:
                os.mkdir(workspace, mode=0o700)
            except Exception as error:
                setup_error = error
        self._finish_rank_zero_stage(distributed, setup_error, "create the training workspace")

        mirror = OptionalMLflowMirror(self._mlflow_exporter if rank == 0 else None)
        try:
            torch.manual_seed(config.seed + rank)
            if device.type == "cuda":
                torch.cuda.manual_seed(config.seed + rank)
            base_model = ReferenceAffine(
                ReferenceAffineConfig(
                    scale=config.initial_scale,
                    bias=config.initial_bias,
                    maximum_input_elements=config.maximum_input_elements,
                )
            ).to(device)
            training_model: nn.Module = (
                wrap_ddp(base_model, distributed) if distributed is not None else base_model
            )
            optimizer = build_optimizer(
                training_model.parameters(),
                AdamWConfig(config.learning_rate, weight_decay=config.weight_decay),
            )
            state = TrainingState()
            data_position = 0
            resume_exact: bool | None = None
            resume_source_world_size: int | None = None
            resume_source_rank: int | None = None
            if resume_reference is not None:
                resume_manifest_bytes = VerifiedArtifactClient(
                    self._artifact_io,
                    maximum_bytes=MAXIMUM_CHECKPOINT_MANIFEST_BYTES,
                ).read(
                    resume_reference,
                    cancelled=lambda: execution.cancellation.is_cancelled,
                )
                resume_topology_digest = (
                    identity.resume_topology_digest or identity.topology.topology_digest
                )
                resume_binding = checkpoint_resume_binding_from_manifest(
                    resume_reference,
                    resume_manifest_bytes,
                    checkpoint_id=cast(str, identity.resume_checkpoint_id),
                    run_id=identity.run_id,
                    topology_digest=resume_topology_digest,
                )
                resume_root = self._validated_resume_root(
                    self._artifact_io.materialize_tree(resume_reference)
                )
                restored = restore_distributed_checkpoint(
                    resume_root,
                    model=training_model,
                    optimizer=optimizer,
                    expected_identity=_checkpoint_identity(
                        checkpoint_id=cast(str, identity.resume_checkpoint_id),
                        run_id=identity.run_id,
                        resolved_config_digest=stage.resolved_config_digest,
                        dataset_digest=dataset_reference.digest.text,
                        model_digest=identity.model_digest,
                        code_digest=identity.code_digest,
                        toolchain_digest=identity.toolchain_digest,
                        topology_digest=resume_topology_digest,
                    ),
                    allow_replicated_world_size_change=(config.allow_replicated_world_size_change),
                    expected_manifest_digest=resume_binding.adapter_manifest.digest,
                )
                validate_checkpoint_resume_binding(
                    resume_binding,
                    restored.manifest,
                    resolved_config=stage.inputs[0],
                    dataset=dataset_reference,
                    model_digest=identity.model_digest,
                    source_digest=identity.code_digest,
                    toolchain_digest=identity.toolchain_digest,
                    environment_digest=identity.runtime_image_digest,
                    compatibility_policy_digest=identity.compatibility_policy_digest,
                )
                state = restored.training_state
                data_position = restored.data_position
                resume_exact = restored.exact_resume
                resume_source_world_size = restored.manifest.world_size
                resume_source_rank = restored.source_rank
                if data_position != state.samples:
                    raise FailedPrecondition(
                        "reference-affine resume cursor does not match the sample counter",
                        reason="training_reference_resume_cursor",
                    )
                same_world = resume_source_world_size == identity.topology.world_size
                if resume_exact != same_world:
                    raise FailedPrecondition(
                        "reference-affine resume exactness contradicts its source topology",
                        reason="training_reference_resume_exactness",
                    )
            if state.optimizer_steps >= config.maximum_optimizer_steps:
                raise FailedPrecondition(
                    "resume checkpoint has already reached maximum_optimizer_steps",
                    reason="training_reference_already_complete",
                )

            trainer = Trainer(
                training_model,
                SupervisedMSETask(),
                optimizer,
                config=TrainerConfig(
                    accumulation_steps=config.accumulation_steps,
                    maximum_microbatches_per_call=config.accumulation_steps,
                    gradient_clip_norm=config.gradient_clip_norm,
                ),
                state=state,
                reducer=DDPReducer(distributed) if distributed is not None else None,
            )
            device_dataset = ReferenceDataset(
                dataset.inputs.to(device=device, non_blocking=False),
                dataset.targets.to(device=device, non_blocking=False),
            )
            if rank == 0:
                mirror.start(
                    MirrorIdentity(
                        run_id=identity.run_id,
                        resolved_config_digest=stage.resolved_config_digest,
                        source_revision=cast(str, identity.source_revision),
                        runtime_image_digest=identity.runtime_image_digest,
                        attempt=stage.attempt,
                        model_digest=identity.model_digest,
                        dataset=dataset_reference,
                        resume_checkpoint=resume_reference,
                        classification=identity.classification,
                    ),
                    config.parameters(),
                )

            target_step = min(
                config.maximum_optimizer_steps,
                state.optimizer_steps + config.optimizer_steps_per_execution,
            )
            last_loss = 0.0
            interruption = _InterruptionProbe(execution)
            while trainer.state.optimizer_steps < target_step:
                batches: list[SupervisedBatch] = []
                for _ in range(config.accumulation_steps):
                    batch = self._next_batch(
                        device_dataset,
                        data_position=data_position,
                        batch_size=config.microbatch_size,
                    )
                    if distributed is not None:
                        batch = shard_supervised_batch(
                            batch,
                            rank=distributed.rank,
                            world_size=distributed.world_size,
                            global_position=data_position,
                        ).batch
                    batches.append(batch)
                    if data_position > MAXIMUM_PROGRESS_COUNTER - config.microbatch_size:
                        raise ResourceExhausted(
                            "reference-affine data position counter exhausted",
                            reason="training_reference_data_position",
                        )
                    data_position += config.microbatch_size

                try:
                    results = trainer.train(
                        tuple(batches),
                        cancellation_check=interruption,
                    )
                except Canceled as error:
                    interruption.raise_if_requested(
                        distributed=distributed,
                        device=device,
                        operation=error.operation or TRAINING_OPERATION,
                        cause=error,
                    )
                    raise AssertionError("unreachable interruption classification") from error
                if len(results) != 1:
                    raise FailedPrecondition(
                        "authoritative trainer returned an unexpected optimizer-step count",
                        reason="training_reference_step_result",
                    )
                last = results[0]
                last_loss = last.mean_loss
                # A rank-zero-only clock read here can strand peers at the next Trainer
                # collective. Observe and classify every rank first, then use rank zero's
                # already-validated timestamp only for the optional mirror.
                interruption()
                interruption.raise_if_requested(
                    distributed=distributed,
                    device=device,
                    operation="reference_affine_step_telemetry",
                )
                if rank == 0:
                    mirror.log_step(
                        {"loss": last_loss},
                        step=last.state.optimizer_steps,
                        timestamp_millis=cast(int, interruption.observed_millis),
                    )

            interruption()
            interruption.raise_if_requested(
                distributed=distributed,
                device=device,
                operation="reference_affine_checkpoint",
            )
            if data_position != trainer.state.samples:
                raise FailedPrecondition(
                    "reference-affine data cursor diverged from the sample counter",
                    reason="training_reference_data_cursor",
                )
            checkpoint = workspace / "checkpoint"
            manifest = save_distributed_checkpoint(
                checkpoint,
                model=training_model,
                optimizer=optimizer,
                training_state=trainer.state,
                identity=_checkpoint_identity(
                    checkpoint_id=identity.checkpoint_id,
                    run_id=identity.run_id,
                    resolved_config_digest=stage.resolved_config_digest,
                    dataset_digest=dataset_reference.digest.text,
                    model_digest=identity.model_digest,
                    code_digest=identity.code_digest,
                    toolchain_digest=identity.toolchain_digest,
                    topology_digest=identity.topology.topology_digest,
                ),
                data_position=data_position,
            )

            # DCP can run long enough for a new cancellation, deadline, or rank-local clock
            # failure. Resolve it collectively before rank zero begins non-authoritative
            # checkpoint/output staging, and retain rank zero's validated time for the manifest.
            interruption()
            interruption.raise_if_requested(
                distributed=distributed,
                device=device,
                operation="reference_affine_checkpoint_stage",
            )
            created_at = cast(int, interruption.observed_millis)

            outputs: tuple[ArtifactRef, ...] = ()
            request: CheckpointCommitRequest | None = None
            plan: CheckpointCommitPlan | None = None
            staging_error: Exception | None = None
            if rank == 0:
                try:
                    request = CheckpointCommitRequest(
                        stage_id=stage.stage_id,
                        checkpoint_id=identity.checkpoint_id,
                        run_id=identity.run_id,
                        output_namespace=stage.output_namespace,
                        attempt=stage.attempt,
                        checkpoint_attempt=stage.attempt,
                        fencing_token=stage.fencing_token,
                        deadline_unix_millis=execution.deadline_unix_millis,
                        created_at_unix_millis=created_at,
                        resolved_config=stage.inputs[0],
                        dataset=dataset_reference,
                        parent_manifest=resume_reference,
                        model_digest=identity.model_digest,
                        source_digest=identity.code_digest,
                        toolchain_digest=identity.toolchain_digest,
                        environment_digest=identity.runtime_image_digest,
                        compatibility_policy_digest=identity.compatibility_policy_digest,
                        topology_digest=identity.topology.topology_digest,
                        dcp_root=checkpoint,
                        dcp_manifest=manifest,
                    )
                    committer = cast(CheckpointCommitter, self._checkpoint_committer)
                    provenance = committer.resolve_provenance(request)
                    canonical_manifest = build_checkpoint_manifest(
                        request,
                        provenance,
                        backend=identity.topology.backend,
                        device_type=identity.topology.device_type,
                        world_size=identity.topology.world_size,
                        local_world_size=identity.topology.local_world_size,
                    )
                    plan = committer.prepare(
                        request,
                        manifest_bytes=canonical_manifest,
                    )
                    validate_checkpoint_plan(
                        request,
                        manifest_bytes=canonical_manifest,
                        plan=plan,
                    )
                    outputs = self._publish_outputs(
                        stage,
                        base_model,
                        plan,
                        dataset_reference,
                        resume_reference,
                        identity,
                        trainer.state,
                        data_position,
                        last_loss,
                        mirror.failures,
                        workspace,
                        resume_exact=resume_exact,
                        resume_source_rank=resume_source_rank,
                        resume_source_world_size=resume_source_world_size,
                        reached_maximum=(
                            trainer.state.optimizer_steps == config.maximum_optimizer_steps
                        ),
                    )
                except Exception as error:
                    staging_error = error
            self._finish_rank_zero_stage(
                distributed,
                staging_error,
                "stage canonical training outputs",
            )

            # This is the final collective. Every rank contributes only nonthrowing local
            # interruption observations, then receives the same deterministic classification.
            # After it succeeds, rank zero alone crosses the external terminal-commit boundary;
            # no collective or deadline reclassification is permitted after that point.
            interruption()
            interruption.raise_if_requested(
                distributed=distributed,
                device=device,
                operation="reference_affine_terminal_commit",
            )
            metrics = {
                "data_position": float(data_position),
                "loss": last_loss,
                "microbatches": float(trainer.state.microbatches),
                "mirror_failures": float(mirror.failures),
                "optimizer_steps": float(trainer.state.optimizer_steps),
                "reached_maximum_optimizer_steps": float(
                    trainer.state.optimizer_steps == config.maximum_optimizer_steps
                ),
                "samples": float(trainer.state.samples),
                "world_size": float(identity.topology.world_size),
            }
            # Construct and validate the exact result before crossing the irreversible boundary.
            # The committer receives this same tuple and is responsible for atomically attesting it.
            result = StageResult(outputs, metrics)
            if rank == 0:
                if request is None or plan is None:
                    raise AssertionError("rank-zero checkpoint staging lost its commit state")
                committer = cast(CheckpointCommitter, self._checkpoint_committer)
                committer.commit(
                    request,
                    plan=plan,
                    stage_outputs=result.outputs,
                )
            # A successful return from commit is already verified and idempotently reconcilable.
            # Everything below is diagnostic-only and BaseException-contained so an accepted
            # terminal result cannot be reclassified into a retry.
            with suppress(BaseException):
                mirror.finish(status="FINISHED")
            if rank == 0:
                # Any retained attempt workspace is safe for the bounded scratch reaper. There is
                # deliberately no fallible result/status mutation after terminal acceptance.
                with suppress(BaseException):
                    self._remove_workspace(workspace)
            return result
        except BaseException as error:
            with suppress(BaseException):
                mirror.finish(
                    status=(
                        "KILLED" if isinstance(error, Canceled | DeadlineExceeded) else "FAILED"
                    )
                )
            raise

    def _validate_inputs(
        self,
        stage: StageEnvelope,
    ) -> tuple[ArtifactRef, ArtifactRef, ArtifactRef | None]:
        if len(stage.inputs) not in {2, 3}:
            raise InvalidArgument(
                "reference-affine training requires config, dataset, and optional resume inputs",
                reason="training_reference_input_count",
            )
        config = stage.inputs[0]
        dataset = stage.inputs[1]
        _require_artifact_contract(
            config,
            logical_kind=CONFIG_LOGICAL_KIND,
            media_type=CONFIG_MEDIA_TYPE,
        )
        _require_artifact_contract(
            dataset,
            logical_kind=DATASET_LOGICAL_KIND,
            media_type=DATASET_MEDIA_TYPE,
        )
        resume = stage.inputs[2] if len(stage.inputs) == 3 else None
        if resume is not None:
            _require_artifact_contract(
                resume,
                logical_kind=CHECKPOINT_LOGICAL_KIND,
                media_type=CHECKPOINT_MEDIA_TYPE,
            )
        return config, dataset, resume

    def _publish_outputs(
        self,
        stage: StageEnvelope,
        model: ReferenceAffine,
        checkpoint: CheckpointCommitPlan,
        dataset: ArtifactRef,
        resume: ArtifactRef | None,
        identity: TrainingIdentity,
        state: TrainingState,
        data_position: int,
        loss: float,
        mirror_failures: int,
        workspace: Path,
        *,
        resume_exact: bool | None,
        resume_source_rank: int | None,
        resume_source_world_size: int | None,
        reached_maximum: bool,
    ) -> tuple[ArtifactRef, ...]:
        export = workspace / "model-export"
        export.mkdir(mode=0o700)
        save_reference_affine(model, export / "model.safetensors")
        (export / "config.json").write_bytes(reference_affine_config_bytes(model.config))
        bundle = workspace / "model-bundle"
        bundle_manifest = build_model_bundle(
            export,
            bundle,
            REFERENCE_AFFINE_MODEL_NAME,
            schema_version=1,
        )
        bundle_reference = ArtifactRef(
            Digest.parse(bundle_manifest["digest"]),
            bundle_manifest["size_bytes"],
            MANIFEST_MEDIA_TYPE,
            MODEL_BUNDLE_LOGICAL_KIND,
            1,
        )
        published_bundle = self._artifact_io.publish_tree(
            namespace=stage.output_namespace,
            name="model-bundle",
            source=bundle,
            reference=bundle_reference,
        )
        _require_exact_publication(published_bundle, bundle_reference, "model bundle")

        evidence = canonical_json_bytes(
            {
                "attempt": stage.attempt,
                "checkpoint_commit": checkpoint.commit.to_document(),
                "checkpoint_manifest": checkpoint.manifest.to_document(),
                "cohort_digest": identity.cohort_digest,
                "counters": {
                    "microbatches": state.microbatches,
                    "optimizer_steps": state.optimizer_steps,
                    "samples": state.samples,
                },
                "data_position": data_position,
                "dataset": dataset.to_document(),
                "fencing_token": stage.fencing_token,
                # Hex text is the exact, locale-independent representation of the measured
                # binary64 value. Canonical identity JSON intentionally forbids JSON floats.
                "loss_float64_hex": loss.hex(),
                "mirror_failures": mirror_failures,
                "model_bundle": bundle_reference.to_document(),
                "reached_maximum_optimizer_steps": reached_maximum,
                "resume_checkpoint": resume.to_document() if resume is not None else None,
                "resume_exact": resume_exact,
                "resume_source_rank": resume_source_rank,
                "resume_source_world_size": resume_source_world_size,
                "resume_target_world_size": (
                    identity.topology.world_size if resume is not None else None
                ),
                "registry_record_digest": checkpoint.registry_record_digest,
                "run_id": identity.run_id,
                "schema_version": 1,
                "topology_digest": identity.topology.topology_digest,
                "world_size": identity.topology.world_size,
            }
        )
        evidence_reference = reference_bytes(
            evidence,
            media_type=RUN_EVIDENCE_MEDIA_TYPE,
            logical_kind=RUN_EVIDENCE_LOGICAL_KIND,
            maximum_bytes=MAXIMUM_CONFIG_BYTES,
        )
        published_evidence = self._artifact_io.publish_bytes(
            namespace=stage.output_namespace,
            name="run-evidence.json",
            content=evidence,
            reference=evidence_reference,
        )
        _require_exact_publication(published_evidence, evidence_reference, "run evidence")
        return checkpoint.manifest, checkpoint.commit, bundle_reference, evidence_reference

    def _next_batch(
        self,
        dataset: ReferenceDataset,
        *,
        data_position: int,
        batch_size: int,
    ) -> SupervisedBatch:
        start = data_position % dataset.samples
        indices = torch.arange(batch_size, dtype=torch.int64, device=dataset.inputs.device)
        indices = torch.remainder(indices + start, dataset.samples)
        return SupervisedBatch(
            dataset.inputs.index_select(0, indices),
            dataset.targets.index_select(0, indices),
        )

    def _validated_resume_root(self, value: object) -> Path:
        if not isinstance(value, Path) or not value.is_absolute():
            raise FailedPrecondition(
                "materialized checkpoint must be returned as an absolute Path",
                reason="training_reference_resume_path",
            )
        if value.is_symlink() or not value.is_dir():
            raise FailedPrecondition(
                "materialized checkpoint must be a non-symlink directory",
                reason="training_reference_resume_path",
            )
        return value

    @staticmethod
    def _remove_workspace(workspace: Path) -> None:
        shutil.rmtree(workspace)

    def _validate_local_environment(self) -> None:
        environment = os.environ if self._environ is None else self._environ
        world_size = environment.get("WORLD_SIZE")
        if world_size not in {None, "1"}:
            raise FailedPrecondition(
                "world-size-one training cannot run inside a multi-rank torchrun environment",
                reason="training_reference_topology_runtime",
            )
        if torch.distributed.is_available() and torch.distributed.is_initialized():
            raise FailedPrecondition(
                "world-size-one training refuses a foreign initialized process group",
                reason="training_reference_topology_runtime",
            )

    @staticmethod
    def _local_device(device_type: str) -> torch.device:
        if device_type == "cpu":
            return torch.device("cpu")
        if not torch.cuda.is_available() or torch.cuda.device_count() < 1:
            raise FailedPrecondition(
                "CUDA world-size-one training requested but no CUDA device is available",
                reason="training_reference_device_unavailable",
            )
        torch.cuda.set_device(0)
        return torch.device("cuda", 0)

    @staticmethod
    def _finish_rank_zero_stage(
        distributed: DistributedContext | None,
        error: Exception | None,
        operation: str,
    ) -> None:
        if distributed is None:
            if error is not None:
                raise error
            return
        successful = torch.tensor(int(error is None), dtype=torch.int32, device=distributed.device)
        torch.distributed.all_reduce(successful, op=torch.distributed.ReduceOp.MIN)
        if bool(successful.to(device="cpu").item()):
            return
        if error is not None:
            raise error
        raise FailedPrecondition(
            f"rank zero failed to {operation}",
            reason="training_reference_rank_zero_stage",
        )


def _validate_training_working_set(
    config: ReferenceAffineTrainingConfig,
    dataset: ReferenceDataset,
    *,
    encoded_dataset_bytes: int,
) -> None:
    """Reject composed tensor products before device copies or batch allocation.

    Accounting is deliberately conservative and topology-independent because `_next_batch`
    creates each global microbatch before rank sharding. Decode peak includes the bounded encoded
    artifact, parser tensors, and owned clones. Runtime peak includes decoded plus device copies of
    both dataset tensors, every accumulated input/target batch, four additional accumulated
    input-sized activation/autograd buffers, and the arange/add/remainder int64 index temporaries.
    """

    if not 0 < encoded_dataset_bytes <= MAXIMUM_DATASET_BYTES:
        raise ResourceExhausted(
            "reference-affine encoded dataset size is outside the closed byte budget",
            reason="training_reference_dataset_size",
        )
    per_sample_input_elements = dataset.inputs.numel() // dataset.samples
    microbatch_input_elements = per_sample_input_elements * config.microbatch_size
    if microbatch_input_elements > config.maximum_input_elements:
        raise ResourceExhausted(
            "reference-affine microbatch exceeds the model input-element budget",
            reason="training_reference_microbatch_elements",
        )
    accumulated_input_elements = microbatch_input_elements * config.accumulation_steps
    dataset_pair_elements = dataset.inputs.numel() + dataset.targets.numel()
    accumulated_pair_elements = accumulated_input_elements * 2
    dataset_pair_bytes = dataset_pair_elements * _FLOAT32_BYTES
    decode_peak_bytes = encoded_dataset_bytes + dataset_pair_bytes * _DECODED_DATASET_PAIR_COPIES
    runtime_peak_bytes = (
        dataset_pair_bytes * _RESIDENT_DATASET_PAIR_COPIES
        + accumulated_pair_elements * _FLOAT32_BYTES
        + accumulated_input_elements * _ACCUMULATED_ACTIVATION_COPIES * _FLOAT32_BYTES
        + config.microbatch_size * _BATCH_INDEX_BUFFER_COPIES * _INT64_BYTES
    )
    working_bytes = max(decode_peak_bytes, runtime_peak_bytes)
    if working_bytes > MAXIMUM_REFERENCE_WORKING_SET_BYTES:
        raise ResourceExhausted(
            "reference-affine composed working set exceeds the closed byte budget",
            reason="training_reference_working_set_bytes",
            fields={
                "maximum_bytes": str(MAXIMUM_REFERENCE_WORKING_SET_BYTES),
                "required_bytes": str(working_bytes),
            },
        )


def _checkpoint_identity(
    *,
    checkpoint_id: str,
    run_id: str,
    resolved_config_digest: str,
    dataset_digest: str,
    model_digest: str,
    code_digest: str,
    toolchain_digest: str,
    topology_digest: str,
) -> CheckpointIdentity:
    return CheckpointIdentity(
        checkpoint_id,
        run_id,
        resolved_config_digest,
        dataset_digest,
        model_digest,
        code_digest,
        toolchain_digest,
        topology_digest,
    )


def load_reference_affine_export(weights: Path) -> ReferenceAffine:
    """Load a worker-produced eager export through the model-owned strict loader."""

    return load_reference_affine(weights)


def _require_artifact_contract(
    reference: ArtifactRef,
    *,
    logical_kind: str,
    media_type: str,
) -> None:
    if (
        reference.logical_kind != logical_kind
        or reference.media_type != media_type
        or reference.schema_version != 1
        or reference.size_bytes == 0
    ):
        raise InvalidArgument(
            f"artifact must be {logical_kind} schema v1 with media type {media_type}",
            reason="training_reference_artifact_contract",
        )


def _require_exact_publication(
    actual: object,
    expected: ArtifactRef,
    name: str,
) -> None:
    if not isinstance(actual, ArtifactRef) or actual != expected:
        raise FailedPrecondition(
            f"artifact adapter returned a different {name} identity",
            reason="training_reference_publication_identity",
        )


def _boolean(value: object, name: str) -> bool:
    if not isinstance(value, bool):
        raise InvalidArgument(
            f"reference-affine config {name} must be boolean",
            reason="training_reference_config_value",
        )
    return value


def _integer(
    value: object,
    name: str,
    *,
    maximum: int,
    minimum: int = 1,
) -> int:
    if isinstance(value, bool) or not isinstance(value, int) or not minimum <= value <= maximum:
        raise InvalidArgument(
            f"reference-affine config {name} is outside bounds",
            reason="training_reference_config_value",
        )
    return value


def _decimal(value: object, name: str, *, maximum: int) -> int:
    if (
        not isinstance(value, str)
        or not value.isascii()
        or not value.isdecimal()
        or (len(value) > 1 and value.startswith("0"))
    ):
        raise InvalidArgument(
            f"training metadata {name} must be canonical decimal text",
            reason="training_reference_metadata_value",
        )
    return _integer(int(value), name, maximum=maximum)


def _number(
    value: object,
    name: str,
    *,
    minimum: float,
    maximum: float,
    minimum_inclusive: bool = True,
) -> float:
    if isinstance(value, bool) or not isinstance(value, int | float) or not math.isfinite(value):
        raise InvalidArgument(
            f"reference-affine config {name} must be finite",
            reason="training_reference_config_value",
        )
    normalized = float(value)
    valid_minimum = normalized >= minimum if minimum_inclusive else normalized > minimum
    if not valid_minimum or normalized > maximum:
        raise InvalidArgument(
            f"reference-affine config {name} is outside bounds",
            reason="training_reference_config_value",
        )
    return normalized


def _text_number(
    value: object,
    name: str,
    *,
    minimum: float,
    maximum: float,
    minimum_inclusive: bool = True,
) -> float:
    if not isinstance(value, str) or _CANONICAL_DECIMAL.fullmatch(value) is None or value == "-0":
        raise InvalidArgument(
            f"reference-affine config {name} must be canonical decimal text",
            reason="training_reference_config_value",
        )
    return _number(
        float(value),
        name,
        minimum=minimum,
        maximum=maximum,
        minimum_inclusive=minimum_inclusive,
    )


def _float32_text_number(value: object, name: str) -> float:
    maximum = torch.finfo(torch.float32).max
    return _text_number(value, name, minimum=-maximum, maximum=maximum)


def _optional_positive_number(value: object) -> float | None:
    if value is None:
        return None
    return _text_number(
        value,
        "gradient_clip_norm",
        minimum=0.0,
        maximum=1_000_000.0,
        minimum_inclusive=False,
    )


__all__ = [
    "CHECKPOINT_COMMIT_LOGICAL_KIND",
    "CHECKPOINT_COMMIT_MEDIA_TYPE",
    "CHECKPOINT_LOGICAL_KIND",
    "CHECKPOINT_MEDIA_TYPE",
    "CONFIG_LOGICAL_KIND",
    "CONFIG_MEDIA_TYPE",
    "DATASET_LOGICAL_KIND",
    "DATASET_MEDIA_TYPE",
    "MODEL_BUNDLE_LOGICAL_KIND",
    "RUN_EVIDENCE_LOGICAL_KIND",
    "RUN_EVIDENCE_MEDIA_TYPE",
    "TRAINING_OPERATION",
    "ReferenceAffineTrainingConfig",
    "ReferenceAffineTrainingEngine",
    "ReferenceDataset",
    "TrainingIdentity",
    "TrainingTopology",
    "load_reference_affine_export",
    "reference_topology_digest",
]
