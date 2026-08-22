# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Launch the concrete StageEngine under a real two-process Gloo world."""

from __future__ import annotations

import json
import os
import socket
import subprocess
import sys
from pathlib import Path

import torch
from safetensors.torch import save as save_safetensors

from libs.python.serialization import canonical_json_bytes
from services.workers.training import TRAINING_OPERATION


def _write_inputs(tmp_path: Path) -> None:
    (tmp_path / "objects").mkdir()
    (tmp_path / "config.json").write_bytes(
        canonical_json_bytes(
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
                "maximum_optimizer_steps": 2,
                "microbatch_size": 5,
                "model": "reference-affine-v1",
                "model_operation": "reference.affine.v1",
                "optimizer_steps_per_execution": 2,
                "schema_version": 1,
                "seed": 37,
                "weight_decay": "0",
            }
        )
    )
    inputs = torch.tensor([[-2.0], [-1.0], [0.0], [1.0], [2.0]], dtype=torch.float32)
    (tmp_path / "dataset.safetensors").write_bytes(
        save_safetensors({"inputs": inputs, "targets": (inputs * 3.0) - 1.0})
    )


def _run_world(tmp_path: Path, *, mode: str = "success") -> subprocess.CompletedProcess[str]:
    environment = os.environ.copy()
    environment["MINDCLADE_TRAINING_ENGINE_TEST_ROOT"] = str(tmp_path.resolve())
    environment["MINDCLADE_TRAINING_ENGINE_TEST_MODE"] = mode
    environment["PYTHONPATH"] = os.pathsep.join(dict.fromkeys(filter(None, sys.path)))
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as listener:
        listener.bind(("127.0.0.1", 0))
        port = listener.getsockname()[1]
    return subprocess.run(
        [
            sys.executable,
            "-m",
            "torch.distributed.run",
            "--master-addr=127.0.0.1",
            f"--master-port={port}",
            "--nproc-per-node=2",
            "--module",
            "services.workers.training.tests.ddp_worker",
        ],
        check=False,
        capture_output=True,
        text=True,
        timeout=120,
        env=environment,
    )


def test_two_rank_stage_engine_trains_checkpoints_and_publishes_on_rank_zero(
    tmp_path: Path,
) -> None:
    _write_inputs(tmp_path)
    completed = _run_world(tmp_path)
    assert completed.returncode == 0, f"stdout:\n{completed.stdout}\nstderr:\n{completed.stderr}"
    result = json.loads((tmp_path / "success.json").read_text(encoding="utf-8"))
    assert result["world_size"] == 2.0
    assert result["optimizer_steps"] == 2.0
    assert result["samples"] == 10.0
    assert result["checkpoint_digest"].startswith("sha256:")


def test_rank_one_deadline_at_final_collective_prevents_terminal_commit(tmp_path: Path) -> None:
    _write_inputs(tmp_path)
    completed = _run_world(tmp_path, mode="rank1-deadline")

    assert completed.returncode != 0
    diagnostics = completed.stdout + completed.stderr
    assert "stage deadline" in diagnostics or "DeadlineExceeded" in diagnostics
    assert not list((tmp_path / "committed-checkpoints").glob("*/TERMINAL"))
    assert not (tmp_path / "success.json").exists()


def test_rank_zero_clock_fault_completes_collectively_without_terminal_commit(
    tmp_path: Path,
) -> None:
    _write_inputs(tmp_path)
    completed = _run_world(tmp_path, mode="rank-zero-clock-fault")

    assert completed.returncode != 0
    diagnostics = completed.stdout + completed.stderr
    assert "stage clock failed" in diagnostics or "stage_clock" in diagnostics
    assert not list((tmp_path / "committed-checkpoints").glob("*/TERMINAL"))
    assert not (tmp_path / "success.json").exists()
