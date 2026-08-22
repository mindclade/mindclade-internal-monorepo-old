# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Fail-closed capacity, queue, and activation-contract relationship checks."""

from __future__ import annotations

import argparse
import hashlib
import json
import pathlib
import re
from decimal import Decimal
from typing import Any

from cross_resource import pod_templates

Resource = dict[str, Any]
ZERO_DIGEST = "sha256:" + "0" * 64
NONZERO_DIGEST = re.compile(r"^sha256:(?!0{64}$)[0-9a-f]{64}$")
CONTRACT_NAME = re.compile(r"^mindclade-capacity-contract-sha256-([0-9a-f]{64})$")
DOMAINS = {
    "mindclade-batch-cpu": ("batch-cpu", "mindclade-cpu-general-ondemand"),
    "mindclade-training-h100": ("training-h100", "mindclade-h100"),
    "mindclade-training-b200": ("training-b200", "mindclade-b200"),
}
SCHEMA_DATA_KEYS = {
    "activationState",
    "activeContractNamePattern",
    "capacityValues",
    "contractObject",
    "contractSchema",
    "digestCanonicalization",
    "queueName",
    "repositoryBasePolicy",
    "requiredEvidence",
    "requiredQuotaFields",
    "secretMaterial",
    "workloadClass",
}
QUANTITY_SUFFIXES = {
    "": Decimal(1),
    "n": Decimal("1e-9"),
    "u": Decimal("1e-6"),
    "m": Decimal("1e-3"),
    "k": Decimal(1000),
    "M": Decimal(1000) ** 2,
    "G": Decimal(1000) ** 3,
    "T": Decimal(1000) ** 4,
    "P": Decimal(1000) ** 5,
    "E": Decimal(1000) ** 6,
    "Ki": Decimal(1024),
    "Mi": Decimal(1024) ** 2,
    "Gi": Decimal(1024) ** 3,
    "Ti": Decimal(1024) ** 4,
    "Pi": Decimal(1024) ** 5,
    "Ei": Decimal(1024) ** 6,
}


def load(path: pathlib.Path) -> list[Resource]:
    value = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(value, list) or not all(isinstance(item, dict) for item in value):
        raise SystemExit(f"{path}: expected a normalized JSON resource list")
    return value


def quantity(value: Any) -> Decimal:
    match = re.fullmatch(r"([+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+))([EPTGMKkmun]?i?)", str(value))
    if match is None or match.group(2) not in QUANTITY_SUFFIXES:
        raise ValueError(f"unsupported Kubernetes quantity {value!r}")
    return Decimal(match.group(1)) * QUANTITY_SUFFIXES[match.group(2)]


def one(resources: list[Resource], kind: str, namespace: str, name: str) -> Resource:
    matches = [
        item
        for item in resources
        if item.get("kind") == kind
        and (item.get("metadata", {}) or {}).get("namespace", "") == namespace
        and (item.get("metadata", {}) or {}).get("name") == name
    ]
    if len(matches) != 1:
        raise ValueError(f"expected one {kind}/{namespace}/{name}, found {len(matches)}")
    return matches[0]


def check_active_contract(
    resources: list[Resource], namespace: Resource, failures: list[str]
) -> None:
    metadata = namespace.get("metadata", {}) or {}
    namespace_name = str(metadata.get("name", ""))
    labels = metadata.get("labels", {}) or {}
    annotations = metadata.get("annotations", {}) or {}
    if labels.get("mindclade.dev/workload-activation") != "active":
        return
    if labels.get("mindclade.dev/kueue-enabled") != "true":
        failures.append(f"Namespace/{namespace_name}: active capacity must set kueue-enabled=true")
    contract_name = str(annotations.get("mindclade.dev/capacity-contract-name", ""))
    name_match = CONTRACT_NAME.fullmatch(contract_name)
    if name_match is None:
        failures.append(f"Namespace/{namespace_name}: active capacity contract name is invalid")
        return
    evidence_map = {
        "mindclade.dev/activation-bundle-digest": "activationBundleDigest",
        "mindclade.dev/capacity-evidence-digest": "capacityEvidenceDigest",
        "mindclade.dev/release-evidence-digest": "releaseEvidenceDigest",
    }
    for annotation_key in evidence_map:
        if NONZERO_DIGEST.fullmatch(str(annotations.get(annotation_key, ""))) is None:
            failures.append(f"Namespace/{namespace_name}: {annotation_key} is not a nonzero digest")

    contracts = [
        item
        for item in resources
        if item.get("kind") == "ConfigMap"
        and (item.get("metadata", {}) or {}).get("namespace") == namespace_name
        and (item.get("metadata", {}) or {}).get("name") == contract_name
    ]
    if len(contracts) != 1:
        failures.append(
            f"Namespace/{namespace_name}: expected one referenced capacity contract, found {len(contracts)}"
        )
        return
    contract = contracts[0]
    data = contract.get("data", {}) or {}
    workload_class = DOMAINS[namespace_name][0]
    required = {
        "activationState": "active",
        "contractSchema": "mindclade.dev/capacity-activation/v1",
        "digestCanonicalization": "sha256-of-utf8-jq-sort-keys-compact-json-data",
        "queueName": namespace_name,
        "workloadClass": workload_class,
    }
    if contract.get("immutable") is not True or any(data.get(k) != v for k, v in required.items()):
        failures.append(
            f"ConfigMap/{namespace_name}/{contract_name}: active contract metadata drifted"
        )
    for annotation_key, data_key in evidence_map.items():
        if data.get(data_key) != annotations.get(annotation_key):
            failures.append(
                f"ConfigMap/{namespace_name}/{contract_name}: {data_key} does not match Namespace evidence"
            )
    if NONZERO_DIGEST.fullmatch(str(data.get("quotaManifestDigest", ""))) is None:
        failures.append(
            f"ConfigMap/{namespace_name}/{contract_name}: quotaManifestDigest is invalid"
        )
    # `jq -S -c '.data'` emits one trailing newline; keep that byte in the content address.
    canonical = json.dumps(data, ensure_ascii=False, separators=(",", ":"), sort_keys=True) + "\n"
    actual_suffix = hashlib.sha256(canonical.encode("utf-8")).hexdigest()
    if actual_suffix != name_match.group(1):
        failures.append(
            f"ConfigMap/{namespace_name}/{contract_name}: content-address suffix is invalid"
        )


def validate_contracts(
    *,
    base: list[Resource],
    policies: list[Resource],
    queues: list[Resource],
    all_resources: list[Resource],
    workloads: list[Resource],
) -> list[str]:
    """Return every capacity-contract violation without terminating the caller."""

    failures: list[str] = []

    for namespace_name, (workload_class, flavor_name) in DOMAINS.items():
        try:
            namespace = one(base, "Namespace", "", namespace_name)
            schema = one(base, "ConfigMap", namespace_name, "mindclade-capacity-contract-schema-v1")
            quota = one(policies, "ResourceQuota", namespace_name, f"{namespace_name}-capacity")
            limit_range = one(policies, "LimitRange", namespace_name, f"{namespace_name}-bounds")
            cluster_queue = one(queues, "ClusterQueue", "", namespace_name)
            local_queue = one(queues, "LocalQueue", namespace_name, namespace_name)
        except ValueError as error:
            failures.append(str(error))
            continue

        labels = (namespace.get("metadata", {}) or {}).get("labels", {}) or {}
        annotations = (namespace.get("metadata", {}) or {}).get("annotations", {}) or {}
        exact_labels = {
            "mindclade.dev/admission": "enforced",
            "mindclade.dev/queue-enforcement": "enforced",
            "mindclade.dev/workload-activation": "blocked",
            "mindclade.dev/kueue-enabled": "false",
            "mindclade.dev/workload-class": workload_class,
        }
        if any(labels.get(key) != value for key, value in exact_labels.items()):
            failures.append(f"Namespace/{namespace_name}: blocked capacity label contract drifted")
        for key in (
            "mindclade.dev/capacity-contract-name",
            "mindclade.dev/activation-bundle-digest",
            "mindclade.dev/capacity-evidence-digest",
            "mindclade.dev/release-evidence-digest",
        ):
            if annotations.get(key) != "blocked":
                failures.append(f"Namespace/{namespace_name}: {key} must remain blocked in base")

        schema_data = schema.get("data", {}) or {}
        if (
            schema.get("immutable") is not True
            or set(schema_data) != SCHEMA_DATA_KEYS
            or schema_data.get("activationState") != "blocked"
            or schema_data.get("capacityValues") != "absent-until-measured"
            or schema_data.get("contractSchema") != "mindclade.dev/capacity-activation/v1"
            or schema_data.get("queueName") != namespace_name
            or schema_data.get("workloadClass") != workload_class
        ):
            failures.append(
                f"ConfigMap/{namespace_name}/mindclade-capacity-contract-schema-v1 drifted"
            )

        hard = (quota.get("spec", {}) or {}).get("hard", {}) or {}
        if hard.get("configmaps") != "10" or any(
            str(value) != "0" for key, value in hard.items() if key != "configmaps"
        ):
            failures.append(f"ResourceQuota/{namespace_name}: inactive capacity must remain zero")
        if "limits.nvidia.com/gpu" in hard:
            failures.append(
                f"ResourceQuota/{namespace_name}: limits.nvidia.com/gpu is not a valid quota"
            )
        for required_key in (
            "pods",
            "count/jobs.batch",
            "count/jobsets.jobset.x-k8s.io",
            "requests.cpu",
            "requests.memory",
            "requests.ephemeral-storage",
            "requests.nvidia.com/gpu",
            "limits.cpu",
            "limits.memory",
            "limits.ephemeral-storage",
        ):
            if hard.get(required_key) != "0":
                failures.append(f"ResourceQuota/{namespace_name}: {required_key} must equal zero")

        cq_spec = cluster_queue.get("spec", {}) or {}
        groups = cq_spec.get("resourceGroups", []) or []
        expected_resources = {"cpu", "memory", "ephemeral-storage", "pods"}
        if namespace_name != "mindclade-batch-cpu":
            expected_resources.add("nvidia.com/gpu")
        expected_namespace_selector = {
            "matchLabels": {
                "mindclade.dev/kueue-enabled": "true",
                "mindclade.dev/workload-class": workload_class,
            }
        }
        if cq_spec.get("stopPolicy") != "Hold" or len(groups) != 1:
            failures.append(f"ClusterQueue/{namespace_name}: expected one held resourceGroup")
        elif (
            cq_spec.get("namespaceSelector") != expected_namespace_selector
            or set(groups[0].get("coveredResources", []) or []) != expected_resources
            or len(groups[0].get("flavors", []) or []) != 1
            or groups[0]["flavors"][0].get("name") != flavor_name
            or {item.get("name") for item in groups[0]["flavors"][0].get("resources", []) or []}
            != expected_resources
            or len(groups[0]["flavors"][0].get("resources", []) or []) != len(expected_resources)
            or any(
                str(resource.get("nominalQuota")) != "0"
                for flavor in groups[0].get("flavors", []) or []
                for resource in flavor.get("resources", []) or []
            )
        ):
            failures.append(f"ClusterQueue/{namespace_name}: flavor or zero nominal quota drifted")
        lq_spec = local_queue.get("spec", {}) or {}
        if lq_spec.get("stopPolicy") != "Hold" or lq_spec.get("clusterQueue") != namespace_name:
            failures.append(f"LocalQueue/{namespace_name}: held one-to-one queue contract drifted")

        if namespace_name != "mindclade-batch-cpu":
            try:
                flavor = one(queues, "ResourceFlavor", "", flavor_name)
            except ValueError as error:
                failures.append(str(error))
            else:
                expected_node_labels = {
                    **(
                        {"mindclade.dev/capacity-type": "on-demand"}
                        if namespace_name == "mindclade-training-h100"
                        else {}
                    ),
                    "mindclade.dev/gpu-profile": (
                        "gke-h100-a3-megagpu-8g"
                        if namespace_name == "mindclade-training-h100"
                        else "gke-b200-a4-highgpu-8g"
                    ),
                    "mindclade.dev/node-pool": "gpu",
                }
                flavor_spec = flavor.get("spec", {}) or {}
                if (
                    flavor_spec.get("topologyName") != "mindclade-gpu-zone-host"
                    or flavor_spec.get("nodeLabels") != expected_node_labels
                ):
                    failures.append(
                        f"ResourceFlavor/{flavor_name}: topology or node labels drifted"
                    )

        limit_max = {
            item.get("type"): item.get("max", {}) or []
            for item in (limit_range.get("spec", {}) or {}).get("limits", []) or []
        }
        for workload in workloads:
            if (workload.get("metadata", {}) or {}).get("namespace") != namespace_name:
                continue
            for template in pod_templates(workload):
                pod_totals: dict[str, Decimal] = {}
                for container in (template.get("spec", {}) or {}).get("containers", []) or []:
                    for scope in ("requests", "limits"):
                        for resource_name, value in (
                            (container.get("resources", {}) or {}).get(scope, {}) or {}
                        ).items():
                            try:
                                parsed = quantity(value)
                                maximum = quantity(
                                    limit_max.get("Container", {}).get(resource_name)
                                )
                            except (TypeError, ValueError) as error:
                                failures.append(
                                    f"{namespace_name}: cannot compare {resource_name}: {error}"
                                )
                                continue
                            if parsed > maximum:
                                failures.append(
                                    f"{namespace_name}: container {resource_name} exceeds LimitRange max"
                                )
                            if scope == "limits":
                                pod_totals[resource_name] = (
                                    pod_totals.get(resource_name, Decimal(0)) + parsed
                                )
                for resource_name, value in pod_totals.items():
                    try:
                        maximum = quantity(limit_max.get("Pod", {}).get(resource_name))
                    except (TypeError, ValueError) as error:
                        failures.append(
                            f"{namespace_name}: cannot compare Pod {resource_name}: {error}"
                        )
                        continue
                    if value > maximum:
                        failures.append(
                            f"{namespace_name}: Pod {resource_name} exceeds LimitRange max"
                        )

    for workload in workloads:
        if workload.get("kind") not in {"Job", "JobSet"}:
            continue
        metadata = workload.get("metadata", {}) or {}
        namespace_name = str(metadata.get("namespace", ""))
        workload_class = DOMAINS.get(namespace_name, ("", ""))[0]
        labels = metadata.get("labels", {}) or {}
        if (
            (workload.get("spec", {}) or {}).get("suspend") is not True
            or labels.get("kueue.x-k8s.io/queue-name") != namespace_name
            or labels.get("mindclade.dev/workload-class") != workload_class
        ):
            failures.append(
                f"{workload.get('kind')}/{namespace_name}/{metadata.get('name')}: queue contract drifted"
            )
        for template in pod_templates(workload):
            for container in (template.get("spec", {}) or {}).get("containers", []) or []:
                image = str(container.get("image", ""))
                if not image.startswith("registry.invalid/") or not image.endswith(
                    "@" + ZERO_DIGEST
                ):
                    failures.append(
                        f"{workload.get('kind')}/{namespace_name}/{metadata.get('name')}: image is not activation-gated"
                    )

    for resource in all_resources:
        if (
            resource.get("kind") == "Namespace"
            and (resource.get("metadata", {}) or {}).get("name") in DOMAINS
        ):
            check_active_contract(all_resources, resource, failures)

    return failures


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--base", type=pathlib.Path, required=True)
    parser.add_argument("--policies", type=pathlib.Path, required=True)
    parser.add_argument("--queues", type=pathlib.Path, required=True)
    parser.add_argument("--all", dest="all_resources", type=pathlib.Path, required=True)
    parser.add_argument("--workloads", type=pathlib.Path, action="append", default=[])
    args = parser.parse_args()

    failures = validate_contracts(
        base=load(args.base),
        policies=load(args.policies),
        queues=load(args.queues),
        all_resources=load(args.all_resources),
        workloads=[resource for path in args.workloads for resource in load(path)],
    )

    if failures:
        for failure in failures:
            print(f"ERROR: capacity contract: {failure}")
        raise SystemExit(1)
    print(
        "CAPACITY           3 blocked domains, held queues, zero quotas, and activation contracts"
    )


if __name__ == "__main__":
    main()
