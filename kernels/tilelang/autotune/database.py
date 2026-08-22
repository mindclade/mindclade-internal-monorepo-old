# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Deterministic machine-readable tuning result set."""

from __future__ import annotations

import json
from dataclasses import asdict, dataclass, field

from kernels.tilelang.autotune.validation import CandidateResult


@dataclass(slots=True)
class TuningResults:
    environment_digest: str
    source_digest: str
    results: dict[str, CandidateResult] = field(default_factory=dict)

    def add(self, result: CandidateResult) -> None:
        if result.candidate_digest in self.results:
            raise ValueError("candidate result already exists")
        self.results[result.candidate_digest] = result

    def to_json(self) -> str:
        payload = {
            "environment_digest": self.environment_digest,
            "results": [asdict(self.results[key]) for key in sorted(self.results)],
            "schema_version": 1,
            "source_digest": self.source_digest,
        }
        return json.dumps(payload, sort_keys=True, separators=(",", ":"))
