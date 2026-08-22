# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Launch the real two-process Gloo qualification smoke."""

from __future__ import annotations

import json
import os
import socket
import subprocess
import sys
from pathlib import Path

import torch

from libs.python.identifiers import Digest
from models.reference import ReferenceAffine
from training.checkpointing import (
    CheckpointIdentity,
    restore_distributed_checkpoint,
    save_distributed_checkpoint,
)
from training.contracts import SupervisedBatch
from training.core import Trainer
from training.tasks import SupervisedMSETask


def identity(topology: str) -> CheckpointIdentity:
    if topology.endswith("2"):
        checkpoint_id = "checkpoint_018bcfe8754073febc5bc0c0db31a415"
        run_id = "run_018bcfe875417430ac4115e39394de8b"
    else:
        checkpoint_id = "checkpoint_018bcfe9fbe0719ab771d281cd16bcd5"
        run_id = "run_018bcfe9fbe17211a807eaa7090f4105"
    return CheckpointIdentity(
        checkpoint_id=checkpoint_id,
        run_id=run_id,
        resolved_config_digest=Digest.of_text("distributed config").text,
        dataset_digest=Digest.of_text("deterministic affine samples").text,
        model_digest=Digest.of_text("reference-affine-v1").text,
        code_digest=Digest.of_text("source tree").text,
        toolchain_digest=Digest.of_text("pinned torch toolchain").text,
        topology_digest=Digest.of_text(topology).text,
    )


def optimizer(model: torch.nn.Module) -> torch.optim.AdamW:
    return torch.optim.AdamW(model.parameters(), lr=0.025, weight_decay=0.01, foreach=False)


def prepare_world_size_one_checkpoint(root: Path) -> None:
    model = ReferenceAffine()
    optim = optimizer(model)
    inputs = torch.tensor([[-2.0], [-0.5], [1.0], [2.0], [3.0]], dtype=torch.float32)
    trainer = Trainer(model, SupervisedMSETask(), optim)
    trainer.train((SupervisedBatch(inputs, (inputs * 3.0) - 1.0),))
    save_distributed_checkpoint(
        root / "world-size-1",
        model=model,
        optimizer=optim,
        training_state=trainer.state,
        identity=identity("cpu-world-size-1"),
        data_position=inputs.shape[0],
    )


def test_two_rank_gloo_training_dcp_and_replicated_portability(tmp_path: Path) -> None:
    prepare_world_size_one_checkpoint(tmp_path)
    environment = os.environ.copy()
    environment["MINDCLADE_DDP_TEST_ROOT"] = str(tmp_path)
    # rules_python assembles third-party wheels on the parent process's import path.
    # Propagate that hermetic runfiles path to torchrun's fresh interpreter so it sees
    # the same pinned PyTorch and workspace modules rather than host-site packages.
    environment["PYTHONPATH"] = os.pathsep.join(dict.fromkeys(filter(None, sys.path)))
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as listener:
        listener.bind(("127.0.0.1", 0))
        port = listener.getsockname()[1]
    completed = subprocess.run(
        [
            sys.executable,
            "-m",
            "torch.distributed.run",
            "--master-addr=127.0.0.1",
            f"--master-port={port}",
            "--nproc-per-node=2",
            "--module",
            "training.distributed.tests.ddp_worker",
        ],
        check=False,
        capture_output=True,
        text=True,
        timeout=120,
        env=environment,
    )
    assert completed.returncode == 0, f"stdout:\n{completed.stdout}\nstderr:\n{completed.stderr}"
    assert json.loads((tmp_path / "success.json").read_text(encoding="utf-8")) == {
        "world_size": 2,
        "backend": "gloo",
    }

    # Prove the reverse 2→1 replicated-state path outside a process group.
    model = ReferenceAffine()
    optim = optimizer(model)
    restored = restore_distributed_checkpoint(
        tmp_path / "world-size-2",
        model=model,
        optimizer=optim,
        expected_identity=identity("cpu-world-size-2"),
        allow_replicated_world_size_change=True,
    )
    assert not restored.exact_resume
    assert restored.source_rank == 0
    assert restored.training_state.optimizer_steps == 2
