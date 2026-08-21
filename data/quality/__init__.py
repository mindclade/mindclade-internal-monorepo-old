# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Aggregate-only data quality, integrity, privacy, and leakage gates."""

from .gates import QualityGate
from .integrity import duplicate_sample_findings, verify_bytes
from .leakage import group_split_findings
from .report import QualityFinding, QualityMetric, QualityReport, Severity
from .validators import FunctionValidator, Validator

__all__ = [
    "FunctionValidator",
    "QualityFinding",
    "QualityGate",
    "QualityMetric",
    "QualityReport",
    "Severity",
    "Validator",
    "duplicate_sample_findings",
    "group_split_findings",
    "verify_bytes",
]

"""Mindclade scaffold package for data/quality."""
