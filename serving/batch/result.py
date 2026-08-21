# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Exact-cardinality result contract for batch inference."""

from __future__ import annotations

from dataclasses import dataclass

from serving.contracts import InferenceResult


@dataclass(frozen=True, slots=True)
class BatchResult:
    job_id: str
    fencing_token: int
    results: tuple[InferenceResult, ...]
    batch_count: int

    def validate(self, expected_request_ids: tuple[str, ...]) -> None:
        if not self.job_id or self.fencing_token <= 0 or self.batch_count <= 0:
            raise ValueError("batch result identity is invalid")
        produced = tuple(result.request_id for result in self.results)
        if produced != expected_request_ids:
            raise ValueError("batch result order/cardinality does not match the job")
        for result in self.results:
            result.validate()
