# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Validate the held, single-node H100 reference-training qualification profile."""

from __future__ import annotations

import argparse
import json
import pathlib
from typing import Any

Resource = dict[str, Any]
ZERO_IMAGE_SUFFIX = "@sha256:" + "0" * 64
NAMESPACE = "mindclade-training-h100"
QUEUE = "mindclade-training-h100"
PROFILE = "gke-h100-a3-megagpu-8g"
CAPACITY_TYPE = "on-demand"


def _load(path: pathlib.Path) -> list[Resource]:
    value = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(value, list) or not all(isinstance(item, dict) for item in value):
        raise ValueError(f"{path}: expected a normalized JSON resource list")
    return value


def _one(resources: list[Resource], kind: str, name: str) -> Resource:
    matches = [
        item
        for item in resources
        if item.get("kind") == kind
        and (item.get("metadata", {}) or {}).get("name") == name
        and (item.get("metadata", {}) or {}).get("namespace") == NAMESPACE
    ]
    if len(matches) != 1:
        raise ValueError(f"expected one {kind}/{NAMESPACE}/{name}, found {len(matches)}")
    return matches[0]


def _pod_spec(workload: Resource) -> Resource:
    spec = workload.get("spec", {}) or {}
    if workload.get("kind") == "Job":
        return (spec.get("template", {}) or {}).get("spec", {}) or {}
    replicated = spec.get("replicatedJobs", []) or []
    if len(replicated) != 1 or replicated[0].get("name") != "trainer":
        raise ValueError("H100 qualification JobSet must have one trainer ReplicatedJob")
    if replicated[0].get("replicas") != 1:
        raise ValueError("H100 qualification JobSet must have exactly one Pod replica")
    job_spec = (replicated[0].get("template", {}) or {}).get("spec", {}) or {}
    if job_spec.get("parallelism") != 1 or job_spec.get("completions") != 1:
        raise ValueError("H100 qualification JobSet must create one non-retrying Job Pod")
    return (job_spec.get("template", {}) or {}).get("spec", {}) or {}


def _validate_workload(workload: Resource, *, phase: str, gpu_count: str) -> list[str]:
    errors: list[str] = []
    metadata = workload.get("metadata", {}) or {}
    labels = metadata.get("labels", {}) or {}
    spec = workload.get("spec", {}) or {}
    try:
        pod_spec = _pod_spec(workload)
    except ValueError as error:
        return [str(error)]
    containers = pod_spec.get("containers", []) or []
    trainer = next(
        (item for item in containers if isinstance(item, dict) and item.get("name") == "trainer"),
        {},
    )
    requests = (trainer.get("resources", {}) or {}).get("requests", {}) or {}
    limits = (trainer.get("resources", {}) or {}).get("limits", {}) or {}
    node_selector = pod_spec.get("nodeSelector", {}) or {}
    expected_labels = {
        "kueue.x-k8s.io/queue-name": QUEUE,
        "mindclade.dev/capacity-type": CAPACITY_TYPE,
        "mindclade.dev/qualification-phase": phase,
        "mindclade.dev/workload-class": "training-h100",
        "mindclade.dev/workload-state": "blocked",
    }
    if metadata.get("namespace") != NAMESPACE:
        errors.append(f"{phase}: namespace must remain {NAMESPACE}")
    if any(labels.get(key) != value for key, value in expected_labels.items()):
        errors.append(f"{phase}: held queue, capacity, phase, or workload labels drifted")
    if spec.get("suspend") is not True:
        errors.append(f"{phase}: workload must remain suspended")
    if len(containers) != 1 or not trainer:
        errors.append(f"{phase}: infra profile must contain only the gated trainer container")
    image = str(trainer.get("image", ""))
    if not image.startswith("registry.invalid/") or not image.endswith(ZERO_IMAGE_SUFFIX):
        errors.append(f"{phase}: trainer image must remain the all-zero digest sentinel")
    if requests.get("nvidia.com/gpu") != gpu_count or limits.get("nvidia.com/gpu") != gpu_count:
        errors.append(f"{phase}: GPU request and limit must both equal {gpu_count}")
    expected_selector = {
        "mindclade.dev/capacity-type": CAPACITY_TYPE,
        "mindclade.dev/gpu-profile": PROFILE,
        "mindclade.dev/node-pool": "gpu",
    }
    if node_selector != expected_selector:
        errors.append(f"{phase}: exact on-demand H100 node selector drifted")
    if pod_spec.get("serviceAccountName") != NAMESPACE:
        errors.append(f"{phase}: tokenless namespace ServiceAccount drifted")
    if pod_spec.get("automountServiceAccountToken") is not False:
        errors.append(f"{phase}: Kubernetes API token automount must remain disabled")
    if pod_spec.get("restartPolicy") != "Never":
        errors.append(f"{phase}: restartPolicy must be Never")
    return errors


def validate_profile(workloads: list[Resource], queues: list[Resource]) -> list[str]:
    errors: list[str] = []
    try:
        smoke = _one(workloads, "Job", "mindclade-h100-1g-packed-template")
        distributed = _one(workloads, "JobSet", "mindclade-h100-8g-single-node-template")
        checkpoint = _one(workloads, "ConfigMap", "mindclade-checkpoint-sidecar-contract")
        flavor = next(
            item
            for item in queues
            if item.get("kind") == "ResourceFlavor"
            and (item.get("metadata", {}) or {}).get("name") == "mindclade-h100"
        )
    except (StopIteration, ValueError) as error:
        return [str(error)]

    errors.extend(_validate_workload(smoke, phase="h100-1g-smoke", gpu_count="1"))
    errors.extend(_validate_workload(distributed, phase="h100-8g-ddp-dcp", gpu_count="8"))
    smoke_annotations = (smoke.get("metadata", {}) or {}).get("annotations", {}) or {}
    annotations = (distributed.get("metadata", {}) or {}).get("annotations", {}) or {}
    if smoke_annotations.get("mindclade.dev/qualification-template-authority") != (
        "tools/qualification/training_gke/h100-1g-smoke.json"
    ):
        errors.append("h100-1g-smoke: qualification template authority drifted")
    if annotations.get("mindclade.dev/execution-profile") != "single-node-world8":
        errors.append("h100-8g-ddp-dcp: exact single-node world-eight contract drifted")
    if annotations.get("mindclade.dev/qualification-template-authority") != (
        "tools/qualification/training_gke/h100-8g-ddp-dcp.json"
    ):
        errors.append("h100-8g-ddp-dcp: qualification template authority drifted")
    checkpoint_data = checkpoint.get("data", {}) or {}
    if (
        checkpoint_data.get("activationState") != "blocked"
        or checkpoint_data.get("consistency") != "trainer-safe-point-required"
        or checkpoint_data.get("commitProtocol") != "write-verify-atomic-publish"
    ):
        errors.append("checkpoint sidecar contract must remain blocked and atomic")
    flavor_labels = (flavor.get("spec", {}) or {}).get("nodeLabels", {}) or {}
    if flavor_labels != {
        "mindclade.dev/capacity-type": CAPACITY_TYPE,
        "mindclade.dev/gpu-profile": PROFILE,
        "mindclade.dev/node-pool": "gpu",
    }:
        errors.append("mindclade-h100 ResourceFlavor must select only on-demand H100 nodes")
    return sorted(set(errors))


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--workloads", type=pathlib.Path, required=True)
    parser.add_argument("--queues", type=pathlib.Path, required=True)
    args = parser.parse_args()
    try:
        errors = validate_profile(_load(args.workloads), _load(args.queues))
    except (OSError, ValueError, json.JSONDecodeError) as error:
        errors = [str(error)]
    for error in errors:
        print(f"ERROR: H100 qualification profile: {error}")
    if errors:
        return 1
    print("H100 PROFILE       held 1g smoke + single-node 8g on-demand qualification")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
