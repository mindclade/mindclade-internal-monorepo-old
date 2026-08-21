# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

from copy import deepcopy

import pytest

from tools.qualification.training_gke.run import (
    PHASES,
    load_template,
    render_resource,
    validate_live_capacity,
    validate_phase_evidence,
)

IMAGE = "us-docker.pkg.dev/project/releases/trainer@sha256:" + "a" * 64
AGENT = "us-docker.pkg.dev/project/releases/checkpoint@sha256:" + "b" * 64


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
                        {"resources": [{"name": "nvidia.com/gpu", "nominalQuota": str(quota)}]}
                    ]
                }
            ]
        }
    }


def _nodes() -> dict:
    return {
        "items": [
            {
                "metadata": {
                    "labels": {
                        "mindclade.dev/gpu-profile": "gke-h100-a3-megagpu-8g",
                        "mindclade.dev/capacity-type": "on-demand",
                    }
                },
                "status": {
                    "allocatable": {"nvidia.com/gpu": "8"},
                    "conditions": [{"type": "Ready", "status": "True"}],
                },
            }
        ]
    }


def _evidence(phase) -> dict:
    return {
        "schema_version": 1,
        "phase": phase.name,
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
        "gpu_hours": 0.1,
    }


def test_templates_are_held_and_render_only_with_two_pinned_images() -> None:
    for phase in PHASES:
        template = load_template(phase)
        assert template["spec"]["suspend"] is True
        rendered = render_resource(
            phase,
            trainer_image=IMAGE,
            checkpoint_image=AGENT,
            run_id="qualification-1",
        )
        assert rendered["spec"]["suspend"] is False
        assert rendered["metadata"]["name"].endswith("qualification-1")
    with pytest.raises(ValueError, match="nonzero sha256"):
        render_resource(
            PHASES[0],
            trainer_image="trainer:latest",
            checkpoint_image=AGENT,
            run_id="qualification-1",
        )


def test_capacity_is_phase_specific_and_on_demand() -> None:
    for phase in PHASES:
        validate_live_capacity(phase, _namespace(), _queue(), _nodes())
        with pytest.raises(RuntimeError, match="measured quota"):
            validate_live_capacity(phase, _namespace(), _queue(phase.gpu_count - 1), _nodes())
    spot = _nodes()
    spot["items"][0]["metadata"]["labels"]["mindclade.dev/capacity-type"] = "spot"
    with pytest.raises(RuntimeError, match="on-demand"):
        validate_live_capacity(PHASES[0], _namespace(), _queue(), spot)
    blocked = _namespace()
    blocked["metadata"]["labels"]["mindclade.dev/workload-activation"] = "blocked"
    with pytest.raises(RuntimeError, match="qualification activation"):
        validate_live_capacity(PHASES[0], blocked, _queue(), _nodes())


def test_evidence_is_bound_to_exact_phase_and_success_invariants() -> None:
    for phase in PHASES:
        evidence = _evidence(phase)
        assert validate_phase_evidence(phase, evidence) == evidence
        wrong_rank = deepcopy(evidence)
        wrong_rank["ranks_completed"] -= 1
        with pytest.raises(ValueError, match="rank count"):
            validate_phase_evidence(phase, wrong_rank)
        failed_resume = deepcopy(evidence)
        failed_resume["resume_exact"] = False
        with pytest.raises(ValueError, match="resume_exact"):
            validate_phase_evidence(phase, failed_resume)
