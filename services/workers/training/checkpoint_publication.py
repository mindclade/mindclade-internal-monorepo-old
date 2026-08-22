# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Canonical checkpoint handoff between Python semantics, Rust bytes, and Go registry.

Python supplies a deterministic semantic manifest over the bounded DCP components. An injected
committer resolves already-admitted provenance and may verify/stage components, but only its final
call may atomically publish the manifest last, register the checkpoint, bind the exact stage
outputs and succeeded status, and accept the stage fence/deadline. Merely writing the adapter-local
DCP tree or returning a prepared record is never a committed checkpoint.
"""

from __future__ import annotations

import re
from collections.abc import Mapping
from dataclasses import dataclass
from pathlib import Path
from types import MappingProxyType
from typing import Final, Protocol, cast, runtime_checkable

from mindclade.artifact.v1 import checkpoint_pb2 as artifact_checkpoint_pb2
from mindclade.registry.v1 import checkpoint_pb2 as registry_checkpoint_pb2
from mindclade.training.v1 import checkpoint_pb2 as training_checkpoint_pb2
from mindclade.training.v1 import topology_pb2

from libs.python.artifacts import reference_bytes, verify_bytes
from libs.python.errors import FailedPrecondition, InvalidArgument, ResourceExhausted
from libs.python.identifiers import ArtifactRef, Digest, ResourceId
from libs.python.serialization import canonical_json_bytes
from training.checkpointing import DCPManifest

CHECKPOINT_MANIFEST_LOGICAL_KIND: Final = "training.checkpoint.manifest"
CHECKPOINT_MANIFEST_MEDIA_TYPE: Final = (
    "application/vnd.mindclade.training-checkpoint-manifest.v1+proto"
)
CHECKPOINT_COMMIT_LOGICAL_KIND: Final = "artifact.checkpoint.commit"
CHECKPOINT_COMMIT_MEDIA_TYPE: Final = (
    "application/vnd.mindclade.artifact-checkpoint-commit.v1+proto"
)
DCP_MANIFEST_LOGICAL_KIND: Final = "training.checkpoint.dcp-manifest"
DCP_MANIFEST_MEDIA_TYPE: Final = "application/vnd.mindclade.distributed-checkpoint.v1+json"
DCP_MANIFEST_PATH: Final = "manifest.json"

MODEL_LOGICAL_KIND: Final = "training.model"
SOURCE_LOGICAL_KIND: Final = "source.archive"
TOOLCHAIN_LOGICAL_KIND: Final = "toolchain.manifest"
ENVIRONMENT_LOGICAL_KIND: Final = "training.environment"
COMPATIBILITY_POLICY_LOGICAL_KIND: Final = "training.checkpoint.compatibility-policy"

MAXIMUM_MANIFEST_BYTES: Final = 4 << 20
MAXIMUM_COMMIT_BYTES: Final = 1 << 20
MAXIMUM_REGISTRY_RECORD_BYTES: Final = 1 << 20
MAXIMUM_WRITER_ID_LENGTH: Final = 256
_RANK_RNG_PATH: Final = re.compile(r"rank-([0-9]{5})\.rng\.(?:json|safetensors)")
_REGISTRY_DOCUMENT: Final = "checkpoint-registry-record/v1"


class _CanonicalProto(Protocol):
    def ParseFromString(self, value: bytes) -> int: ...

    def CopyFrom(self, other: object) -> None: ...

    def DiscardUnknownFields(self) -> None: ...

    def SerializeToString(self, *, deterministic: bool) -> bytes: ...


class _ArtifactRefProto(Protocol):
    digest: str
    size_bytes: int
    media_type: str
    logical_kind: str
    schema_version: int


class _TimestampProto(Protocol):
    seconds: int
    nanos: int


@dataclass(frozen=True, slots=True)
class CheckpointProvenance:
    """Artifact-catalog identities needed by the canonical semantic manifest."""

    model: ArtifactRef
    source: ArtifactRef
    toolchain: ArtifactRef
    environment: ArtifactRef
    compatibility_policy: ArtifactRef

    def __post_init__(self) -> None:
        for name in (
            "model",
            "source",
            "toolchain",
            "environment",
            "compatibility_policy",
        ):
            if not isinstance(getattr(self, name), ArtifactRef):
                raise InvalidArgument(
                    "checkpoint provenance must contain ArtifactRef values",
                    reason="training_checkpoint_provenance",
                )


@dataclass(frozen=True, slots=True)
class CheckpointResumeComponent:
    """Immutable semantic component extracted from an admitted outer manifest."""

    artifact: ArtifactRef
    kind: int
    rank: int
    tensor_layout: int
    tensor_fqns: tuple[str, ...]


@dataclass(frozen=True, slots=True)
class CheckpointResumeBinding:
    """Strict outer-manifest facts retained for post-restore semantic comparison."""

    adapter_manifest: ArtifactRef
    components: Mapping[str, CheckpointResumeComponent]
    counters: tuple[int, int, int]
    data_position: int
    resolved_config: ArtifactRef
    dataset: ArtifactRef
    model: ArtifactRef
    source: ArtifactRef
    toolchain: ArtifactRef
    environment: ArtifactRef
    compatibility_policy: ArtifactRef
    topology_digest: str
    world_size: int

    def __post_init__(self) -> None:
        if not isinstance(self.components, Mapping):
            raise InvalidArgument(
                "checkpoint resume components must be a mapping",
                reason="training_checkpoint_resume_manifest",
            )
        object.__setattr__(self, "components", MappingProxyType(dict(self.components)))


@dataclass(frozen=True, slots=True)
class CheckpointCommitRequest:
    """Bounded semantic and fencing input to one manifest-last commit."""

    stage_id: str
    checkpoint_id: str
    run_id: str
    output_namespace: str
    attempt: int
    checkpoint_attempt: int
    fencing_token: int
    deadline_unix_millis: int
    created_at_unix_millis: int
    resolved_config: ArtifactRef
    dataset: ArtifactRef
    parent_manifest: ArtifactRef | None
    model_digest: str
    source_digest: str
    toolchain_digest: str
    environment_digest: str
    compatibility_policy_digest: str
    topology_digest: str
    dcp_root: Path
    dcp_manifest: DCPManifest

    def __post_init__(self) -> None:
        for value, kind in (
            (self.stage_id, "stage"),
            (self.checkpoint_id, "checkpoint"),
            (self.run_id, "run"),
        ):
            resource = ResourceId.parse(value)
            if resource.kind != kind:
                raise InvalidArgument(
                    "checkpoint commit resource identifier has the wrong kind",
                    reason="training_checkpoint_commit_identity",
                )
        if not isinstance(self.output_namespace, str) or not self.output_namespace:
            raise InvalidArgument(
                "checkpoint output namespace is required",
                reason="training_checkpoint_commit_identity",
            )
        for name in (
            "attempt",
            "checkpoint_attempt",
            "fencing_token",
            "deadline_unix_millis",
            "created_at_unix_millis",
        ):
            value = getattr(self, name)
            if isinstance(value, bool) or not isinstance(value, int) or value <= 0:
                raise InvalidArgument(
                    "checkpoint commit counters and timestamps must be positive integers",
                    reason="training_checkpoint_commit_counter",
                )
        if self.attempt > (1 << 32) - 1 or self.created_at_unix_millis >= self.deadline_unix_millis:
            raise InvalidArgument(
                "checkpoint commit attempt or creation time is outside bounds",
                reason="training_checkpoint_commit_counter",
            )
        if self.checkpoint_attempt != self.attempt:
            raise InvalidArgument(
                "reference checkpoint_attempt must equal the admitted stage attempt",
                reason="training_checkpoint_attempt",
            )
        for reference in (self.resolved_config, self.dataset):
            if not isinstance(reference, ArtifactRef):
                raise InvalidArgument(
                    "checkpoint request artifacts are invalid",
                    reason="training_checkpoint_commit_artifact",
                )
        if self.parent_manifest is not None:
            _require_artifact_contract(
                self.parent_manifest,
                logical_kind=CHECKPOINT_MANIFEST_LOGICAL_KIND,
                media_type=CHECKPOINT_MANIFEST_MEDIA_TYPE,
            )
        for name in (
            "model_digest",
            "source_digest",
            "toolchain_digest",
            "environment_digest",
            "compatibility_policy_digest",
            "topology_digest",
        ):
            Digest.parse(getattr(self, name))
        if not isinstance(self.dcp_root, Path) or not self.dcp_root.is_absolute():
            raise InvalidArgument(
                "checkpoint DCP root must be an absolute Path",
                reason="training_checkpoint_dcp_root",
            )
        if self.dcp_root.is_symlink() or not self.dcp_root.is_dir():
            raise FailedPrecondition(
                "checkpoint DCP root must be an existing non-symlink directory",
                reason="training_checkpoint_dcp_root",
            )
        if not isinstance(self.dcp_manifest, DCPManifest):
            raise InvalidArgument(
                "checkpoint request requires the adapter-local DCP manifest",
                reason="training_checkpoint_dcp_manifest",
            )
        identity = self.dcp_manifest.identity
        expected = {
            "checkpoint_id": self.checkpoint_id,
            "run_id": self.run_id,
            "resolved_config_digest": self.resolved_config.digest.text,
            "dataset_digest": self.dataset.digest.text,
            "model_digest": self.model_digest,
            "code_digest": self.source_digest,
            "toolchain_digest": self.toolchain_digest,
            "topology_digest": self.topology_digest,
        }
        if identity.to_document() != expected:
            raise FailedPrecondition(
                "DCP identity does not match the canonical commit request",
                reason="training_checkpoint_dcp_identity",
            )


@dataclass(frozen=True, slots=True)
class CheckpointCommitPlan:
    """Canonical records prepared but not yet published or registered."""

    manifest: ArtifactRef
    commit: ArtifactRef
    manifest_bytes: bytes
    commit_bytes: bytes
    registry_record_bytes: bytes
    registry_record_digest: str

    def __post_init__(self) -> None:
        _require_artifact_contract(
            self.manifest,
            logical_kind=CHECKPOINT_MANIFEST_LOGICAL_KIND,
            media_type=CHECKPOINT_MANIFEST_MEDIA_TYPE,
        )
        _require_artifact_contract(
            self.commit,
            logical_kind=CHECKPOINT_COMMIT_LOGICAL_KIND,
            media_type=CHECKPOINT_COMMIT_MEDIA_TYPE,
        )
        _bounded_bytes(self.manifest_bytes, MAXIMUM_MANIFEST_BYTES, "manifest")
        _bounded_bytes(self.commit_bytes, MAXIMUM_COMMIT_BYTES, "commit")
        _bounded_bytes(
            self.registry_record_bytes,
            MAXIMUM_REGISTRY_RECORD_BYTES,
            "registry record",
        )
        verify_bytes(self.manifest, self.manifest_bytes)
        verify_bytes(self.commit, self.commit_bytes)
        Digest.parse(self.registry_record_digest)


@dataclass(frozen=True, slots=True)
class CheckpointCommitReceipt:
    """Preverified attestation returned after an idempotently reconcilable terminal commit."""

    plan: CheckpointCommitPlan
    stage_outputs: tuple[ArtifactRef, ...]
    fencing_token: int
    terminal_status: str
    terminal_commit_digest: str

    def __post_init__(self) -> None:
        if not isinstance(self.plan, CheckpointCommitPlan):
            raise InvalidArgument(
                "checkpoint terminal receipt requires a prepared plan",
                reason="training_checkpoint_terminal_receipt",
            )
        try:
            outputs = tuple(self.stage_outputs)
        except TypeError as error:
            raise InvalidArgument(
                "checkpoint terminal receipt outputs must be iterable",
                reason="training_checkpoint_terminal_receipt",
                cause=error,
            ) from error
        if not outputs or any(not isinstance(item, ArtifactRef) for item in outputs):
            raise InvalidArgument(
                "checkpoint terminal receipt outputs are invalid",
                reason="training_checkpoint_terminal_receipt",
            )
        object.__setattr__(self, "stage_outputs", outputs)
        if (
            isinstance(self.fencing_token, bool)
            or not isinstance(self.fencing_token, int)
            or self.fencing_token <= 0
            or self.terminal_status != "succeeded"
        ):
            raise InvalidArgument(
                "checkpoint terminal receipt status or fence is invalid",
                reason="training_checkpoint_terminal_receipt",
            )
        Digest.parse(self.terminal_commit_digest)


@runtime_checkable
class CheckpointCommitter(Protocol):
    """Injected Rust/artifact-plane and Go-registry checkpoint authority.

    A production implementation must key commit/reconciliation by the admitted stage, attempt, and
    fencing token. It may raise only when it can prove the terminal transaction was not accepted.
    If a provider response is ambiguous, it must reconcile idempotently and either return the same
    preverified receipt or a definitive rejection; it must never expose ambiguity as a generic
    retryable failure after accepting the transaction.
    """

    def resolve_provenance(self, request: CheckpointCommitRequest) -> CheckpointProvenance:
        """Resolve admitted digests to immutable catalog references without provider locations."""

    def prepare(
        self,
        request: CheckpointCommitRequest,
        *,
        manifest_bytes: bytes,
    ) -> CheckpointCommitPlan:
        """Verify/stage components and prepare commit/registry records without publishing them."""

    def commit(
        self,
        request: CheckpointCommitRequest,
        *,
        plan: CheckpointCommitPlan,
        stage_outputs: tuple[ArtifactRef, ...],
    ) -> CheckpointCommitReceipt:
        """Return a preverified receipt for the atomic checkpoint/output/status transaction."""


def build_checkpoint_manifest(
    request: CheckpointCommitRequest,
    provenance: CheckpointProvenance,
    *,
    backend: str,
    device_type: str,
    world_size: int,
    local_world_size: int,
) -> bytes:
    """Build deterministic canonical ``training.v1.CheckpointManifest`` bytes."""

    validate_checkpoint_provenance(request, provenance)
    backend_value = {
        "gloo": topology_pb2.COLLECTIVE_BACKEND_GLOO,
        "nccl": topology_pb2.COLLECTIVE_BACKEND_NCCL,
    }.get(backend)
    device_value = {
        "cpu": topology_pb2.TRAINING_DEVICE_TYPE_CPU,
        "cuda": topology_pb2.TRAINING_DEVICE_TYPE_CUDA,
    }.get(device_type)
    if backend_value is None or device_value is None:
        raise InvalidArgument(
            "checkpoint topology enums are unsupported",
            reason="training_checkpoint_topology",
        )
    manifest = training_checkpoint_pb2.CheckpointManifest(
        schema_version=1,
        checkpoint_id=request.checkpoint_id,
        run_id=request.run_id,
        attempt_id=f"attempt-{request.attempt}",
        checkpoint_attempt=request.checkpoint_attempt,
        data_position=request.dcp_manifest.data_position,
    )
    manifest.counters.microbatches = request.dcp_manifest.training_state.microbatches
    manifest.counters.optimizer_steps = request.dcp_manifest.training_state.optimizer_steps
    manifest.counters.samples = request.dcp_manifest.training_state.samples
    for destination, reference in (
        (manifest.resolved_config, request.resolved_config),
        (manifest.dataset, request.dataset),
        (manifest.model, provenance.model),
        (manifest.source, provenance.source),
        (manifest.toolchain, provenance.toolchain),
        (manifest.environment, provenance.environment),
        (manifest.compatibility_policy, provenance.compatibility_policy),
    ):
        copy_artifact_ref(destination, reference)
    manifest.topology.world_size = world_size
    manifest.topology.local_world_size = local_world_size
    manifest.topology.node_count = 1
    manifest.topology.data_parallel_size = world_size
    manifest.topology.tensor_parallel_size = 1
    manifest.topology.pipeline_parallel_size = 1
    manifest.topology.backend = backend_value
    manifest.topology.device_type = device_value
    manifest.topology.topology_fingerprint = request.topology_digest
    dcp_manifest_reference = reference_bytes(
        request.dcp_manifest.encode(),
        media_type=DCP_MANIFEST_MEDIA_TYPE,
        logical_kind=DCP_MANIFEST_LOGICAL_KIND,
        maximum_bytes=MAXIMUM_MANIFEST_BYTES,
    )
    components = {
        **request.dcp_manifest.artifacts,
        DCP_MANIFEST_PATH: dcp_manifest_reference,
    }
    for path, reference in sorted(components.items()):
        component = manifest.components.add(
            name=path,
            relative_path=path,
        )
        copy_artifact_ref(component.artifact, reference)
        if path == DCP_MANIFEST_PATH:
            component.kind = training_checkpoint_pb2.CHECKPOINT_COMPONENT_KIND_TRAINER
            component.tensor_layout = training_checkpoint_pb2.CHECKPOINT_TENSOR_LAYOUT_REPLICATED
        elif path.startswith("model."):
            component.kind = training_checkpoint_pb2.CHECKPOINT_COMPONENT_KIND_MODEL
            component.tensor_layout = training_checkpoint_pb2.CHECKPOINT_TENSOR_LAYOUT_REPLICATED
            if path.endswith(".safetensors"):
                component.tensor_fqns.extend(request.dcp_manifest.model_fqns)
        elif path.startswith("optimizer."):
            component.kind = training_checkpoint_pb2.CHECKPOINT_COMPONENT_KIND_OPTIMIZER
            component.tensor_layout = training_checkpoint_pb2.CHECKPOINT_TENSOR_LAYOUT_REPLICATED
            if path.endswith(".safetensors"):
                component.tensor_fqns.extend(request.dcp_manifest.optimizer_fqns)
        else:
            match = _RANK_RNG_PATH.fullmatch(path)
            if match is None:
                raise FailedPrecondition(
                    "DCP manifest contains an unsupported canonical component path",
                    reason="training_checkpoint_component_path",
                )
            component.kind = training_checkpoint_pb2.CHECKPOINT_COMPONENT_KIND_RNG
            component.rank = int(match.group(1))
            component.tensor_layout = training_checkpoint_pb2.CHECKPOINT_TENSOR_LAYOUT_SHARDED
    if request.parent_manifest is not None:
        copy_artifact_ref(manifest.parent_manifest, request.parent_manifest)
    _set_timestamp(manifest.created_at, request.created_at_unix_millis)
    encoded: bytes = manifest.SerializeToString(deterministic=True)
    _bounded_bytes(encoded, MAXIMUM_MANIFEST_BYTES, "manifest")
    return encoded


def validate_checkpoint_provenance(
    request: CheckpointCommitRequest,
    provenance: CheckpointProvenance,
) -> None:
    if not isinstance(provenance, CheckpointProvenance):
        raise FailedPrecondition(
            "checkpoint committer returned invalid provenance",
            reason="training_checkpoint_provenance",
        )
    for reference, digest, kind in (
        (provenance.model, request.model_digest, MODEL_LOGICAL_KIND),
        (provenance.source, request.source_digest, SOURCE_LOGICAL_KIND),
        (provenance.toolchain, request.toolchain_digest, TOOLCHAIN_LOGICAL_KIND),
        (provenance.environment, request.environment_digest, ENVIRONMENT_LOGICAL_KIND),
        (
            provenance.compatibility_policy,
            request.compatibility_policy_digest,
            COMPATIBILITY_POLICY_LOGICAL_KIND,
        ),
    ):
        if (
            reference.digest.text != digest
            or reference.logical_kind != kind
            or reference.schema_version != 1
            or reference.size_bytes <= 0
        ):
            raise FailedPrecondition(
                "checkpoint provenance does not match the admitted artifact identity",
                reason="training_checkpoint_provenance_identity",
            )


def validate_checkpoint_plan(
    request: CheckpointCommitRequest,
    *,
    manifest_bytes: bytes,
    plan: CheckpointCommitPlan,
) -> None:
    """Fail closed unless every prepared canonical record binds the admitted attempt."""

    if not isinstance(plan, CheckpointCommitPlan):
        raise FailedPrecondition(
            "checkpoint committer returned an invalid plan",
            reason="training_checkpoint_plan",
        )
    if plan.manifest_bytes != manifest_bytes:
        raise FailedPrecondition(
            "checkpoint committer changed the semantic manifest bytes",
            reason="training_checkpoint_manifest_changed",
        )
    manifest = _parse_canonical(
        training_checkpoint_pb2.CheckpointManifest,
        plan.manifest_bytes,
        MAXIMUM_MANIFEST_BYTES,
        "manifest",
    )
    _validate_manifest(request, manifest)
    commit = _parse_canonical(
        artifact_checkpoint_pb2.CheckpointCommit,
        plan.commit_bytes,
        MAXIMUM_COMMIT_BYTES,
        "commit",
    )
    manifest_reference = artifact_ref_from_proto(commit.manifest)
    if (
        commit.schema_version != 1
        or commit.checkpoint_id != request.checkpoint_id
        or commit.run_id != request.run_id
        or manifest_reference != plan.manifest
        or commit.checkpoint_attempt != request.checkpoint_attempt
        or commit.fencing_token != request.fencing_token
        or not isinstance(commit.writer_id, str)
        or not 0 < len(commit.writer_id) <= MAXIMUM_WRITER_ID_LENGTH
    ):
        raise FailedPrecondition(
            "checkpoint commit does not bind the admitted manifest and fence",
            reason="training_checkpoint_commit_binding",
        )
    committed_at = _timestamp_millis(commit.committed_at, "committed_at")
    if committed_at < request.created_at_unix_millis:
        raise FailedPrecondition(
            "checkpoint commit predates its semantic manifest",
            reason="training_checkpoint_commit_time",
        )
    expected_commit_digest = checkpoint_commit_digest(commit)
    if commit.commit_digest != expected_commit_digest:
        raise FailedPrecondition(
            "checkpoint commit digest is absent or invalid",
            reason="training_checkpoint_commit_digest",
        )

    record = _parse_canonical(
        registry_checkpoint_pb2.CheckpointRecord,
        plan.registry_record_bytes,
        MAXIMUM_REGISTRY_RECORD_BYTES,
        "registry record",
    )
    parent = (
        artifact_ref_from_proto(record.parent_manifest)
        if record.HasField("parent_manifest")
        else None
    )
    if (
        record.schema_version != 1
        or record.checkpoint_id != request.checkpoint_id
        or record.run_id != request.run_id
        or artifact_ref_from_proto(record.manifest) != plan.manifest
        or artifact_ref_from_proto(record.commit) != plan.commit
        or record.optimizer_step != request.dcp_manifest.training_state.optimizer_steps
        or record.checkpoint_attempt != request.checkpoint_attempt
        or record.fencing_token != request.fencing_token
        or record.topology_fingerprint != request.topology_digest
        or parent != request.parent_manifest
        or record.lifecycle != registry_checkpoint_pb2.CHECKPOINT_LIFECYCLE_COMMITTED
        or record.policy_epoch <= 0
        or record.resource_version != 1
        or record.record_digest != plan.registry_record_digest
    ):
        raise FailedPrecondition(
            "checkpoint registry receipt does not bind the committed checkpoint",
            reason="training_checkpoint_registry_binding",
        )
    registered_at = _timestamp_millis(record.created_at, "registry created_at")
    retain_until = _timestamp_millis(record.retain_until, "retain_until")
    if registered_at < committed_at or retain_until <= registered_at:
        raise FailedPrecondition(
            "checkpoint registry receipt timestamps are invalid",
            reason="training_checkpoint_registry_time",
        )
    if record.record_digest != checkpoint_record_digest(record):
        raise FailedPrecondition(
            "checkpoint registry receipt digest is absent or invalid",
            reason="training_checkpoint_registry_digest",
        )


def validate_checkpoint_receipt(
    request: CheckpointCommitRequest,
    *,
    plan: CheckpointCommitPlan,
    stage_outputs: tuple[ArtifactRef, ...],
    receipt: CheckpointCommitReceipt,
) -> None:
    """Verify the final atomic checkpoint/output/status attestation."""

    if not isinstance(receipt, CheckpointCommitReceipt):
        raise FailedPrecondition(
            "checkpoint committer returned an invalid terminal receipt",
            reason="training_checkpoint_terminal_receipt",
        )
    expected_outputs = tuple(stage_outputs)
    if (
        len(expected_outputs) != 4
        or expected_outputs[:2] != (plan.manifest, plan.commit)
        or receipt.plan != plan
        or receipt.stage_outputs != expected_outputs
        or receipt.fencing_token != request.fencing_token
        or receipt.terminal_status != "succeeded"
        or receipt.terminal_commit_digest
        != checkpoint_terminal_commit_digest(request, plan, expected_outputs)
    ):
        raise FailedPrecondition(
            "checkpoint terminal receipt does not bind outputs, status, and fence",
            reason="training_checkpoint_terminal_binding",
        )


def checkpoint_resume_binding_from_manifest(
    outer_reference: ArtifactRef,
    manifest_bytes: bytes,
    *,
    checkpoint_id: str,
    run_id: str,
    topology_digest: str,
) -> CheckpointResumeBinding:
    """Resolve the adapter-manifest anchor from one admitted canonical manifest."""

    _require_artifact_contract(
        outer_reference,
        logical_kind=CHECKPOINT_MANIFEST_LOGICAL_KIND,
        media_type=CHECKPOINT_MANIFEST_MEDIA_TYPE,
    )
    verify_bytes(outer_reference, manifest_bytes)
    manifest = _parse_canonical(
        training_checkpoint_pb2.CheckpointManifest,
        manifest_bytes,
        MAXIMUM_MANIFEST_BYTES,
        "manifest",
    )
    topology = manifest.topology
    backend = {
        topology_pb2.COLLECTIVE_BACKEND_GLOO: "gloo",
        topology_pb2.COLLECTIVE_BACKEND_NCCL: "nccl",
    }.get(topology.backend)
    device_type = {
        topology_pb2.TRAINING_DEVICE_TYPE_CPU: "cpu",
        topology_pb2.TRAINING_DEVICE_TYPE_CUDA: "cuda",
    }.get(topology.device_type)
    if backend is None or device_type is None:
        raise FailedPrecondition(
            "admitted checkpoint topology uses unsupported enums",
            reason="training_checkpoint_resume_manifest",
        )
    computed_topology_digest = Digest.of(
        canonical_json_bytes(
            {
                "backend": backend,
                "data_parallel_size": topology.data_parallel_size,
                "device_type": device_type,
                "local_world_size": topology.local_world_size,
                "node_count": topology.node_count,
                "pipeline_parallel_size": topology.pipeline_parallel_size,
                "tensor_parallel_size": topology.tensor_parallel_size,
                "world_size": topology.world_size,
            }
        )
    ).text
    if (
        manifest.schema_version != 1
        or manifest.checkpoint_id != checkpoint_id
        or manifest.run_id != run_id
        or topology.world_size <= 0
        or topology.local_world_size != topology.world_size
        or topology.node_count != 1
        or topology.data_parallel_size != topology.world_size
        or topology.tensor_parallel_size != 1
        or topology.pipeline_parallel_size != 1
        or topology.topology_fingerprint != topology_digest
        or computed_topology_digest != topology_digest
    ):
        raise FailedPrecondition(
            "admitted checkpoint manifest identity does not match the resume request",
            reason="training_checkpoint_resume_manifest",
        )
    paths = [component.relative_path for component in manifest.components]
    names = [component.name for component in manifest.components]
    if (
        not paths
        or paths != sorted(paths)
        or len(set(paths)) != len(paths)
        or len(set(names)) != len(names)
        or any(not name for name in names)
    ):
        raise FailedPrecondition(
            "admitted checkpoint component identities are not canonical",
            reason="training_checkpoint_resume_adapter_manifest",
        )
    components = {
        component.relative_path: CheckpointResumeComponent(
            artifact_ref_from_proto(component.artifact),
            component.kind,
            component.rank,
            component.tensor_layout,
            tuple(component.tensor_fqns),
        )
        for component in manifest.components
    }
    adapter_component = components.get(DCP_MANIFEST_PATH)
    if adapter_component is None:
        raise FailedPrecondition(
            "admitted checkpoint does not bind one adapter manifest",
            reason="training_checkpoint_resume_adapter_manifest",
        )
    component = manifest.components[paths.index(DCP_MANIFEST_PATH)]
    reference = adapter_component.artifact
    if (
        component.name != DCP_MANIFEST_PATH
        or component.kind != training_checkpoint_pb2.CHECKPOINT_COMPONENT_KIND_TRAINER
        or component.tensor_layout != training_checkpoint_pb2.CHECKPOINT_TENSOR_LAYOUT_REPLICATED
        or component.rank != 0
        or component.tensor_fqns
        or reference.logical_kind != DCP_MANIFEST_LOGICAL_KIND
        or reference.media_type != DCP_MANIFEST_MEDIA_TYPE
        or reference.schema_version != 1
    ):
        raise FailedPrecondition(
            "admitted checkpoint adapter-manifest binding is invalid",
            reason="training_checkpoint_resume_adapter_manifest",
        )
    return CheckpointResumeBinding(
        reference,
        components,
        (
            manifest.counters.microbatches,
            manifest.counters.optimizer_steps,
            manifest.counters.samples,
        ),
        manifest.data_position,
        artifact_ref_from_proto(manifest.resolved_config),
        artifact_ref_from_proto(manifest.dataset),
        artifact_ref_from_proto(manifest.model),
        artifact_ref_from_proto(manifest.source),
        artifact_ref_from_proto(manifest.toolchain),
        artifact_ref_from_proto(manifest.environment),
        artifact_ref_from_proto(manifest.compatibility_policy),
        topology_digest,
        topology.world_size,
    )


def validate_checkpoint_resume_binding(
    binding: CheckpointResumeBinding,
    inner: DCPManifest,
    *,
    resolved_config: ArtifactRef,
    dataset: ArtifactRef,
    model_digest: str,
    source_digest: str,
    toolchain_digest: str,
    environment_digest: str,
    compatibility_policy_digest: str,
) -> None:
    """Require the admitted outer semantics to equal the restored inner DCP state."""

    if not isinstance(binding, CheckpointResumeBinding) or not isinstance(inner, DCPManifest):
        raise FailedPrecondition(
            "checkpoint resume binding types are invalid",
            reason="training_checkpoint_resume_semantics",
        )
    inner_manifest_reference = reference_bytes(
        inner.encode(),
        media_type=DCP_MANIFEST_MEDIA_TYPE,
        logical_kind=DCP_MANIFEST_LOGICAL_KIND,
        maximum_bytes=MAXIMUM_MANIFEST_BYTES,
    )
    expected_components: dict[str, CheckpointResumeComponent] = {
        DCP_MANIFEST_PATH: CheckpointResumeComponent(
            inner_manifest_reference,
            training_checkpoint_pb2.CHECKPOINT_COMPONENT_KIND_TRAINER,
            0,
            training_checkpoint_pb2.CHECKPOINT_TENSOR_LAYOUT_REPLICATED,
            (),
        )
    }
    for path, reference in inner.artifacts.items():
        if path.startswith("model."):
            kind = training_checkpoint_pb2.CHECKPOINT_COMPONENT_KIND_MODEL
            layout = training_checkpoint_pb2.CHECKPOINT_TENSOR_LAYOUT_REPLICATED
            rank = 0
            fqns = inner.model_fqns if path.endswith(".safetensors") else ()
        elif path.startswith("optimizer."):
            kind = training_checkpoint_pb2.CHECKPOINT_COMPONENT_KIND_OPTIMIZER
            layout = training_checkpoint_pb2.CHECKPOINT_TENSOR_LAYOUT_REPLICATED
            rank = 0
            fqns = inner.optimizer_fqns if path.endswith(".safetensors") else ()
        else:
            match = _RANK_RNG_PATH.fullmatch(path)
            if match is None:
                raise FailedPrecondition(
                    "restored DCP component path is unsupported",
                    reason="training_checkpoint_resume_semantics",
                )
            kind = training_checkpoint_pb2.CHECKPOINT_COMPONENT_KIND_RNG
            layout = training_checkpoint_pb2.CHECKPOINT_TENSOR_LAYOUT_SHARDED
            rank = int(match.group(1))
            fqns = ()
        expected_components[path] = CheckpointResumeComponent(
            reference,
            kind,
            rank,
            layout,
            tuple(fqns),
        )
    expected_provenance = (
        (binding.model, model_digest, MODEL_LOGICAL_KIND),
        (binding.source, source_digest, SOURCE_LOGICAL_KIND),
        (binding.toolchain, toolchain_digest, TOOLCHAIN_LOGICAL_KIND),
        (binding.environment, environment_digest, ENVIRONMENT_LOGICAL_KIND),
        (
            binding.compatibility_policy,
            compatibility_policy_digest,
            COMPATIBILITY_POLICY_LOGICAL_KIND,
        ),
    )
    if (
        binding.adapter_manifest != inner_manifest_reference
        or dict(binding.components) != expected_components
        or binding.counters
        != (
            inner.training_state.microbatches,
            inner.training_state.optimizer_steps,
            inner.training_state.samples,
        )
        or binding.data_position != inner.data_position
        or binding.resolved_config != resolved_config
        or binding.dataset != dataset
        or binding.topology_digest != inner.identity.topology_digest
        or binding.world_size != inner.world_size
        or any(
            reference.digest.text != digest
            or reference.logical_kind != logical_kind
            or reference.schema_version != 1
            or reference.size_bytes <= 0
            for reference, digest, logical_kind in expected_provenance
        )
    ):
        raise FailedPrecondition(
            "canonical outer checkpoint semantics do not match restored DCP state",
            reason="training_checkpoint_resume_semantics",
        )


def checkpoint_terminal_commit_digest(
    request: CheckpointCommitRequest,
    plan: CheckpointCommitPlan,
    stage_outputs: tuple[ArtifactRef, ...],
) -> str:
    return Digest.of(checkpoint_terminal_commit_bytes(request, plan, stage_outputs)).text


def checkpoint_terminal_commit_bytes(
    request: CheckpointCommitRequest,
    plan: CheckpointCommitPlan,
    stage_outputs: tuple[ArtifactRef, ...],
) -> bytes:
    """Encode the local semantic terminal record whose durability is adapter-owned."""

    document = {
        "attempt": request.attempt,
        "checkpoint_id": request.checkpoint_id,
        "checkpoint_commit": plan.commit.to_document(),
        "checkpoint_manifest": plan.manifest.to_document(),
        "deadline_unix_millis": request.deadline_unix_millis,
        "fencing_token": request.fencing_token,
        "output_namespace": request.output_namespace,
        "registry_record_digest": plan.registry_record_digest,
        "run_id": request.run_id,
        "schema_version": 1,
        "stage_id": request.stage_id,
        "stage_outputs": [item.to_document() for item in stage_outputs],
        "terminal_status": "succeeded",
    }
    return canonical_json_bytes(document)


def checkpoint_commit_digest(commit: artifact_checkpoint_pb2.CheckpointCommit) -> str:
    clone = artifact_checkpoint_pb2.CheckpointCommit()
    clone.CopyFrom(commit)
    clone.ClearField("commit_digest")
    return Digest.of(clone.SerializeToString(deterministic=True)).text


def checkpoint_record_digest(record: registry_checkpoint_pb2.CheckpointRecord) -> str:
    """Compute the frozen Go registry canonical digest for a checkpoint record."""

    manifest = artifact_ref_from_proto(record.manifest)
    commit = artifact_ref_from_proto(record.commit)
    created_at = _timestamp_millis(record.created_at, "registry created_at")
    retain_until = _timestamp_millis(record.retain_until, "retain_until")
    fields = (
        record.checkpoint_id,
        record.run_id,
        str(record.optimizer_step),
        str(record.checkpoint_attempt),
        str(record.fencing_token),
        record.topology_fingerprint,
        "committed",
        str(created_at),
        str(retain_until),
        str(record.policy_epoch),
        str(record.resource_version),
    )
    if any(not _canonical_registry_text(value) for value in fields):
        raise InvalidArgument(
            "checkpoint registry canonical field is invalid",
            reason="training_checkpoint_registry_field",
        )
    lines = [_REGISTRY_DOCUMENT, *fields]
    lines.append(_registry_artifact_line("manifest", manifest))
    lines.append(_registry_artifact_line("commit", commit))
    if record.HasField("parent_manifest"):
        lines.append(
            _registry_artifact_line("parent", artifact_ref_from_proto(record.parent_manifest))
        )
    else:
        lines.append("parent|-")
    return Digest.of(("\n".join(lines) + "\n").encode("utf-8")).text


def copy_artifact_ref(destination: _ArtifactRefProto, source: ArtifactRef) -> None:
    destination.digest = source.digest.text
    destination.size_bytes = source.size_bytes
    destination.media_type = source.media_type
    destination.logical_kind = source.logical_kind
    destination.schema_version = source.schema_version


def artifact_ref_from_proto(value: _ArtifactRefProto) -> ArtifactRef:
    return ArtifactRef(
        Digest.parse(value.digest),
        value.size_bytes,
        value.media_type,
        value.logical_kind,
        value.schema_version,
    )


def set_timestamp_millis(destination: _TimestampProto, unix_millis: int) -> None:
    _set_timestamp(destination, unix_millis)


def _validate_manifest(
    request: CheckpointCommitRequest,
    manifest: training_checkpoint_pb2.CheckpointManifest,
) -> None:
    parent = (
        artifact_ref_from_proto(manifest.parent_manifest)
        if manifest.HasField("parent_manifest")
        else None
    )
    expected_topology = (
        request.dcp_manifest.world_size,
        request.topology_digest,
    )
    topology = manifest.topology
    if (
        manifest.schema_version != 1
        or manifest.checkpoint_id != request.checkpoint_id
        or manifest.run_id != request.run_id
        or manifest.attempt_id != f"attempt-{request.attempt}"
        or manifest.checkpoint_attempt != request.checkpoint_attempt
        or manifest.counters.microbatches != request.dcp_manifest.training_state.microbatches
        or manifest.counters.optimizer_steps != request.dcp_manifest.training_state.optimizer_steps
        or manifest.counters.samples != request.dcp_manifest.training_state.samples
        or manifest.data_position != request.dcp_manifest.data_position
        or artifact_ref_from_proto(manifest.resolved_config) != request.resolved_config
        or artifact_ref_from_proto(manifest.dataset) != request.dataset
        or topology.world_size != expected_topology[0]
        or topology.local_world_size != expected_topology[0]
        or topology.node_count != 1
        or topology.data_parallel_size != expected_topology[0]
        or topology.tensor_parallel_size != 1
        or topology.pipeline_parallel_size != 1
        or topology.topology_fingerprint != expected_topology[1]
        or parent != request.parent_manifest
    ):
        raise FailedPrecondition(
            "canonical checkpoint manifest does not bind the semantic request",
            reason="training_checkpoint_manifest_binding",
        )
    _timestamp_millis(manifest.created_at, "manifest created_at")
    expected_artifacts = {
        **request.dcp_manifest.artifacts,
        DCP_MANIFEST_PATH: reference_bytes(
            request.dcp_manifest.encode(),
            media_type=DCP_MANIFEST_MEDIA_TYPE,
            logical_kind=DCP_MANIFEST_LOGICAL_KIND,
            maximum_bytes=MAXIMUM_MANIFEST_BYTES,
        ),
    }
    paths = [component.relative_path for component in manifest.components]
    if paths != sorted(expected_artifacts):
        raise FailedPrecondition(
            "canonical checkpoint components do not match the DCP manifest",
            reason="training_checkpoint_components",
        )
    names: set[str] = set()
    for component in manifest.components:
        path = component.relative_path
        expected_reference = expected_artifacts[path]
        if (
            not component.name
            or component.name in names
            or artifact_ref_from_proto(component.artifact) != expected_reference
        ):
            raise FailedPrecondition(
                "canonical checkpoint component identity is invalid",
                reason="training_checkpoint_components",
            )
        names.add(component.name)
        if path == DCP_MANIFEST_PATH:
            kind = training_checkpoint_pb2.CHECKPOINT_COMPONENT_KIND_TRAINER
            layout = training_checkpoint_pb2.CHECKPOINT_TENSOR_LAYOUT_REPLICATED
            rank = 0
            fqns: tuple[str, ...] = ()
        elif path.startswith("model."):
            kind = training_checkpoint_pb2.CHECKPOINT_COMPONENT_KIND_MODEL
            layout = training_checkpoint_pb2.CHECKPOINT_TENSOR_LAYOUT_REPLICATED
            rank = 0
            fqns = request.dcp_manifest.model_fqns if path.endswith(".safetensors") else ()
        elif path.startswith("optimizer."):
            kind = training_checkpoint_pb2.CHECKPOINT_COMPONENT_KIND_OPTIMIZER
            layout = training_checkpoint_pb2.CHECKPOINT_TENSOR_LAYOUT_REPLICATED
            rank = 0
            fqns = request.dcp_manifest.optimizer_fqns if path.endswith(".safetensors") else ()
        else:
            match = _RANK_RNG_PATH.fullmatch(path)
            if match is None:
                raise FailedPrecondition(
                    "canonical checkpoint component path is unsupported",
                    reason="training_checkpoint_components",
                )
            kind = training_checkpoint_pb2.CHECKPOINT_COMPONENT_KIND_RNG
            layout = training_checkpoint_pb2.CHECKPOINT_TENSOR_LAYOUT_SHARDED
            rank = int(match.group(1))
            fqns = ()
        if (
            component.kind != kind
            or component.tensor_layout != layout
            or component.rank != rank
            or tuple(component.tensor_fqns) != fqns
        ):
            raise FailedPrecondition(
                "canonical checkpoint component semantics are invalid",
                reason="training_checkpoint_components",
            )


def _parse_canonical[ProtoT](
    message_type: type[ProtoT],
    value: bytes,
    maximum: int,
    name: str,
) -> ProtoT:
    _bounded_bytes(value, maximum, name)
    message = message_type()
    parsed = cast(_CanonicalProto, message)
    try:
        parsed.ParseFromString(value)
    except Exception as error:
        raise FailedPrecondition(
            f"checkpoint {name} is not valid protobuf",
            reason="training_checkpoint_receipt_proto",
            cause=error,
        ) from error
    without_unknown = message_type()
    canonical = cast(_CanonicalProto, without_unknown)
    canonical.CopyFrom(message)
    canonical.DiscardUnknownFields()
    if canonical.SerializeToString(deterministic=True) != value:
        raise FailedPrecondition(
            f"checkpoint {name} is not canonical deterministic protobuf",
            reason="training_checkpoint_receipt_canonical",
        )
    return message


def _set_timestamp(destination: _TimestampProto, unix_millis: int) -> None:
    if isinstance(unix_millis, bool) or not isinstance(unix_millis, int) or unix_millis <= 0:
        raise InvalidArgument(
            "checkpoint timestamp must be positive Unix milliseconds",
            reason="training_checkpoint_timestamp",
        )
    destination.seconds, remainder = divmod(unix_millis, 1_000)
    destination.nanos = remainder * 1_000_000


def _timestamp_millis(value: _TimestampProto, name: str) -> int:
    if (
        value.seconds < 0
        or (value.seconds == 0 and value.nanos == 0)
        or not 0 <= value.nanos < 1_000_000_000
        or value.nanos % 1_000_000 != 0
    ):
        raise FailedPrecondition(
            f"checkpoint {name} must use positive millisecond precision",
            reason="training_checkpoint_timestamp",
        )
    return value.seconds * 1_000 + value.nanos // 1_000_000


def _require_artifact_contract(
    reference: ArtifactRef,
    *,
    logical_kind: str,
    media_type: str,
) -> None:
    if (
        not isinstance(reference, ArtifactRef)
        or reference.logical_kind != logical_kind
        or reference.media_type != media_type
        or reference.schema_version != 1
        or reference.size_bytes <= 0
    ):
        raise InvalidArgument(
            "checkpoint artifact reference has the wrong canonical contract",
            reason="training_checkpoint_artifact_contract",
        )


def _bounded_bytes(value: object, maximum: int, name: str) -> bytes:
    if not isinstance(value, bytes) or not value or len(value) > maximum:
        raise ResourceExhausted(
            f"checkpoint {name} bytes are outside bounds",
            reason="training_checkpoint_record_size",
        )
    return value


def _canonical_registry_text(value: object) -> bool:
    return (
        isinstance(value, str) and 0 < len(value) <= 4096 and "\n" not in value and "|" not in value
    )


def _registry_artifact_line(label: str, reference: ArtifactRef) -> str:
    values = (
        label,
        reference.digest.text,
        str(reference.size_bytes),
        reference.media_type,
        reference.logical_kind,
        str(reference.schema_version),
    )
    if any(not _canonical_registry_text(value) for value in values):
        raise InvalidArgument(
            "checkpoint registry artifact field is invalid",
            reason="training_checkpoint_registry_field",
        )
    return "|".join(values)


__all__ = [
    "CHECKPOINT_COMMIT_LOGICAL_KIND",
    "CHECKPOINT_COMMIT_MEDIA_TYPE",
    "CHECKPOINT_MANIFEST_LOGICAL_KIND",
    "CHECKPOINT_MANIFEST_MEDIA_TYPE",
    "COMPATIBILITY_POLICY_LOGICAL_KIND",
    "DCP_MANIFEST_LOGICAL_KIND",
    "DCP_MANIFEST_MEDIA_TYPE",
    "DCP_MANIFEST_PATH",
    "ENVIRONMENT_LOGICAL_KIND",
    "MODEL_LOGICAL_KIND",
    "SOURCE_LOGICAL_KIND",
    "TOOLCHAIN_LOGICAL_KIND",
    "CheckpointCommitPlan",
    "CheckpointCommitReceipt",
    "CheckpointCommitRequest",
    "CheckpointCommitter",
    "CheckpointProvenance",
    "CheckpointResumeBinding",
    "CheckpointResumeComponent",
    "artifact_ref_from_proto",
    "build_checkpoint_manifest",
    "checkpoint_commit_digest",
    "checkpoint_record_digest",
    "checkpoint_resume_binding_from_manifest",
    "checkpoint_terminal_commit_bytes",
    "checkpoint_terminal_commit_digest",
    "copy_artifact_ref",
    "set_timestamp_millis",
    "validate_checkpoint_plan",
    "validate_checkpoint_provenance",
    "validate_checkpoint_receipt",
    "validate_checkpoint_resume_binding",
]
