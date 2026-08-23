# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Model-independent planner for structure-model preprocessing."""

from __future__ import annotations

import re

from preprocessing.contracts import (
    ArtifactRef,
    PipelinePlan,
    PlannedStage,
    StageInput,
    StageKind,
)
from preprocessing.contracts.validation import require_sha256

_PREFIX = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$")
_SHA256 = re.compile(r"^sha256:[0-9a-f]{64}$")


def plan_structure_pipeline(
    *,
    prefix: str,
    input_artifact: ArtifactRef,
    config_digest: str,
    reference_snapshot_digest: str,
    include_msa: bool = True,
    include_templates: bool = True,
    include_ligands: bool = True,
) -> PipelinePlan:
    if _PREFIX.fullmatch(prefix) is None:
        raise ValueError("preprocessing pipeline prefix must be a bounded safe identifier")
    require_sha256(config_digest, "config_digest")
    require_sha256(reference_snapshot_digest, "reference_snapshot_digest")
    if _SHA256.fullmatch(config_digest) is None:
        raise ValueError("config_digest must be canonical lowercase sha256 digest")
    if _SHA256.fullmatch(reference_snapshot_digest) is None:
        raise ValueError("reference_snapshot_digest must be canonical lowercase sha256 digest")
    for name, value in (
        ("include_msa", include_msa),
        ("include_templates", include_templates),
        ("include_ligands", include_ligands),
    ):
        if type(value) is not bool:
            raise ValueError(f"{name} must be a boolean")
    stages: list[PlannedStage] = []
    canonical = f"{prefix}:canonicalize"
    stages.append(
        PlannedStage(
            StageInput(
                canonical,
                StageKind.ENTITY_CANONICALIZE,
                (input_artifact,),
                f"{prefix}/canonical",
                config_digest,
                reference_snapshot_digest,
            )
        )
    )
    dependencies = [canonical]

    if include_msa:
        msa = f"{prefix}:msa"
        stages.append(
            PlannedStage(
                StageInput(
                    msa,
                    StageKind.MSA_SEARCH,
                    (),
                    f"{prefix}/msa",
                    config_digest,
                    reference_snapshot_digest,
                ),
                (canonical,),
            )
        )
        dependencies.append(msa)

    if include_templates:
        templates = f"{prefix}:templates"
        stages.append(
            PlannedStage(
                StageInput(
                    templates,
                    StageKind.TEMPLATE_SEARCH,
                    (),
                    f"{prefix}/templates",
                    config_digest,
                    reference_snapshot_digest,
                ),
                tuple(dependencies),
            )
        )
        dependencies.append(templates)

    if include_ligands:
        ligands = f"{prefix}:ligands"
        stages.append(
            PlannedStage(
                StageInput(
                    ligands,
                    StageKind.LIGAND_PREPARE,
                    (),
                    f"{prefix}/ligands",
                    config_digest,
                    reference_snapshot_digest,
                ),
                (canonical,),
            )
        )
        dependencies.append(ligands)

    features = f"{prefix}:features"
    stages.append(
        PlannedStage(
            StageInput(
                features,
                StageKind.FEATURIZE,
                (),
                f"{prefix}/features",
                config_digest,
                reference_snapshot_digest,
            ),
            tuple(sorted(set(dependencies))),
        )
    )
    plan = PipelinePlan(tuple(stages))
    plan.validate()
    return plan
