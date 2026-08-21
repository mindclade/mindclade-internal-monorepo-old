# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Deterministic evaluation evidence and fail-closed release qualification."""

from .evidence import EvaluationEvidence
from .promotion import PromotionDecision
from .release_gate import EvaluationBatch, EvaluationPlan, GateResult, build_evidence, evaluate_release
from .thresholds import (
    Direction,
    MetricCategory,
    MetricObservation,
    SliceObservation,
    ThresholdOutcome,
    ThresholdRule,
)
from .verification import Attestation, VerificationPolicy, VerificationResult

__all__ = [
    "Attestation",
    "Direction",
    "EvaluationBatch",
    "EvaluationEvidence",
    "EvaluationPlan",
    "GateResult",
    "MetricCategory",
    "MetricObservation",
    "PromotionDecision",
    "SliceObservation",
    "ThresholdOutcome",
    "ThresholdRule",
    "VerificationPolicy",
    "VerificationResult",
    "build_evidence",
    "evaluate_release",
]
