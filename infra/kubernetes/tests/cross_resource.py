# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Cross-object checks that schema and per-document policy validators cannot express."""

from __future__ import annotations

import argparse
import json
import pathlib
from typing import Any

Resource = dict[str, Any]
Template = tuple[Resource, Resource]


def selector_matches(selector: Resource, labels: dict[str, str]) -> bool:
    """Implement the Kubernetes LabelSelector matchLabels/matchExpressions contract."""
    if not isinstance(selector, dict):
        return False
    match_labels = selector.get("matchLabels", {}) or {}
    if not isinstance(match_labels, dict) or any(
        labels.get(k) != v for k, v in match_labels.items()
    ):
        return False
    expressions = selector.get("matchExpressions", []) or []
    if not isinstance(expressions, list):
        return False
    for expression in expressions:
        if not isinstance(expression, dict):
            return False
        key = expression.get("key")
        operator = expression.get("operator")
        values = expression.get("values", []) or []
        if not isinstance(key, str) or not isinstance(values, list):
            return False
        if operator == "In" and labels.get(key) not in values:
            return False
        if operator == "NotIn" and (key not in labels or labels.get(key) in values):
            return False
        if operator == "Exists" and key not in labels:
            return False
        if operator == "DoesNotExist" and key in labels:
            return False
        if operator not in {"In", "NotIn", "Exists", "DoesNotExist"}:
            return False
    return True


def flat_selector_matches(selector: Resource, labels: dict[str, str]) -> bool:
    return bool(selector) and selector_matches({"matchLabels": selector}, labels)


def pod_templates(resource: Resource) -> list[Resource]:
    kind = resource.get("kind")
    spec = resource.get("spec", {}) or {}
    if kind == "Pod":
        return [{"metadata": resource.get("metadata", {}), "spec": spec}]
    if kind in {"Deployment", "StatefulSet", "DaemonSet", "ReplicaSet", "Job"}:
        template = spec.get("template")
        return [template] if isinstance(template, dict) else []
    if kind == "CronJob":
        template = spec.get("jobTemplate", {}).get("spec", {}).get("template")
        return [template] if isinstance(template, dict) else []
    if kind == "JobSet":
        result = []
        for replicated_job in spec.get("replicatedJobs", []) or []:
            template = replicated_job.get("template", {}).get("spec", {}).get("template")
            if isinstance(template, dict):
                result.append(template)
        return result
    return []


def identity(resource: Resource) -> str:
    metadata = resource.get("metadata", {}) or {}
    return "/".join(
        (
            str(resource.get("apiVersion", "<apiVersion>")),
            str(resource.get("kind", "<kind>")),
            str(metadata.get("namespace", "_cluster")),
            str(metadata.get("name", "<name>")),
        )
    )


def template_namespace(workload: Resource) -> str:
    return str((workload.get("metadata", {}) or {}).get("namespace", ""))


def template_labels(template: Resource) -> dict[str, str]:
    labels = (template.get("metadata", {}) or {}).get("labels", {}) or {}
    return labels if isinstance(labels, dict) else {}


def selected_templates(
    templates: list[Template], namespace: str, selector: Resource
) -> list[Resource]:
    return [
        template
        for workload, template in templates
        if template_namespace(workload) == namespace
        and selector_matches(selector, template_labels(template))
    ]


def exposed_ports(templates: list[Resource]) -> tuple[set[str], set[int]]:
    names: set[str] = set()
    numbers: set[int] = set()
    for template in templates:
        for container in (template.get("spec", {}) or {}).get("containers", []) or []:
            for container_port in container.get("ports", []) or []:
                name = container_port.get("name")
                number = container_port.get("containerPort")
                if isinstance(name, str):
                    names.add(name)
                if isinstance(number, int):
                    numbers.add(number)
    return names, numbers


def port_is_exposed(port: Any, templates: list[Resource]) -> bool:
    names, numbers = exposed_ports(templates)
    return (isinstance(port, str) and port in names) or (isinstance(port, int) and port in numbers)


def grants_service_reference(
    grants: list[Resource], route_namespace: str, service_namespace: str, service_name: str
) -> bool:
    """Require an exact Gateway API ReferenceGrant for a cross-namespace Service."""
    for grant in grants:
        metadata = grant.get("metadata", {}) or {}
        if str(metadata.get("namespace", "")) != service_namespace:
            continue
        spec = grant.get("spec", {}) or {}
        allowed_from = any(
            isinstance(source, dict)
            and source.get("group") == "gateway.networking.k8s.io"
            and source.get("kind") == "HTTPRoute"
            and source.get("namespace") == route_namespace
            for source in spec.get("from", []) or []
        )
        allowed_to = any(
            isinstance(target, dict)
            and target.get("group", "") in {"", "core"}
            and target.get("kind") == "Service"
            and target.get("name") == service_name
            for target in spec.get("to", []) or []
        )
        if allowed_from and allowed_to:
            return True
    return False


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("manifest_json", type=pathlib.Path)
    parser.add_argument("--label", required=True)
    parser.add_argument("--monitoring-only", action="store_true")
    parser.add_argument("--allow-unresolved-service-ref", action="append", default=[])
    parser.add_argument("--allow-unmatched-network-policy", action="append", default=[])
    args = parser.parse_args()

    resources = json.loads(args.manifest_json.read_text(encoding="utf-8"))
    if not isinstance(resources, list):
        raise SystemExit(f"{args.label}: normalized manifest must be a JSON list")

    failures: list[str] = []
    allowed_unresolved_refs = set(args.allow_unresolved_service_ref)
    allowed_unmatched_network_policies = set(args.allow_unmatched_network_policy)
    templates: list[Template] = []
    service_accounts: set[tuple[str, str]] = set()
    services: dict[tuple[str, str], Resource] = {}
    roles: set[tuple[str, str, str]] = set()
    reference_grants: list[Resource] = []
    for resource in resources:
        if not isinstance(resource, dict):
            continue
        metadata = resource.get("metadata", {}) or {}
        namespace = str(metadata.get("namespace", ""))
        name = str(metadata.get("name", ""))
        kind = resource.get("kind")
        if kind == "ServiceAccount":
            service_accounts.add((namespace, name))
        elif kind == "Service":
            services[(namespace, name)] = resource
        elif kind in {"Role", "ClusterRole"}:
            roles.add((str(kind), namespace, name))
        elif (
            kind == "ReferenceGrant"
            and resource.get("apiVersion") == "gateway.networking.k8s.io/v1beta1"
        ):
            reference_grants.append(resource)
        for template in pod_templates(resource):
            templates.append((resource, template))

    if not args.monitoring_only:
        for resource, template in templates:
            kind = str(resource.get("kind", ""))
            metadata = resource.get("metadata", {}) or {}
            namespace = str(metadata.get("namespace", ""))
            name = str(metadata.get("name", ""))
            labels = template_labels(template)
            if kind in {"Deployment", "StatefulSet", "DaemonSet", "ReplicaSet"}:
                selector = (resource.get("spec", {}) or {}).get("selector", {}) or {}
                if not selector_matches(selector, labels):
                    failures.append(
                        f"{kind}/{namespace}/{name}: selector does not match pod template"
                    )

            service_account = str((template.get("spec", {}) or {}).get("serviceAccountName", ""))
            if (
                service_accounts
                and service_account
                and (namespace, service_account) not in service_accounts
            ):
                failures.append(
                    f"{kind}/{namespace}/{name}: ServiceAccount/{service_account} is not rendered"
                )

        for resource in resources:
            if not isinstance(resource, dict):
                continue
            kind = resource.get("kind")
            metadata = resource.get("metadata", {}) or {}
            namespace = str(metadata.get("namespace", ""))
            name = str(metadata.get("name", ""))
            spec = resource.get("spec", {}) or {}
            if kind == "Service" and spec.get("selector"):
                matches = [
                    template
                    for workload, template in templates
                    if template_namespace(workload) == namespace
                    and flat_selector_matches(spec["selector"], template_labels(template))
                ]
                if not matches:
                    failures.append(
                        f"Service/{namespace}/{name}: selector matches no rendered pod template"
                    )
                    continue
                for port in spec.get("ports", []) or []:
                    target = port.get("targetPort", port.get("port"))
                    if not port_is_exposed(target, matches):
                        failures.append(
                            f"Service/{namespace}/{name}: targetPort {target!r} is not exposed by a selected pod"
                        )
            elif kind == "PodDisruptionBudget":
                selector = spec.get("selector", {}) or {}
                if not selected_templates(templates, namespace, selector):
                    failures.append(
                        f"PodDisruptionBudget/{namespace}/{name}: selector matches no pod template"
                    )
            elif kind == "NetworkPolicy":
                selector = spec.get("podSelector", {}) or {}
                # Empty selects every Pod and is reserved for the separately checked default deny.
                if selector:
                    matches = selected_templates(templates, namespace, selector)
                    policy_ref = f"{namespace}/{name}"
                    if not matches and policy_ref not in allowed_unmatched_network_policies:
                        failures.append(
                            f"NetworkPolicy/{namespace}/{name}: selector matches no pod template"
                        )
                    if not matches and policy_ref in allowed_unmatched_network_policies:
                        policy_types = set(spec.get("policyTypes", []) or [])
                        if (
                            policy_types != {"Ingress", "Egress"}
                            or bool(spec.get("ingress", []) or [])
                            or bool(spec.get("egress", []) or [])
                        ):
                            failures.append(
                                f"NetworkPolicy/{namespace}/{name}: unmatched-policy exception "
                                "must remain an ingress-and-egress deny with no allow rules"
                            )
                    if matches and policy_ref in allowed_unmatched_network_policies:
                        failures.append(
                            f"NetworkPolicy/{namespace}/{name}: stale unmatched-policy exception"
                        )
                    for ingress in spec.get("ingress", []) or []:
                        for port in ingress.get("ports", []) or []:
                            target = port.get("port")
                            if (
                                matches
                                and target is not None
                                and not port_is_exposed(target, matches)
                            ):
                                failures.append(
                                    f"NetworkPolicy/{namespace}/{name}: ingress port {target!r} is not exposed by a selected pod"
                                )
            elif kind == "HTTPRoute":
                for rule in spec.get("rules", []) or []:
                    for backend in rule.get("backendRefs", []) or []:
                        backend_group = backend.get("group", "")
                        backend_kind = backend.get("kind", "Service")
                        backend_namespace = str(backend.get("namespace", namespace))
                        backend_name = str(backend.get("name", ""))
                        backend_port = backend.get("port")
                        unresolved_ref = f"{backend_namespace}/{backend_name}:{backend_port}"
                        if backend_group not in {"", "core"} or backend_kind != "Service":
                            failures.append(
                                f"HTTPRoute/{namespace}/{name}: unsupported backend {backend_group}/{backend_kind}/{backend_name}"
                            )
                            continue
                        service = services.get((backend_namespace, backend_name))
                        if service is None:
                            if unresolved_ref not in allowed_unresolved_refs:
                                failures.append(
                                    f"HTTPRoute/{namespace}/{name}: backend Service/{backend_namespace}/{backend_name} is not rendered"
                                )
                            continue
                        if backend_namespace != namespace and not grants_service_reference(
                            reference_grants, namespace, backend_namespace, backend_name
                        ):
                            failures.append(
                                f"HTTPRoute/{namespace}/{name}: cross-namespace backend "
                                f"Service/{backend_namespace}/{backend_name} lacks an exact ReferenceGrant"
                            )
                        if unresolved_ref in allowed_unresolved_refs:
                            failures.append(
                                f"HTTPRoute/{namespace}/{name}: stale unresolved-ref exception {unresolved_ref}"
                            )
                        service_ports = {
                            item.get("port")
                            for item in (service.get("spec", {}) or {}).get("ports", []) or []
                        }
                        if backend_port not in service_ports:
                            failures.append(
                                f"HTTPRoute/{namespace}/{name}: backend port {backend_port!r} is absent from Service/{backend_name}"
                            )
            elif kind == "GCPBackendPolicy":
                target = spec.get("targetRef", {}) or {}
                target_group = target.get("group", "")
                target_kind = target.get("kind", "Service")
                target_name = str(target.get("name", ""))
                if target_group not in {"", "core"} or target_kind != "Service":
                    failures.append(
                        f"GCPBackendPolicy/{namespace}/{name}: targetRef must be a core Service"
                    )
                elif (namespace, target_name) not in services:
                    failures.append(
                        f"GCPBackendPolicy/{namespace}/{name}: target Service/{target_name} is not rendered"
                    )
            elif kind in {"RoleBinding", "ClusterRoleBinding"}:
                role_ref = resource.get("roleRef", {}) or {}
                ref_kind = str(role_ref.get("kind", ""))
                ref_name = str(role_ref.get("name", ""))
                ref_namespace = namespace if ref_kind == "Role" else ""
                if role_ref.get("apiGroup") != "rbac.authorization.k8s.io":
                    failures.append(f"{kind}/{namespace}/{name}: roleRef apiGroup is invalid")
                if (
                    ref_kind not in {"Role", "ClusterRole"}
                    or (ref_kind, ref_namespace, ref_name) not in roles
                ):
                    failures.append(
                        f"{kind}/{namespace}/{name}: roleRef {ref_kind}/{ref_name} is not rendered"
                    )
                if kind == "ClusterRoleBinding" and ref_kind != "ClusterRole":
                    failures.append(
                        f"ClusterRoleBinding/{name}: cannot reference namespaced Role/{ref_name}"
                    )
                for subject in resource.get("subjects", []) or []:
                    subject_kind = subject.get("kind")
                    subject_name = str(subject.get("name", ""))
                    subject_namespace = str(subject.get("namespace", ""))
                    if not subject_name:
                        failures.append(f"{kind}/{namespace}/{name}: RBAC subject has no name")
                    if (
                        subject_kind == "ServiceAccount"
                        and service_accounts
                        and (
                            subject_namespace,
                            subject_name,
                        )
                        not in service_accounts
                    ):
                        failures.append(
                            f"{kind}/{namespace}/{name}: subject ServiceAccount/{subject_namespace}/{subject_name} is not rendered"
                        )

    for resource in resources:
        if not isinstance(resource, dict) or resource.get("kind") != "PodMonitoring":
            continue
        metadata = resource.get("metadata", {}) or {}
        namespace = str(metadata.get("namespace", ""))
        name = str(metadata.get("name", ""))
        selector = (resource.get("spec", {}) or {}).get("selector", {}) or {}
        matches = selected_templates(templates, namespace, selector)
        if not matches:
            failures.append(
                f"PodMonitoring/{namespace}/{name}: selector matches no rendered pod template"
            )
            continue
        for endpoint in (resource.get("spec", {}) or {}).get("endpoints", []) or []:
            port = endpoint.get("port")
            if not port_is_exposed(port, matches):
                failures.append(
                    f"PodMonitoring/{namespace}/{name}: endpoint port {port!r} is not exposed by a selected pod"
                )

    if failures:
        for failure in failures:
            print(f"ERROR: {args.label}: {failure}")
        raise SystemExit(1)

    monitoring_count = sum(
        isinstance(resource, dict) and resource.get("kind") == "PodMonitoring"
        for resource in resources
    )
    print(f"RELATIONS          {args.label} (PodMonitoring={monitoring_count})")


if __name__ == "__main__":
    main()
