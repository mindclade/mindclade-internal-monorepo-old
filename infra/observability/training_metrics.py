# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Validate the bounded reference-training metrics, rules, alerts, and dashboard contract."""

from __future__ import annotations

from pathlib import Path
from typing import Any, cast

from configs.contract_validation import load_json, validate, validate_schema_subset
from infra.observability.alert_contracts import load_json_yaml

_FORBIDDEN = {
    "artifact",
    "checkpoint",
    "dataset",
    "feature",
    "label",
    "model",
    "molecule",
    "prompt",
    "request-id",
    "run",
    "sequence",
    "tenant",
    "user",
    "workspace",
}


def _dict(value: object) -> dict[str, Any]:
    return cast("dict[str, Any]", value) if isinstance(value, dict) else {}


def _list(value: object) -> list[Any]:
    return cast("list[Any]", value) if isinstance(value, list) else []


def validate_training_metrics(root: Path) -> list[str]:
    errors: list[str] = []
    try:
        schema = load_json(root / "training-metrics.schema.json")
        contract = load_json(root / "training-metrics.json")
        dashboard_schema = load_json(root / "dashboard-contract.schema.json")
        dashboard = load_json(root / "dashboards/training.json")
        alerts = {
            name: load_json_yaml(root / "alerts" / f"{name}.yaml")
            for name in ("checkpoint-failed", "training-stalled")
        }
    except (OSError, ValueError) as error:
        return [str(error)]

    errors.extend(validate_schema_subset(schema))
    errors.extend(
        f"training-metrics.json {failure.path}: {failure.message}"
        for failure in validate(contract, schema)
    )
    errors.extend(
        f"dashboards/training.json {failure.path}: {failure.message}"
        for failure in validate(dashboard, dashboard_schema)
    )

    metrics = [_dict(item) for item in _list(contract.get("metrics"))]
    metric_names = [str(item.get("name", "")) for item in metrics]
    if metric_names != sorted(metric_names) or len(metric_names) != len(set(metric_names)):
        errors.append("training metrics must be sorted and unique")
    total_series = 0
    all_labels: set[str] = set()
    for metric in metrics:
        name = str(metric.get("name", ""))
        labels = [str(item) for item in _list(metric.get("labels"))]
        allowed = _dict(metric.get("allowedLabelValues"))
        if labels != sorted(labels) or set(labels) != set(allowed):
            errors.append(f"{name}: labels must be sorted and match allowedLabelValues")
        maximum = 1
        for label in labels:
            values = [str(item) for item in _list(allowed.get(label))]
            if values != sorted(values) or len(values) != len(set(values)):
                errors.append(f"{name}: values for {label} must be sorted and unique")
            maximum *= len(values)
            all_labels.add(label)
        if metric.get("maximumSeries") != maximum:
            errors.append(f"{name}: maximumSeries must equal the bounded label product")
        total_series += maximum
    if contract.get("maximumExporterSeries") != total_series:
        errors.append("maximumExporterSeries must equal the exact family series bounds")
    forbidden = {str(item) for item in _list(_dict(contract.get("privacy")).get("forbiddenLabels"))}
    if not _FORBIDDEN.issubset(forbidden) or all_labels & forbidden:
        errors.append("training metric labels violate the privacy/cardinality boundary")

    rules = [_dict(item) for item in _list(contract.get("recordingRules"))]
    rule_names = [str(item.get("name", "")) for item in rules]
    semantic_names = [str(item.get("semanticMetric", "")) for item in rules]
    if rule_names != sorted(rule_names) or len(rule_names) != len(set(rule_names)):
        errors.append("training recording rules must be sorted and unique")
    if len(semantic_names) != len(set(semantic_names)):
        errors.append("training semantic metric names must be unique")
    for rule in rules:
        sources = [str(item) for item in _list(rule.get("sourceMetrics"))]
        if sources != sorted(sources) or not set(sources).issubset(metric_names):
            errors.append(f"{rule.get('name')}: source metrics must be sorted and declared")
    rule_by_semantic = {
        str(item.get("semanticMetric", "")): str(item.get("name", "")) for item in rules
    }

    signal_items: list[dict[str, Any]] = []
    for name, alert in alerts.items():
        signals = [_dict(item) for item in _list(alert.get("signals"))]
        if any(item.get("thresholdStatus") != "proposed" for item in signals):
            errors.append(f"{name}: every training threshold must remain explicitly proposed")
        signal_items.extend(signals)
        signal_items.extend(_dict(item) for item in _list(alert.get("observedSignals")))
    signal_by_name = {str(item.get("name", "")): item for item in signal_items}
    if len(signal_by_name) != len(signal_items):
        errors.append("training alert and observed signal names must be globally unique")
    for signal, item in signal_by_name.items():
        if str(item.get("metric", "")) not in rule_by_semantic:
            errors.append(f"{signal}: alert/dashboard metric has no declared recording rule")

    panels = [_dict(item) for item in _list(dashboard.get("panels"))]
    panel_by_signal = {str(item.get("signal", "")): item for item in panels}
    if (
        dashboard.get("name") != "training"
        or dashboard.get("status") != "activation-blocked"
        or set(panel_by_signal) != set(signal_by_name)
        or len(panel_by_signal) != len(panels)
    ):
        errors.append("training dashboard must cover each training signal exactly once")
    for signal, item in signal_by_name.items():
        panel = _dict(panel_by_signal.get(signal))
        metric = str(item.get("metric", ""))
        if panel.get("metric") != metric or panel.get("recordingRule") != rule_by_semantic.get(
            metric
        ):
            errors.append(f"training dashboard mapping drifted for {signal!r}")
    return sorted(set(errors))


def main() -> int:
    errors = validate_training_metrics(Path(__file__).resolve().parent)
    for error in errors:
        print(f"ERROR: {error}")
    if errors:
        return 1
    print("training observability contract validation passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
