#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#
"""Submit fail-closed H100 and H200 GKE release-qualification Jobs."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[3]
TEMPLATE_ROOT = Path(__file__).resolve().parent
ZERO_IMAGE = "registry.invalid/mindclade/release-qualification@sha256:" + "0" * 64
PINNED_IMAGE = re.compile(r"^[a-z0-9][a-z0-9._/:~-]*@sha256:(?!0{64}$)[0-9a-f]{64}$")
RUN_ID = re.compile(r"^[a-z0-9](?:[a-z0-9-]{0,38}[a-z0-9])?$")
REQUIRED_METRICS = {
    "schema_version",
    "hardware_profile",
    "gpu_name",
    "gpu_memory_bytes",
    "gpu_matmul_p50_ms",
    "gpu_matmul_p95_ms",
    "gpu_matmul_p99_ms",
    "gpu_peak_allocated_bytes",
    "checkpoint_staging_mib_per_s",
    "worker_startup_p95_ms",
    "unix_ipc_mib_per_s",
    "verified_range_4k_ops_per_s",
    "local_store_contended_4k_ops_per_s",
    "data_stream_copy_bytes_per_byte",
    "parser_allocated_bytes_per_input_byte",
}


@dataclass(frozen=True, slots=True)
class Profile:
    name: str
    namespace: str
    queue: str
    workload_class: str
    node_profile: str


PROFILES = (
    Profile(
        "h100",
        "mindclade-training-h100",
        "mindclade-training-h100",
        "training-h100",
        "gke-h100-a3-megagpu-8g",
    ),
    Profile(
        "h200",
        "mindclade-training-h200",
        "mindclade-training-h200",
        "training-h200",
        "gke-h200-a3-ultragpu-8g",
    ),
)


class Kubectl:
    def __init__(self, context: str) -> None:
        self._context = context

    def run(
        self,
        *arguments: str,
        input_text: str | None = None,
        capture_output: bool = True,
    ) -> subprocess.CompletedProcess[str]:
        return subprocess.run(
            ["kubectl", "--context", self._context, *arguments],
            cwd=ROOT,
            check=True,
            text=True,
            input=input_text,
            capture_output=capture_output,
        )

    def json(self, *arguments: str) -> dict[str, Any]:
        value = json.loads(self.run(*arguments, "-o", "json").stdout)
        if not isinstance(value, dict):
            raise RuntimeError("kubectl did not return a JSON object")
        return value


def load_template(profile: Profile) -> dict[str, Any]:
    value = json.loads((TEMPLATE_ROOT / f"{profile.name}-job.json").read_text())
    validate_template(profile, value)
    return value


def validate_template(profile: Profile, job: dict[str, Any]) -> None:
    metadata = job.get("metadata", {})
    labels = metadata.get("labels", {})
    pod_spec = job.get("spec", {}).get("template", {}).get("spec", {})
    containers = pod_spec.get("containers", [])
    if (
        job.get("apiVersion") != "batch/v1"
        or job.get("kind") != "Job"
        or metadata.get("namespace") != profile.namespace
        or labels.get("kueue.x-k8s.io/queue-name") != profile.queue
        or labels.get("mindclade.dev/workload-class") != profile.workload_class
        or labels.get("mindclade.dev/hardware-profile") != profile.name
        or job.get("spec", {}).get("suspend") is not True
        or job.get("spec", {}).get("backoffLimit") != 0
        or len(containers) != 1
        or containers[0].get("image") != ZERO_IMAGE
        or pod_spec.get("automountServiceAccountToken") is not False
        or pod_spec.get("restartPolicy") != "Never"
        or pod_spec.get("nodeSelector", {}).get("mindclade.dev/gpu-profile") != profile.node_profile
        or containers[0].get("resources", {}).get("requests", {}).get("nvidia.com/gpu") != "1"
        or containers[0].get("resources", {}).get("limits", {}).get("nvidia.com/gpu") != "1"
    ):
        raise ValueError(f"{profile.name} qualification Job template drifted")


def render_job(profile: Profile, image: str, run_id: str) -> dict[str, Any]:
    if PINNED_IMAGE.fullmatch(image) is None:
        raise ValueError("qualification image must have a nonzero sha256 digest")
    if RUN_ID.fullmatch(run_id) is None:
        raise ValueError("qualification run id is not a bounded DNS label")
    job = load_template(profile)
    job["metadata"]["name"] = f"mindclade-release-{profile.name}-{run_id}"
    job["metadata"]["labels"]["mindclade.dev/qualification-run"] = run_id
    job["spec"]["suspend"] = False
    container = job["spec"]["template"]["spec"]["containers"][0]
    container["image"] = image
    container["args"].extend(["--run-id", run_id])
    return job


def validate_live_capacity(
    profile: Profile,
    namespace: dict[str, Any],
    cluster_queue: dict[str, Any],
    nodes: dict[str, Any],
) -> None:
    labels = namespace.get("metadata", {}).get("labels", {})
    if (
        labels.get("mindclade.dev/workload-activation") != "active"
        or labels.get("mindclade.dev/kueue-enabled") != "true"
        or labels.get("mindclade.dev/workload-class") != profile.workload_class
    ):
        raise RuntimeError(f"{profile.name} namespace capacity is not activated")
    queue_spec = cluster_queue.get("spec", {})
    if queue_spec.get("stopPolicy") in {"Hold", "HoldAndDrain"}:
        raise RuntimeError(f"{profile.name} ClusterQueue is held")
    gpu_quota = 0.0
    for group in queue_spec.get("resourceGroups", []):
        for flavor in group.get("flavors", []):
            for resource in flavor.get("resources", []):
                if resource.get("name") == "nvidia.com/gpu":
                    gpu_quota += float(resource.get("nominalQuota", 0))
    if gpu_quota < 1:
        raise RuntimeError(f"{profile.name} ClusterQueue has no GPU quota")

    ready = []
    for node in nodes.get("items", []):
        node_labels = node.get("metadata", {}).get("labels", {})
        conditions = node.get("status", {}).get("conditions", [])
        allocatable = node.get("status", {}).get("allocatable", {})
        if (
            node_labels.get("mindclade.dev/gpu-profile") == profile.node_profile
            and float(allocatable.get("nvidia.com/gpu", 0)) >= 1
            and any(
                condition.get("type") == "Ready" and condition.get("status") == "True"
                for condition in conditions
            )
        ):
            ready.append(node)
    if not ready:
        raise RuntimeError(f"{profile.name} has no Ready node with allocatable GPU capacity")


def validate_evidence(profile: Profile, value: Any) -> dict[str, Any]:
    if not isinstance(value, dict) or set(value) != REQUIRED_METRICS:
        raise ValueError(f"{profile.name} evidence fields are incomplete")
    if value["schema_version"] != 1 or value["hardware_profile"] != profile.name:
        raise ValueError(f"{profile.name} evidence identity is invalid")
    if (
        not isinstance(value["gpu_name"], str)
        or profile.name.upper() not in value["gpu_name"].upper()
    ):
        raise ValueError(f"{profile.name} evidence reports the wrong GPU")
    for key in REQUIRED_METRICS - {"schema_version", "hardware_profile", "gpu_name"}:
        candidate = value[key]
        if isinstance(candidate, bool) or not isinstance(candidate, (int, float)) or candidate <= 0:
            raise ValueError(f"{profile.name} evidence metric is invalid: {key}")
    return value


def preflight(kubectl: Kubectl, context: str, profile: Profile) -> None:
    current = subprocess.run(
        ["kubectl", "config", "current-context"],
        cwd=ROOT,
        check=True,
        capture_output=True,
        text=True,
    ).stdout.strip()
    if current != context:
        raise RuntimeError(f"kubectl current context {current!r} does not match {context!r}")
    for verb, resource in (("create", "jobs.batch"), ("get", "pods"), ("get", "nodes")):
        allowed = kubectl.run(
            "auth",
            "can-i",
            verb,
            resource,
            "--namespace",
            profile.namespace,
        ).stdout.strip()
        if allowed != "yes":
            raise RuntimeError(
                f"Kubernetes credentials cannot {verb} {resource} for {profile.name}"
            )
    validate_live_capacity(
        profile,
        kubectl.json("get", "namespace", profile.namespace),
        kubectl.json("get", "clusterqueue", profile.queue),
        kubectl.json(
            "get",
            "nodes",
            "-l",
            f"mindclade.dev/gpu-profile={profile.node_profile},mindclade.dev/node-pool=gpu",
        ),
    )


def execute_profile(
    kubectl: Kubectl,
    profile: Profile,
    job: dict[str, Any],
    timeout_seconds: int,
) -> dict[str, Any]:
    name = job["metadata"]["name"]
    encoded = json.dumps(job, separators=(",", ":"))
    kubectl.run("create", "-f", "-", input_text=encoded)
    kubectl.run(
        "wait",
        "--for=condition=complete",
        f"job/{name}",
        "--namespace",
        profile.namespace,
        f"--timeout={timeout_seconds}s",
    )
    observed = kubectl.json("get", "job", name, "--namespace", profile.namespace)
    if observed.get("status", {}).get("succeeded") != 1 or observed.get("status", {}).get(
        "failed", 0
    ):
        raise RuntimeError(f"{profile.name} qualification Job did not succeed exactly once")
    logs = kubectl.run(
        "logs",
        f"job/{name}",
        "--namespace",
        profile.namespace,
        "--container",
        "qualification",
    ).stdout
    lines = [line for line in logs.splitlines() if line.strip().startswith("{")]
    if len(lines) != 1:
        raise RuntimeError(f"{profile.name} qualification Job did not emit exactly one JSON result")
    return validate_evidence(profile, json.loads(lines[0]))


def write_evidence(
    output: Path,
    context: str,
    image: str,
    run_id: str,
    results: dict[str, dict[str, Any]],
) -> None:
    if not output.is_absolute():
        raise ValueError("evidence output path must be absolute")
    document = {
        "schema_version": 1,
        "context": context,
        "image": image,
        "run_id": run_id,
        "profiles": results,
    }
    canonical = json.dumps(document, sort_keys=True, separators=(",", ":")).encode()
    document["evidence_digest"] = "sha256:" + hashlib.sha256(canonical).hexdigest()
    output.parent.mkdir(parents=True, exist_ok=True)
    temporary = output.with_suffix(output.suffix + ".tmp")
    temporary.write_text(json.dumps(document, sort_keys=True, indent=2) + "\n")
    os.replace(temporary, output)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--context", default=os.environ.get("MINDCLADE_GKE_CLUSTER_CONTEXT"))
    parser.add_argument("--image", default=os.environ.get("MINDCLADE_QUALIFICATION_IMAGE"))
    parser.add_argument("--run-id", default=os.environ.get("MINDCLADE_QUALIFICATION_RUN_ID"))
    parser.add_argument("--output", type=Path)
    parser.add_argument("--timeout-seconds", type=int, default=3600)
    parser.add_argument("--validate-only", action="store_true")
    arguments = parser.parse_args()
    try:
        for profile in PROFILES:
            load_template(profile)
        if arguments.validate_only:
            print("GKE H100/H200 qualification templates passed")
            return 0
        if (
            not arguments.context
            or not arguments.image
            or not arguments.run_id
            or not arguments.output
        ):
            raise ValueError("live qualification requires context, image, run id, and output")
        if arguments.timeout_seconds < 60 or arguments.timeout_seconds > 14_400:
            raise ValueError("qualification timeout must be in [60, 14400] seconds")
        kubectl = Kubectl(arguments.context)
        jobs: dict[str, dict[str, Any]] = {}
        for profile in PROFILES:
            preflight(kubectl, arguments.context, profile)
            jobs[profile.name] = render_job(profile, arguments.image, arguments.run_id)
        results = {
            profile.name: execute_profile(
                kubectl,
                profile,
                jobs[profile.name],
                arguments.timeout_seconds,
            )
            for profile in PROFILES
        }
        write_evidence(
            arguments.output,
            arguments.context,
            arguments.image,
            arguments.run_id,
            results,
        )
    except (
        OSError,
        ValueError,
        RuntimeError,
        subprocess.CalledProcessError,
        json.JSONDecodeError,
    ) as error:
        print(f"GKE release qualification failed: {error}", file=sys.stderr)
        return 1
    print(f"GKE H100/H200 release qualification passed: {arguments.output}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
