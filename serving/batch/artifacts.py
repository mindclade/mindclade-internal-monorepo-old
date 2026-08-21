# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Canonical lineage manifest for a completed batch job."""

from __future__ import annotations

import hashlib
import json
from dataclasses import dataclass

from .job import BatchJob
from .result import BatchResult


@dataclass(frozen=True, slots=True)
class ResultManifest:
    document: bytes
    digest: str


def build_manifest(job: BatchJob, result: BatchResult) -> ResultManifest:
    result.validate(tuple(request.request_id for request in job.requests))
    value = {
        "schema_version": 1,
        "job_id": job.job_id,
        "model_bundle_digest": job.model_bundle_digest,
        "attempt": job.attempt,
        "fencing_token": job.fencing_token,
        "outputs": [
            {
                "request_id": item.request_id,
                "digest": item.output_digest,
                "size_bytes": item.output_bytes,
            }
            for item in result.results
        ],
    }
    document = json.dumps(value, sort_keys=True, separators=(",", ":")).encode("utf-8")
    return ResultManifest(document, "sha256:" + hashlib.sha256(document).hexdigest())
