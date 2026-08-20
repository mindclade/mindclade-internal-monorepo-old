# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Process-local projection of the canonical orchestration workload envelope."""

from __future__ import annotations

from dataclasses import dataclass

from libs.python.errors import InvalidArgument

from .contracts import MAXIMUM_UINT64, StageEnvelope, _bounded_positive_integer, _bounded_text
from .contracts import _resource_id as _validated_resource_id


@dataclass(frozen=True, slots=True)
class WorkloadEnvelope:
    workload_id: str
    run_id: str
    job_id: str
    tenant_id: str
    workspace_id: str
    execution_ticket_id: str
    stage: StageEnvelope
    resource_class: str
    created_unix_millis: int

    def __post_init__(self) -> None:
        self.validate()

    def validate(self) -> None:
        for field, value, kind in (
            ("workload_id", self.workload_id, "workload"),
            ("run_id", self.run_id, "run"),
            ("job_id", self.job_id, "job"),
            ("tenant_id", self.tenant_id, "tenant"),
            ("workspace_id", self.workspace_id, "workspace"),
            ("execution_ticket_id", self.execution_ticket_id, "ticket"),
        ):
            _validated_resource_id(value, name=field, kind=kind)
        if not isinstance(self.stage, StageEnvelope):
            raise InvalidArgument(
                "workload stage must be a StageEnvelope",
                reason="workload_stage",
            )
        _bounded_text(self.resource_class, name="resource_class")
        _bounded_positive_integer(
            self.created_unix_millis,
            name="created_unix_millis",
            maximum=MAXIMUM_UINT64,
        )
        self.stage.validate()
