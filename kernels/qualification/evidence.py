# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import hashlib
import json
from dataclasses import dataclass

from kernels.qualification.numerical import NumericalEvidence
from kernels.qualification.performance import PerformanceEvidence


@dataclass(frozen=True, slots=True)
class QualificationEvidence:
    request_digest: str
    implementation_digest: str
    source_revision: str
    generated_source_digest: str
    environment_digest: str
    numerical: NumericalEvidence
    performance: PerformanceEvidence
    soak_digest: str

    def __post_init__(self) -> None:
        for name, digest in (
            ("request_digest", self.request_digest),
            ("implementation_digest", self.implementation_digest),
            ("generated_source_digest", self.generated_source_digest),
            ("environment_digest", self.environment_digest),
            ("soak_digest", self.soak_digest),
        ):
            if len(digest) != 64 or any(c not in "0123456789abcdef" for c in digest):
                raise ValueError(f"{name} must be a lowercase SHA-256 digest")
        if not self.source_revision.strip():
            raise ValueError("source_revision is required")

    @property
    def digest(self) -> str:
        payload = {
            "environment_digest": self.environment_digest,
            "generated_source_digest": self.generated_source_digest,
            "implementation_digest": self.implementation_digest,
            "numerical_digest": self.numerical.digest,
            "performance_digest": self.performance.digest,
            "request_digest": self.request_digest,
            "soak_digest": self.soak_digest,
            "source_revision": self.source_revision,
        }
        encoded = json.dumps(payload, sort_keys=True, separators=(",", ":")).encode()
        return hashlib.sha256(encoded).hexdigest()
