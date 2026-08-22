# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Two-rank process body for the concrete training StageEngine test."""

from __future__ import annotations

import json
import os
import shutil
from collections.abc import Iterable
from pathlib import Path
from typing import Any
from unittest.mock import patch

import torch

from libs.python.artifacts import reference_bytes, verify_bytes
from libs.python.identifiers import ArtifactRef, Digest
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
from services.workers.training.qualification import (
    LocalCheckpointCommitter,
    local_checkpoint_provenance,
)


class FilesystemArtifactIO:
    def __init__(self, root: Path) -> None:
        self._root = root
        config = (root / "config.json").read_bytes()
        dataset = (root / "dataset.safetensors").read_bytes()
        self.config = reference_bytes(
            config,
            media_type=CONFIG_MEDIA_TYPE,
            logical_kind=CONFIG_LOGICAL_KIND,
        )
        self.dataset = reference_bytes(
            dataset,
            media_type=DATASET_MEDIA_TYPE,
            logical_kind=DATASET_LOGICAL_KIND,
        )
        self._bytes = {
            self.config.digest.text: config,
            self.dataset.digest.text: dataset,
        }

    def read(self, reference: ArtifactRef) -> Iterable[bytes]:
        return (self._bytes[reference.digest.text],)

    def materialize_tree(self, reference: ArtifactRef) -> Path:
        return (self._root / "objects" / reference.digest.hex).resolve()

    def register_checkpoint(
        self,
        reference: ArtifactRef,
        manifest_bytes: bytes,
        source: Path,
    ) -> None:
        del reference, manifest_bytes, source

    def publish_bytes(
        self,
        *,
        namespace: str,
        name: str,
        content: bytes,
        reference: ArtifactRef,
    ) -> ArtifactRef:
        del namespace, name
        verify_bytes(reference, content)
        (self._root / "objects" / f"{reference.digest.hex}.bin").write_bytes(content)
        return reference

    def publish_tree(
        self,
        *,
        namespace: str,
        name: str,
        source: Path,
        reference: ArtifactRef,
    ) -> ArtifactRef:
        del namespace, name
        if os.environ.get("MINDCLADE_TRAINING_ENGINE_TEST_MODE") == "rank1-deadline":
            (self._root / "rank-zero-staging").touch()
        destination = self._root / "objects" / reference.digest.hex
        shutil.copytree(source, destination)
        return reference


def resource(kind: str, suffix: int) -> str:
    return f"{kind}_019c00000000700080000000000000{suffix:02x}"


def main() -> None:
    root = Path(os.environ["MINDCLADE_TRAINING_ENGINE_TEST_ROOT"]).resolve()
    mode = os.environ.get("MINDCLADE_TRAINING_ENGINE_TEST_MODE", "success")
    artifacts = FilesystemArtifactIO(root)
    topology = reference_topology_digest(
        backend="gloo",
        device_type="cpu",
        world_size=2,
        local_world_size=2,
    )
    stage = StageEnvelope(
        stage_id=resource("stage", 41),
        kind=StageKind.TRAINING,
        operation=TRAINING_OPERATION,
        inputs=(artifacts.config, artifacts.dataset),
        output_namespace="integration/ddp-reference-training",
        resolved_config_digest=artifacts.config.digest.text,
        reference_snapshot_digest=None,
        attempt=1,
        fencing_token=41,
        deadline_unix_millis=5 if mode == "rank1-deadline" else 9_000_000_000_000,
        metadata={
            "backend": "gloo",
            "checkpoint_id": resource("checkpoint", 42),
            "code_digest": Digest.of_text("ddp-integration-source").text,
            "compatibility_policy_digest": Digest.of_text("replicated-adamw-v1").text,
            "device_type": "cpu",
            "local_world_size": "2",
            "model_digest": Digest.of_text("reference-affine-v1").text,
            "run_id": resource("run", 43),
            "runtime_image_digest": Digest.of_text("ddp-test-runtime").text,
            "toolchain_digest": Digest.of_text("ddp-integration-toolchain").text,
            "topology_digest": topology,
            "world_size": "2",
        },
    )
    rank = int(os.environ["RANK"])
    if mode == "rank-zero-clock-fault":
        original_step = torch.optim.AdamW.step

        def marked_step(optimizer: Any, *args: Any, **kwargs: Any) -> Any:
            result = original_step(optimizer, *args, **kwargs)
            (root / f"optimizer-stepped-rank-{rank}").touch()
            return result

        patch.object(torch.optim.AdamW, "step", marked_step).start()

    def now_millis() -> int:
        if mode == "rank1-deadline" and rank == 1 and (root / "rank-zero-staging").exists():
            return 5
        if (
            mode == "rank-zero-clock-fault"
            and rank == 0
            and (root / "optimizer-stepped-rank-0").exists()
        ):
            raise RuntimeError("injected rank-zero clock fault")
        return 1

    result = build_executor(
        ReferenceAffineTrainingEngine(
            artifacts,
            workspace_root=root,
            checkpoint_committer=LocalCheckpointCommitter(
                root,
                local_checkpoint_provenance(
                    model=b"reference-affine-v1",
                    source=b"ddp-integration-source",
                    toolchain=b"ddp-integration-toolchain",
                    environment=b"ddp-test-runtime",
                    compatibility_policy=b"replicated-adamw-v1",
                ),
                artifacts,
            ),
            distributed_timeout_seconds=60,
        ),
        now_millis=now_millis,
    ).execute(stage)
    if rank == 0:
        if len(result.outputs) != 4:
            raise AssertionError("rank zero did not publish all training outputs")
        (root / "success.json").write_text(
            json.dumps(
                {
                    "checkpoint_digest": result.outputs[0].digest.text,
                    "optimizer_steps": result.metrics["optimizer_steps"],
                    "samples": result.metrics["samples"],
                    "world_size": result.metrics["world_size"],
                },
                sort_keys=True,
            ),
            encoding="utf-8",
        )
    elif result.outputs:
        raise AssertionError("nonzero rank attempted authoritative publication")


if __name__ == "__main__":
    main()
