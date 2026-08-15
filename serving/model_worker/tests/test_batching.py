# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from serving.model_worker import ModelRequest, WorkerLimits
from serving.model_worker.batching import BatchPlanner

D = "sha256:" + "1" * 64


def req(request_id: str, bucket: str = "a") -> ModelRequest:
    return ModelRequest(
        request_id, "deployment_a", D, "bf16", "gpu", 10, 20, "segment:a", {"shape_bucket": bucket}
    )


def test_planner_groups_only_final_tensor_compatible_requests() -> None:
    planner = BatchPlanner(WorkerLimits(maximum_batch_size=2))
    batches = planner.plan((req("r1"), req("r2"), req("r3", "b")))
    assert [len(batch.requests) for batch in batches] == [2, 1]
    assert batches[0].key.shape_bucket == "a"
    assert batches[1].key.shape_bucket == "b"
