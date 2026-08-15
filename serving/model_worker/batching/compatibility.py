# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Final Python-owned tensor-batch compatibility keys."""

from __future__ import annotations

from dataclasses import dataclass

from serving.model_worker.protocol import ModelRequest


@dataclass(frozen=True, order=True, slots=True)
class TensorCompatibilityKey:
    deployment_id: str
    model_bundle_digest: str
    precision_class: str
    execution_class: str
    shape_bucket: str


def compatibility_key(request: ModelRequest) -> TensorCompatibilityKey:
    """Derive the final model-side compatibility key.

    Rust may group requests coarsely. The Python worker is the last authority
    before tensor construction, so model-specific shape/compile metadata belongs
    here rather than in the Rust runtime.
    """
    request.validate()
    shape_bucket = request.options.get("shape_bucket", "default")
    if not shape_bucket or len(shape_bucket) > 128:
        raise ValueError("shape bucket is invalid")
    return TensorCompatibilityKey(
        request.deployment_id,
        request.model_bundle_digest,
        request.precision_class,
        request.execution_class,
        shape_bucket,
    )
