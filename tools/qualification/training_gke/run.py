#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Run one explicit, fail-closed H100 training qualification phase.

This source runner deliberately never activates capacity, changes queue policy, or selects a
release. Those actions remain reviewed infrastructure-live and GitOps responsibilities. It may
submit one already-authorized qualification object after proving the selected namespace, queue,
node profile, images, and requested phase agree exactly.
"""

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
from typing import Any, Final

ROOT: Final = Path(__file__).resolve().parents[3]
TEMPLATE_ROOT: Final = Path(__file__).resolve().parent
NAMESPACE: Final = "mindclade-training-h100"
QUEUE: Final = "mindclade-training-h100"
WORKLOAD_CLASS: Final = "training-h100"
NODE_PROFILE: Final = "gke-h100-a3-megagpu-8g"
ZERO_DIGEST: Final = "sha256:" + "0" * 64
ZERO_TRAINER_IMAGE: Final = "registry.invalid/mindclade/training-worker@" + ZERO_DIGEST
ZERO_CHECKPOINT_IMAGE: Final = "registry.invalid/mindclade/checkpoint-agent@" + ZERO_DIGEST
PINNED_IMAGE: Final = re.compile(
    r"^[a-z0-9][a-z0-9._/:~-]*@sha256:(?!0{64}$)[0-9a-f]{64}$"
)
RUN_ID: Final = re.compile(r"^[a-z0-9](?:[a-z0-9-]{0,38}[a-z0-9])?$")
DIGEST: Final = re.compile(r"^sha256:(?!0{64}$)[0-9a-f]{64}$")


@dataclass(frozen=True, slots=True)
class Phase:
    name: str
    kind: str
    template: str
    gpu_count: int
    world_size: int
    wait_condition: str


PHASES: Final = (
    Phase("h100-1g-smoke", "Job", "h100-1g-smoke.json", 1, 1, "complete"),
    Phase("h100-8g-ddp-dcp", "JobSet", "h100-8g-ddp-dcp.json", 8, 8, "Completed"),
)
PHASE_BY_NAME: Final = {phase.name: phase for phase in PHASES}

_EVIDENCE_FIELDS: Final = {
    "schema_version",
    "phase",
    "hardware_profile",
    "capacity_type",
    "gpu_name",
    "world_size",
    "ranks_completed",
    "samples",
    "optimizer_steps",
    "loss_numerator",
    "loss_denominator",
    "checkpoint_digest",
    "model_bundle_digest",
    "resume_exact",
    "serving_parity",
    "duration_seconds",
    "gpu_hours",
}


class Kubectl:
    def __init__(self, context: str) -> None:
        self._context = context

    def run(
        self,
        *arguments: str,
        input_text: str | None = None,
    ) -> subprocess.CompletedProcess[str]:
        return subprocess.run(
            ["kubectl", "--context", self._context, *arguments],
            cwd=ROOT,
            check=True,
            text=True,
            input=input_text,
            capture_output=True,
        )

    def json(self, *arguments: str) -> dict[str, Any]:
        value = json.loads(self.run(*arguments, "-o", "json").stdout)
        if not isinstance(value, dict):
            raise RuntimeError("kubectl did not return a JSON object")
        return value


def phase_named(name: str) -> Phase:
    try:
        return PHASE_BY_NAME[name]
    except KeyError as error:
        raise ValueError(f"unsupported training qualification phase: {name}") from error


def load_template(phase: Phase) -> dict[str, Any]:
    value = json.loads((TEMPLATE_ROOT / phase.template).read_text(encoding="utf-8"))
    if not isinstance(value, dict):
        raise ValueError(f"{phase.name} template must contain one JSON object")
    validate_template(phase, value)
    return value


def _pod_spec(phase: Phase, resource: dict[str, Any]) -> dict[str, Any]:
    if phase.kind == "Job":
        return resource.get("spec", {}).get("template", {}).get("spec", {})
    jobs = resource.get("spec", {}).get("replicatedJobs", [])
    if len(jobs) != 1 or jobs[0].get("replicas") != 1:
        return {}
    return jobs[0].get("template", {}).get("spec", {}).get("template", {}).get("spec", {})


def _suspended(phase: Phase, resource: dict[str, Any]) -> bool:
    if phase.kind == "Job":
        return resource.get("spec", {}).get("suspend") is True
    return resource.get("spec", {}).get("suspend") is True


def validate_template(phase: Phase, resource: dict[str, Any]) -> None:
    metadata = resource.get("metadata", {})
    labels = metadata.get("labels", {})
    pod_spec = _pod_spec(phase, resource)
    containers = pod_spec.get("containers", [])
    by_name = {
        container.get("name"): container for container in containers if isinstance(container, dict)
    }
    trainer = by_name.get("trainer", {})
    checkpoint = by_name.get("checkpoint-agent", {})
    expected_api = "batch/v1" if phase.kind == "Job" else "jobset.x-k8s.io/v1alpha2"
    requests = trainer.get("resources", {}).get("requests", {})
    limits = trainer.get("resources", {}).get("limits", {})
    if (
        resource.get("apiVersion") != expected_api
        or resource.get("kind") != phase.kind
        or metadata.get("namespace") != NAMESPACE
        or labels.get("kueue.x-k8s.io/queue-name") != QUEUE
        or labels.get("mindclade.dev/workload-class") != WORKLOAD_CLASS
        or labels.get("mindclade.dev/qualification-phase") != phase.name
        or labels.get("mindclade.dev/capacity-type") != "on-demand"
        or not _suspended(phase, resource)
        or set(by_name) != {"trainer", "checkpoint-agent"}
        or trainer.get("image") != ZERO_TRAINER_IMAGE
        or checkpoint.get("image") != ZERO_CHECKPOINT_IMAGE
        or requests.get("nvidia.com/gpu") != str(phase.gpu_count)
        or limits.get("nvidia.com/gpu") != str(phase.gpu_count)
        or pod_spec.get("nodeSelector", {}).get("mindclade.dev/gpu-profile") != NODE_PROFILE
        or pod_spec.get("nodeSelector", {}).get("mindclade.dev/capacity-type") != "on-demand"
        or pod_spec.get("serviceAccountName") != NAMESPACE
        or pod_spec.get("restartPolicy") != "Never"
    ):
        raise ValueError(f"{phase.name} qualification template drifted")


def render_resource(
    phase: Phase,
    *,
    trainer_image: str,
    checkpoint_image: str,
    run_id: str,
) -> dict[str, Any]:
    for label, value in (
        ("trainer image", trainer_image),
        ("checkpoint-agent image", checkpoint_image),
    ):
        if PINNED_IMAGE.fullmatch(value) is None:
            raise ValueError(f"{label} must have a nonzero sha256 digest")
    if RUN_ID.fullmatch(run_id) is None:
        raise ValueError("qualification run id is not a bounded DNS label")
    resource = load_template(phase)
    resource["metadata"]["name"] = f"mindclade-{phase.name}-{run_id}"
    resource["metadata"]["labels"]["mindclade.dev/qualification-run"] = run_id
    resource["spec"]["suspend"] = False
    pod_spec = _pod_spec(phase, resource)
    by_name = {container["name"]: container for container in pod_spec["containers"]}
    by_name["trainer"]["image"] = trainer_image
    by_name["trainer"]["args"].extend(["--run-id", run_id])
    by_name["checkpoint-agent"]["image"] = checkpoint_image
    return resource


def validate_live_capacity(
    phase: Phase,
    namespace: dict[str, Any],
    cluster_queue: dict[str, Any],
    nodes: dict[str, Any],
) -> None:
    labels = namespace.get("metadata", {}).get("labels", {})
    if (
        labels.get("mindclade.dev/workload-activation") != "qualification"
        or labels.get("mindclade.dev/kueue-enabled") != "true"
        or labels.get("mindclade.dev/workload-class") != WORKLOAD_CLASS
    ):
        raise RuntimeError("H100 training namespace is not in qualification activation")
    queue_spec = cluster_queue.get("spec", {})
    if queue_spec.get("stopPolicy") in {"Hold", "HoldAndDrain"}:
        raise RuntimeError("H100 training ClusterQueue is held")
    quota = sum(
        float(resource.get("nominalQuota", 0))
        for group in queue_spec.get("resourceGroups", [])
        for flavor in group.get("flavors", [])
        for resource in flavor.get("resources", [])
        if resource.get("name") == "nvidia.com/gpu"
    )
    if quota < phase.gpu_count:
        raise RuntimeError(f"{phase.name} requires measured quota for {phase.gpu_count} GPUs")
    ready = [
        node
        for node in nodes.get("items", [])
        if node.get("metadata", {}).get("labels", {}).get("mindclade.dev/gpu-profile")
        == NODE_PROFILE
        and node.get("metadata", {}).get("labels", {}).get("mindclade.dev/capacity-type")
        == "on-demand"
        and float(node.get("status", {}).get("allocatable", {}).get("nvidia.com/gpu", 0))
        >= phase.gpu_count
        and any(
            condition.get("type") == "Ready" and condition.get("status") == "True"
            for condition in node.get("status", {}).get("conditions", [])
        )
    ]
    if not ready:
        raise RuntimeError(f"{phase.name} has no ready on-demand H100 node")


def validate_phase_evidence(phase: Phase, value: Any) -> dict[str, Any]:
    if not isinstance(value, dict) or set(value) != _EVIDENCE_FIELDS:
        raise ValueError(f"{phase.name} evidence fields are incomplete")
    if (
        value["schema_version"] != 1
        or value["phase"] != phase.name
        or value["hardware_profile"] != "h100"
        or value["capacity_type"] != "on-demand"
        or value["world_size"] != phase.world_size
        or value["ranks_completed"] != phase.world_size
    ):
        raise ValueError(f"{phase.name} evidence identity or rank count is invalid")
    if not isinstance(value["gpu_name"], str) or "H100" not in value["gpu_name"].upper():
        raise ValueError(f"{phase.name} evidence reports the wrong GPU")
    for field in ("samples", "optimizer_steps", "loss_denominator"):
        candidate = value[field]
        if isinstance(candidate, bool) or not isinstance(candidate, int) or candidate <= 0:
            raise ValueError(f"{phase.name} evidence field is invalid: {field}")
    for field in ("loss_numerator", "duration_seconds", "gpu_hours"):
        candidate = value[field]
        if isinstance(candidate, bool) or not isinstance(candidate, int | float) or candidate <= 0:
            raise ValueError(f"{phase.name} evidence field is invalid: {field}")
    for field in ("checkpoint_digest", "model_bundle_digest"):
        if not isinstance(value[field], str) or DIGEST.fullmatch(value[field]) is None:
            raise ValueError(f"{phase.name} evidence field is invalid: {field}")
    for field in ("resume_exact", "serving_parity"):
        if value[field] is not True:
            raise ValueError(f"{phase.name} evidence did not prove {field}")
    return value


def preflight(kubectl: Kubectl, context: str, phase: Phase) -> None:
    current = subprocess.run(
        ["kubectl", "config", "current-context"],
        cwd=ROOT,
        check=True,
        capture_output=True,
        text=True,
    ).stdout.strip()
    if current != context:
        raise RuntimeError(f"kubectl current context {current!r} does not match {context!r}")
    plural = "jobs.batch" if phase.kind == "Job" else "jobsets.jobset.x-k8s.io"
    for verb, resource in (("create", plural), ("get", "pods"), ("get", "nodes")):
        if kubectl.run("auth", "can-i", verb, resource, "--namespace", NAMESPACE).stdout.strip() != "yes":
            raise RuntimeError(f"Kubernetes credentials cannot {verb} {resource}")
    validate_live_capacity(
        phase,
        kubectl.json("get", "namespace", NAMESPACE),
        kubectl.json("get", "clusterqueue", QUEUE),
        kubectl.json(
            "get",
            "nodes",
            "-l",
            f"mindclade.dev/gpu-profile={NODE_PROFILE},mindclade.dev/capacity-type=on-demand",
        ),
    )


def _result_logs(kubectl: Kubectl, phase: Phase, name: str) -> str:
    if phase.kind == "Job":
        return kubectl.run("logs", f"job/{name}", "--namespace", NAMESPACE, "--container", "trainer").stdout
    pods = kubectl.json(
        "get",
        "pods",
        "--namespace",
        NAMESPACE,
        "-l",
        f"jobset.sigs.k8s.io/jobset-name={name}",
    ).get("items", [])
    names = sorted(
        pod.get("metadata", {}).get("name", "") for pod in pods if isinstance(pod, dict)
    )
    if len(names) != 1 or not names[0]:
        raise RuntimeError("single-node JobSet did not create exactly one trainer Pod")
    return kubectl.run("logs", f"pod/{names[0]}", "--namespace", NAMESPACE, "--container", "trainer").stdout


def execute_phase(
    kubectl: Kubectl,
    phase: Phase,
    resource: dict[str, Any],
    timeout_seconds: int,
) -> dict[str, Any]:
    name = resource["metadata"]["name"]
    kubectl.run("create", "-f", "-", input_text=json.dumps(resource, separators=(",", ":")))
    kind = "job" if phase.kind == "Job" else "jobset"
    kubectl.run(
        "wait",
        f"--for=condition={phase.wait_condition}",
        f"{kind}/{name}",
        "--namespace",
        NAMESPACE,
        f"--timeout={timeout_seconds}s",
    )
    lines = [line for line in _result_logs(kubectl, phase, name).splitlines() if line.startswith("{")]
    if len(lines) != 1:
        raise RuntimeError("training qualification must emit exactly one JSON result")
    return validate_phase_evidence(phase, json.loads(lines[0]))


def write_evidence(
    output: Path,
    *,
    context: str,
    trainer_image: str,
    checkpoint_image: str,
    run_id: str,
    result: dict[str, Any],
) -> None:
    if not output.is_absolute():
        raise ValueError("evidence output path must be absolute")
    document: dict[str, Any] = {
        "schema_version": "mindclade.dev/training-gke-qualification/v1",
        "context": context,
        "trainer_image": trainer_image,
        "checkpoint_agent_image": checkpoint_image,
        "run_id": run_id,
        "result": result,
    }
    canonical = json.dumps(document, sort_keys=True, separators=(",", ":")).encode()
    document["evidence_digest"] = "sha256:" + hashlib.sha256(canonical).hexdigest()
    output.parent.mkdir(parents=True, exist_ok=True)
    temporary = output.with_suffix(output.suffix + ".tmp")
    temporary.write_text(json.dumps(document, sort_keys=True, indent=2) + "\n", encoding="utf-8")
    os.replace(temporary, output)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--phase", choices=tuple(PHASE_BY_NAME))
    parser.add_argument("--context", default=os.environ.get("MINDCLADE_GKE_CLUSTER_CONTEXT"))
    parser.add_argument("--trainer-image", default=os.environ.get("MINDCLADE_TRAINING_IMAGE"))
    parser.add_argument(
        "--checkpoint-agent-image", default=os.environ.get("MINDCLADE_CHECKPOINT_AGENT_IMAGE")
    )
    parser.add_argument("--run-id", default=os.environ.get("MINDCLADE_QUALIFICATION_RUN_ID"))
    parser.add_argument("--output", type=Path)
    parser.add_argument("--timeout-seconds", type=int, default=3600)
    parser.add_argument("--validate-only", action="store_true")
    arguments = parser.parse_args()
    try:
        for candidate in PHASES:
            load_template(candidate)
        if arguments.validate_only:
            print("H100 training qualification templates passed")
            return 0
        if not all(
            (
                arguments.phase,
                arguments.context,
                arguments.trainer_image,
                arguments.checkpoint_agent_image,
                arguments.run_id,
                arguments.output,
            )
        ):
            raise ValueError("live qualification requires phase, context, both images, run id, and output")
        if not 60 <= arguments.timeout_seconds <= 14_400:
            raise ValueError("qualification timeout must be in [60, 14400] seconds")
        phase = phase_named(arguments.phase)
        kubectl = Kubectl(arguments.context)
        preflight(kubectl, arguments.context, phase)
        resource = render_resource(
            phase,
            trainer_image=arguments.trainer_image,
            checkpoint_image=arguments.checkpoint_agent_image,
            run_id=arguments.run_id,
        )
        result = execute_phase(kubectl, phase, resource, arguments.timeout_seconds)
        write_evidence(
            arguments.output,
            context=arguments.context,
            trainer_image=arguments.trainer_image,
            checkpoint_image=arguments.checkpoint_agent_image,
            run_id=arguments.run_id,
            result=result,
        )
    except (
        OSError,
        ValueError,
        RuntimeError,
        subprocess.CalledProcessError,
        json.JSONDecodeError,
    ) as error:
        print(f"H100 training qualification failed: {error}", file=sys.stderr)
        return 1
    print(f"H100 training qualification passed: {arguments.output}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
