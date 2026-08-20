# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

import pytest

from libs.python.worker_runtime import StageEnvelope, StageKind, WorkloadEnvelope

DIGEST_TEXT = "sha256:" + "1" * 64


def resource_id(kind: str, suffix: int) -> str:
    return f"{kind}_019c00000000700080000000000000{suffix:02x}"


def stage() -> StageEnvelope:
    return StageEnvelope(
        stage_id=resource_id("stage", 1),
        kind=StageKind.PREPROCESS,
        operation="features",
        inputs=(),
        output_namespace="tenant/a",
        resolved_config_digest=DIGEST_TEXT,
        reference_snapshot_digest=None,
        attempt=1,
        fencing_token=7,
        deadline_unix_millis=5_000,
    )


def workload(*, run_id: str | None = None, resource_class: str = "cpu-highmem") -> WorkloadEnvelope:
    return WorkloadEnvelope(
        workload_id=resource_id("workload", 2),
        run_id=run_id or resource_id("run", 3),
        job_id=resource_id("job", 4),
        tenant_id=resource_id("tenant", 5),
        workspace_id=resource_id("workspace", 6),
        execution_ticket_id=resource_id("ticket", 7),
        stage=stage(),
        resource_class=resource_class,
        created_unix_millis=1_000,
    )


def test_workload_envelope_delegates_scientific_stage_validation() -> None:
    workload().validate()


def test_workload_enforces_the_kind_of_each_identifier() -> None:
    with pytest.raises(ValueError, match="resource kind"):
        workload(run_id=resource_id("job", 3))


@pytest.mark.parametrize("resource_class", ["", "x" * 129, "line\nbreak"])
def test_workload_resource_class_is_bounded_text(resource_class: str) -> None:
    with pytest.raises(ValueError, match="resource_class"):
        workload(resource_class=resource_class)
