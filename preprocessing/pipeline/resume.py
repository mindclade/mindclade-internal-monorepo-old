# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Deterministic, fail-closed preprocessing resume decisions."""

from __future__ import annotations

import hashlib
import json
import re
from collections.abc import Sequence
from dataclasses import dataclass

from preprocessing.contracts import ArtifactRef, PipelinePlan, PlannedStage

_SHA256 = re.compile(r"^sha256:[0-9a-f]{64}$")
_MAXIMUM_STAGES = 4096
_MAXIMUM_OUTPUTS_PER_STAGE = 64


def _artifact_identity(artifact: ArtifactRef) -> dict[str, object]:
    return {
        "digest": artifact.digest,
        "logical_kind": artifact.logical_kind,
        "media_type": artifact.media_type,
        "schema_version": artifact.schema_version,
        "size_bytes": artifact.size_bytes,
    }


def _stage_identity(stage: PlannedStage) -> dict[str, object]:
    parameters = stage.spec.parameters or {}
    return {
        "config_digest": stage.spec.config_digest,
        "dependencies": list(stage.dependencies),
        "inputs": [_artifact_identity(artifact) for artifact in stage.spec.inputs],
        "kind": str(stage.spec.kind),
        "output_namespace": stage.spec.output_namespace,
        "parameters": {key: parameters[key] for key in sorted(parameters)},
        "reference_snapshot_digest": stage.spec.reference_snapshot_digest,
        "stage_id": stage.spec.stage_id,
    }


def stage_descriptor_digest(stage: PlannedStage) -> str:
    """Return the exact digest a completion must bind before it can be reused."""

    encoded = json.dumps(
        _stage_identity(stage),
        ensure_ascii=False,
        separators=(",", ":"),
        sort_keys=True,
    ).encode("utf-8")
    return "sha256:" + hashlib.sha256(encoded).hexdigest()


@dataclass(frozen=True)
class StageCompletion:
    """Immutable completion evidence retained beside durable workflow state."""

    stage_id: str
    descriptor_digest: str
    output_artifacts: tuple[ArtifactRef, ...]

    def __post_init__(self) -> None:
        if not self.stage_id:
            raise ValueError("completed stage id is required")
        if _SHA256.fullmatch(self.descriptor_digest) is None:
            raise ValueError("completed stage descriptor digest must be canonical sha256")
        if not self.output_artifacts:
            raise ValueError("completed stage must retain at least one output artifact")
        if len(self.output_artifacts) > _MAXIMUM_OUTPUTS_PER_STAGE:
            raise ValueError("completed stage output artifact bound exceeded")
        digests = [artifact.digest for artifact in self.output_artifacts]
        if any(_SHA256.fullmatch(digest) is None for digest in digests):
            raise ValueError("completed stage output artifact digest must be canonical sha256")
        if len(set(digests)) != len(digests):
            raise ValueError("completed stage output artifact digests must be unique")


@dataclass(frozen=True)
class ResumePlan:
    """Validated completion set and the next dependency-ready frontier."""

    completed_stage_ids: tuple[str, ...]
    runnable: tuple[PlannedStage, ...]
    finished: bool


def plan_resume(plan: PipelinePlan, completions: Sequence[StageCompletion]) -> ResumePlan:
    """Validate completed work against ``plan`` and return only safe next stages."""

    plan.validate()
    if len(plan.stages) > _MAXIMUM_STAGES:
        raise ValueError("preprocessing resume stage bound exceeded")
    if len(completions) > len(plan.stages):
        raise ValueError("preprocessing completion count exceeds plan size")

    stages = {stage.spec.stage_id: stage for stage in plan.stages}
    completed: dict[str, StageCompletion] = {}
    for completion in completions:
        if completion.stage_id in completed:
            raise ValueError(f"duplicate completed preprocessing stage {completion.stage_id}")
        stage = stages.get(completion.stage_id)
        if stage is None:
            raise ValueError(
                f"completed preprocessing stage is absent from plan: {completion.stage_id}"
            )
        if completion.descriptor_digest != stage_descriptor_digest(stage):
            raise ValueError(
                f"completed preprocessing stage descriptor changed: {completion.stage_id}"
            )
        completed[completion.stage_id] = completion

    completed_ids = set(completed)
    for stage_id in completed_ids:
        missing = set(stages[stage_id].dependencies) - completed_ids
        if missing:
            raise ValueError(
                f"completed preprocessing stage {stage_id} is missing completed dependencies: "
                + ", ".join(sorted(missing))
            )

    runnable = tuple(
        stage
        for stage in plan.stages
        if stage.spec.stage_id not in completed_ids and set(stage.dependencies) <= completed_ids
    )
    finished = len(completed_ids) == len(plan.stages)
    if not finished and not runnable:
        raise ValueError("preprocessing resume has unfinished work but no runnable frontier")
    return ResumePlan(
        completed_stage_ids=tuple(
            stage.spec.stage_id for stage in plan.stages if stage.spec.stage_id in completed_ids
        ),
        runnable=runnable,
        finished=finished,
    )


__all__ = ["ResumePlan", "StageCompletion", "plan_resume", "stage_descriptor_digest"]
