# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Mutation coverage for the fail-closed CPU, H100, and B200 capacity contract."""

from __future__ import annotations

import argparse
import copy
import pathlib
from collections.abc import Callable
from dataclasses import dataclass
from typing import Any

from capacity_contract import load, one, validate_contracts
from cross_resource import pod_templates

Resource = dict[str, Any]


@dataclass
class Inputs:
    base: list[Resource]
    policies: list[Resource]
    queues: list[Resource]
    all_resources: list[Resource]
    workloads: list[Resource]


Mutation = Callable[[Inputs], None]


def workload(inputs: Inputs, namespace: str, name: str) -> Resource:
    matches = [
        item
        for item in inputs.workloads
        if (item.get("metadata", {}) or {}).get("namespace") == namespace
        and (item.get("metadata", {}) or {}).get("name") == name
    ]
    if len(matches) != 1:
        raise AssertionError(f"expected one mutation target {namespace}/{name}")
    return matches[0]


def trainer(inputs: Inputs, namespace: str, name: str) -> Resource:
    templates = pod_templates(workload(inputs, namespace, name))
    if len(templates) != 1:
        raise AssertionError(f"expected one Pod template for {namespace}/{name}")
    containers = (templates[0].get("spec", {}) or {}).get("containers", []) or []
    matches = [item for item in containers if item.get("name") == "trainer"]
    if len(matches) != 1:
        raise AssertionError(f"expected one trainer container for {namespace}/{name}")
    return matches[0]


def expect_failure(baseline: Inputs, label: str, mutate: Mutation, fragment: str) -> None:
    candidate = copy.deepcopy(baseline)
    mutate(candidate)
    failures = validate_contracts(
        base=candidate.base,
        policies=candidate.policies,
        queues=candidate.queues,
        all_resources=candidate.all_resources,
        workloads=candidate.workloads,
    )
    if not any(fragment in failure for failure in failures):
        rendered = "\n".join(failures) if failures else "<validator accepted mutation>"
        raise AssertionError(f"{label}: expected {fragment!r}, got:\n{rendered}")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--base", type=pathlib.Path, required=True)
    parser.add_argument("--policies", type=pathlib.Path, required=True)
    parser.add_argument("--queues", type=pathlib.Path, required=True)
    parser.add_argument("--all", dest="all_resources", type=pathlib.Path, required=True)
    parser.add_argument("--workloads", type=pathlib.Path, action="append", required=True)
    args = parser.parse_args()

    baseline = Inputs(
        base=load(args.base),
        policies=load(args.policies),
        queues=load(args.queues),
        all_resources=load(args.all_resources),
        workloads=[resource for path in args.workloads for resource in load(path)],
    )
    baseline_failures = validate_contracts(
        base=baseline.base,
        policies=baseline.policies,
        queues=baseline.queues,
        all_resources=baseline.all_resources,
        workloads=baseline.workloads,
    )
    if baseline_failures:
        raise AssertionError(
            "capacity mutation baseline is invalid:\n" + "\n".join(baseline_failures)
        )

    def set_cpu_quota(inputs: Inputs) -> None:
        quota = one(
            inputs.policies,
            "ResourceQuota",
            "mindclade-batch-cpu",
            "mindclade-batch-cpu-capacity",
        )
        quota["spec"]["hard"]["pods"] = "1"

    def release_h100_queue(inputs: Inputs) -> None:
        one(inputs.queues, "ClusterQueue", "", "mindclade-training-h100")["spec"][
            "stopPolicy"
        ] = "None"

    def add_b200_nominal_quota(inputs: Inputs) -> None:
        queue = one(inputs.queues, "ClusterQueue", "", "mindclade-training-b200")
        resources = queue["spec"]["resourceGroups"][0]["flavors"][0]["resources"]
        gpu = next(item for item in resources if item.get("name") == "nvidia.com/gpu")
        gpu["nominalQuota"] = "8"

    def drift_flavor(inputs: Inputs, flavor_name: str) -> None:
        flavor = one(inputs.queues, "ResourceFlavor", "", flavor_name)
        flavor["spec"]["nodeLabels"]["mindclade.dev/gpu-profile"] = "unreviewed"

    def unsuspend_h100(inputs: Inputs) -> None:
        workload(
            inputs,
            "mindclade-training-h100",
            "mindclade-h100-1g-packed-template",
        )["spec"]["suspend"] = False

    def drift_b200_queue(inputs: Inputs) -> None:
        metadata = workload(
            inputs,
            "mindclade-training-b200",
            "mindclade-b200-1g-packed-template",
        )["metadata"]
        metadata["labels"]["kueue.x-k8s.io/queue-name"] = "mindclade-training-h100"

    def activate_h100_image(inputs: Inputs) -> None:
        trainer(
            inputs,
            "mindclade-training-h100",
            "mindclade-h100-1g-packed-template",
        )["image"] = "registry.example/mindclade/training@sha256:" + "1" * 64

    def exceed_b200_gpu_bound(inputs: Inputs) -> None:
        trainer(
            inputs,
            "mindclade-training-b200",
            "mindclade-b200-8g-full-template",
        )["resources"]["limits"]["nvidia.com/gpu"] = "9"

    cases: tuple[tuple[str, Mutation, str], ...] = (
        ("CPU quota", set_cpu_quota, "inactive capacity must remain zero"),
        ("H100 held queue", release_h100_queue, "expected one held resourceGroup"),
        ("B200 zero quota", add_b200_nominal_quota, "flavor or zero nominal quota drifted"),
        (
            "H100 flavor selector",
            lambda inputs: drift_flavor(inputs, "mindclade-h100"),
            "topology or node labels drifted",
        ),
        (
            "B200 flavor selector",
            lambda inputs: drift_flavor(inputs, "mindclade-b200"),
            "topology or node labels drifted",
        ),
        ("H100 suspension", unsuspend_h100, "queue contract drifted"),
        ("B200 queue", drift_b200_queue, "queue contract drifted"),
        ("H100 image", activate_h100_image, "image is not activation-gated"),
        ("B200 GPU bound", exceed_b200_gpu_bound, "container nvidia.com/gpu exceeds"),
    )
    for label, mutate, fragment in cases:
        expect_failure(baseline, label, mutate, fragment)

    print(f"CAPACITY-MUTATION  {len(cases)} fail-closed CPU/H100/B200 mutations rejected")


if __name__ == "__main__":
    main()
