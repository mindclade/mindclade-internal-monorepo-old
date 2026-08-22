# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

from copy import deepcopy
from pathlib import Path

from configs.contract_validation import load_json, validate
from infra.observability.alert_contracts import load_json_yaml
from infra.observability.training_metrics import validate_training_metrics

ROOT = Path(__file__).resolve().parents[1]


def test_training_observability_contract_is_bounded_and_fail_closed() -> None:
    assert validate_training_metrics(ROOT) == []
    contract = load_json(ROOT / "training-metrics.json")
    assert contract["status"] == "external-producer-contract"
    assert contract["producer"]["implementationState"] == "external-contract-required"
    assert contract["producer"]["metricsEndpoint"]["podMonitoringState"] == (
        "blocked-until-exporter-exists"
    )
    assert sum(item["maximumSeries"] for item in contract["metrics"]) == 73


def test_metric_schema_rejects_an_unbounded_family() -> None:
    schema = load_json(ROOT / "training-metrics.schema.json")
    contract = load_json(ROOT / "training-metrics.json")
    invalid = deepcopy(contract)
    invalid["metrics"][0]["labels"].append("run")
    invalid["metrics"][0]["allowedLabelValues"]["run"] = ["arbitrary"]
    invalid["metrics"][0]["maximumSeries"] = 1000
    assert validate(invalid, schema)
    assert "run" in contract["privacy"]["forbiddenLabels"]


def test_training_alert_thresholds_are_proposals_and_disabled() -> None:
    for name in ("checkpoint-failed", "training-stalled"):
        contract = load_json_yaml(ROOT / "alerts" / f"{name}.yaml")
        assert contract["activationPolicy"]["enabledByDefault"] is False
        assert all(item["thresholdStatus"] == "proposed" for item in contract["signals"])


def test_training_dashboard_is_source_only() -> None:
    dashboard = load_json(ROOT / "dashboards/training.json")
    assert dashboard["status"] == "activation-blocked"
    assert dashboard["deployment"]["deployableArtifact"] is False
    assert dashboard["activationPolicy"]["requiresConnectedQualification"] is True
