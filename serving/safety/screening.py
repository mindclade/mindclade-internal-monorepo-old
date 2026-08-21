# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Fail-closed composition over injected qualified screeners."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Protocol

from .policy import Decision, SafetyPolicy, Severity

MAXIMUM_CONTENT_BYTES = 16 * 1024 * 1024


@dataclass(frozen=True, slots=True)
class ScreeningRequest:
    request_id: str
    input_digest: str
    media_type: str
    content: bytes

    def validate(self) -> None:
        if not self.request_id or len(self.request_id) > 256:
            raise ValueError("screening request id is invalid")
        if not self.input_digest.startswith("sha256:") or len(self.input_digest) != 71:
            raise ValueError("screening input digest is invalid")
        if not self.media_type or len(self.media_type) > 255:
            raise ValueError("screening media type is invalid")
        if not isinstance(self.content, bytes) or len(self.content) > MAXIMUM_CONTENT_BYTES:
            raise ValueError("screening content is outside bounds")


@dataclass(frozen=True, slots=True)
class Finding:
    screener: str
    code: str
    severity: Severity

    def validate(self) -> None:
        for name, value in (("screener", self.screener), ("code", self.code)):
            if not value or len(value) > 128 or any(ord(character) < 0x20 for character in value):
                raise ValueError(f"safety finding {name} is invalid")
        if not isinstance(self.severity, Severity):
            raise ValueError("safety finding severity is invalid")


class Screener(Protocol):
    name: str

    def screen(self, request: ScreeningRequest) -> tuple[Finding, ...]: ...


@dataclass(frozen=True, slots=True)
class ScreeningResult:
    request_id: str
    input_digest: str
    policy_digest: str
    decision: Decision
    findings: tuple[Finding, ...]
    incomplete_screeners: tuple[str, ...] = ()


class SafetyEngine:
    def __init__(self, policy: SafetyPolicy, screeners: tuple[Screener, ...]) -> None:
        policy.validate()
        names = tuple(screener.name for screener in screeners)
        if len(names) != len(set(names)):
            raise ValueError("screener names must be unique")
        self._policy = policy
        self._screeners = {screener.name: screener for screener in screeners}

    def screen(self, request: ScreeningRequest) -> ScreeningResult:
        request.validate()
        findings: list[Finding] = []
        incomplete: list[str] = []
        for name in self._policy.required_screeners:
            screener = self._screeners.get(name)
            if screener is None:
                incomplete.append(name)
                continue
            try:
                produced = screener.screen(request)
            except Exception:
                incomplete.append(name)
                continue
            for finding in produced:
                finding.validate()
                if finding.screener != name:
                    raise ValueError("screener returned a finding under another identity")
                findings.append(finding)
                if len(findings) > self._policy.maximum_findings:
                    incomplete.append(name)
                    findings = findings[: self._policy.maximum_findings]
                    break
        maximum = max((finding.severity for finding in findings), default=Severity.INFORMATIONAL)
        if incomplete and self._policy.fail_closed:
            decision = Decision.DENY
        elif incomplete or maximum >= self._policy.review_at:
            decision = Decision.REVIEW
        else:
            decision = Decision.ALLOW
        if maximum >= self._policy.deny_at:
            decision = Decision.DENY
        return ScreeningResult(
            request.request_id,
            request.input_digest,
            self._policy.digest,
            decision,
            tuple(findings),
            tuple(sorted(set(incomplete))),
        )
