# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Validated deterministic serving fixtures."""

from __future__ import annotations

from serving.contracts import InferenceRequest, InputDescriptor


def inference_request(
    request_id: str = "request-1",
    *,
    model_digest: str = "sha256:" + "a" * 64,
    deadline_unix_millis: int = 10_000,
) -> InferenceRequest:
    descriptor = InputDescriptor(
        "segment-1",
        "sha256:" + "b" * 64,
        "/buffers/input",
        4,
        1,
        deadline_unix_millis + 1_000,
    )
    value = InferenceRequest(
        request_id,
        model_digest,
        request_id.encode(),
        (descriptor,),
        (),
        4,
        4,
        deadline_unix_millis,
    )
    value.validate(1_000)
    return value
