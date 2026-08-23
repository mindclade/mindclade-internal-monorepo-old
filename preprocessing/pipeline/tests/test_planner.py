# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Structure of the plan `preprocessing.pipeline` builds, compiles and validates.

This file covers construction: which stages a set of toggles produces, how they are wired, and
what `compile_plan` and `validate_plan` guarantee about a plan handed to them. The consequences
for an interrupted run — replanning, frontier advance, stale completion state — are asserted in
`test_resume.py`, which is the same package from the other end.

`plan_structure_pipeline` is the only place that knows the scientific order of a structure
pipeline, and the compiled descriptor is what a non-Python executor consumes. Both are pinned
here field by field, because an edge that quietly disappears still produces a plan that
validates — it just runs featurization on inputs nobody searched for.
"""

from __future__ import annotations

import itertools

import pytest

from preprocessing.contracts import (
    ArtifactRef,
    PipelinePlan,
    PlannedStage,
    StageInput,
    StageKind,
)
from preprocessing.pipeline import compile_plan, plan_structure_pipeline, validate_plan

_INPUT = ArtifactRef("sha256:" + "a" * 64, 512, "chemical/x-fasta", "job_input", 1)
_CONFIG = "sha256:" + "b" * 64
_SNAPSHOT = "sha256:" + "c" * 64

_DESCRIPTOR_KEYS = {
    "stage_id",
    "kind",
    "dependencies",
    "output_namespace",
    "config_digest",
    "reference_snapshot_digest",
}


def _plan(
    *,
    prefix: str = "job",
    include_msa: bool = True,
    include_templates: bool = True,
    include_ligands: bool = True,
) -> PipelinePlan:
    return plan_structure_pipeline(
        prefix=prefix,
        input_artifact=_INPUT,
        config_digest=_CONFIG,
        reference_snapshot_digest=_SNAPSHOT,
        include_msa=include_msa,
        include_templates=include_templates,
        include_ligands=include_ligands,
    )


def _edges(plan: PipelinePlan) -> dict[str, tuple[str, ...]]:
    return {stage.spec.stage_id: stage.dependencies for stage in plan.stages}


def test_default_plan_is_the_canonical_structure_pipeline() -> None:
    plan = _plan()
    assert [(s.spec.stage_id, s.spec.kind) for s in plan.stages] == [
        ("job:canonicalize", StageKind.ENTITY_CANONICALIZE),
        ("job:msa", StageKind.MSA_SEARCH),
        ("job:templates", StageKind.TEMPLATE_SEARCH),
        ("job:ligands", StageKind.LIGAND_PREPARE),
        ("job:features", StageKind.FEATURIZE),
    ]


def test_default_plan_wires_exactly_these_edges() -> None:
    # The scientific claims in one assertion. Template search consumes the MSA profile, so it
    # waits for it; ligand preparation reads only the canonical entity, so it does not wait for
    # either search and can run alongside them; featurization needs everything.
    assert _edges(_plan()) == {
        "job:canonicalize": (),
        "job:msa": ("job:canonicalize",),
        "job:templates": ("job:canonicalize", "job:msa"),
        "job:ligands": ("job:canonicalize",),
        "job:features": ("job:canonicalize", "job:ligands", "job:msa", "job:templates"),
    }


@pytest.mark.parametrize(
    ("include_msa", "include_templates", "include_ligands"),
    list(itertools.product([True, False], repeat=3)),
)
def test_every_toggle_combination_yields_a_validated_topological_plan(
    include_msa: bool, include_templates: bool, include_ligands: bool
) -> None:
    # All eight shapes, because the toggles rewire edges rather than only removing nodes: with
    # `include_msa=False`, template search has to fall back to depending on canonicalization.
    plan = _plan(
        include_msa=include_msa,
        include_templates=include_templates,
        include_ligands=include_ligands,
    )
    plan.validate()

    order = [stage.spec.stage_id for stage in plan.stages]
    assert len(set(order)) == len(order)
    assert order[0] == "job:canonicalize"
    assert order[-1] == "job:features"

    # Position, not just reachability: the stage list is what an executor walks, so a
    # dependency appearing after its dependent would run work before its input exists.
    position = {stage_id: index for index, stage_id in enumerate(order)}
    for stage in plan.stages:
        for dependency in stage.dependencies:
            assert position[dependency] < position[stage.spec.stage_id]


@pytest.mark.parametrize(
    ("include_msa", "include_templates", "include_ligands", "expected"),
    [
        (True, True, True, ("job:canonicalize", "job:ligands", "job:msa", "job:templates")),
        (False, True, True, ("job:canonicalize", "job:ligands", "job:templates")),
        (True, False, True, ("job:canonicalize", "job:ligands", "job:msa")),
        (True, True, False, ("job:canonicalize", "job:msa", "job:templates")),
        (False, False, False, ("job:canonicalize",)),
    ],
)
def test_featurization_depends_on_every_enabled_stage_in_sorted_order(
    include_msa: bool, include_templates: bool, include_ligands: bool, expected: tuple[str, ...]
) -> None:
    # Sorted and de-duplicated, so the edge tuple is a function of the enabled set alone. If it
    # followed insertion order instead, two runs of the same job would compile to descriptors
    # that differ only in edge order — enough to miss a cache hit keyed on the descriptor.
    plan = _plan(
        include_msa=include_msa,
        include_templates=include_templates,
        include_ligands=include_ligands,
    )
    assert plan.stages[-1].dependencies == expected


@pytest.mark.parametrize(
    ("toggle", "absent"),
    [
        ("include_msa", "job:msa"),
        ("include_templates", "job:templates"),
        ("include_ligands", "job:ligands"),
    ],
)
def test_a_disabled_stage_leaves_no_node_and_no_edge_behind(toggle: str, absent: str) -> None:
    # A dangling edge would be caught by `validate`, but a *stale* one pointing at a stage that
    # a different toggle happens to re-add would not be. Assert the id is gone from both sides.
    edges = _edges(_plan(**{toggle: False}))
    assert absent not in edges
    assert all(absent not in dependencies for dependencies in edges.values())


def test_minimal_plan_is_canonicalize_then_featurize() -> None:
    # Canonicalization and featurization are not optional: with every search disabled the plan
    # is still two stages, not an empty one.
    plan = _plan(include_msa=False, include_templates=False, include_ligands=False)
    assert _edges(plan) == {
        "job:canonicalize": (),
        "job:features": ("job:canonicalize",),
    }


def test_prefix_namespaces_both_the_stage_ids_and_the_output_paths() -> None:
    plan = _plan(prefix="run-42")
    assert all(stage.spec.stage_id.startswith("run-42:") for stage in plan.stages)
    assert all(stage.spec.output_namespace.startswith("run-42/") for stage in plan.stages)


def test_distinct_prefixes_keep_two_jobs_mergeable_and_one_prefix_does_not() -> None:
    # The prefix is the whole isolation mechanism between concurrent jobs. Merged plans from two
    # prefixes validate; reusing a prefix collides on every stage id, which is why the duplicate
    # check in `PipelinePlan.validate` is the backstop rather than a formality.
    first = _plan(prefix="job-a")
    second = _plan(prefix="job-b")
    PipelinePlan(first.stages + second.stages).validate()

    with pytest.raises(ValueError, match="duplicate preprocessing stage id"):
        PipelinePlan(first.stages + _plan(prefix="job-a").stages).validate()


def test_compile_plan_emits_one_declarative_descriptor_per_stage() -> None:
    plan = _plan()
    descriptors = compile_plan(plan)

    assert len(descriptors) == len(plan.stages)
    assert [d["stage_id"] for d in descriptors] == [s.spec.stage_id for s in plan.stages]
    # Exactly these keys: the descriptor is the hand-off to a non-Python executor, so an added
    # field is a contract change and a dropped one is a silent loss of provenance.
    assert all(set(d) == _DESCRIPTOR_KEYS for d in descriptors)

    msa = descriptors[1]
    assert msa["dependencies"] == ("job:canonicalize",)
    assert msa["output_namespace"] == "job/msa"
    assert (msa["config_digest"], msa["reference_snapshot_digest"]) == (_CONFIG, _SNAPSHOT)
    # `str`, not `StageKind`: the descriptor has to survive serialization, and a bare enum
    # member would not round-trip through JSON as the value the executor dispatches on.
    assert msa["kind"] == "msa_search"
    assert type(msa["kind"]) is str


def test_compile_plan_revalidates_the_plan_it_is_handed() -> None:
    # The planner validates its own output, so this only matters for a plan assembled elsewhere
    # — a resumed or merged one. Compiling must not be the step that trusts the caller.
    broken = PipelinePlan(
        (
            PlannedStage(
                StageInput("job:features", StageKind.FEATURIZE, (), "job/features", _CONFIG),
                ("job:msa",),
            ),
        )
    )
    with pytest.raises(ValueError, match="unknown stage dependency job:msa"):
        compile_plan(broken)


def test_validate_plan_returns_the_same_plan_object() -> None:
    # A pass-through, not a copy: callers chain it, and a returned clone would break identity
    # comparisons and quietly double the memory of a large plan.
    plan = _plan()
    assert validate_plan(plan) is plan


def test_validate_plan_raises_on_an_invalid_plan() -> None:
    duplicated = PipelinePlan(_plan().stages + _plan().stages)
    with pytest.raises(ValueError, match="duplicate preprocessing stage id"):
        validate_plan(duplicated)


@pytest.mark.parametrize(
    "overrides",
    [
        {"prefix": ""},
        {"prefix": "../escape"},
        {"config_digest": "not-a-digest"},
        {"reference_snapshot_digest": ""},
        {"include_msa": "true"},
    ],
)
def test_planner_rejects_unsafe_identity_inputs(overrides: dict[str, object]) -> None:
    arguments: dict[str, object] = {
        "prefix": "job",
        "input_artifact": _INPUT,
        "config_digest": _CONFIG,
        "reference_snapshot_digest": _SNAPSHOT,
    }
    arguments.update(overrides)
    with pytest.raises(ValueError):
        plan_structure_pipeline(**arguments)  # type: ignore[arg-type]
