# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

import json
import sys
import time
from copy import deepcopy
from pathlib import Path

import pytest

from configs.contract_validation import load_json, validate, validate_schema_subset
from tools.qualification.training_gke.run import (
    PHASES,
    _run_bounded,
    cohort_digest,
    load_smoke_prerequisite,
    load_template,
    main,
    render_resource,
    validate_cohort,
    validate_live_capacity,
    validate_phase_evidence,
    validate_smoke_prerequisite,
    validate_template,
    write_evidence,
)

IMAGE = "us-docker.pkg.dev/project/releases/trainer@sha256:" + "a" * 64
AGENT = "us-docker.pkg.dev/project/releases/checkpoint@sha256:" + "b" * 64
SCHEMA = Path(__file__).resolve().parents[1] / "cohort.schema.json"


def _namespace() -> dict:
    return {
        "metadata": {
            "labels": {
                "mindclade.dev/workload-activation": "qualification",
                "mindclade.dev/kueue-enabled": "true",
                "mindclade.dev/workload-class": "training-h100",
            }
        }
    }


def _queue(quota: int = 8) -> dict:
    return {
        "spec": {
            "resourceGroups": [
                {
                    "flavors": [
                        {
                            "name": "mindclade-h100",
                            "resources": [{"name": "nvidia.com/gpu", "nominalQuota": str(quota)}],
                        }
                    ]
                }
            ]
        }
    }


def _local_queue() -> dict:
    return {"spec": {"clusterQueue": "mindclade-training-h100"}}


def _nodes() -> dict:
    return {
        "items": [
            {
                "metadata": {
                    "labels": {
                        "mindclade.dev/gpu-profile": "gke-h100-a3-megagpu-8g",
                        "mindclade.dev/capacity-type": "on-demand",
                        "topology.kubernetes.io/zone": "us-central1-a",
                    }
                },
                "status": {
                    "allocatable": {"nvidia.com/gpu": "8"},
                    "conditions": [{"type": "Ready", "status": "True"}],
                },
            }
        ]
    }


def _cohort() -> dict:
    return {
        "schema_version": "mindclade.dev/training-qualification-cohort/v1",
        "source_repository": "mindclade/mindclade-internal-monorepo",
        "source_revision": "e" * 40,
        "resolved_config_digest": "sha256:" + "1" * 64,
        "dataset_digest": "sha256:" + "2" * 64,
        "model_contract_digest": "sha256:" + "3" * 64,
        "toolchain_digest": "sha256:" + "4" * 64,
        "trainer_image": IMAGE,
        "checkpoint_agent_image": AGENT,
        "checkpoint_schema_version": "mindclade.dev/training-checkpoint/dcp-v1",
        "zone": "us-central1-a",
        "node_profile": "gke-h100-a3-megagpu-8g",
        "capacity_type": "on-demand",
        "pricing_snapshot_digest": "sha256:" + "5" * 64,
        "phases": [phase.name for phase in PHASES],
    }


def _evidence(phase) -> dict:
    return {
        "schema_version": 1,
        "phase": phase.name,
        "cohort_digest": cohort_digest(_cohort()),
        "hardware_profile": "h100",
        "capacity_type": "on-demand",
        "gpu_name": "NVIDIA H100 80GB HBM3",
        "world_size": phase.world_size,
        "ranks_completed": phase.world_size,
        "samples": 64,
        "optimizer_steps": 4,
        "loss_numerator": 1.0,
        "loss_denominator": 64,
        "checkpoint_digest": "sha256:" + "c" * 64,
        "model_bundle_digest": "sha256:" + "d" * 64,
        "resume_exact": True,
        "serving_parity": True,
        "duration_seconds": 10.0,
        "gpu_hours": 10.0 * phase.gpu_count / 3_600,
    }


def test_templates_are_held_and_render_only_with_two_pinned_images() -> None:
    digest = cohort_digest(_cohort())
    for phase in PHASES:
        template = load_template(phase)
        assert template["spec"]["suspend"] is True
        rendered = render_resource(
            phase,
            trainer_image=IMAGE,
            checkpoint_image=AGENT,
            run_id="qualification-1",
            qualification_cohort_digest=digest,
            zone="us-central1-a",
        )
        assert rendered["spec"]["suspend"] is False
        assert rendered["metadata"]["name"].endswith("qualification-1")
        drifted = deepcopy(template)
        drifted["spec"]["suspend"] = False
        with pytest.raises(ValueError, match="template digest drifted"):
            validate_template(phase, drifted)
    with pytest.raises(ValueError, match="nonzero sha256"):
        render_resource(
            PHASES[0],
            trainer_image="trainer:latest",
            checkpoint_image=AGENT,
            run_id="qualification-1",
            qualification_cohort_digest=digest,
            zone="us-central1-a",
        )
    with pytest.raises(ValueError, match="exceed 63"):
        render_resource(
            PHASES[1],
            trainer_image=IMAGE,
            checkpoint_image=AGENT,
            run_id="q" * 40,
            qualification_cohort_digest=digest,
            zone="us-central1-a",
        )


def test_connected_cli_is_inert_until_external_verifier_is_wired(
    monkeypatch: pytest.MonkeyPatch,
    capsys: pytest.CaptureFixture[str],
) -> None:
    monkeypatch.setattr(sys, "argv", ["training-gke-run"])

    assert main() == 1
    assert "connected qualification is disabled" in capsys.readouterr().err


def test_subprocess_output_is_bounded() -> None:
    with pytest.raises(RuntimeError, match="output exceeded"):
        _run_bounded(
            [sys.executable, "-c", "print('x' * 4096)"],
            input_text=None,
            timeout_seconds=5,
            maximum_output_bytes=1024,
        )


def test_subprocess_deadline_includes_blocked_stdin_write() -> None:
    started = time.monotonic()
    with pytest.raises(RuntimeError, match="exceeded its deadline"):
        _run_bounded(
            [sys.executable, "-c", "import time; time.sleep(30)"],
            input_text="x" * (128 * 1024),
            timeout_seconds=1,
            maximum_output_bytes=1024,
        )
    assert time.monotonic() - started < 5


def test_cohort_is_closed_and_binds_every_immutable_input() -> None:
    cohort = _cohort()
    assert validate_cohort(cohort) == cohort
    assert cohort_digest(cohort).startswith("sha256:")
    schema = load_json(SCHEMA)
    assert validate_schema_subset(schema) == ()
    assert validate(cohort, schema) == ()
    assert set(schema["required"]) == set(cohort)
    missing = deepcopy(cohort)
    del missing["pricing_snapshot_digest"]
    with pytest.raises(ValueError, match="fields are incomplete"):
        validate_cohort(missing)
    mutable = deepcopy(cohort)
    mutable["trainer_image"] = "trainer:latest"
    with pytest.raises(ValueError, match="image is invalid"):
        validate_cohort(mutable)


def test_capacity_is_phase_specific_and_on_demand() -> None:
    for phase in PHASES:
        validate_live_capacity(
            phase, _namespace(), _queue(), _local_queue(), _nodes(), zone="us-central1-a"
        )
        with pytest.raises(RuntimeError, match="measured quota"):
            validate_live_capacity(
                phase,
                _namespace(),
                _queue(phase.gpu_count - 1),
                _local_queue(),
                _nodes(),
                zone="us-central1-a",
            )
    spot = _nodes()
    spot["items"][0]["metadata"]["labels"]["mindclade.dev/capacity-type"] = "spot"
    with pytest.raises(RuntimeError, match="on-demand"):
        validate_live_capacity(
            PHASES[0], _namespace(), _queue(), _local_queue(), spot, zone="us-central1-a"
        )
    blocked = _namespace()
    blocked["metadata"]["labels"]["mindclade.dev/workload-activation"] = "blocked"
    with pytest.raises(RuntimeError, match="qualification activation"):
        validate_live_capacity(
            PHASES[0], blocked, _queue(), _local_queue(), _nodes(), zone="us-central1-a"
        )
    with pytest.raises(RuntimeError, match="on-demand"):
        validate_live_capacity(
            PHASES[0],
            _namespace(),
            _queue(),
            _local_queue(),
            _nodes(),
            zone="us-east1-b",
        )
    invalid_quota = _queue()
    invalid_quota["spec"]["resourceGroups"][0]["flavors"][0]["resources"][0]["nominalQuota"] = "NaN"
    with pytest.raises(RuntimeError, match="quota is invalid"):
        validate_live_capacity(
            PHASES[0],
            _namespace(),
            invalid_quota,
            _local_queue(),
            _nodes(),
            zone="us-central1-a",
        )
    wrong_flavor = _queue()
    wrong_flavor["spec"]["resourceGroups"][0]["flavors"][0]["name"] = "mindclade-b200"
    with pytest.raises(RuntimeError, match="approved H100 flavor"):
        validate_live_capacity(
            PHASES[0],
            _namespace(),
            wrong_flavor,
            _local_queue(),
            _nodes(),
            zone="us-central1-a",
        )
    held_local_queue = _local_queue()
    held_local_queue["spec"]["stopPolicy"] = "Hold"
    with pytest.raises(RuntimeError, match="LocalQueue"):
        validate_live_capacity(
            PHASES[0],
            _namespace(),
            _queue(),
            held_local_queue,
            _nodes(),
            zone="us-central1-a",
        )


def test_evidence_is_bound_to_exact_phase_and_success_invariants() -> None:
    for phase in PHASES:
        evidence = _evidence(phase)
        digest = cohort_digest(_cohort())
        assert (
            validate_phase_evidence(
                phase,
                evidence,
                qualification_cohort_digest=digest,
            )
            == evidence
        )
        wrong_rank = deepcopy(evidence)
        wrong_rank["ranks_completed"] -= 1
        with pytest.raises(ValueError, match="rank count"):
            validate_phase_evidence(
                phase,
                wrong_rank,
                qualification_cohort_digest=digest,
            )
        boolean_version = deepcopy(evidence)
        boolean_version["schema_version"] = True
        with pytest.raises(ValueError, match="identity or rank count"):
            validate_phase_evidence(
                phase,
                boolean_version,
                qualification_cohort_digest=digest,
            )
        failed_resume = deepcopy(evidence)
        failed_resume["resume_exact"] = False
        with pytest.raises(ValueError, match="resume_exact"):
            validate_phase_evidence(
                phase,
                failed_resume,
                qualification_cohort_digest=digest,
            )
        nonfinite = deepcopy(evidence)
        nonfinite["loss_numerator"] = float("nan")
        with pytest.raises(ValueError, match="loss_numerator"):
            validate_phase_evidence(
                phase,
                nonfinite,
                qualification_cohort_digest=digest,
            )


def test_evidence_is_append_only_and_binds_the_cohort(tmp_path: Path) -> None:
    output = tmp_path / "evidence.json"
    phase = PHASES[0]
    cohort = _cohort()
    write_evidence(
        output,
        context="qualification-cluster",
        trainer_image=IMAGE,
        checkpoint_image=AGENT,
        run_id="qualification-1",
        cohort=cohort,
        result=_evidence(phase),
    )
    written = json.loads(output.read_text(encoding="utf-8"))
    assert written["cohort_digest"] == cohort_digest(cohort)
    assert validate_smoke_prerequisite(written, cohort=cohort) == written["evidence_digest"]
    assert load_smoke_prerequisite(output, cohort=cohort) == written["evidence_digest"]
    tampered = deepcopy(written)
    tampered["context"] = "different-cluster"
    with pytest.raises(ValueError, match="digest does not match"):
        validate_smoke_prerequisite(tampered, cohort=cohort)
    with pytest.raises(FileExistsError, match="already exists"):
        write_evidence(
            output,
            context="qualification-cluster",
            trainer_image=IMAGE,
            checkpoint_image=AGENT,
            run_id="qualification-1",
            cohort=cohort,
            result=_evidence(phase),
        )
