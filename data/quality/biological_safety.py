# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Content-free biological-safety attestation contract."""

from __future__ import annotations

import re
from dataclasses import dataclass

_DIGEST = re.compile(r"sha256:[0-9a-f]{64}")


@dataclass(frozen=True, slots=True)
class BiologicalSafetyEvidence:
    dataset_manifest_digest: str
    policy_digest: str
    attestation_digest: str
    decision: str

    def __post_init__(self) -> None:
        for value in (
            self.dataset_manifest_digest,
            self.policy_digest,
            self.attestation_digest,
        ):
            if not _DIGEST.fullmatch(value):
                raise ValueError("biological-safety evidence digest is invalid")
        if self.decision not in {"approved", "rejected", "manual-review"}:
            raise ValueError("biological-safety decision is invalid")

    @property
    def release_eligible(self) -> bool:
        return self.decision == "approved"
