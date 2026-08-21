# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

from pathlib import Path

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


def test_studio_contract_preserves_actionable_and_observe_only_signals() -> None:
    contract = load_json_yaml(ROOT / "alerts/studio-browser-plane.yaml")
    assert len(contract["signals"]) == 12
    assert [item["name"] for item in contract["observedSignals"]] == [
        "api-success-rate",
        "iap-signin-redirect-rate",
        "stream-resume-rate",
        "trace-propagation",
    ]
