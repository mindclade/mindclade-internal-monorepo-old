# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

import pytest

from serving.safety import (
    Decision,
    Finding,
    SafetyEngine,
    SafetyPolicy,
    ScreeningRequest,
    Severity,
    to_audit_record,
    validate_composition,
)

DIGEST = "sha256:" + "a" * 64
POLICY = "sha256:" + "b" * 64


class Clean:
    name = "clean"

    def screen(self, request):
        return ()


class Risky:
    name = "risk"

    def screen(self, request):
        return (Finding(self.name, "qualified-risk", Severity.HIGH),)


def policy(*names: str, fail_closed: bool = True) -> SafetyPolicy:
    return SafetyPolicy(POLICY, 1, tuple(sorted(names)), fail_closed=fail_closed)


def request(content: bytes = b"sensitive payload") -> ScreeningRequest:
    return ScreeningRequest("request-1", DIGEST, "application/octet-stream", content)


def test_clean_required_screeners_allow() -> None:
    result = SafetyEngine(policy("clean"), (Clean(),)).screen(request())
    assert result.decision is Decision.ALLOW
    assert result.findings == ()


def test_high_severity_finding_denies() -> None:
    result = SafetyEngine(policy("risk"), (Risky(),)).screen(request())
    assert result.decision is Decision.DENY


def test_missing_or_failed_required_screener_fails_closed() -> None:
    result = SafetyEngine(policy("missing"), ()).screen(request())
    assert result.decision is Decision.DENY
    assert result.incomplete_screeners == ("missing",)


def test_audit_projection_contains_no_raw_content() -> None:
    result = SafetyEngine(policy("risk"), (Risky(),)).screen(request(b"secret"))
    record = to_audit_record(result)
    assert record.input_digest == DIGEST
    assert "secret" not in repr(record)
    assert record.finding_codes == ("risk:qualified-risk",)


def test_non_fail_closed_policy_requires_complete_startup_composition() -> None:
    with pytest.raises(ValueError, match="missing required"):
        validate_composition(policy("missing", fail_closed=False), ())
