# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from pathlib import Path
from typing import Any

import pytest

from ci.gpu.pipeline import GpuTarget, load_matrix


def test_gpu_matrix_models_all_reviewed_targets_and_promotes_nothing() -> None:
    targets = load_matrix(Path(__file__).with_name("targets.yaml"))
    assert {target.architecture for target in targets} == {
        "sm_90",
        "sm_100",
        "sm_120",
        "gfx90a",
        "gfx942",
        "gfx950",
    }
    qualified = [target for target in targets if target.qualification]
    assert qualified == []
    assert all(part.startswith("//") for target in targets for part in target.bazel_targets)
    hopper = next(target for target in targets if target.architecture == "sm_90")
    assert hopper.required_sanitizers == (
        "memcheck",
        "racecheck",
        "initcheck",
        "synccheck",
    )
    assert hopper.command()[:6] == (
        "tools/dev/nixw",
        "develop",
        ".#ci-bazel",
        "--command",
        "tools/dev/bazelw",
        "test",
    )
    assert "--test_env=MINDCLADE_RUNTIME_IMAGE_DIGEST" in hopper.command()


def test_gpu_target_parser_rejects_boolean_coercion_and_unknown_fields() -> None:
    payload: dict[str, Any] = {
        "name": "test",
        "target": "cuda",
        "architecture": "sm_90",
        "runner_label": "h100",
        "qualification": "false",
        "required_sanitizers": [],
        "bazel_targets": ["//tests:test"],
    }
    with pytest.raises(TypeError, match="qualification"):
        GpuTarget.from_dict(payload)
    payload["qualification"] = False
    payload["unexpected"] = True
    with pytest.raises(ValueError, match="unknown"):
        GpuTarget.from_dict(payload)


@pytest.mark.parametrize(
    "payload, error",
    [
        ('{"schema_version":1,"schema_version":1,"targets":[]}', "duplicate"),
        ('{"schema_version":true,"targets":[]}', "schema"),
        ('{"schema_version":1,"targets":[],"unexpected":true}', "unknown"),
        ('{"schema_version":1,"targets":{}}', "JSON array"),
    ],
)
def test_gpu_matrix_rejects_noncanonical_top_level_payloads(
    tmp_path: Path, payload: str, error: str
) -> None:
    matrix = tmp_path / "matrix.json"
    matrix.write_text(payload)
    with pytest.raises((TypeError, ValueError), match=error):
        load_matrix(matrix)


def test_gpu_target_rejects_backend_architecture_mismatch() -> None:
    with pytest.raises(ValueError, match="does not match"):
        GpuTarget(
            name="mismatch",
            target="hip",
            architecture="sm_90",
            runner_label="gpu",
            qualification=False,
            required_sanitizers=(),
            bazel_targets=("//tests:test",),
        )


def test_gpu_matrix_rejects_untrusted_symbolic_links(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.delenv("RUNFILES_DIR", raising=False)
    monkeypatch.delenv("TEST_SRCDIR", raising=False)
    target = tmp_path / "target.json"
    target.write_text('{"schema_version":1,"targets":[]}')
    symbolic = tmp_path / "symbolic.json"
    symbolic.symlink_to(target)
    with pytest.raises(ValueError, match="symbolic-link"):
        load_matrix(symbolic)
