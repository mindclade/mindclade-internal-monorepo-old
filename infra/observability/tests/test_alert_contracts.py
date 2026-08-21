# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

from copy import deepcopy
from pathlib import Path

from configs.contract_validation import load_json, validate
from infra.observability.alert_contracts import load_json_yaml, validate_catalog

ROOT = Path(__file__).resolve().parents[1]


def test_complete_catalog_is_fail_closed() -> None:
    assert validate_catalog(ROOT) == []


def test_every_contract_is_inactive_and_requires_chat_email_evidence() -> None:
    for path in sorted((ROOT / "alerts").glob("*.yaml")):
        contract = load_json_yaml(path)
        assert contract["activationPolicy"]["enabledByDefault"] is False
        assert "googleChatNotificationChannelResourceNames" in contract["requiredEnvironmentInputs"]
        assert "emailNotificationChannelResourceNames" in contract["requiredEnvironmentInputs"]
        assert contract["signals"]


def test_profiles_are_proposals_not_activation_claims() -> None:
    profiles = load_json_yaml(ROOT / "availability-profiles.yaml")
    assert profiles["approvalStatus"] == "proposed"
    assert profiles["activationPolicy"] == {
        "enabledByDefault": False,
        "requiresEnvironmentApproval": True,
    }


def test_control_admission_contract_is_bounded_and_fail_closed() -> None:
    contract = load_json_yaml(ROOT / "alerts/control-admission-degraded.yaml")
    signals = {item["name"]: item for item in contract["signals"]}
    assert contract["availabilityProfile"] == "control-admission-critical"
    correctness_signals = {
        "control-admission-api-metric-contract-incomplete",
        "control-admission-audit-outbox-drift",
        "control-admission-backlog-after-two-successful-sweeps",
        "control-admission-expired-reservation-age",
        "control-admission-maintenance-metric-contract-incomplete",
        "control-admission-sweep-stale",
    }
    assert all(signals[name]["duration"] == "1m" for name in correctness_signals)
    assert all(signals[name]["missingData"] == "fire" for name in correctness_signals)
    assert signals["control-admission-fast-error-budget-burn"] == {
        "name": "control-admission-fast-error-budget-burn",
        "description": "Both the five-minute and one-hour admission windows are consuming the 99.95 percent objective error budget above the reviewed fast-burn rate.",
        "metric": "mindclade.control_admission.error_budget_fast_burn_pair",
        "condition": "above",
        "threshold": 14.4,
        "duration": "5m",
        "severity": "page",
        "missingData": "fire",
        "minimumSamples": 100,
        "runbookAnchor": "control-admission-fast-error-budget-burn",
    }
    assert signals["control-admission-slow-error-budget-burn"]["metric"] == (
        "mindclade.control_admission.error_budget_slow_burn_pair"
    )
    latency = signals["control-admission-decision-latency-slo-breached"]
    assert latency["metric"] == (
        "mindclade.control_admission.decision_latency_objective_ratio_5m"
    )
    assert latency["condition"] == "below"
    assert latency["threshold"] == 0.99
    assert signals["control-admission-api-metric-contract-incomplete"]["metric"] == (
        "mindclade.control_admission.api_metric_contract_complete"
    )
    assert signals["control-admission-maintenance-metric-contract-incomplete"]["metric"] == (
        "mindclade.control_admission.maintenance_metric_contract_complete"
    )
    assert {
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
    }.issubset(contract["cardinality"]["forbiddenDimensions"])


def test_alert_schema_rejects_subminute_correctness_duration() -> None:
    schema = load_json(ROOT / "alert-contract.schema.json")
    contract = load_json_yaml(ROOT / "alerts/control-admission-degraded.yaml")
    assert validate(contract, schema) == ()

    invalid = deepcopy(contract)
    invalid["signals"][1]["duration"] = "0s"
    assert any(error.path.endswith(".duration") for error in validate(invalid, schema))


def test_control_admission_dashboard_covers_contract_without_deployment_claim() -> None:
    dashboard = load_json(ROOT / "dashboards/control-plane.json")
    contract = load_json_yaml(ROOT / "alerts/control-admission-degraded.yaml")
    contract_signals = {item["name"] for item in contract["signals"] + contract["observedSignals"]}
    assert dashboard["name"] == "control-plane"
    assert dashboard["status"] == "activation-blocked"
    assert dashboard["deployment"]["deployableArtifact"] is False
    assert {item["signal"] for item in dashboard["panels"]} == contract_signals


def test_control_admission_profile_matches_monthly_slo() -> None:
    profiles = load_json_yaml(ROOT / "availability-profiles.yaml")
    admission = next(
        item for item in profiles["profiles"] if item["name"] == "control-admission-critical"
    )
    assert admission == {
        "name": "control-admission-critical",
        "serviceClasses": ["control-plane"],
        "sli": "availability",
        "objective": 0.9995,
        "window": "30d",
        "minimumEvents": 1000,
        "fastBurn": 14.4,
        "slowBurn": 6.0,
    }


def test_studio_contract_preserves_actionable_and_observe_only_signals() -> None:
    contract = load_json_yaml(ROOT / "alerts/studio-browser-plane.yaml")
    assert len(contract["signals"]) == 12
    assert [item["name"] for item in contract["observedSignals"]] == [
        "api-success-rate",
        "iap-signin-redirect-rate",
        "stream-resume-rate",
        "trace-propagation",
    ]
