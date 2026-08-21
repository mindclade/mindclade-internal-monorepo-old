# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Immutable batch job accepted after Rust ticket admission."""

from __future__ import annotations

from dataclasses import dataclass

from serving.contracts import InferenceRequest

from .config import BatchLimits


@dataclass(frozen=True, slots=True)
class BatchJob:
    job_id: str
    model_bundle_digest: str
    requests: tuple[InferenceRequest, ...]
    attempt: int
    fencing_token: int
    deadline_unix_millis: int

    def validate(self, limits: BatchLimits, *, now_unix_millis: int) -> None:
        if not self.job_id or len(self.job_id) > 256:
            raise ValueError("batch job id is invalid")
        if (
            not self.model_bundle_digest.startswith("sha256:")
            or len(self.model_bundle_digest) != 71
        ):
            raise ValueError("batch model bundle digest is invalid")
        if not self.requests or len(self.requests) > limits.maximum_requests_per_job:
            raise ValueError("batch job request count is outside bounds")
        if isinstance(self.attempt, bool) or not 1 <= self.attempt <= limits.maximum_attempts:
            raise ValueError("batch job attempt is outside bounds")
        if isinstance(self.fencing_token, bool) or self.fencing_token <= 0:
            raise ValueError("batch fencing token must be positive")
        if self.deadline_unix_millis <= now_unix_millis:
            raise ValueError("batch job deadline has expired")
        identifiers: set[str] = set()
        for request in self.requests:
            request.validate(now_unix_millis)
            if request.model_bundle_digest != self.model_bundle_digest:
                raise ValueError("batch job mixes model bundle digests")
            if request.request_id in identifiers:
                raise ValueError("batch job contains duplicate request ids")
            identifiers.add(request.request_id)
