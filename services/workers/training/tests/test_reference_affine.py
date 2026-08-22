# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import json
import shutil
from collections.abc import Callable, Iterable
from pathlib import Path
from types import SimpleNamespace
from typing import Any, Never

import pytest
import torch
from mindclade.training.v1 import checkpoint_pb2 as training_checkpoint_pb2
from safetensors.torch import load_file
from safetensors.torch import save as save_safetensors

from libs.python.artifacts import reference_bytes, verify_bytes
from libs.python.errors import (
    Canceled,
    DeadlineExceeded,
    FailedPrecondition,
    InvalidArgument,
    ResourceExhausted,
)
from libs.python.identifiers import ArtifactRef, Digest
from libs.python.serialization import canonical_json_bytes
from libs.python.worker_runtime import CancellationToken, ExecutionContext, StageEnvelope, StageKind
from services.workers.training import (
    CHECKPOINT_COMMIT_LOGICAL_KIND,
    CHECKPOINT_LOGICAL_KIND,
    CONFIG_LOGICAL_KIND,
    CONFIG_MEDIA_TYPE,
    DATASET_LOGICAL_KIND,
    DATASET_MEDIA_TYPE,
    RUN_EVIDENCE_LOGICAL_KIND,
    TRAINING_OPERATION,
    ReferenceAffineTrainingConfig,
    ReferenceAffineTrainingEngine,
    build_executor,
    reference_topology_digest,
)
from services.workers.training.checkpoint_publication import (
    DCP_MANIFEST_LOGICAL_KIND,
    DCP_MANIFEST_MEDIA_TYPE,
    CheckpointCommitPlan,
    CheckpointCommitReceipt,
    CheckpointCommitRequest,
    CheckpointProvenance,
    copy_artifact_ref,
)
from services.workers.training.qualification import (
    LocalCheckpointCommitter,
    local_checkpoint_provenance,
)
from training.runtime.telemetry.exporters import MLflowExporter


class MemoryArtifactIO:
    def __init__(self, root: Path) -> None:
        self.root = root
        self.bytes: dict[str, bytes] = {}
        self.trees: dict[str, Path] = {}
        self.publications: list[tuple[str, str, ArtifactRef]] = []
        (root / "objects").mkdir(parents=True)

    def add_bytes(self, content: bytes, *, media_type: str, logical_kind: str) -> ArtifactRef:
        reference = reference_bytes(
            content,
            media_type=media_type,
            logical_kind=logical_kind,
        )
        self.bytes[reference.digest.text] = content
        return reference

    def read(self, reference: ArtifactRef) -> Iterable[bytes]:
        return (self.bytes[reference.digest.text],)

    def materialize_tree(self, reference: ArtifactRef) -> Path:
        return self.trees[reference.digest.text]

    def register_checkpoint(
        self,
        reference: ArtifactRef,
        manifest_bytes: bytes,
        source: Path,
    ) -> None:
        verify_bytes(reference, manifest_bytes)
        self.bytes[reference.digest.text] = manifest_bytes
        self.trees[reference.digest.text] = source.resolve()

    def publish_bytes(
        self,
        *,
        namespace: str,
        name: str,
        content: bytes,
        reference: ArtifactRef,
    ) -> ArtifactRef:
        verify_bytes(reference, content)
        self.bytes[reference.digest.text] = content
        self.publications.append((namespace, name, reference))
        return reference

    def publish_tree(
        self,
        *,
        namespace: str,
        name: str,
        source: Path,
        reference: ArtifactRef,
    ) -> ArtifactRef:
        destination = self.root / "objects" / reference.digest.hex
        shutil.copytree(source, destination)
        if reference.logical_kind == CHECKPOINT_LOGICAL_KIND:
            verify_bytes(reference, (destination / "manifest.json").read_bytes())
        elif reference.logical_kind == "model.bundle":
            manifest = json.loads((destination / "manifest.json").read_text(encoding="utf-8"))
            assert manifest["digest"] == reference.digest.text
            assert manifest["size_bytes"] == reference.size_bytes
        self.trees[reference.digest.text] = destination.resolve()
        self.publications.append((namespace, name, reference))
        return reference


class RecordingMLflowClient:
    def __init__(self, *, cancel_on_parameter: CancellationToken | None = None) -> None:
        self._cancel_on_parameter = cancel_on_parameter
        self.statuses: list[str] = []

    def create_run(
        self,
        experiment_id: Any,
        *,
        tags: Any = None,
        run_name: Any = None,
    ) -> SimpleNamespace:
        del experiment_id, tags, run_name
        return SimpleNamespace(info=SimpleNamespace(run_id="mirror-run"))

    def log_text(self, *args: Any, **kwargs: Any) -> None:
        del args, kwargs

    def log_param(self, *args: Any, **kwargs: Any) -> None:
        del args, kwargs
        if self._cancel_on_parameter is not None:
            self._cancel_on_parameter.cancel()

    def log_metric(self, *args: Any, **kwargs: Any) -> None:
        del args, kwargs

    def set_tag(self, *args: Any, **kwargs: Any) -> None:
        del args, kwargs

    def set_terminated(self, run_id: Any, *, status: str) -> None:
        del run_id
        self.statuses.append(status)


class RecordingCheckpointCommitter:
    """Observe the strict local fake without weakening its canonical validation."""

    def __init__(
        self,
        delegate: LocalCheckpointCommitter,
        *,
        after_commit: Callable[[], None] | None = None,
    ) -> None:
        self._delegate = delegate
        self._after_commit = after_commit
        self.prepare_calls = 0
        self.commit_calls = 0

    def resolve_provenance(self, request: CheckpointCommitRequest) -> CheckpointProvenance:
        return self._delegate.resolve_provenance(request)

    def prepare(
        self,
        request: CheckpointCommitRequest,
        *,
        manifest_bytes: bytes,
    ) -> CheckpointCommitPlan:
        self.prepare_calls += 1
        return self._delegate.prepare(request, manifest_bytes=manifest_bytes)

    def commit(
        self,
        request: CheckpointCommitRequest,
        *,
        plan: CheckpointCommitPlan,
        stage_outputs: tuple[ArtifactRef, ...],
    ) -> CheckpointCommitReceipt:
        self.commit_calls += 1
        receipt = self._delegate.commit(request, plan=plan, stage_outputs=stage_outputs)
        if self._after_commit is not None:
            self._after_commit()
        return receipt


def resource(kind: str, suffix: int) -> str:
    return f"{kind}_019c00000000700080000000000000{suffix:02x}"


def config_bytes(*, maximum_steps: int = 8, steps_per_execution: int = 4) -> bytes:
    return canonical_json_bytes(
        {
            "accumulation_steps": 1,
            "allow_replicated_world_size_change": False,
            "dtype": "float32",
            "engine": TRAINING_OPERATION,
            "gradient_clip_norm": "10",
            "initial_bias": "0.5",
            "initial_scale": "2",
            "learning_rate": "0.1",
            "maximum_input_elements": 1024,
            "maximum_optimizer_steps": maximum_steps,
            "microbatch_size": 5,
            "model": "reference-affine-v1",
            "model_operation": "reference.affine.v1",
            "optimizer_steps_per_execution": steps_per_execution,
            "schema_version": 1,
            "seed": 11,
            "weight_decay": "0",
        }
    )


def add_inputs(store: MemoryArtifactIO) -> tuple[ArtifactRef, ArtifactRef]:
    config = store.add_bytes(
        config_bytes(),
        media_type=CONFIG_MEDIA_TYPE,
        logical_kind=CONFIG_LOGICAL_KIND,
    )
    inputs = torch.tensor([[-2.0], [-1.0], [0.0], [1.0], [2.0]], dtype=torch.float32)
    dataset = store.add_bytes(
        save_safetensors({"inputs": inputs, "targets": (inputs * 3.0) - 1.0}),
        media_type=DATASET_MEDIA_TYPE,
        logical_kind=DATASET_LOGICAL_KIND,
    )
    return config, dataset


def checkpoint_committer(store: MemoryArtifactIO) -> LocalCheckpointCommitter:
    return LocalCheckpointCommitter(
        store.root,
        local_checkpoint_provenance(
            model=b"reference-affine-v1",
            source=b"source",
            toolchain=b"toolchain",
            environment=b"runtime-image",
            compatibility_policy=b"replicated-adamw-v1",
        ),
        store,
    )


def stage(
    config: ArtifactRef,
    dataset: ArtifactRef,
    *,
    suffix: int,
    resume: ArtifactRef | None = None,
    extra_metadata: dict[str, str] | None = None,
) -> StageEnvelope:
    topology = reference_topology_digest(
        backend="gloo",
        device_type="cpu",
        world_size=1,
        local_world_size=1,
    )
    metadata = {
        "backend": "gloo",
        "checkpoint_id": resource("checkpoint", suffix),
        "code_digest": Digest.of_text("source").text,
        "compatibility_policy_digest": Digest.of_text("replicated-adamw-v1").text,
        "device_type": "cpu",
        "local_world_size": "1",
        "model_digest": Digest.of_text("reference-affine-v1").text,
        "run_id": resource("run", 30),
        "runtime_image_digest": Digest.of_text("runtime-image").text,
        "toolchain_digest": Digest.of_text("toolchain").text,
        "topology_digest": topology,
        "world_size": "1",
    }
    if resume is not None:
        metadata["resume_checkpoint_id"] = resource("checkpoint", suffix - 1)
    if extra_metadata:
        metadata.update(extra_metadata)
    return StageEnvelope(
        stage_id=resource("stage", suffix),
        kind=StageKind.TRAINING,
        operation=TRAINING_OPERATION,
        inputs=(config, dataset) if resume is None else (config, dataset, resume),
        output_namespace=f"tenant/reference/{suffix}",
        resolved_config_digest=config.digest.text,
        reference_snapshot_digest=None,
        attempt=1,
        fencing_token=suffix,
        deadline_unix_millis=9_000_000_000_000,
        metadata=metadata,
    )


def rehash_checkpoint_manifest(
    store: MemoryArtifactIO,
    admitted: ArtifactRef,
    tree: Path,
    mutate: Callable[[training_checkpoint_pb2.CheckpointManifest], None],
) -> ArtifactRef:
    outer = training_checkpoint_pb2.CheckpointManifest()
    outer.ParseFromString(store.bytes[admitted.digest.text])
    mutate(outer)
    changed_bytes = outer.SerializeToString(deterministic=True)
    changed = reference_bytes(
        changed_bytes,
        media_type=admitted.media_type,
        logical_kind=admitted.logical_kind,
    )
    store.bytes[changed.digest.text] = changed_bytes
    store.trees[changed.digest.text] = tree
    return changed


def test_reference_engine_trains_resumes_and_publishes_exact_outputs(tmp_path: Path) -> None:
    store = MemoryArtifactIO(tmp_path)
    config, dataset = add_inputs(store)
    engine = ReferenceAffineTrainingEngine(
        store,
        workspace_root=tmp_path,
        checkpoint_committer=checkpoint_committer(store),
    )

    first = build_executor(engine).execute(stage(config, dataset, suffix=11))
    assert [item.logical_kind for item in first.outputs] == [
        CHECKPOINT_LOGICAL_KIND,
        CHECKPOINT_COMMIT_LOGICAL_KIND,
        "model.bundle",
        RUN_EVIDENCE_LOGICAL_KIND,
    ]
    assert first.metrics["optimizer_steps"] == 4.0
    assert first.metrics["reached_maximum_optimizer_steps"] == 0.0
    assert not list(tmp_path.glob("stage_*.a*.f*"))
    first_manifest = training_checkpoint_pb2.CheckpointManifest()
    first_manifest.ParseFromString(store.bytes[first.outputs[0].digest.text])
    assert first_manifest.attempt_id == "attempt-1"
    assert first_manifest.checkpoint_attempt == 1
    first_evidence = json.loads(store.bytes[first.outputs[3].digest.text])
    assert first_evidence["checkpoint_manifest"] == first.outputs[0].to_document()
    assert first_evidence["checkpoint_commit"] == first.outputs[1].to_document()
    assert first_evidence["model_bundle"] == first.outputs[2].to_document()
    terminal_paths = list((tmp_path / "committed-checkpoints").glob("*/TERMINAL"))
    assert len(terminal_paths) == 1
    terminal = json.loads(terminal_paths[0].read_bytes())
    assert terminal["terminal_status"] == "succeeded"
    assert terminal["stage_outputs"] == [item.to_document() for item in first.outputs]

    resumed = build_executor(engine).execute(
        stage(config, dataset, suffix=12, resume=first.outputs[0])
    )
    assert resumed.metrics["optimizer_steps"] == 8.0
    assert resumed.metrics["reached_maximum_optimizer_steps"] == 1.0
    assert resumed.metrics["data_position"] == 40.0
    resumed_evidence = json.loads(store.bytes[resumed.outputs[3].digest.text])
    assert resumed_evidence["resume_exact"] is True
    assert resumed_evidence["resume_source_rank"] == 0
    assert resumed_evidence["resume_source_world_size"] == 1
    assert resumed_evidence["resume_target_world_size"] == 1
    bundle = store.materialize_tree(resumed.outputs[2])
    state = load_file(bundle / "model.safetensors")
    assert set(state) == {"bias", "scale"}
    assert all(torch.isfinite(value).all() for value in state.values())
    assert float(state["scale"].item()) != 2.0 or float(state["bias"].item()) != 0.5


def test_config_and_artifact_contracts_fail_closed(tmp_path: Path) -> None:
    with pytest.raises(InvalidArgument, match="canonical JSON"):
        ReferenceAffineTrainingConfig.decode(
            json.dumps(json.loads(config_bytes()), indent=2, sort_keys=True).encode()
        )
    boolean_version = json.loads(config_bytes())
    boolean_version["schema_version"] = True
    with pytest.raises(InvalidArgument, match="schema version"):
        ReferenceAffineTrainingConfig.decode(canonical_json_bytes(boolean_version))
    with pytest.raises(InvalidArgument, match="unique-key UTF-8 JSON"):
        ReferenceAffineTrainingConfig.decode(b"[" * 10_000 + b"0" + b"]" * 10_000)
    deeply_nested_field = config_bytes().replace(
        b'"gradient_clip_norm":"10"',
        b'"gradient_clip_norm":' + b"[" * 5_000 + b"0" + b"]" * 5_000,
    )
    with pytest.raises(InvalidArgument, match="canonical JSON"):
        ReferenceAffineTrainingConfig.decode(deeply_nested_field)

    store = MemoryArtifactIO(tmp_path)
    config, dataset = add_inputs(store)
    wrong_config = ArtifactRef(
        config.digest,
        config.size_bytes,
        "application/json",
        config.logical_kind,
        1,
    )
    engine = ReferenceAffineTrainingEngine(
        store,
        workspace_root=tmp_path,
        checkpoint_committer=checkpoint_committer(store),
    )
    with pytest.raises(InvalidArgument, match="artifact must be"):
        build_executor(engine).execute(stage(wrong_config, dataset, suffix=13))


def test_composed_working_set_is_rejected_before_execution_allocation(tmp_path: Path) -> None:
    store = MemoryArtifactIO(tmp_path)
    document = json.loads(config_bytes())
    document.update(
        {
            "accumulation_steps": 2,
            "maximum_input_elements": 16_777_216,
            "microbatch_size": 1024,
        }
    )
    config = store.add_bytes(
        canonical_json_bytes(document),
        media_type=CONFIG_MEDIA_TYPE,
        logical_kind=CONFIG_LOGICAL_KIND,
    )
    inputs = torch.zeros((1, 16_384), dtype=torch.float32)
    dataset = store.add_bytes(
        save_safetensors({"inputs": inputs, "targets": inputs.clone()}),
        media_type=DATASET_MEDIA_TYPE,
        logical_kind=DATASET_LOGICAL_KIND,
    )
    engine = ReferenceAffineTrainingEngine(
        store,
        workspace_root=tmp_path,
        checkpoint_committer=checkpoint_committer(store),
    )

    with pytest.raises(ResourceExhausted, match="working set"):
        build_executor(engine).execute(stage(config, dataset, suffix=27))

    assert not store.publications
    assert not list(tmp_path.glob("stage_*.a*.f*"))


def test_index_buffers_are_admitted_before_batch_allocation(tmp_path: Path) -> None:
    store = MemoryArtifactIO(tmp_path)
    document = json.loads(config_bytes())
    document.update(
        {
            "accumulation_steps": 11,
            "maximum_input_elements": 1_000_000,
            "microbatch_size": 1_000_000,
        }
    )
    config = store.add_bytes(
        canonical_json_bytes(document),
        media_type=CONFIG_MEDIA_TYPE,
        logical_kind=CONFIG_LOGICAL_KIND,
    )
    inputs = torch.zeros((1, 1), dtype=torch.float32)
    dataset = store.add_bytes(
        save_safetensors({"inputs": inputs, "targets": inputs.clone()}),
        media_type=DATASET_MEDIA_TYPE,
        logical_kind=DATASET_LOGICAL_KIND,
    )
    engine = ReferenceAffineTrainingEngine(
        store,
        workspace_root=tmp_path,
        checkpoint_committer=checkpoint_committer(store),
    )

    with pytest.raises(ResourceExhausted, match="working set"):
        build_executor(engine).execute(stage(config, dataset, suffix=30))

    assert not store.publications
    assert not list(tmp_path.glob("stage_*.a*.f*"))


def test_engine_requires_canonical_checkpoint_committer_before_work(tmp_path: Path) -> None:
    store = MemoryArtifactIO(tmp_path)
    config, dataset = add_inputs(store)
    engine = ReferenceAffineTrainingEngine(store, workspace_root=tmp_path)

    with pytest.raises(FailedPrecondition, match="canonical checkpoint committer"):
        build_executor(engine).execute(stage(config, dataset, suffix=21))

    assert not store.publications
    assert not list(tmp_path.glob("stage_*.a*.f*"))


def test_resume_rejects_cursor_counter_divergence(tmp_path: Path) -> None:
    store = MemoryArtifactIO(tmp_path)
    config, dataset = add_inputs(store)
    engine = ReferenceAffineTrainingEngine(
        store,
        workspace_root=tmp_path,
        checkpoint_committer=checkpoint_committer(store),
    )
    first = build_executor(engine).execute(stage(config, dataset, suffix=23))
    checkpoint_root = store.materialize_tree(first.outputs[0])
    manifest_path = checkpoint_root / "manifest.json"
    manifest = json.loads(manifest_path.read_bytes())
    manifest["data_position"] += 1
    changed_manifest = canonical_json_bytes(manifest)
    manifest_path.write_bytes(changed_manifest)
    changed_inner_reference = reference_bytes(
        changed_manifest,
        media_type=DCP_MANIFEST_MEDIA_TYPE,
        logical_kind=DCP_MANIFEST_LOGICAL_KIND,
    )

    def bind_changed_inner(outer: training_checkpoint_pb2.CheckpointManifest) -> None:
        outer.data_position = int(manifest["data_position"])
        copy_artifact_ref(
            next(
                component
                for component in outer.components
                if component.relative_path == "manifest.json"
            ).artifact,
            changed_inner_reference,
        )

    changed_resume = rehash_checkpoint_manifest(
        store,
        first.outputs[0],
        checkpoint_root,
        bind_changed_inner,
    )
    publication_count = len(store.publications)

    with pytest.raises(FailedPrecondition, match="cursor does not match"):
        build_executor(engine).execute(stage(config, dataset, suffix=24, resume=changed_resume))

    assert len(store.publications) == publication_count


def test_resume_rejects_outer_inner_semantic_mismatches(tmp_path: Path) -> None:
    store = MemoryArtifactIO(tmp_path)
    config, dataset = add_inputs(store)
    engine = ReferenceAffineTrainingEngine(
        store,
        workspace_root=tmp_path,
        checkpoint_committer=checkpoint_committer(store),
    )
    first = build_executor(engine).execute(stage(config, dataset, suffix=28))
    checkpoint_root = store.materialize_tree(first.outputs[0])

    def change_counter(outer: training_checkpoint_pb2.CheckpointManifest) -> None:
        outer.counters.samples += 1

    def change_provenance(outer: training_checkpoint_pb2.CheckpointManifest) -> None:
        outer.source.digest = Digest.of_text("substituted-source").text

    def change_component(outer: training_checkpoint_pb2.CheckpointManifest) -> None:
        component = next(
            item for item in outer.components if item.relative_path == "model.safetensors"
        )
        component.artifact.digest = Digest.of_text("substituted-model-component").text

    publication_count = len(store.publications)
    for index, mutate in enumerate((change_counter, change_provenance, change_component), start=29):
        changed_resume = rehash_checkpoint_manifest(
            store,
            first.outputs[0],
            checkpoint_root,
            mutate,
        )
        with pytest.raises(FailedPrecondition, match="outer checkpoint semantics"):
            build_executor(engine).execute(
                stage(
                    config,
                    dataset,
                    suffix=index,
                    resume=changed_resume,
                    extra_metadata={"resume_checkpoint_id": resource("checkpoint", 28)},
                )
            )

    assert len(store.publications) == publication_count


def test_resume_rejects_materialized_tree_substitution(tmp_path: Path) -> None:
    store = MemoryArtifactIO(tmp_path)
    config, dataset = add_inputs(store)
    engine = ReferenceAffineTrainingEngine(
        store,
        workspace_root=tmp_path,
        checkpoint_committer=checkpoint_committer(store),
    )
    first = build_executor(engine).execute(stage(config, dataset, suffix=25))
    admitted_root = store.materialize_tree(first.outputs[0])
    substitute = (tmp_path / "substituted-checkpoint").resolve()
    shutil.copytree(admitted_root, substitute)
    manifest_path = substitute / "manifest.json"
    manifest = json.loads(manifest_path.read_bytes())
    manifest["data_position"] += 1
    manifest_path.write_bytes(canonical_json_bytes(manifest))
    store.trees[first.outputs[0].digest.text] = substitute
    publication_count = len(store.publications)

    with pytest.raises(FailedPrecondition, match="externally admitted digest"):
        build_executor(engine).execute(stage(config, dataset, suffix=26, resume=first.outputs[0]))

    assert len(store.publications) == publication_count


def test_cancellation_and_deadline_prevent_publication(tmp_path: Path) -> None:
    store = MemoryArtifactIO(tmp_path)
    config, dataset = add_inputs(store)
    engine = ReferenceAffineTrainingEngine(
        store,
        workspace_root=tmp_path,
        checkpoint_committer=checkpoint_committer(store),
    )
    token = CancellationToken()
    token.cancel()
    canceled = ExecutionContext(100, lambda: 0, token)
    with pytest.raises(Canceled):
        engine.execute(stage(config, dataset, suffix=14), canceled)
    assert not store.publications

    ticks = iter((0, 1, 2, 3, 4, 5, 6, 7, 8, 9))
    expired = ExecutionContext(3, lambda: next(ticks), CancellationToken())
    with pytest.raises(DeadlineExceeded):
        engine.execute(stage(config, dataset, suffix=15), expired)
    assert not store.publications


def test_optional_mlflow_outage_never_becomes_training_authority(tmp_path: Path) -> None:
    class FailingClient:
        def create_run(
            self,
            experiment_id: Any,
            *,
            tags: Any = None,
            run_name: Any = None,
        ) -> Never:
            del experiment_id, tags, run_name
            raise ConnectionError("tracking unavailable")

        def log_text(self, *args: Any, **kwargs: Any) -> None:
            del args, kwargs

        def log_param(self, *args: Any, **kwargs: Any) -> None:
            del args, kwargs

        def log_metric(self, *args: Any, **kwargs: Any) -> None:
            del args, kwargs

        def set_tag(self, *args: Any, **kwargs: Any) -> None:
            del args, kwargs

        def set_terminated(self, *args: Any, **kwargs: Any) -> None:
            del args, kwargs

    store = MemoryArtifactIO(tmp_path)
    config, dataset = add_inputs(store)
    exporter = MLflowExporter(FailingClient(), "reference")
    engine = ReferenceAffineTrainingEngine(
        store,
        workspace_root=tmp_path,
        checkpoint_committer=checkpoint_committer(store),
        mlflow_exporter=exporter,
    )
    result = build_executor(engine).execute(
        stage(
            config,
            dataset,
            suffix=16,
            extra_metadata={
                "runtime_image_digest": Digest.of_text("runtime-image").text,
                "source_revision": "a" * 40,
            },
        )
    )
    assert len(result.outputs) == 4
    assert result.metrics["mirror_failures"] == 1.0

    with pytest.raises(InvalidArgument, match="optional MLflow mirror"):
        ReferenceAffineTrainingEngine(
            store,
            workspace_root=tmp_path,
            checkpoint_committer=checkpoint_committer(store),
            mlflow_exporter=MLflowExporter(FailingClient(), "reference", required=True),
        )


def test_mlflow_terminal_status_preserves_stage_fault_classification(tmp_path: Path) -> None:
    metadata = {
        "runtime_image_digest": Digest.of_text("runtime-image").text,
        "source_revision": "a" * 40,
    }

    canceled_store = MemoryArtifactIO(tmp_path / "canceled")
    canceled_config, canceled_dataset = add_inputs(canceled_store)
    token = CancellationToken()
    canceled_client = RecordingMLflowClient(cancel_on_parameter=token)
    canceled_engine = ReferenceAffineTrainingEngine(
        canceled_store,
        workspace_root=canceled_store.root,
        checkpoint_committer=checkpoint_committer(canceled_store),
        mlflow_exporter=MLflowExporter(canceled_client, "reference"),
    )
    with pytest.raises(Canceled):
        canceled_engine.execute(
            stage(
                canceled_config,
                canceled_dataset,
                suffix=17,
                extra_metadata=metadata,
            ),
            ExecutionContext(9_000_000_000_000, lambda: 0, token),
        )
    assert canceled_client.statuses == ["KILLED"]
    assert not canceled_store.publications

    deadline_store = MemoryArtifactIO(tmp_path / "deadline")
    deadline_config, deadline_dataset = add_inputs(deadline_store)
    deadline_client = RecordingMLflowClient()
    clock_calls = 0

    def expiring_clock() -> int:
        nonlocal clock_calls
        clock_calls += 1
        return 0 if clock_calls <= 2 else 5

    deadline_engine = ReferenceAffineTrainingEngine(
        deadline_store,
        workspace_root=deadline_store.root,
        checkpoint_committer=checkpoint_committer(deadline_store),
        mlflow_exporter=MLflowExporter(deadline_client, "reference"),
    )
    with pytest.raises(DeadlineExceeded):
        deadline_engine.execute(
            stage(
                deadline_config,
                deadline_dataset,
                suffix=18,
                extra_metadata=metadata,
            ),
            ExecutionContext(5, expiring_clock, CancellationToken()),
        )
    assert deadline_client.statuses == ["KILLED"]
    assert not deadline_store.publications

    class FailingPublicationStore(MemoryArtifactIO):
        def publish_tree(
            self,
            *,
            namespace: str,
            name: str,
            source: Path,
            reference: ArtifactRef,
        ) -> ArtifactRef:
            del namespace, name, source, reference
            raise OSError("authoritative publication unavailable")

    failed_store = FailingPublicationStore(tmp_path / "failed")
    failed_config, failed_dataset = add_inputs(failed_store)
    failed_client = RecordingMLflowClient()
    failed_committer = RecordingCheckpointCommitter(checkpoint_committer(failed_store))
    failed_engine = ReferenceAffineTrainingEngine(
        failed_store,
        workspace_root=failed_store.root,
        checkpoint_committer=failed_committer,
        mlflow_exporter=MLflowExporter(failed_client, "reference"),
    )
    with pytest.raises(OSError, match="publication unavailable"):
        failed_engine.execute(
            stage(
                failed_config,
                failed_dataset,
                suffix=19,
                extra_metadata=metadata,
            ),
            ExecutionContext(9_000_000_000_000, lambda: 1, CancellationToken()),
        )
    assert failed_client.statuses == ["FAILED"]
    assert failed_committer.prepare_calls == 1
    assert failed_committer.commit_calls == 0


def test_cleanup_failure_cannot_reclassify_committed_outputs(tmp_path: Path) -> None:
    class CleanupFailingEngine(ReferenceAffineTrainingEngine):
        @staticmethod
        def _remove_workspace(workspace: Path) -> None:
            del workspace
            raise OSError("scratch cleanup unavailable")

    store = MemoryArtifactIO(tmp_path)
    config, dataset = add_inputs(store)
    result = build_executor(
        CleanupFailingEngine(
            store,
            workspace_root=tmp_path,
            checkpoint_committer=checkpoint_committer(store),
        )
    ).execute(stage(config, dataset, suffix=20))
    assert len(result.outputs) == 4
    assert "cleanup_failures" not in result.metrics
    assert list(tmp_path.glob("stage_*.a*.f*"))


def test_deadline_after_terminal_commit_cannot_reclassify_success(tmp_path: Path) -> None:
    class MutableClock:
        value = 1

        def __call__(self) -> int:
            return self.value

    clock = MutableClock()
    store = MemoryArtifactIO(tmp_path)
    config, dataset = add_inputs(store)
    committer = RecordingCheckpointCommitter(
        checkpoint_committer(store),
        after_commit=lambda: setattr(clock, "value", 9_000_000_000_000),
    )
    result = build_executor(
        ReferenceAffineTrainingEngine(
            store,
            workspace_root=tmp_path,
            checkpoint_committer=committer,
        ),
        now_millis=clock,
    ).execute(stage(config, dataset, suffix=22))

    assert committer.commit_calls == 1
    assert clock.value == 9_000_000_000_000
    assert len(result.outputs) == 4
