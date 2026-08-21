# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Immutable promotion decision records; this module never mutates a registry."""

from __future__ import annotations

import json
from dataclasses import dataclass
from typing import Final

from libs.python.errors import InvalidArgument
from libs.python.identifiers import Digest

from .verification import VerificationResult

SCHEMA_VERSION: Final = "mindclade.dev/evaluation-promotion-decision/v1"


@dataclass(frozen=True, slots=True)
class PromotionDecision:
    """A control-plane input. ``authorized`` is not itself a registry mutation."""

    authorized: bool
    candidate_digest: Digest
    evidence_digest: Digest
    policy_digest: Digest
    reasons: tuple[str, ...]
    attestor_identity: str | None

    def __post_init__(self) -> None:
        if not isinstance(self.authorized, bool):
            raise _invalid("promotion authorization must be boolean", "promotion_authorized")
        if any(
            not isinstance(value, Digest)
            for value in (self.candidate_digest, self.evidence_digest, self.policy_digest)
        ):
            raise _invalid("promotion digest is invalid", "promotion_digest")
        if any(not isinstance(reason, str) or not reason for reason in self.reasons):
            raise _invalid("promotion reason is invalid", "promotion_reason")
        if self.authorized and (self.reasons or not self.attestor_identity):
            raise _invalid("authorized decision must be clean and attested", "promotion_invariant")
        if not self.authorized and not self.reasons:
            raise _invalid("denied decision must explain why", "promotion_invariant")

    def canonical_document(self) -> bytes:
        value = {
            "schema_version": SCHEMA_VERSION,
            "authorized": self.authorized,
            "candidate_digest": self.candidate_digest.text,
            "evidence_digest": self.evidence_digest.text,
            "policy_digest": self.policy_digest.text,
            "reasons": sorted(self.reasons),
            "attestor_identity": self.attestor_identity,
            "mutation_authority": "control-plane-only",
        }
        return (json.dumps(value, sort_keys=True, separators=(",", ":")) + "\n").encode()

    def digest(self) -> Digest:
        return Digest.of(self.canonical_document())


def make_promotion_decision(
    candidate_digest: Digest, verification: VerificationResult
) -> PromotionDecision:
    if not isinstance(candidate_digest, Digest) or not isinstance(verification, VerificationResult):
        raise _invalid("promotion inputs are invalid", "promotion_input")
    return PromotionDecision(
        authorized=verification.accepted,
        candidate_digest=candidate_digest,
        evidence_digest=verification.evidence_digest,
        policy_digest=verification.policy_digest,
        reasons=verification.reasons,
        attestor_identity=verification.attestor_identity,
    )


def _invalid(message: str, reason: str) -> InvalidArgument:
    return InvalidArgument(message, reason=reason, operation="evaluation.qualification")
