# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Resume semantics of a preprocessing plan.

`preprocessing/pipeline/resume.py` is still a scaffold boundary — it exports a path constant and
no behavior. What already exists, and what any resumer will be built on, is the plan: an
interrupted run is resumed by replanning from the same arguments and matching completed work
against the reconstructed stage ids. That makes four properties load-bearing today, and every
one of them is a property of code that is implemented:

  * replanning is a pure function of its arguments, or completed work cannot be matched at all;
  * a stage id is stable under a configuration change, so identity alone is *not* sufficient
    evidence that finished work is still valid;
  * a plan may not be pruned to "what is left" — the validator requires the whole DAG;
  * the DAG admits an incremental frontier, so a resumed run does strictly the remaining work.

`test_planner.py` covers how the plan is constructed; this file covers what survives an
interruption. The last test here is a tripwire on the scaffold itself.
"""

from __future__ import annotations

import pytest

from preprocessing import pipeline
from preprocessing.contracts import ArtifactRef, PipelinePlan
from preprocessing.pipeline import compile_plan, plan_structure_pipeline, resume

_INPUT = ArtifactRef("sha256:" + "a" * 64, 512, "chemical/x-fasta", "job_input", 1)
_CONFIG = "sha256:" + "b" * 64
_SNAPSHOT = "sha256:" + "c" * 64


def _plan(
    *,
    config_digest: str = _CONFIG,
    reference_snapshot_digest: str = _SNAPSHOT,
    include_msa: bool = True,
) -> PipelinePlan:
    return plan_structure_pipeline(
        prefix="job",
        input_artifact=_INPUT,
        config_digest=config_digest,
        reference_snapshot_digest=reference_snapshot_digest,
        include_msa=include_msa,
    )


def _runnable(plan: PipelinePlan, completed: frozenset[str]) -> frozenset[str]:
    """Stages whose dependencies are all satisfied and which are not already done.

    Deliberately trivial. What is under test is the plan's shape — that this rule terminates,
    covers every stage exactly once, and never offers a stage before its inputs exist.
    """
    return frozenset(
        stage.spec.stage_id
        for stage in plan.stages
        if stage.spec.stage_id not in completed and set(stage.dependencies) <= completed
    )


def test_replanning_reconstructs_an_identical_plan() -> None:
    # The whole resume contract rests on this. The planner holds no counter, clock or random
    # source, so a second call with the same arguments must be equal by value — otherwise the
    # ids recorded by the interrupted run name stages that the resumed run does not have.
    first = _plan()
    second = _plan()
    assert first == second
    assert compile_plan(first) == compile_plan(second)


def test_stage_ids_survive_a_configuration_change_but_descriptors_do_not() -> None:
    # The hazard this pins down: stage ids are derived from the prefix alone, so a rerun with a
    # new config digest or a newer reference database produces *the same ids*. A resumer that
    # treats a matching id as "already done" would hand the model features computed against the
    # previous configuration. The discriminator is in the descriptor, not the id.
    baseline = _plan()
    reconfigured = _plan(config_digest="sha256:" + "d" * 64)
    resnapshotted = _plan(reference_snapshot_digest="sha256:" + "e" * 64)

    ids = [stage.spec.stage_id for stage in baseline.stages]
    assert [stage.spec.stage_id for stage in reconfigured.stages] == ids
    assert [stage.spec.stage_id for stage in resnapshotted.stages] == ids

    assert compile_plan(baseline) != compile_plan(reconfigured)
    assert compile_plan(baseline) != compile_plan(resnapshotted)
    assert {d["config_digest"] for d in compile_plan(reconfigured)} == {"sha256:" + "d" * 64}
    assert {d["reference_snapshot_digest"] for d in compile_plan(resnapshotted)} == {
        "sha256:" + "e" * 64
    }


def test_a_plan_pruned_to_the_remaining_work_no_longer_validates() -> None:
    # The obvious way to write a resumer — drop the finished stages and run what is left — is
    # rejected by the contract, because the surviving stages still name their dependencies and
    # `PipelinePlan.validate` requires every named stage to be present. Resume therefore carries
    # the whole plan and tracks completion beside it. This is the failure that says so.
    plan = _plan()
    completed = "job:canonicalize"
    pruned = PipelinePlan(tuple(s for s in plan.stages if s.spec.stage_id != completed))

    # Anchored, so this is message equality rather than a substring match: the dangling id has
    # to be named, or the operator is told a plan is broken without being told where.
    with pytest.raises(ValueError, match=rf"^unknown stage dependency {completed}$"):
        pruned.validate()


def test_the_resume_frontier_advances_in_dependency_layers() -> None:
    # Walked from empty, the frontier reproduces the parallelism the planner encoded: the two
    # searches that share only canonicalization are offered together, template search waits for
    # the MSA, and featurization is last. A resumed run starting from any of these completion
    # sets does exactly the remaining work.
    plan = _plan()
    completed: frozenset[str] = frozenset()
    layers = []

    while True:
        frontier = _runnable(plan, completed)
        if not frontier:
            break
        layers.append(frontier)
        completed |= frontier

    assert layers == [
        frozenset({"job:canonicalize"}),
        frozenset({"job:msa", "job:ligands"}),
        frozenset({"job:templates"}),
        frozenset({"job:features"}),
    ]
    # Terminates having scheduled every stage exactly once: nothing stranded, nothing repeated.
    assert sum(len(layer) for layer in layers) == len(plan.stages)
    assert completed == {stage.spec.stage_id for stage in plan.stages}


def test_resuming_from_a_partially_completed_run_offers_only_what_is_left() -> None:
    # The frontier is a function of the completion set, so a run that died after the searches
    # resumes at the stage that needed them and never re-offers the finished ones.
    plan = _plan()
    completed = frozenset({"job:canonicalize", "job:msa", "job:ligands"})

    assert _runnable(plan, completed) == {"job:templates"}
    assert _runnable(plan, completed | {"job:templates"}) == {"job:features"}
    assert _runnable(plan, completed | {"job:templates", "job:features"}) == frozenset()


def test_completion_state_from_a_differently_toggled_run_is_stale_not_reusable() -> None:
    # Resuming with different toggles is not resuming the same pipeline. `job:msa` is completed
    # state that the new plan has no stage for, and — the sharper half — `job:templates` keeps
    # its id while losing its dependency on the MSA, so it is a different computation under the
    # same name. Neither is detectable from the id, which is why a resumer has to compare the
    # compiled descriptor before honouring completed work.
    completed = {stage.spec.stage_id for stage in _plan().stages}
    without_msa = _plan(include_msa=False)
    ids = {stage.spec.stage_id for stage in without_msa.stages}

    assert completed - ids == {"job:msa"}
    assert "job:templates" in ids
    edges = {s.spec.stage_id: s.dependencies for s in without_msa.stages}
    assert edges["job:templates"] == ("job:canonicalize",)
    assert _runnable(without_msa, frozenset(completed)) == frozenset()


def test_resume_module_is_still_a_scaffold_boundary() -> None:
    # A tripwire, not an endorsement. `resume.py` declares a scaffold path and exports no
    # behavior, and `preprocessing.pipeline` publishes no resume entry point — the properties
    # above are all this package can promise today. The day resume is implemented this test
    # fails, which is exactly when the tests above have to be joined by real coverage of it.
    assert resume.SCAFFOLD_PATH == "preprocessing/pipeline/resume.py"
    exported = [
        name
        for name in vars(resume)
        if not name.startswith("_") and callable(getattr(resume, name))
    ]
    assert exported == []
    assert [name for name in pipeline.__all__ if "resume" in name] == []
