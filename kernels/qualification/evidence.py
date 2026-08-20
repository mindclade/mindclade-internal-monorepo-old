# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

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
