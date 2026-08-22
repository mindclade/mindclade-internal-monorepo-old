# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#
"""Prove the source qualification package cannot consume live capacity."""

from __future__ import annotations

import argparse
import json
import pathlib
from typing import Any

Resource = dict[str, Any]
NAMESPACE = "mindclade-qualification"
SERVICE_ACCOUNT = "mindclade-foundation-qualification"
CONTRACT = "gke-foundation-v1"
ZERO_IMAGE = "registry.invalid/mindclade/foundation-gke-qualification@sha256:" + "0" * 64
PROFILES = {
    "cpu": {
        "nodeSelector": {
            "mindclade.dev/capacity-type": "on-demand",
            "mindclade.dev/node-pool": "cpu",
            "mindclade.dev/workload-profile": "general-purpose",
        },
        "requests": {"cpu": "1", "memory": "1Gi", "ephemeral-storage": "2Gi"},
        "limits": {"cpu": "2", "memory": "2Gi", "ephemeral-storage": "4Gi"},
    },
    "h100": {
        "nodeSelector": {
            "mindclade.dev/gpu-profile": "gke-h100-a3-megagpu-8g",
            "mindclade.dev/node-pool": "gpu",
        },
        "requests": {
            "cpu": "8",
            "memory": "64Gi",
            "ephemeral-storage": "32Gi",
            "nvidia.com/gpu": "8",
        },
        "limits": {
            "cpu": "16",
            "memory": "128Gi",
            "ephemeral-storage": "64Gi",
            "nvidia.com/gpu": "8",
        },
    },
    "b200": {
        "nodeSelector": {
            "mindclade.dev/gpu-profile": "gke-b200-a4-highgpu-8g",
            "mindclade.dev/node-pool": "gpu",
        },
        "requests": {
            "cpu": "8",
            "memory": "64Gi",
            "ephemeral-storage": "32Gi",
            "nvidia.com/gpu": "8",
        },
        "limits": {
            "cpu": "16",
            "memory": "128Gi",
            "ephemeral-storage": "64Gi",
            "nvidia.com/gpu": "8",
        },
    },
}


def one(resources: list[Resource], kind: str, name: str) -> Resource:
    matches = [
        resource
        for resource in resources
        if resource.get("kind") == kind and (resource.get("metadata", {}) or {}).get("name") == name
    ]
    if len(matches) != 1:
        raise ValueError(f"expected one {kind}/{name}, found {len(matches)}")
    return matches[0]


def validate(resources: list[Resource]) -> list[str]:
    failures: list[str] = []
    expected_kinds = {
        "Namespace": 1,
        "ServiceAccount": 1,
        "ResourceQuota": 1,
        "LimitRange": 1,
        "NetworkPolicy": 1,
        "Job": 3,
    }
    actual_kinds: dict[str, int] = {}
    for resource in resources:
        kind = str(resource.get("kind", ""))
        actual_kinds[kind] = actual_kinds.get(kind, 0) + 1
    if actual_kinds != expected_kinds:
        failures.append(
            f"resource inventory must be exact: expected {expected_kinds}, got {actual_kinds}"
        )
        return failures

    try:
        namespace = one(resources, "Namespace", NAMESPACE)
        service_account = one(resources, "ServiceAccount", SERVICE_ACCOUNT)
        quota = one(resources, "ResourceQuota", "mindclade-qualification-capacity")
        limit_range = one(resources, "LimitRange", "mindclade-qualification-bounds")
        network_policy = one(resources, "NetworkPolicy", "default-deny")
    except ValueError as error:
        failures.append(str(error))
        return failures

    namespace_labels = (namespace.get("metadata", {}) or {}).get("labels", {}) or {}
    required_namespace_labels = {
        "kubernetes.io/metadata.name": NAMESPACE,
        "mindclade.dev/admission": "enforced",
        "mindclade.dev/workload-activation": "blocked",
        "mindclade.dev/workload-class": "foundation-qualification",
        "pod-security.kubernetes.io/enforce": "restricted",
        "pod-security.kubernetes.io/enforce-version": "v1.36",
        "pod-security.kubernetes.io/audit": "restricted",
        "pod-security.kubernetes.io/audit-version": "v1.36",
        "pod-security.kubernetes.io/warn": "restricted",
        "pod-security.kubernetes.io/warn-version": "v1.36",
    }
    if any(namespace_labels.get(key) != value for key, value in required_namespace_labels.items()):
        failures.append("qualification Namespace fail-closed labels drifted")
    if "mindclade.dev/queue-enforcement" in namespace_labels:
        failures.append("qualification Namespace must not attach to a workload queue")

    sa_metadata = service_account.get("metadata", {}) or {}
    if (
        sa_metadata.get("namespace") != NAMESPACE
        or service_account.get("automountServiceAccountToken") is not False
        or (sa_metadata.get("labels", {}) or {}).get("mindclade.dev/identity-mode")
        != "kubernetes-only"
    ):
        failures.append("qualification ServiceAccount identity contract drifted")

    hard = (quota.get("spec", {}) or {}).get("hard", {}) or {}
    if not hard or any(str(value) != "0" for value in hard.values()):
        failures.append("qualification ResourceQuota must keep every capacity field at zero")
    required_quota_keys = {
        "pods",
        "count/jobs.batch",
        "requests.cpu",
        "requests.memory",
        "requests.ephemeral-storage",
        "requests.nvidia.com/gpu",
        "limits.cpu",
        "limits.memory",
        "limits.ephemeral-storage",
    }
    if not required_quota_keys.issubset(hard):
        failures.append("qualification ResourceQuota is missing a fail-closed capacity field")

    expected_limits = [
        {
            "type": "Container",
            "max": {
                "cpu": "16",
                "ephemeral-storage": "64Gi",
                "memory": "128Gi",
                "nvidia.com/gpu": "8",
            },
            "min": {"cpu": "10m", "ephemeral-storage": "16Mi", "memory": "32Mi"},
        },
        {
            "type": "Pod",
            "max": {
                "cpu": "16",
                "ephemeral-storage": "64Gi",
                "memory": "128Gi",
                "nvidia.com/gpu": "8",
            },
        },
    ]
    if (limit_range.get("spec", {}) or {}).get("limits") != expected_limits:
        failures.append("qualification LimitRange bounds drifted")

    network_spec = network_policy.get("spec", {}) or {}
    if (
        (network_policy.get("metadata", {}) or {}).get("namespace") != NAMESPACE
        or network_spec.get("podSelector") != {}
        or set(network_spec.get("policyTypes", []) or []) != {"Ingress", "Egress"}
        or network_spec.get("ingress") != []
        or network_spec.get("egress") != []
    ):
        failures.append("qualification namespace must remain a complete ingress/egress deny")

    jobs = {
        (resource.get("metadata", {}) or {})
        .get("labels", {})
        .get("mindclade.dev/qualification-profile"): resource
        for resource in resources
        if resource.get("kind") == "Job"
    }
    if set(jobs) != set(PROFILES):
        failures.append("qualification Job profile inventory drifted")
        return failures
    for profile, expected in PROFILES.items():
        job = jobs[profile]
        metadata = job.get("metadata", {}) or {}
        labels = metadata.get("labels", {}) or {}
        spec = job.get("spec", {}) or {}
        pod_spec = (spec.get("template", {}) or {}).get("spec", {}) or {}
        containers = pod_spec.get("containers", []) or []
        pod_security = pod_spec.get("securityContext", {}) or {}
        if (
            metadata.get("name") != f"mindclade-foundation-qualification-{profile}"
            or metadata.get("namespace") != NAMESPACE
            or labels.get("mindclade.dev/qualification-contract") != CONTRACT
            or labels.get("mindclade.dev/workload-class") != "foundation-qualification"
            or "kueue.x-k8s.io/queue-name" in labels
            or spec.get("suspend") is not True
            or spec.get("backoffLimit") != 0
            or len(containers) != 1
            or pod_spec.get("serviceAccountName") != SERVICE_ACCOUNT
            or pod_spec.get("automountServiceAccountToken") is not False
            or pod_spec.get("nodeSelector") != expected["nodeSelector"]
            or pod_security.get("runAsUser") != 65532
            or pod_security.get("runAsGroup") != 65532
            or pod_security.get("fsGroup") != 65532
            or pod_security.get("fsGroupChangePolicy") != "OnRootMismatch"
            or containers[0].get("image") != ZERO_IMAGE
            or (containers[0].get("resources", {}) or {}).get("requests") != expected["requests"]
            or (containers[0].get("resources", {}) or {}).get("limits") != expected["limits"]
        ):
            failures.append(f"qualification Job/{profile} fail-closed contract drifted")

    return failures


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("manifest_json", type=pathlib.Path)
    args = parser.parse_args()
    value = json.loads(args.manifest_json.read_text(encoding="utf-8"))
    if not isinstance(value, list) or not all(isinstance(item, dict) for item in value):
        raise SystemExit("qualification contract input must be a JSON resource list")
    failures = validate(value)
    if failures:
        for failure in failures:
            print(f"ERROR: qualification contract: {failure}")
        raise SystemExit(1)
    print("QUALIFICATION      blocked namespace, zero quota, and 3 suspended profiles")


if __name__ == "__main__":
    main()
