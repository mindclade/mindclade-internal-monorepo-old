# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from serving.contracts import (
    BatchPlan,
    CompatibilityKey,
    InferenceRequest,
    InputDescriptor,
)


def test_batch_contract_keeps_tensor_compatibility_in_python() -> None:
    digest = "sha256:" + "a" * 64
    descriptor = InputDescriptor("segment", digest, "/tmp/segment", 4, 1, 10_000)
    request = InferenceRequest(
        "request-1",
        digest,
        b"key",
        (descriptor,),
        ("structure",),
        4,
        4,
        9_000,
    )
    request.validate(1_000)
    batch = BatchPlan(
        CompatibilityKey(digest, "structure", "bf16", "small"),
        (request,),
        1024,
        "small-bf16",
    )
    batch.validate()
