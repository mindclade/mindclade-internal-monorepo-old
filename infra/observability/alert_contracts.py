# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Validate the provider-neutral alert catalog without a YAML runtime dependency."""

from __future__ import annotations

import json
from pathlib import Path
from typing import Any, cast

from configs.contract_validation import load_json, validate

_REQUIRED_ENVIRONMENT_INPUTS = {
    "emailNotificationChannelResourceNames",
    "environment",
    "googleChatNotificationChannelResourceNames",
    "httpsRunbookUrl",
    "projectId",
    "qualificationEvidenceSha256",
}
_FORBIDDEN_DIMENSIONS = {
    "dataset",
    "feature",
    "label",
    "model",
    "molecule",
    "prompt",
    "request-id",
    "sequence",
    "tenant",
    "user",
}
_DASHBOARD_VARIABLES = {"cluster", "environment", "location", "namespace"}
_CONTROL_ADMISSION_FORBIDDEN_DIMENSIONS = {
    "idempotency-key",
    "model",
    "provider",
    "reason",
    "request-id",
    "reservation-id",
    "route",
    "subject",
    "tenant",
    "workspace",
}
_BACKEND_COMPATIBILITY = {
    "grafana-datasources.yaml": {
        "authoritativeServices": ["cloud-monitoring", "google-managed-prometheus"],
        "forbiddenResponsibilities": [
            "alerting",
            "dashboard-deployment",
            "datasource-provisioning",
        ],
        "replacementSources": [
            "infra/observability/dashboards/control-plane.json",
            "infra/terraform/modules/monitoring/main.tf",
        ],
    },
    "otel-collector.yaml": {
        "authoritativeServices": [
            "cloud-logging",
            "cloud-monitoring",
            "cloud-trace",
            "google-managed-prometheus",
        ],
        "forbiddenResponsibilities": [
            "collector-deployment",
            "credential-distribution",
            "exporter-endpoints",
        ],
        "replacementSources": [
            "infra/kubernetes/platform/observability/pod-monitoring.yaml",
            "infra/terraform/modules/observability/main.tf",
        ],
    },
}


def _as_dict(value: object) -> dict[str, Any]:
    return cast("dict[str, Any]", value) if isinstance(value, dict) else {}


def _as_list(value: object) -> list[Any]:
    return cast("list[Any]", value) if isinstance(value, list) else []


def _as_string_set(value: object) -> set[str]:
    items = _as_list(value)
    return set(items) if all(isinstance(item, str) for item in items) else set()


def load_json_yaml(path: Path) -> dict[str, Any]:
    payload = "\n".join(
        line
        for line in path.read_text(encoding="utf-8").splitlines()
        if not line.startswith("#") and line.strip() != "---"
    ).strip()
    value = json.loads(payload)
    if not isinstance(value, dict):
        raise ValueError(f"{path}: expected one object")
    return value


def _validate_backend_compatibility(root: Path) -> list[str]:
    errors: list[str] = []
    try:
        schema = load_json(root / "backend-compatibility.schema.json")
    except (OSError, ValueError, json.JSONDecodeError) as exc:
        return [str(exc)]
    repository = root.parents[1]
    for filename, expected in sorted(_BACKEND_COMPATIBILITY.items()):
        try:
            document = load_json_yaml(root / filename)
        except (OSError, ValueError, json.JSONDecodeError) as exc:
            errors.append(f"{filename}: {exc}")
            continue
        errors.extend(
            f"{filename} {failure.path}: {failure.message}"
            for failure in validate(document, schema)
        )
        if document.get("name") != Path(filename).stem:
            errors.append(f"{filename}: name must match the file stem")
        for field, value in expected.items():
            if document.get(field) != value:
                errors.append(f"{filename}: {field} drifted from the backend authority decision")
        for source in _as_list(document.get("replacementSources")):
            if not isinstance(source, str):
                continue
            candidate = repository / source
            try:
                candidate.resolve().relative_to(repository.resolve())
            except OSError, ValueError:
                errors.append(f"{filename}: replacement source escapes the repository: {source!r}")
                continue
            if not candidate.is_file():
                errors.append(f"{filename}: replacement source is missing: {source!r}")
    return errors


def validate_catalog(root: Path) -> list[str]:
    errors = _validate_backend_compatibility(root)
    try:
        alert_schema = load_json(root / "alert-contract.schema.json")
        profile_schema = load_json(root / "availability-profiles.schema.json")
        profile_document = load_json_yaml(root / "availability-profiles.yaml")
        dashboard_schema = load_json(root / "dashboard-contract.schema.json")
        dashboard_document = load_json(root / "dashboards/control-plane.json")
    except (OSError, ValueError, json.JSONDecodeError) as exc:
        return [str(exc)]
    errors.extend(
        f"availability-profiles.yaml {failure.path}: {failure.message}"
        for failure in validate(profile_document, profile_schema)
    )
    errors.extend(
        f"dashboards/control-plane.json {failure.path}: {failure.message}"
        for failure in validate(dashboard_document, dashboard_schema)
    )
    raw_profiles = _as_list(profile_document.get("profiles"))
    profile_names = [str(item.get("name", "")) for item in raw_profiles if isinstance(item, dict)]
    if profile_names != sorted(profile_names) or len(set(profile_names)) != len(profile_names):
        errors.append("availability profiles must be sorted and unique")
    profile_classes = {
        str(item.get("name", "")): _as_string_set(item.get("serviceClasses"))
        for item in raw_profiles
        if isinstance(item, dict)
    }
    used_profiles: set[str] = set()
    global_signals: set[str] = set()
    alert_documents: dict[str, dict[str, Any]] = {}
    alert_paths = sorted((root / "alerts").glob("*.yaml"))
    if not alert_paths:
        errors.append("alert catalog is empty")
    for path in alert_paths:
        try:
            document = load_json_yaml(path)
        except (OSError, ValueError, json.JSONDecodeError) as exc:
            errors.append(f"{path.name}: {exc}")
            continue
        errors.extend(
            f"{path.name} {failure.path}: {failure.message}"
            for failure in validate(document, alert_schema)
        )
        if document.get("name") != path.stem:
            errors.append(f"{path.name}: name must match the file stem")
        alert_documents[path.stem] = document
        environment_inputs = _as_string_set(document.get("requiredEnvironmentInputs"))
        if environment_inputs != _REQUIRED_ENVIRONMENT_INPUTS:
            errors.append(
                f"{path.name}: Google Chat, email, runbook, project, environment, and evidence inputs are required"
            )
        profile = str(document.get("availabilityProfile", ""))
        used_profiles.add(profile)
        service_class = str(document.get("serviceClass", ""))
        if service_class not in profile_classes.get(profile, set()):
            errors.append(
                f"{path.name}: availability profile does not permit service class {service_class!r}"
            )
        cardinality = _as_dict(document.get("cardinality"))
        if not _FORBIDDEN_DIMENSIONS.issubset(
            _as_string_set(cardinality.get("forbiddenDimensions"))
        ):
            errors.append(
                f"{path.name}: sensitive/high-cardinality dimensions are not fully forbidden"
            )
        if path.stem == "control-admission-degraded" and not (
            _CONTROL_ADMISSION_FORBIDDEN_DIMENSIONS
        ).issubset(_as_string_set(cardinality.get("forbiddenDimensions"))):
            errors.append(
                "control-admission-degraded.yaml: backend-sensitive dimensions are not fully forbidden"
            )
        signals = _as_list(document.get("signals"))
        names = [str(item.get("name", "")) for item in signals if isinstance(item, dict)]
        if names != sorted(names) or len(set(names)) != len(names):
            errors.append(f"{path.name}: signals must be sorted and unique")
        observed = _as_list(document.get("observedSignals"))
        observed_names = [str(item.get("name", "")) for item in observed if isinstance(item, dict)]
        if observed_names != sorted(observed_names) or len(set(observed_names)) != len(
            observed_names
        ):
            errors.append(f"{path.name}: observed signals must be sorted and unique")
        for name in names + observed_names:
            if name in global_signals:
                errors.append(f"{path.name}: duplicate global signal name {name!r}")
            global_signals.add(name)
    unused = set(profile_names) - used_profiles
    if unused:
        errors.append(f"availability profiles have no alert consumer: {sorted(unused)}")

    dashboard_environment_inputs = _as_string_set(
        dashboard_document.get("requiredEnvironmentInputs")
    )
    if dashboard_document.get("name") != "control-plane":
        errors.append("control-plane dashboard name must match its file stem")
    if dashboard_environment_inputs != _REQUIRED_ENVIRONMENT_INPUTS:
        errors.append(
            "control-plane dashboard requires Google Chat, email, runbook, project, environment, and evidence inputs"
        )
    dashboard_variables = _as_list(dashboard_document.get("variables"))
    if (
        dashboard_variables != sorted(dashboard_variables)
        or set(dashboard_variables) != _DASHBOARD_VARIABLES
    ):
        errors.append(
            "control-plane dashboard variables must be the sorted bounded environment dimensions"
        )
    panels = _as_list(dashboard_document.get("panels"))
    panel_signals = [str(item.get("signal", "")) for item in panels if isinstance(item, dict)]
    if panel_signals != sorted(panel_signals) or len(set(panel_signals)) != len(panel_signals):
        errors.append("control-plane dashboard panel signals must be sorted and unique")

    admission_contract = _as_dict(alert_documents.get("control-admission-degraded"))
    admission_items = _as_list(admission_contract.get("signals")) + _as_list(
        admission_contract.get("observedSignals")
    )
    admission_signals = {
        str(item.get("name", "")): item
        for item in _as_list(admission_contract.get("signals"))
        if isinstance(item, dict)
    }
    expected_admission_semantics = {
        "control-admission-api-metric-contract-incomplete": (
            "mindclade.control_admission.api_metric_contract_complete",
            "below",
            1,
        ),
        "control-admission-decision-latency-slo-breached": (
            "mindclade.control_admission.decision_latency_objective_ratio_5m",
            "below",
            0.99,
        ),
        "control-admission-fast-error-budget-burn": (
            "mindclade.control_admission.error_budget_fast_burn_pair",
            "above",
            14.4,
        ),
        "control-admission-maintenance-metric-contract-incomplete": (
            "mindclade.control_admission.maintenance_metric_contract_complete",
            "below",
            1,
        ),
        "control-admission-slow-error-budget-burn": (
            "mindclade.control_admission.error_budget_slow_burn_pair",
            "above",
            6.0,
        ),
    }
    for signal, (metric, condition, threshold) in expected_admission_semantics.items():
        item = _as_dict(admission_signals.get(signal))
        if (
            item.get("metric") != metric
            or item.get("condition") != condition
            or item.get("threshold") != threshold
        ):
            errors.append(
                f"control-admission-degraded.yaml: paired burn/latency semantics drifted for {signal!r}"
            )
    if "control-admission-decision-p99" in admission_signals:
        errors.append(
            "control-admission-degraded.yaml: interpolated p99 must remain diagnostic, not actionable"
        )
    admission_metrics = {
        str(item.get("name", "")): str(item.get("metric", ""))
        for item in admission_items
        if isinstance(item, dict)
    }
    if admission_metrics.get("control-admission-decision-p99") != (
        "mindclade.control_admission.decision_p99_seconds"
    ):
        errors.append(
            "control-admission-degraded.yaml: diagnostic p99 signal is missing or drifted"
        )
    dashboard_panels = {
        str(item.get("signal", "")): item for item in panels if isinstance(item, dict)
    }
    if set(dashboard_panels) != set(admission_metrics):
        errors.append(
            "control-plane dashboard must cover every actionable and observed control-admission signal exactly once"
        )
    for signal, metric in sorted(admission_metrics.items()):
        panel = _as_dict(dashboard_panels.get(signal))
        if panel.get("metric") != metric:
            errors.append(f"control-plane dashboard metric drifted for signal {signal!r}")
        recording_rule = str(panel.get("recordingRule", ""))
        if not recording_rule.startswith("mindclade:control_admission_"):
            errors.append(
                f"control-plane dashboard recording rule is not admission-owned for {signal!r}"
            )
    return sorted(set(errors))


def main() -> int:
    root = Path(__file__).resolve().parent
    errors = validate_catalog(root)
    for error in errors:
        print(f"ERROR: {error}")
    if errors:
        return 1
    print("observability alert contract validation passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
