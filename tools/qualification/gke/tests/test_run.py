# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

from copy import deepcopy

import pytest

from tools.qualification.gke.run import (
    PROFILES,
    REQUIRED_METRICS,
    load_template,
    render_job,
    validate_evidence,
    validate_live_capacity,
)


def _namespace(profile_index: int) -> dict:
    profile = PROFILES[profile_index]
    return {
        "metadata": {
            "labels": {
                "mindclade.dev/workload-activation": "active",
                "mindclade.dev/kueue-enabled": "true",
                "mindclade.dev/workload-class": profile.workload_class,
            }
        }
    }


def _queue() -> dict:
    return {
        "spec": {
            "resourceGroups": [
                {"flavors": [{"resources": [{"name": "nvidia.com/gpu", "nominalQuota": "8"}]}]}
            ]
        }
    }


def _nodes(profile_index: int) -> dict:
    profile = PROFILES[profile_index]
    return {
        "items": [
            {
                "metadata": {"labels": {"mindclade.dev/gpu-profile": profile.node_profile}},
                "status": {
                    "allocatable": {"nvidia.com/gpu": "8"},
                    "conditions": [{"type": "Ready", "status": "True"}],
                },
            }
        ]
    }


def test_templates_are_suspended_and_render_only_with_pinned_image() -> None:
    image = "us-docker.pkg.dev/project/release/qualification@sha256:" + "a" * 64
    for profile in PROFILES:
        assert load_template(profile)["spec"]["suspend"] is True
        rendered = render_job(profile, image, "release-123")
        assert rendered["spec"]["suspend"] is False
        assert rendered["metadata"]["name"].endswith(f"{profile.name}-release-123")
        assert rendered["spec"]["template"]["spec"]["containers"][0]["image"] == image
    with pytest.raises(ValueError, match="nonzero sha256"):
        render_job(PROFILES[0], "qualification:latest", "release-123")


def test_live_capacity_requires_active_queue_quota_and_ready_profile_node() -> None:
    for index, profile in enumerate(PROFILES):
        validate_live_capacity(profile, _namespace(index), _queue(), _nodes(index))

        blocked = _namespace(index)
        blocked["metadata"]["labels"]["mindclade.dev/workload-activation"] = "blocked"
        with pytest.raises(RuntimeError, match="not activated"):
            validate_live_capacity(profile, blocked, _queue(), _nodes(index))

        held = _queue()
        held["spec"]["stopPolicy"] = "Hold"
        with pytest.raises(RuntimeError, match="held"):
            validate_live_capacity(profile, _namespace(index), held, _nodes(index))

        no_nodes = deepcopy(_nodes(index))
        no_nodes["items"][0]["status"]["conditions"][0]["status"] = "False"
        with pytest.raises(RuntimeError, match="no Ready node"):
            validate_live_capacity(profile, _namespace(index), _queue(), no_nodes)


def test_evidence_requires_exact_positive_metrics_and_matching_gpu() -> None:
    for profile in PROFILES:
        evidence = {key: 1.0 for key in REQUIRED_METRICS}
        evidence.update(
            schema_version=1,
            hardware_profile=profile.name,
            gpu_name=f"NVIDIA {profile.name.upper()} 80GB",
        )
        assert validate_evidence(profile, evidence) == evidence
        evidence["gpu_memory_bytes"] = 0
        with pytest.raises(ValueError, match="gpu_memory_bytes"):
            validate_evidence(profile, evidence)
