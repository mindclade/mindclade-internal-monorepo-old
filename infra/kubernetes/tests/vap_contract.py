#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Lock the complete native admission-policy and binding contract."""

from __future__ import annotations

import argparse
import hashlib
import json
import re
import sys
from copy import deepcopy
from pathlib import Path
from typing import Any

POLICY_KIND = "ValidatingAdmissionPolicy"
BINDING_KIND = "ValidatingAdmissionPolicyBinding"
API_VERSION = "admissionregistration.k8s.io/v1"
SYNC_WAVE = "argocd.argoproj.io/sync-wave"
POLICY_NAMES = {
    "mindclade-block-deployment-activation",
    "mindclade-block-job-activation",
    "mindclade-capacity-contract-object",
    "mindclade-capacity-namespace-activation",
    "mindclade-capacity-queue-contract",
    "mindclade-internal-services",
    "mindclade-restricted-pods",
}
DIGEST_PATTERN = re.compile(r"^sha256:[0-9a-f]{64}$")


class ContractError(ValueError):
    """The admission contract is incomplete or differs from its reviewed digest."""


def read_json(path: Path) -> Any:
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeDecodeError, json.JSONDecodeError) as error:
        raise ContractError(f"cannot read admission contract JSON {path}: {error}") from error


def normalize(resources: Any) -> list[dict[str, Any]]:
    if not isinstance(resources, list) or not all(isinstance(item, dict) for item in resources):
        raise ContractError("admission contract input must be one JSON resource array")

    normalized = []
    for resource in resources:
        kind = resource.get("kind")
        if kind not in {POLICY_KIND, BINDING_KIND}:
            raise ContractError(f"unexpected admission contract resource kind: {kind!r}")
        metadata = resource.get("metadata")
        spec = resource.get("spec")
        expected_wave = "10" if kind == POLICY_KIND else "11"
        if (
            resource.get("apiVersion") != API_VERSION
            or not isinstance(metadata, dict)
            or set(metadata) != {"name", "annotations"}
            or metadata.get("annotations") != {SYNC_WAVE: expected_wave}
            or not isinstance(metadata.get("name"), str)
            or not metadata["name"]
            or not isinstance(spec, dict)
        ):
            raise ContractError(f"{kind} identity, sync-wave, or spec shape is invalid")
        normalized.append(
            {
                "apiVersion": API_VERSION,
                "kind": kind,
                "metadata": {
                    "annotations": {SYNC_WAVE: expected_wave},
                    "name": metadata["name"],
                },
                "spec": spec,
            }
        )

    normalized.sort(key=lambda item: (item["kind"], item["metadata"]["name"]))
    policies = [item for item in normalized if item["kind"] == POLICY_KIND]
    bindings = [item for item in normalized if item["kind"] == BINDING_KIND]
    policy_names = {item["metadata"]["name"] for item in policies}
    binding_names = {item["metadata"]["name"] for item in bindings}
    if len(policies) != 7 or policy_names != POLICY_NAMES:
        raise ContractError("admission contract must contain the exact seven reviewed policies")
    if len(bindings) != 7 or binding_names != POLICY_NAMES:
        raise ContractError("admission contract must contain one exact-name binding per policy")

    for policy in policies:
        name = policy["metadata"]["name"]
        validations = policy["spec"].get("validations")
        if (
            policy["spec"].get("failurePolicy") != "Fail"
            or not isinstance(validations, list)
            or not validations
            or any(
                not isinstance(validation, dict)
                or not isinstance(validation.get("expression"), str)
                or not validation["expression"].strip()
                or not isinstance(validation.get("message"), str)
                or not validation["message"].strip()
                for validation in validations
            )
        ):
            raise ContractError(f"{name}: policy is not a nonempty fail-closed CEL contract")
    for binding in bindings:
        name = binding["metadata"]["name"]
        if binding["spec"].get("policyName") != name or binding["spec"].get(
            "validationActions"
        ) != ["Audit", "Deny"]:
            raise ContractError(f"{name}: binding does not target its policy with Audit+Deny")
    return normalized


def contract_digest(resources: Any) -> str:
    encoded = json.dumps(
        normalize(resources), sort_keys=True, separators=(",", ":"), ensure_ascii=False
    ).encode()
    return "sha256:" + hashlib.sha256(encoded).hexdigest()


def require_digest(resources: Any, expected: str) -> None:
    if DIGEST_PATTERN.fullmatch(expected) is None:
        raise ContractError("reviewed admission contract digest is malformed")
    actual = contract_digest(resources)
    if actual != expected:
        raise ContractError(
            f"complete admission policy/binding contract drifted: expected {expected}, found {actual}"
        )


def mutation_self_test(resources: Any, expected: str) -> int:
    normalized = normalize(resources)
    mutations = 0

    def must_reject(mutated: list[dict[str, Any]], label: str) -> None:
        nonlocal mutations
        mutations += 1
        try:
            require_digest(mutated, expected)
        except ContractError:
            return
        raise ContractError(f"admission digest accepted mutation: {label}")

    for index, resource in enumerate(normalized):
        name = resource["metadata"]["name"]
        wave_mutation = deepcopy(normalized)
        wave_mutation[index]["metadata"]["annotations"][SYNC_WAVE] = "99"
        must_reject(wave_mutation, f"{name} sync wave")
        if resource["kind"] == POLICY_KIND:
            failure_mutation = deepcopy(normalized)
            failure_mutation[index]["spec"]["failurePolicy"] = "Ignore"
            must_reject(failure_mutation, f"{name} failure policy")
            for validation_index in range(len(resource["spec"]["validations"])):
                expression_mutation = deepcopy(normalized)
                expression_mutation[index]["spec"]["validations"][validation_index][
                    "expression"
                ] = "true"
                must_reject(expression_mutation, f"{name} validation expression")

                message_mutation = deepcopy(normalized)
                message_mutation[index]["spec"]["validations"][validation_index]["message"] = (
                    "mutated"
                )
                must_reject(message_mutation, f"{name} validation message")
        else:
            action_mutation = deepcopy(normalized)
            action_mutation[index]["spec"]["validationActions"] = ["Audit"]
            must_reject(action_mutation, f"{name} validation actions")

            selector_mutation = deepcopy(normalized)
            selector_mutation[index]["spec"].setdefault("matchResources", {})[
                "namespaceSelector"
            ] = {"matchLabels": {"mindclade.dev/admission": "bypassed"}}
            must_reject(selector_mutation, f"{name} selector")
    return mutations


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("actual", type=Path)
    parser.add_argument("expected_digest", type=Path)
    parser.add_argument("--self-test", action="store_true")
    args = parser.parse_args()
    try:
        resources = read_json(args.actual)
        expected = args.expected_digest.read_text(encoding="utf-8").strip()
        require_digest(resources, expected)
        mutation_count = mutation_self_test(resources, expected) if args.self_test else 0
    except (OSError, UnicodeDecodeError, ContractError) as error:
        print(f"ERROR: {error}", file=sys.stderr)
        return 1
    suffix = f"; {mutation_count} fail-closed mutations rejected" if args.self_test else ""
    print(f"exact admission policy/binding contract passed{suffix}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
