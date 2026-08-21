# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Fail-closed quality gate over bounded validated samples."""

from __future__ import annotations

import datetime as dt
from collections.abc import Iterable

from data.sample import Sample

from .report import QualityFinding, QualityMetric, QualityReport
from .validators import Validator

MAX_SAMPLES = 10_000_000


class QualityGate:
    def __init__(self, policy_version: str, validators: Iterable[Validator]) -> None:
        self._policy_version = policy_version
        self._validators = tuple(validators)
        if not self._validators:
            raise ValueError("quality gate requires at least one validator")
        names = [validator.name for validator in self._validators]
        if len(set(names)) != len(names):
            raise ValueError("quality validator names must be unique")

    def evaluate(
        self,
        dataset_manifest_digest: str,
        samples: Iterable[Sample],
        *,
        evaluated_at: dt.datetime,
    ) -> QualityReport:
        materialized: list[Sample] = []
        for sample in samples:
            if not isinstance(sample, Sample):
                raise TypeError("quality gate inputs must be Sample values")
            materialized.append(sample)
            if len(materialized) > MAX_SAMPLES:
                raise ValueError("quality gate sample count exceeds bound")
        findings: list[QualityFinding] = []
        for validator in self._validators:
            findings.extend(validator.validate(materialized))
        metrics = (
            QualityMetric("sample-count", float(len(materialized)), "records"),
            QualityMetric(
                "finding-count",
                float(sum(finding.count for finding in findings)),
                "records",
            ),
        )
        return QualityReport(
            dataset_manifest_digest,
            self._policy_version,
            evaluated_at,
            tuple(findings),
            metrics,
        )
