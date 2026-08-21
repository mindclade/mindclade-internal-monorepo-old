# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Independent attestation and completeness checks for evaluation evidence."""

from __future__ import annotations

import re
from dataclasses import dataclass
from datetime import datetime
from typing import Final

from libs.python.errors import InvalidArgument
from libs.python.identifiers import Digest

from .evidence import EvaluationEvidence
from .thresholds import MetricCategory

_IDENTITY: Final = re.compile(r"^[a-z][a-z0-9._:/@-]{2,255}$")


@dataclass(frozen=True, slots=True)
class VerificationPolicy:
    policy_digest: Digest
    required_categories: tuple[MetricCategory, ...]
    require_independent_attestor: bool = True

    def __post_init__(self) -> None:
        if not isinstance(self.policy_digest, Digest):
            raise _invalid("verification policy digest is invalid", "policy_digest")
        if not self.required_categories:
            raise _invalid("verification requires at least one category", "required_categories")
        if any(not isinstance(item, MetricCategory) for item in self.required_categories):
            raise _invalid("verification category is invalid", "required_category")
        if len(set(self.required_categories)) != len(self.required_categories):
            raise _invalid("verification categories must be unique", "required_category_duplicate")
        if not isinstance(self.require_independent_attestor, bool):
            raise _invalid("attestor policy must be boolean", "attestor_policy")


@dataclass(frozen=True, slots=True)
class Attestation:
    """A reference to a separately stored signature, not signature bytes or credentials."""

    attestor_identity: str
    subject_digest: Digest
    policy_digest: Digest
    signature_digest: Digest
    signed_at: datetime

    def __post_init__(self) -> None:
        if (
            not isinstance(self.attestor_identity, str)
            or _IDENTITY.fullmatch(self.attestor_identity) is None
        ):
            raise _invalid("attestor identity is invalid", "attestor_identity")
        if any(
            not isinstance(value, Digest)
            for value in (self.subject_digest, self.policy_digest, self.signature_digest)
        ):
            raise _invalid("attestation digest is invalid", "attestation_digest")
        if (
            not isinstance(self.signed_at, datetime)
            or self.signed_at.tzinfo is None
            or self.signed_at.utcoffset() is None
        ):
            raise _invalid("attestation time must be timezone-aware", "attestation_time")


@dataclass(frozen=True, slots=True)
class VerificationResult:
    accepted: bool
    evidence_digest: Digest
    policy_digest: Digest
    reasons: tuple[str, ...]
    attestor_identity: str | None


def verify_evidence(
    evidence: EvaluationEvidence,
    policy: VerificationPolicy,
    attestation: Attestation | None,
) -> VerificationResult:
    """Fail closed on execution failures, missing classes, or unattested evidence."""

    if not isinstance(evidence, EvaluationEvidence):
        raise _invalid("evaluation evidence is invalid", "evidence")
    reasons: list[str] = []
    if evidence.execution_failures:
        reasons.append("execution-failure")
    if evidence.missing_outputs:
        reasons.append("missing-output")
    if any(not item.passed for item in evidence.outcomes):
        reasons.append("threshold-failure")

    present = {item.category for item in evidence.outcomes if item.passed}
    if any(category not in present for category in policy.required_categories):
        reasons.append("required-category-missing")

    evidence_digest = evidence.digest()
    attestor_identity: str | None = None
    if policy.require_independent_attestor:
        if attestation is None:
            reasons.append("attestation-missing")
        elif not attestation.subject_digest.equals(evidence_digest):
            reasons.append("attestation-subject-mismatch")
        elif not attestation.policy_digest.equals(policy.policy_digest):
            reasons.append("attestation-policy-mismatch")
        else:
            attestor_identity = attestation.attestor_identity

    return VerificationResult(
        accepted=not reasons,
        evidence_digest=evidence_digest,
        policy_digest=policy.policy_digest,
        reasons=tuple(sorted(set(reasons))),
        attestor_identity=attestor_identity,
    )


def _invalid(message: str, reason: str) -> InvalidArgument:
    return InvalidArgument(message, reason=reason, operation="evaluation.qualification")
