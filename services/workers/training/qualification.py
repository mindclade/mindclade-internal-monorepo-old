#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Run the provider-free CPU reference-training source check.

This binary intentionally refuses the held H100 phases. Those phases require the checkpoint-
agent socket protocol, immutable images, H100 hardware, and connected evidence collection that
do not exist in this source slice. A successful local invocation emits one JSON line explicitly
marked non-connected; it is not accepted by the GKE qualification evidence validator.
"""

from __future__ import annotations

import argparse
import json
import os
import shutil
import sys
import tempfile
import time
from collections.abc import Iterable
from contextlib import suppress
from pathlib import Path
from typing import Protocol

import torch
from mindclade.artifact.v1 import checkpoint_pb2 as artifact_checkpoint_pb2
from mindclade.registry.v1 import checkpoint_pb2 as registry_checkpoint_pb2
from safetensors.torch import save as save_safetensors

from libs.python.artifacts import reference_bytes, verify_bytes
from libs.python.errors import DeadlineExceeded, FailedPrecondition
from libs.python.identifiers import ArtifactRef, Digest
from libs.python.serialization import canonical_json_bytes
from libs.python.worker_runtime import StageEnvelope, StageKind
from services.workers.training import (
    CONFIG_LOGICAL_KIND,
    CONFIG_MEDIA_TYPE,
    DATASET_LOGICAL_KIND,
    DATASET_MEDIA_TYPE,
    TRAINING_OPERATION,
    ReferenceAffineTrainingEngine,
    build_executor,
    reference_topology_digest,
)
from services.workers.training.checkpoint_publication import (
    CHECKPOINT_COMMIT_LOGICAL_KIND,
    CHECKPOINT_COMMIT_MEDIA_TYPE,
    CHECKPOINT_MANIFEST_LOGICAL_KIND,
    CHECKPOINT_MANIFEST_MEDIA_TYPE,
    COMPATIBILITY_POLICY_LOGICAL_KIND,
    ENVIRONMENT_LOGICAL_KIND,
    MODEL_LOGICAL_KIND,
    SOURCE_LOGICAL_KIND,
    TOOLCHAIN_LOGICAL_KIND,
    CheckpointCommitPlan,
    CheckpointCommitReceipt,
    CheckpointCommitRequest,
    CheckpointProvenance,
    checkpoint_commit_digest,
    checkpoint_record_digest,
    checkpoint_terminal_commit_bytes,
    checkpoint_terminal_commit_digest,
    copy_artifact_ref,
    set_timestamp_millis,
    validate_checkpoint_plan,
    validate_checkpoint_provenance,
    validate_checkpoint_receipt,
)


class LocalScratchArtifactIO:
    """Ephemeral, process-local adapter used only by this source conformance check."""

    def __init__(self, root: Path) -> None:
        self._root = root
        self._bytes: dict[str, bytes] = {}
        self._trees: dict[str, Path] = {}
        (root / "published").mkdir(mode=0o700)

    def add_bytes(self, content: bytes, *, media_type: str, logical_kind: str) -> ArtifactRef:
        reference = reference_bytes(
            content,
            media_type=media_type,
            logical_kind=logical_kind,
        )
        self._bytes[reference.digest.text] = content
        return reference

    def read(self, reference: ArtifactRef) -> Iterable[bytes]:
        try:
            return (self._bytes[reference.digest.text],)
        except KeyError as error:
            raise FileNotFoundError("local qualification artifact is unavailable") from error

    def materialize_tree(self, reference: ArtifactRef) -> Path:
        try:
            return self._trees[reference.digest.text]
        except KeyError as error:
            raise FileNotFoundError("local qualification tree is unavailable") from error

    def publish_bytes(
        self,
        *,
        namespace: str,
        name: str,
        content: bytes,
        reference: ArtifactRef,
    ) -> ArtifactRef:
        del namespace
        if Digest.of(content) != reference.digest:
            raise ValueError("local publication bytes do not match their reference")
        destination = self._root / "published" / name
        destination.write_bytes(content)
        self._bytes[reference.digest.text] = content
        return reference

    def publish_tree(
        self,
        *,
        namespace: str,
        name: str,
        source: Path,
        reference: ArtifactRef,
    ) -> ArtifactRef:
        del namespace
        destination = self._root / "published" / name
        shutil.copytree(source, destination)
        self._trees[reference.digest.text] = destination.resolve()
        return reference

    def register_checkpoint(
        self,
        reference: ArtifactRef,
        manifest_bytes: bytes,
        source: Path,
    ) -> None:
        verify_bytes(reference, manifest_bytes)
        self._bytes[reference.digest.text] = manifest_bytes
        self._trees[reference.digest.text] = source.resolve()


class _CheckpointRegistrar(Protocol):
    def register_checkpoint(
        self,
        reference: ArtifactRef,
        manifest_bytes: bytes,
        source: Path,
    ) -> None: ...


class LocalCheckpointCommitter:
    """Strict filesystem fake for source checks; never connected qualification evidence."""

    def __init__(
        self,
        root: Path,
        provenance: CheckpointProvenance,
        artifact_io: _CheckpointRegistrar,
    ) -> None:
        if not root.is_absolute() or root.is_symlink() or not root.is_dir():
            raise ValueError("local checkpoint root must be an absolute non-symlink directory")
        if not isinstance(provenance, CheckpointProvenance):
            raise TypeError("local checkpoint provenance is invalid")
        if not callable(getattr(artifact_io, "register_checkpoint", None)):
            raise TypeError("local checkpoint artifact adapter is invalid")
        self._root = root
        self._provenance = provenance
        self._artifact_io = artifact_io
        for name in (
            "committed-checkpoints",
            "prepared-checkpoints",
        ):
            directory = root / name
            directory.mkdir(mode=0o700, exist_ok=True)
            if directory.is_symlink() or not directory.is_dir():
                raise ValueError("local checkpoint directory is not a regular directory")

    def resolve_provenance(self, request: CheckpointCommitRequest) -> CheckpointProvenance:
        validate_checkpoint_provenance(request, self._provenance)
        return self._provenance

    def prepare(
        self,
        request: CheckpointCommitRequest,
        *,
        manifest_bytes: bytes,
    ) -> CheckpointCommitPlan:
        validate_checkpoint_provenance(request, self._provenance)
        manifest_reference = reference_bytes(
            manifest_bytes,
            media_type=CHECKPOINT_MANIFEST_MEDIA_TYPE,
            logical_kind=CHECKPOINT_MANIFEST_LOGICAL_KIND,
        )
        destination = self._root / "prepared-checkpoints" / manifest_reference.digest.hex
        staging = destination.with_name(f".{destination.name}.tmp")
        if os.path.lexists(destination) or os.path.lexists(staging):
            raise FileExistsError("local checkpoint commit identity already exists")
        staging.mkdir(mode=0o700)
        try:
            for relative_path, reference in sorted(request.dcp_manifest.artifacts.items()):
                source = request.dcp_root / relative_path
                if source.is_symlink() or not source.is_file():
                    raise FailedPrecondition("local DCP component is not a regular file")
                if source.stat().st_size != reference.size_bytes:
                    raise FailedPrecondition("local DCP component size changed before commit")
                value = source.read_bytes()
                verify_bytes(reference, value)
                target = staging / relative_path
                target.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
                target.write_bytes(value)
            local_manifest = request.dcp_root / "manifest.json"
            if local_manifest.is_symlink() or not local_manifest.is_file():
                raise FailedPrecondition("adapter-local DCP manifest is unavailable")
            local_manifest_bytes = local_manifest.read_bytes()
            if local_manifest_bytes != request.dcp_manifest.encode():
                raise FailedPrecondition("adapter-local DCP manifest changed before prepare")
            (staging / "manifest.json").write_bytes(local_manifest_bytes)
            _fsync_directory(staging)
            os.replace(staging, destination)
            _fsync_directory(destination.parent)
        except BaseException:
            if staging.is_dir() and not staging.is_symlink():
                shutil.rmtree(staging)
            raise
        committed_at = max(request.created_at_unix_millis, time.time_ns() // 1_000_000)
        commit = artifact_checkpoint_pb2.CheckpointCommit(
            schema_version=1,
            checkpoint_id=request.checkpoint_id,
            run_id=request.run_id,
            checkpoint_attempt=request.checkpoint_attempt,
            fencing_token=request.fencing_token,
            writer_id="local-nonconnected-checkpoint-committer",
        )
        copy_artifact_ref(commit.manifest, manifest_reference)
        set_timestamp_millis(commit.committed_at, committed_at)
        commit.commit_digest = checkpoint_commit_digest(commit)
        commit_bytes = commit.SerializeToString(deterministic=True)
        commit_reference = reference_bytes(
            commit_bytes,
            media_type=CHECKPOINT_COMMIT_MEDIA_TYPE,
            logical_kind=CHECKPOINT_COMMIT_LOGICAL_KIND,
        )
        registered_at = max(committed_at, time.time_ns() // 1_000_000)
        record = registry_checkpoint_pb2.CheckpointRecord(
            schema_version=1,
            checkpoint_id=request.checkpoint_id,
            run_id=request.run_id,
            optimizer_step=request.dcp_manifest.training_state.optimizer_steps,
            checkpoint_attempt=request.checkpoint_attempt,
            fencing_token=request.fencing_token,
            topology_fingerprint=request.topology_digest,
            lifecycle=registry_checkpoint_pb2.CHECKPOINT_LIFECYCLE_COMMITTED,
            policy_epoch=1,
            resource_version=1,
        )
        copy_artifact_ref(record.manifest, manifest_reference)
        copy_artifact_ref(record.commit, commit_reference)
        if request.parent_manifest is not None:
            copy_artifact_ref(record.parent_manifest, request.parent_manifest)
        set_timestamp_millis(record.created_at, registered_at)
        set_timestamp_millis(record.retain_until, registered_at + 3_600_000)
        record.record_digest = checkpoint_record_digest(record)
        record_bytes = record.SerializeToString(deterministic=True)
        return CheckpointCommitPlan(
            manifest_reference,
            commit_reference,
            manifest_bytes,
            commit_bytes,
            record_bytes,
            record.record_digest,
        )

    def commit(
        self,
        request: CheckpointCommitRequest,
        *,
        plan: CheckpointCommitPlan,
        stage_outputs: tuple[ArtifactRef, ...],
    ) -> CheckpointCommitReceipt:
        validate_checkpoint_plan(request, manifest_bytes=plan.manifest_bytes, plan=plan)
        if time.time_ns() // 1_000_000 >= request.deadline_unix_millis:
            raise DeadlineExceeded(
                "local terminal checkpoint deadline expired",
                reason="training_checkpoint_terminal_deadline",
                operation="reference_affine_terminal_commit",
            )
        terminal_bytes = checkpoint_terminal_commit_bytes(request, plan, stage_outputs)
        terminal_digest = checkpoint_terminal_commit_digest(request, plan, stage_outputs)
        if Digest.of(terminal_bytes).text != terminal_digest:
            raise AssertionError("local terminal checkpoint digest is inconsistent")
        receipt = CheckpointCommitReceipt(
            plan,
            stage_outputs,
            request.fencing_token,
            "succeeded",
            terminal_digest,
        )
        validate_checkpoint_receipt(
            request,
            plan=plan,
            stage_outputs=stage_outputs,
            receipt=receipt,
        )
        destination = self._root / "committed-checkpoints" / terminal_digest[7:]
        staging = destination.with_name(f".{destination.name}.tmp")
        prepared = self._root / "prepared-checkpoints" / plan.manifest.digest.hex
        if os.path.lexists(destination):
            self._reconcile_terminal(destination, plan, terminal_bytes)
            self._artifact_io.register_checkpoint(
                plan.manifest,
                plan.manifest_bytes,
                destination / "checkpoint",
            )
            return receipt
        if os.path.lexists(staging) or prepared.is_symlink() or not prepared.is_dir():
            raise FailedPrecondition("local terminal checkpoint state is invalid")
        staging.mkdir(mode=0o700)
        try:
            shutil.copytree(prepared, staging / "checkpoint")
            _write_create_only(staging / "manifest.pb", plan.manifest_bytes)
            _write_create_only(staging / "commit.pb", plan.commit_bytes)
            _write_create_only(staging / "registry-record.pb", plan.registry_record_bytes)
            # This create-only record is written and fsynced last inside the attempt directory;
            # the following directory rename is the local fake's single visibility point.
            _write_create_only(staging / "TERMINAL", terminal_bytes)
            _fsync_directory(staging)
            # Preflight the process-local lookup before visibility. It may point at the future
            # destination because `register_checkpoint` performs no provider publication.
            self._artifact_io.register_checkpoint(
                plan.manifest,
                plan.manifest_bytes,
                destination / "checkpoint",
            )
            os.replace(staging, destination)
            _fsync_directory(destination.parent)
        except BaseException:
            if staging.is_dir() and not staging.is_symlink():
                shutil.rmtree(staging)
            raise
        with suppress(OSError):
            shutil.rmtree(prepared)
        return receipt

    @staticmethod
    def _reconcile_terminal(
        destination: Path,
        plan: CheckpointCommitPlan,
        terminal_bytes: bytes,
    ) -> None:
        expected = {
            "TERMINAL": terminal_bytes,
            "commit.pb": plan.commit_bytes,
            "manifest.pb": plan.manifest_bytes,
            "registry-record.pb": plan.registry_record_bytes,
        }
        if destination.is_symlink() or not destination.is_dir():
            raise FailedPrecondition("local terminal checkpoint destination is invalid")
        for name, content in expected.items():
            path = destination / name
            if path.is_symlink() or not path.is_file() or path.read_bytes() != content:
                raise FailedPrecondition("local terminal checkpoint reconciliation failed")
        checkpoint = destination / "checkpoint"
        if checkpoint.is_symlink() or not checkpoint.is_dir():
            raise FailedPrecondition("local terminal checkpoint tree is unavailable")


def local_checkpoint_provenance(
    *,
    model: bytes,
    source: bytes,
    toolchain: bytes,
    environment: bytes,
    compatibility_policy: bytes,
) -> CheckpointProvenance:
    """Create content-true provenance for the provider-free local fixture."""

    values = (
        (model, "application/vnd.mindclade.training-model.v1+proto", MODEL_LOGICAL_KIND),
        (source, "application/vnd.mindclade.source-archive.v1+proto", SOURCE_LOGICAL_KIND),
        (
            toolchain,
            "application/vnd.mindclade.toolchain-manifest.v1+proto",
            TOOLCHAIN_LOGICAL_KIND,
        ),
        (
            environment,
            "application/vnd.mindclade.training-environment.v1+proto",
            ENVIRONMENT_LOGICAL_KIND,
        ),
        (
            compatibility_policy,
            "application/vnd.mindclade.training-checkpoint-compatibility-policy.v1+proto",
            COMPATIBILITY_POLICY_LOGICAL_KIND,
        ),
    )
    references = tuple(
        reference_bytes(content, media_type=media_type, logical_kind=logical_kind)
        for content, media_type, logical_kind in values
    )
    return CheckpointProvenance(*references)


def _resource(kind: str, suffix: int) -> str:
    return f"{kind}_019c00000000700080000000000000{suffix:02x}"


def _config() -> bytes:
    return canonical_json_bytes(
        {
            "accumulation_steps": 1,
            "allow_replicated_world_size_change": False,
            "dtype": "float32",
            "engine": TRAINING_OPERATION,
            "gradient_clip_norm": "10",
            "initial_bias": "0.5",
            "initial_scale": "2",
            "learning_rate": "0.05",
            "maximum_input_elements": 1024,
            "maximum_optimizer_steps": 8,
            "microbatch_size": 5,
            "model": "reference-affine-v1",
            "model_operation": "reference.affine.v1",
            "optimizer_steps_per_execution": 8,
            "schema_version": 1,
            "seed": 7,
            "weight_decay": "0",
        }
    )


def _stage(config: ArtifactRef, dataset: ArtifactRef) -> StageEnvelope:
    topology = reference_topology_digest(
        backend="gloo",
        device_type="cpu",
        world_size=1,
        local_world_size=1,
    )
    return StageEnvelope(
        stage_id=_resource("stage", 1),
        kind=StageKind.TRAINING,
        operation=TRAINING_OPERATION,
        inputs=(config, dataset),
        output_namespace="local/reference-training",
        resolved_config_digest=config.digest.text,
        reference_snapshot_digest=None,
        attempt=1,
        fencing_token=1,
        deadline_unix_millis=time.time_ns() // 1_000_000 + 120_000,
        metadata={
            "backend": "gloo",
            "checkpoint_id": _resource("checkpoint", 2),
            "code_digest": Digest.of_text("local-source-check").text,
            "compatibility_policy_digest": Digest.of_text("replicated-adamw-v1").text,
            "device_type": "cpu",
            "local_world_size": "1",
            "model_digest": Digest.of_text("reference-affine-v1").text,
            "run_id": _resource("run", 3),
            "runtime_image_digest": Digest.of_text("local-runtime-environment").text,
            "toolchain_digest": Digest.of_text("pinned-local-toolchain").text,
            "topology_digest": topology,
            "world_size": "1",
        },
    )


def run_local(scratch: Path, external_run_id: str) -> dict[str, object]:
    if not scratch.is_absolute() or scratch.is_symlink() or not scratch.is_dir():
        raise ValueError("scratch must be an existing absolute non-symlink directory")
    with tempfile.TemporaryDirectory(prefix="reference-training-", dir=scratch) as temporary:
        root = Path(temporary).resolve()
        artifacts = LocalScratchArtifactIO(root)
        config = artifacts.add_bytes(
            _config(),
            media_type=CONFIG_MEDIA_TYPE,
            logical_kind=CONFIG_LOGICAL_KIND,
        )
        inputs = torch.tensor([[-2.0], [-1.0], [0.0], [1.0], [2.0]], dtype=torch.float32)
        dataset = artifacts.add_bytes(
            save_safetensors({"inputs": inputs, "targets": (inputs * 3.0) - 1.0}),
            media_type=DATASET_MEDIA_TYPE,
            logical_kind=DATASET_LOGICAL_KIND,
        )
        provenance = local_checkpoint_provenance(
            model=b"reference-affine-v1",
            source=b"local-source-check",
            toolchain=b"pinned-local-toolchain",
            environment=b"local-runtime-environment",
            compatibility_policy=b"replicated-adamw-v1",
        )
        committer = LocalCheckpointCommitter(root, provenance, artifacts)
        engine = ReferenceAffineTrainingEngine(
            artifacts,
            workspace_root=root,
            checkpoint_committer=committer,
        )
        started = time.monotonic_ns()
        result = build_executor(engine).execute(_stage(config, dataset))
        duration_seconds = (time.monotonic_ns() - started) / 1_000_000_000
        return {
            "connected_qualification": False,
            "duration_seconds": duration_seconds,
            "external_run_id": external_run_id,
            "model_bundle_digest": result.outputs[2].digest.text,
            "optimizer_steps": int(result.metrics["optimizer_steps"]),
            "output_count": len(result.outputs),
            "phase": "local-cpu-source-check",
            "schema_version": "mindclade.dev/reference-training-local/v1",
        }


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument(
        "--phase",
        default="local-cpu-source-check",
        choices=("local-cpu-source-check", "h100-1g-smoke", "h100-8g-ddp-dcp"),
    )
    parser.add_argument("--checkpoint-socket", type=Path)
    parser.add_argument("--scratch", type=Path, default=Path("/scratch"))
    parser.add_argument("--run-id", default="local")
    arguments = parser.parse_args()
    if arguments.phase != "local-cpu-source-check" or arguments.checkpoint_socket is not None:
        print(
            "training-qualification: connected H100/checkpoint-agent integration is not "
            "implemented; the held gate remains fail-closed",
            file=sys.stderr,
        )
        return 2
    try:
        result = run_local(arguments.scratch, arguments.run_id)
    except Exception as error:
        print(f"training-qualification: local source check failed: {error}", file=sys.stderr)
        return 1
    print(json.dumps(result, sort_keys=True, separators=(",", ":")), flush=True)
    return 0


def _write_create_only(path: Path, value: bytes) -> None:
    with path.open("xb") as stream:
        stream.write(value)
        stream.flush()
        os.fsync(stream.fileno())


def _fsync_directory(path: Path) -> None:
    descriptor = os.open(path, os.O_RDONLY)
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


if __name__ == "__main__":
    raise SystemExit(main())
