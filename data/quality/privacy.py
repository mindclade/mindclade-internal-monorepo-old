# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Privacy and telemetry-boundary checks derived from field contracts."""

from __future__ import annotations

from collections.abc import Iterable

from data.contracts import DatasetContract, LogPolicy, Sensitivity

from .report import QualityFinding, Severity


def telemetry_findings(
    contract: DatasetContract, telemetry_fields: Iterable[str]
) -> tuple[QualityFinding, ...]:
    emitted = set(telemetry_fields)
    forbidden = [
        field.name
        for field in contract.fields
        if field.name in emitted
        and (
            field.log_policy is not LogPolicy.ALLOW
            or field.sensitivity in {Sensitivity.PROPRIETARY_INTERNAL, Sensitivity.RESTRICTED}
        )
    ]
    if not forbidden:
        return ()
    return (
        QualityFinding(
            "sensitive-telemetry-field",
            Severity.BLOCKING,
            "telemetry-contract",
            len(forbidden),
            "one or more contract fields are not permitted in verbatim telemetry",
        ),
    )
