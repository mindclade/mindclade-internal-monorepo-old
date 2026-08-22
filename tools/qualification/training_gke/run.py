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
import contextlib
import hashlib
import json
import math
import os
import re
import stat
import subprocess
import sys
import tempfile
import threading
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Final, cast

ROOT: Final = Path(__file__).resolve().parents[3]
TEMPLATE_ROOT: Final = Path(__file__).resolve().parent
NAMESPACE: Final = "mindclade-training-h100"
QUEUE: Final = "mindclade-training-h100"
WORKLOAD_CLASS: Final = "training-h100"
NODE_PROFILE: Final = "gke-h100-a3-megagpu-8g"
RESOURCE_FLAVOR: Final = "mindclade-h100"
ZERO_DIGEST: Final = "sha256:" + "0" * 64
ZERO_TRAINER_IMAGE: Final = "registry.invalid/mindclade/training-worker@" + ZERO_DIGEST
ZERO_CHECKPOINT_IMAGE: Final = "registry.invalid/mindclade/checkpoint-agent@" + ZERO_DIGEST
PINNED_IMAGE: Final = re.compile(r"^[a-z0-9][a-z0-9._/:~-]*@sha256:(?!0{64}$)[0-9a-f]{64}$")
RUN_ID: Final = re.compile(r"^[a-z0-9](?:[a-z0-9-]{0,38}[a-z0-9])?$")
DIGEST: Final = re.compile(r"^sha256:(?!0{64}$)[0-9a-f]{64}$")
SOURCE_REVISION: Final = re.compile(r"^(?!0{40}$)[0-9a-f]{40}$")
GCP_ZONE: Final = re.compile(r"^[a-z]+-[a-z]+[0-9]-[a-z]$")

_COHORT_FIELDS: Final = {
    "schema_version",
    "source_repository",
    "source_revision",
    "resolved_config_digest",
    "dataset_digest",
    "model_contract_digest",
    "toolchain_digest",
    "trainer_image",
    "checkpoint_agent_image",
    "checkpoint_schema_version",
    "zone",
    "node_profile",
    "capacity_type",
    "pricing_snapshot_digest",
    "phases",
}


@dataclass(frozen=True, slots=True)
class Phase:
    name: str
    kind: str
    template: str
    gpu_count: int
    world_size: int
    wait_condition: str
    template_digest: str


PHASES: Final = (
    Phase(
        "h100-1g-smoke",
        "Job",
        "h100-1g-smoke.json",
        1,
        1,
        "complete",
        "sha256:1fe25400174a267fd159a1730791fb0dcda08d73bcd6290c7386b6d5bc4eede8",
    ),
    Phase(
        "h100-8g-ddp-dcp",
        "JobSet",
        "h100-8g-ddp-dcp.json",
        8,
        8,
        "Completed",
        "sha256:0e485f3d782a34f66f5f6c98d577d73cea312aaf895653003f04f7c9839addf5",
    ),
)
PHASE_BY_NAME: Final = {phase.name: phase for phase in PHASES}

_EVIDENCE_FIELDS: Final = {
    "schema_version",
    "phase",
    "cohort_digest",
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
_WRITTEN_EVIDENCE_FIELDS: Final = {
    "schema_version",
    "context",
    "trainer_image",
    "checkpoint_agent_image",
    "run_id",
    "cohort",
    "cohort_digest",
    "result",
    "evidence_digest",
}
MAXIMUM_WRITTEN_EVIDENCE_BYTES: Final = 256 * 1024
MAXIMUM_KUBECTL_OUTPUT_BYTES: Final = 8 * 1024 * 1024
MAXIMUM_RESULT_LOG_BYTES: Final = 256 * 1024
DEFAULT_KUBECTL_TIMEOUT_SECONDS: Final = 30
# Connected execution cannot be enabled until an independent collector derives numerical,
# checkpoint, serving, image, Pod, node, and signer assertions from authoritative sources.
# Trainer-emitted JSON is an observation and is never qualification evidence by itself.
CONNECTED_EVIDENCE_VERIFIER_IMPLEMENTED: Final = False


class Kubectl:
    def __init__(self, context: str) -> None:
        self._context = context

    def run(
        self,
        *arguments: str,
        input_text: str | None = None,
        timeout_seconds: int = DEFAULT_KUBECTL_TIMEOUT_SECONDS,
        maximum_output_bytes: int = MAXIMUM_KUBECTL_OUTPUT_BYTES,
    ) -> subprocess.CompletedProcess[str]:
        if not 1 <= timeout_seconds <= 14_430:
            raise ValueError("kubectl subprocess timeout is outside bounds")
        if not 1 <= maximum_output_bytes <= MAXIMUM_KUBECTL_OUTPUT_BYTES:
            raise ValueError("kubectl output budget is outside bounds")
        return _run_bounded(
            [
                "kubectl",
                "--context",
                self._context,
                f"--request-timeout={timeout_seconds}s",
                *arguments,
            ],
            input_text=input_text,
            timeout_seconds=timeout_seconds,
            maximum_output_bytes=maximum_output_bytes,
        )

    def json(self, *arguments: str) -> dict[str, Any]:
        value = json.loads(self.run(*arguments, "-o", "json").stdout)
        if not isinstance(value, dict):
            raise RuntimeError("kubectl did not return a JSON object")
        return value


def _run_bounded(
    command: list[str],
    *,
    input_text: str | None,
    timeout_seconds: int,
    maximum_output_bytes: int,
) -> subprocess.CompletedProcess[str]:
    """Run one subprocess with wall-clock and stdout/stderr memory bounds."""

    started = time.monotonic()
    deadline = started + timeout_seconds
    input_bytes = input_text.encode("utf-8") if input_text is not None else None
    if input_bytes is not None and len(input_bytes) > MAXIMUM_WRITTEN_EVIDENCE_BYTES:
        raise ValueError("kubectl input exceeds its byte budget")
    process = subprocess.Popen(
        command,
        cwd=ROOT,
        stdin=subprocess.PIPE if input_bytes is not None else subprocess.DEVNULL,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    if process.stdout is None or process.stderr is None:
        process.kill()
        raise RuntimeError("kubectl subprocess pipes are unavailable")
    exceeded = threading.Event()
    stdout = bytearray()
    stderr = bytearray()
    input_errors: list[BaseException] = []

    def kill() -> None:
        with contextlib.suppress(ProcessLookupError):
            process.kill()

    def drain(stream: Any, destination: bytearray) -> None:
        while chunk := stream.read(64 * 1024):
            if len(destination) + len(chunk) > maximum_output_bytes:
                exceeded.set()
                kill()
                return
            destination.extend(chunk)

    def write_input() -> None:
        if input_bytes is None:
            return
        if process.stdin is None:
            input_errors.append(RuntimeError("kubectl subprocess stdin is unavailable"))
            kill()
            return
        try:
            view = memoryview(input_bytes)
            while view:
                written = process.stdin.write(view)
                if written is None or written <= 0:
                    raise BrokenPipeError("kubectl subprocess stopped accepting stdin")
                view = view[written:]
            process.stdin.close()
        except (BrokenPipeError, OSError) as error:
            input_errors.append(error)
            kill()

    readers = (
        threading.Thread(target=drain, args=(process.stdout, stdout), daemon=True),
        threading.Thread(target=drain, args=(process.stderr, stderr), daemon=True),
    )
    for reader in readers:
        reader.start()
    writer = threading.Thread(target=write_input, daemon=True)
    writer.start()
    try:
        try:
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise subprocess.TimeoutExpired(command, timeout_seconds)
            return_code = process.wait(timeout=remaining)
        except subprocess.TimeoutExpired as error:
            kill()
            process.wait()
            raise RuntimeError("kubectl subprocess exceeded its deadline") from error
    finally:
        if process.poll() is None:
            kill()
            process.wait()
        writer.join(timeout=max(0.0, deadline - time.monotonic()))
        for reader in readers:
            reader.join(timeout=max(0.0, deadline - time.monotonic()))
    if writer.is_alive() or any(reader.is_alive() for reader in readers):
        raise RuntimeError("kubectl subprocess exceeded its deadline")
    if input_errors:
        raise RuntimeError(
            "kubectl subprocess did not consume its bounded input"
        ) from input_errors[0]
    if exceeded.is_set():
        raise RuntimeError("kubectl subprocess output exceeded its byte budget")
    try:
        stdout_text = bytes(stdout).decode("utf-8")
        stderr_text = bytes(stderr).decode("utf-8")
    except UnicodeDecodeError as error:
        raise RuntimeError("kubectl subprocess output is not UTF-8") from error
    completed = subprocess.CompletedProcess(command, return_code, stdout_text, stderr_text)
    if return_code != 0:
        raise subprocess.CalledProcessError(
            return_code,
            command,
            output=stdout_text,
            stderr=stderr_text,
        )
    return completed


def _unique_json_object(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    result: dict[str, Any] = {}
    for key, value in pairs:
        if key in result:
            raise ValueError(f"duplicate JSON key: {key}")
        result[key] = value
    return result


def _strict_json(value: bytes | str) -> Any:
    return json.loads(
        value,
        object_pairs_hook=_unique_json_object,
        parse_constant=lambda token: (_ for _ in ()).throw(
            ValueError(f"non-finite JSON number: {token}")
        ),
    )


def validate_cohort(value: Any) -> dict[str, Any]:
    """Validate one immutable, target-profile qualification cohort."""

    if not isinstance(value, dict) or set(value) != _COHORT_FIELDS:
        raise ValueError("training qualification cohort fields are incomplete")
    if (
        value["schema_version"] != "mindclade.dev/training-qualification-cohort/v1"
        or value["source_repository"] != "mindclade/mindclade-internal-monorepo"
        or not isinstance(value["source_revision"], str)
        or SOURCE_REVISION.fullmatch(value["source_revision"]) is None
        or value["checkpoint_schema_version"] != "mindclade.dev/training-checkpoint/dcp-v1"
        or value["node_profile"] != NODE_PROFILE
        or value["capacity_type"] != "on-demand"
        or not isinstance(value["zone"], str)
        or GCP_ZONE.fullmatch(value["zone"]) is None
        or value["phases"] != [phase.name for phase in PHASES]
    ):
        raise ValueError("training qualification cohort identity is invalid")
    for field in (
        "resolved_config_digest",
        "dataset_digest",
        "model_contract_digest",
        "toolchain_digest",
        "pricing_snapshot_digest",
    ):
        candidate = value[field]
        if not isinstance(candidate, str) or DIGEST.fullmatch(candidate) is None:
            raise ValueError(f"training qualification cohort digest is invalid: {field}")
    for field in ("trainer_image", "checkpoint_agent_image"):
        candidate = value[field]
        if not isinstance(candidate, str) or PINNED_IMAGE.fullmatch(candidate) is None:
            raise ValueError(f"training qualification cohort image is invalid: {field}")
    return value


def cohort_digest(value: dict[str, Any]) -> str:
    validated = validate_cohort(value)
    canonical = json.dumps(validated, sort_keys=True, separators=(",", ":")).encode()
    return "sha256:" + hashlib.sha256(canonical).hexdigest()


def load_cohort(path: Path) -> dict[str, Any]:
    if not path.is_absolute():
        raise ValueError("qualification cohort path must be absolute")
    descriptor = os.open(path, os.O_RDONLY | os.O_CLOEXEC | os.O_NOFOLLOW)
    try:
        if not stat.S_ISREG(os.fstat(descriptor).st_mode):
            raise ValueError("qualification cohort must be a regular file")
        chunks: list[bytes] = []
        remaining = 64 * 1024 + 1
        while remaining:
            chunk = os.read(descriptor, remaining)
            if not chunk:
                break
            chunks.append(chunk)
            remaining -= len(chunk)
        raw = b"".join(chunks)
    finally:
        os.close(descriptor)
    if len(raw) > 64 * 1024:
        raise ValueError("qualification cohort exceeds 64 KiB")
    return validate_cohort(_strict_json(raw))


def _load_bounded_regular(path: Path, *, maximum_bytes: int, description: str) -> Any:
    if not path.is_absolute():
        raise ValueError(f"{description} path must be absolute")
    descriptor = os.open(path, os.O_RDONLY | os.O_CLOEXEC | os.O_NOFOLLOW)
    try:
        file_stat = os.fstat(descriptor)
        if not stat.S_ISREG(file_stat.st_mode) or not 0 < file_stat.st_size <= maximum_bytes:
            raise ValueError(f"{description} must be a bounded regular file")
        chunks: list[bytes] = []
        remaining = maximum_bytes + 1
        while remaining:
            chunk = os.read(descriptor, remaining)
            if not chunk:
                break
            chunks.append(chunk)
            remaining -= len(chunk)
    finally:
        os.close(descriptor)
    raw = b"".join(chunks)
    if len(raw) > maximum_bytes:
        raise ValueError(f"{description} exceeds its byte budget")
    return _strict_json(raw)


def validate_smoke_prerequisite(
    value: Any,
    *,
    cohort: dict[str, Any],
) -> str:
    """Validate a previously written one-GPU result for this exact cohort."""

    if not isinstance(value, dict) or set(value) != _WRITTEN_EVIDENCE_FIELDS:
        raise ValueError("one-GPU prerequisite evidence fields are incomplete")
    expected_cohort_digest = cohort_digest(cohort)
    if (
        value["schema_version"] != "mindclade.dev/training-gke-qualification/v1"
        or value["cohort"] != cohort
        or value["cohort_digest"] != expected_cohort_digest
        or value["trainer_image"] != cohort["trainer_image"]
        or value["checkpoint_agent_image"] != cohort["checkpoint_agent_image"]
    ):
        raise ValueError("one-GPU prerequisite does not match the immutable cohort")
    context = value["context"]
    run_id = value["run_id"]
    if (
        not isinstance(context, str)
        or not context
        or len(context) > 256
        or any(ord(character) < 0x20 for character in context)
        or not isinstance(run_id, str)
        or RUN_ID.fullmatch(run_id) is None
    ):
        raise ValueError("one-GPU prerequisite execution identity is invalid")
    validate_phase_evidence(
        PHASE_BY_NAME["h100-1g-smoke"],
        value["result"],
        qualification_cohort_digest=expected_cohort_digest,
    )
    observed_digest = value["evidence_digest"]
    if not isinstance(observed_digest, str) or DIGEST.fullmatch(observed_digest) is None:
        raise ValueError("one-GPU prerequisite evidence digest is invalid")
    projection = dict(value)
    del projection["evidence_digest"]
    expected_digest = (
        "sha256:"
        + hashlib.sha256(
            json.dumps(projection, sort_keys=True, separators=(",", ":")).encode()
        ).hexdigest()
    )
    if observed_digest != expected_digest:
        raise ValueError("one-GPU prerequisite evidence digest does not match its contents")
    return observed_digest


def load_smoke_prerequisite(path: Path, *, cohort: dict[str, Any]) -> str:
    return validate_smoke_prerequisite(
        _load_bounded_regular(
            path,
            maximum_bytes=MAXIMUM_WRITTEN_EVIDENCE_BYTES,
            description="one-GPU prerequisite evidence",
        ),
        cohort=cohort,
    )


def phase_named(name: str) -> Phase:
    try:
        return PHASE_BY_NAME[name]
    except KeyError as error:
        raise ValueError(f"unsupported training qualification phase: {name}") from error


def load_template(phase: Phase) -> dict[str, Any]:
    value = _strict_json((TEMPLATE_ROOT / phase.template).read_text(encoding="utf-8"))
    if not isinstance(value, dict):
        raise ValueError(f"{phase.name} template must contain one JSON object")
    validate_template(phase, value)
    return cast(dict[str, Any], value)


def _pod_spec(phase: Phase, resource: dict[str, Any]) -> dict[str, Any]:
    if phase.kind == "Job":
        candidate = resource.get("spec", {}).get("template", {}).get("spec", {})
        return cast(dict[str, Any], candidate) if isinstance(candidate, dict) else {}
    jobs = resource.get("spec", {}).get("replicatedJobs", [])
    if not isinstance(jobs, list) or len(jobs) != 1 or not isinstance(jobs[0], dict):
        return {}
    if jobs[0].get("replicas") != 1:
        return {}
    candidate = jobs[0].get("template", {}).get("spec", {}).get("template", {}).get("spec", {})
    return cast(dict[str, Any], candidate) if isinstance(candidate, dict) else {}


def _suspended(phase: Phase, resource: dict[str, Any]) -> bool:
    if phase.kind == "Job":
        return resource.get("spec", {}).get("suspend") is True
    return resource.get("spec", {}).get("suspend") is True


def validate_template(phase: Phase, resource: dict[str, Any]) -> None:
    canonical = json.dumps(resource, sort_keys=True, separators=(",", ":")).encode()
    if "sha256:" + hashlib.sha256(canonical).hexdigest() != phase.template_digest:
        raise ValueError(f"{phase.name} qualification template digest drifted")
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
    qualification_cohort_digest: str,
    zone: str,
) -> dict[str, Any]:
    for label, value in (
        ("trainer image", trainer_image),
        ("checkpoint-agent image", checkpoint_image),
    ):
        if PINNED_IMAGE.fullmatch(value) is None:
            raise ValueError(f"{label} must have a nonzero sha256 digest")
    if RUN_ID.fullmatch(run_id) is None:
        raise ValueError("qualification run id is not a bounded DNS label")
    if len(f"mindclade-{phase.name}-{run_id}") > 63:
        raise ValueError("qualification run id makes the Kubernetes object name exceed 63 bytes")
    if DIGEST.fullmatch(qualification_cohort_digest) is None:
        raise ValueError("qualification cohort digest must be a nonzero sha256 digest")
    if GCP_ZONE.fullmatch(zone) is None:
        raise ValueError("qualification zone is invalid")
    resource = load_template(phase)
    resource["metadata"]["name"] = f"mindclade-{phase.name}-{run_id}"
    resource["metadata"]["labels"]["mindclade.dev/qualification-run"] = run_id
    resource["metadata"].setdefault("annotations", {})[
        "mindclade.dev/qualification-cohort-digest"
    ] = qualification_cohort_digest
    resource["spec"]["suspend"] = False
    pod_spec = _pod_spec(phase, resource)
    pod_spec["nodeSelector"]["topology.kubernetes.io/zone"] = zone
    by_name = {container["name"]: container for container in pod_spec["containers"]}
    by_name["trainer"]["image"] = trainer_image
    by_name["trainer"]["args"].extend(["--run-id", run_id])
    by_name["trainer"].setdefault("env", []).extend(
        [
            {
                "name": "MINDCLADE_QUALIFICATION_COHORT_DIGEST",
                "value": qualification_cohort_digest,
            },
            {"name": "MINDCLADE_QUALIFICATION_PHASE", "value": phase.name},
        ]
    )
    by_name["checkpoint-agent"]["image"] = checkpoint_image
    return resource


def validate_live_capacity(
    phase: Phase,
    namespace: dict[str, Any],
    cluster_queue: dict[str, Any],
    local_queue: dict[str, Any],
    nodes: dict[str, Any],
    *,
    zone: str,
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
    local_queue_spec = local_queue.get("spec", {})
    if local_queue_spec.get("clusterQueue") != QUEUE or local_queue_spec.get("stopPolicy") in {
        "Hold",
        "HoldAndDrain",
    }:
        raise RuntimeError("H100 training LocalQueue is held or targets the wrong ClusterQueue")
    resource_groups = queue_spec.get("resourceGroups", [])
    if len(resource_groups) != 1:
        raise RuntimeError("H100 training ClusterQueue must contain one exact resource group")
    flavors = resource_groups[0].get("flavors", [])
    if len(flavors) != 1 or flavors[0].get("name") != RESOURCE_FLAVOR:
        raise RuntimeError("H100 training ClusterQueue must use the approved H100 flavor")
    quota = 0.0
    for resource in flavors[0].get("resources", []):
        if resource.get("name") == "nvidia.com/gpu":
            candidate = resource.get("nominalQuota", 0)
            try:
                numeric = float(candidate)
            except (TypeError, ValueError) as error:
                raise RuntimeError("H100 training GPU quota is invalid") from error
            if not math.isfinite(numeric) or numeric < 0:
                raise RuntimeError("H100 training GPU quota is invalid")
            quota += numeric
    if quota < phase.gpu_count:
        raise RuntimeError(f"{phase.name} requires measured quota for {phase.gpu_count} GPUs")
    ready = [
        node
        for node in nodes.get("items", [])
        if node.get("metadata", {}).get("labels", {}).get("mindclade.dev/gpu-profile")
        == NODE_PROFILE
        and node.get("metadata", {}).get("labels", {}).get("mindclade.dev/capacity-type")
        == "on-demand"
        and node.get("metadata", {}).get("labels", {}).get("topology.kubernetes.io/zone") == zone
        and _allocatable_gpus(node) >= phase.gpu_count
        and any(
            condition.get("type") == "Ready" and condition.get("status") == "True"
            for condition in node.get("status", {}).get("conditions", [])
        )
    ]
    if not ready:
        raise RuntimeError(f"{phase.name} has no ready on-demand H100 node")


def _allocatable_gpus(node: dict[str, Any]) -> float:
    candidate = node.get("status", {}).get("allocatable", {}).get("nvidia.com/gpu", 0)
    try:
        numeric = float(candidate)
    except TypeError, ValueError:
        return -1
    return numeric if math.isfinite(numeric) and numeric >= 0 else -1


def validate_phase_evidence(
    phase: Phase,
    value: Any,
    *,
    qualification_cohort_digest: str,
) -> dict[str, Any]:
    """Validate the shape of a trainer observation, never its truth or promotion authority."""

    if not isinstance(value, dict) or set(value) != _EVIDENCE_FIELDS:
        raise ValueError(f"{phase.name} evidence fields are incomplete")
    if (
        type(value["schema_version"]) is not int
        or value["schema_version"] != 1
        or value["phase"] != phase.name
        or value["cohort_digest"] != qualification_cohort_digest
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
        if (
            isinstance(candidate, bool)
            or not isinstance(candidate, int | float)
            or not math.isfinite(candidate)
            or candidate < 0
            or (field != "loss_numerator" and candidate == 0)
        ):
            raise ValueError(f"{phase.name} evidence field is invalid: {field}")
    if value["loss_denominator"] != value["samples"]:
        raise ValueError(f"{phase.name} loss denominator does not match scalar target count")
    expected_gpu_hours = value["duration_seconds"] * phase.gpu_count / 3_600
    if not math.isclose(value["gpu_hours"], expected_gpu_hours, rel_tol=1e-6, abs_tol=1e-9):
        raise ValueError(f"{phase.name} gpu_hours does not match duration and allocation")
    for field in ("checkpoint_digest", "model_bundle_digest"):
        if not isinstance(value[field], str) or DIGEST.fullmatch(value[field]) is None:
            raise ValueError(f"{phase.name} evidence field is invalid: {field}")
    for field in ("resume_exact", "serving_parity"):
        if value[field] is not True:
            raise ValueError(f"{phase.name} evidence did not prove {field}")
    return value


def preflight(kubectl: Kubectl, context: str, phase: Phase, *, zone: str) -> None:
    current = _run_bounded(
        ["kubectl", "config", "current-context"],
        input_text=None,
        timeout_seconds=DEFAULT_KUBECTL_TIMEOUT_SECONDS,
        maximum_output_bytes=4 * 1024,
    ).stdout.strip()
    if current != context:
        raise RuntimeError(f"kubectl current context {current!r} does not match {context!r}")
    plural = "jobs.batch" if phase.kind == "Job" else "jobsets.jobset.x-k8s.io"
    for verb, resource in (("create", plural), ("get", "pods"), ("get", "nodes")):
        if (
            kubectl.run("auth", "can-i", verb, resource, "--namespace", NAMESPACE).stdout.strip()
            != "yes"
        ):
            raise RuntimeError(f"Kubernetes credentials cannot {verb} {resource}")
    validate_live_capacity(
        phase,
        kubectl.json("get", "namespace", NAMESPACE),
        kubectl.json("get", "clusterqueue", QUEUE),
        kubectl.json("get", "localqueue", QUEUE, "--namespace", NAMESPACE),
        kubectl.json(
            "get",
            "nodes",
            "-l",
            f"mindclade.dev/gpu-profile={NODE_PROFILE},mindclade.dev/capacity-type=on-demand",
        ),
        zone=zone,
    )


def _result_logs(kubectl: Kubectl, phase: Phase, name: str) -> str:
    if phase.kind == "Job":
        return kubectl.run(
            "logs",
            f"job/{name}",
            "--namespace",
            NAMESPACE,
            "--container",
            "trainer",
            f"--limit-bytes={MAXIMUM_RESULT_LOG_BYTES}",
            maximum_output_bytes=MAXIMUM_RESULT_LOG_BYTES,
        ).stdout
    pods = kubectl.json(
        "get",
        "pods",
        "--namespace",
        NAMESPACE,
        "-l",
        f"jobset.sigs.k8s.io/jobset-name={name}",
    ).get("items", [])
    names = sorted(pod.get("metadata", {}).get("name", "") for pod in pods if isinstance(pod, dict))
    if len(names) != 1 or not names[0]:
        raise RuntimeError("single-node JobSet did not create exactly one trainer Pod")
    return kubectl.run(
        "logs",
        f"pod/{names[0]}",
        "--namespace",
        NAMESPACE,
        "--container",
        "trainer",
        f"--limit-bytes={MAXIMUM_RESULT_LOG_BYTES}",
        maximum_output_bytes=MAXIMUM_RESULT_LOG_BYTES,
    ).stdout


def execute_phase(
    kubectl: Kubectl,
    phase: Phase,
    resource: dict[str, Any],
    timeout_seconds: int,
    *,
    qualification_cohort_digest: str,
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
        timeout_seconds=timeout_seconds + DEFAULT_KUBECTL_TIMEOUT_SECONDS,
    )
    observed = kubectl.json("get", kind, name, "--namespace", NAMESPACE)
    if phase.kind == "Job":
        status = observed.get("status", {})
        if status.get("succeeded") != 1 or status.get("failed", 0):
            raise RuntimeError("one-GPU qualification Job did not succeed exactly once")
    else:
        conditions = observed.get("status", {}).get("conditions", [])
        if not any(
            item.get("type") == "Completed" and item.get("status") == "True" for item in conditions
        ) or any(
            item.get("type") == "Failed" and item.get("status") == "True" for item in conditions
        ):
            raise RuntimeError("eight-GPU qualification JobSet did not complete cleanly")
    lines = [
        line for line in _result_logs(kubectl, phase, name).splitlines() if line.startswith("{")
    ]
    if len(lines) != 1:
        raise RuntimeError("training qualification must emit exactly one JSON result")
    return validate_phase_evidence(
        phase,
        _strict_json(lines[0]),
        qualification_cohort_digest=qualification_cohort_digest,
    )


def write_evidence(
    output: Path,
    *,
    context: str,
    trainer_image: str,
    checkpoint_image: str,
    run_id: str,
    cohort: dict[str, Any],
    result: dict[str, Any],
) -> None:
    if not output.is_absolute():
        raise ValueError("evidence output path must be absolute")
    validate_cohort(cohort)
    digest = cohort_digest(cohort)
    if not context or len(context) > 256 or any(ord(character) < 0x20 for character in context):
        raise ValueError("evidence context is outside bounds")
    if RUN_ID.fullmatch(run_id) is None:
        raise ValueError("evidence run id is invalid")
    if (
        trainer_image != cohort["trainer_image"]
        or checkpoint_image != cohort["checkpoint_agent_image"]
    ):
        raise ValueError("evidence images do not match the qualification cohort")
    phase_value = result.get("phase")
    if not isinstance(phase_value, str):
        raise ValueError("phase evidence does not identify a qualification phase")
    phase = phase_named(phase_value)
    validate_phase_evidence(phase, result, qualification_cohort_digest=digest)
    document: dict[str, Any] = {
        "schema_version": "mindclade.dev/training-gke-qualification/v1",
        "context": context,
        "trainer_image": trainer_image,
        "checkpoint_agent_image": checkpoint_image,
        "run_id": run_id,
        "cohort": cohort,
        "cohort_digest": digest,
        "result": result,
    }
    canonical = json.dumps(document, sort_keys=True, separators=(",", ":")).encode()
    document["evidence_digest"] = "sha256:" + hashlib.sha256(canonical).hexdigest()
    output.parent.mkdir(parents=True, exist_ok=True)
    if output.exists() or output.is_symlink():
        raise FileExistsError("qualification evidence output already exists")
    descriptor, temporary_name = tempfile.mkstemp(prefix=f".{output.name}.", dir=output.parent)
    temporary = Path(temporary_name)
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8") as stream:
            stream.write(json.dumps(document, sort_keys=True, indent=2) + "\n")
            stream.flush()
            os.fsync(stream.fileno())
        os.link(temporary, output)
        temporary.unlink()
        directory = os.open(output.parent, os.O_RDONLY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
    finally:
        temporary.unlink(missing_ok=True)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--phase", choices=tuple(PHASE_BY_NAME))
    parser.add_argument("--context", default=os.environ.get("MINDCLADE_GKE_CLUSTER_CONTEXT"))
    parser.add_argument("--trainer-image", default=os.environ.get("MINDCLADE_TRAINING_IMAGE"))
    parser.add_argument(
        "--checkpoint-agent-image", default=os.environ.get("MINDCLADE_CHECKPOINT_AGENT_IMAGE")
    )
    parser.add_argument("--run-id", default=os.environ.get("MINDCLADE_QUALIFICATION_RUN_ID"))
    parser.add_argument("--cohort", type=Path)
    parser.add_argument("--prerequisite-evidence", type=Path)
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
        if not CONNECTED_EVIDENCE_VERIFIER_IMPLEMENTED:
            raise RuntimeError(
                "connected qualification is disabled until an independent evidence verifier "
                "derives Pod/image/node, numerical, checkpoint, serving, and signer assertions"
            )
        if (
            arguments.phase is None
            or arguments.context is None
            or arguments.trainer_image is None
            or arguments.checkpoint_agent_image is None
            or arguments.run_id is None
            or arguments.cohort is None
            or arguments.output is None
        ):
            raise ValueError(
                "live qualification requires phase, context, both images, run id, cohort, and output"
            )
        if not 60 <= arguments.timeout_seconds <= 14_400:
            raise ValueError("qualification timeout must be in [60, 14400] seconds")
        phase = phase_named(arguments.phase)
        cohort = load_cohort(arguments.cohort)
        if (
            cohort["trainer_image"] != arguments.trainer_image
            or cohort["checkpoint_agent_image"] != arguments.checkpoint_agent_image
        ):
            raise ValueError("live images do not match the immutable qualification cohort")
        digest = cohort_digest(cohort)
        if phase.name == "h100-8g-ddp-dcp":
            if arguments.prerequisite_evidence is None:
                raise ValueError("eight-GPU qualification requires one-GPU prerequisite evidence")
            load_smoke_prerequisite(arguments.prerequisite_evidence, cohort=cohort)
        elif arguments.prerequisite_evidence is not None:
            raise ValueError("one-GPU smoke does not accept prerequisite evidence")
        kubectl = Kubectl(arguments.context)
        preflight(kubectl, arguments.context, phase, zone=cohort["zone"])
        resource = render_resource(
            phase,
            trainer_image=arguments.trainer_image,
            checkpoint_image=arguments.checkpoint_agent_image,
            run_id=arguments.run_id,
            qualification_cohort_digest=digest,
            zone=cohort["zone"],
        )
        result = execute_phase(
            kubectl,
            phase,
            resource,
            arguments.timeout_seconds,
            qualification_cohort_digest=digest,
        )
        write_evidence(
            arguments.output,
            context=arguments.context,
            trainer_image=arguments.trainer_image,
            checkpoint_image=arguments.checkpoint_agent_image,
            run_id=arguments.run_id,
            cohort=cohort,
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
