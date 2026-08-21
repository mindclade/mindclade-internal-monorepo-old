# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""License/consent evidence completeness checks without legal-policy invention."""

from __future__ import annotations

import re
from collections.abc import Iterable
from dataclasses import dataclass

from .report import QualityFinding, Severity

_DIGEST = re.compile(r"sha256:[0-9a-f]{64}")


@dataclass(frozen=True, slots=True)
class UseEvidence:
    source_snapshot_digest: str
    license_ref: str
    evidence_digest: str
    approved_uses: tuple[str, ...]

    def __post_init__(self) -> None:
        if not _DIGEST.fullmatch(self.source_snapshot_digest) or not _DIGEST.fullmatch(
            self.evidence_digest
        ):
            raise ValueError("use evidence digest is invalid")
        if not self.license_ref or len(self.license_ref) > 256:
            raise ValueError("use evidence license reference is invalid")
        if not self.approved_uses or len(set(self.approved_uses)) != len(self.approved_uses):
            raise ValueError("use evidence requires unique approved uses")


def use_findings(
    evidence: Iterable[UseEvidence], intended_uses: Iterable[str]
) -> tuple[QualityFinding, ...]:
    items = tuple(evidence)
    intended = set(intended_uses)
    unsupported = sum(1 for item in items if not intended.issubset(set(item.approved_uses)))
    if items and not unsupported:
        return ()
    return (
        QualityFinding(
            "license-use-not-approved",
            Severity.BLOCKING,
            "source-evidence",
            unsupported if items else 1,
            "source license/consent evidence does not approve every intended use",
        ),
    )
