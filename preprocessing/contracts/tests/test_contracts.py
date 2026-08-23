# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Behavioral tests for the preprocessing contract types.

`preprocessing.contracts` is the layer every other preprocessing package keys off: cache keys,
plan validation and provenance manifests all quote the digests and stage ids minted here. So the
invariants worth pinning are the ones a caller cannot re-check downstream — construction-time
rejection of malformed values, and the exact framing of anything that becomes a cache address.

Several tests below are labelled CURRENT BEHAVIOUR. Those record a validator that is weaker than
its neighbour rather than the behaviour we want; they exist so the gap is visible and dated
instead of being rediscovered from a corrupt cache entry. Source under `preprocessing/` is out of
scope for this change, so the gaps are documented, not fixed.
"""

from __future__ import annotations

import dataclasses

import pytest

from preprocessing.contracts import (
    ArtifactRef,
    Entity,
    EntityType,
    FeatureBundle,
    PipelinePlan,
    PlannedStage,
    SearchPolicy,
    StageInput,
    StageKind,
    ToolResult,
)
from preprocessing.contracts.validation import require_sha256

_DIGEST = "sha256:" + "a" * 64
_OTHER_DIGEST = "sha256:" + "b" * 64


def _artifact() -> ArtifactRef:
    return ArtifactRef(_DIGEST, 128, "application/octet-stream", "msa", 1)


def _policy() -> SearchPolicy:
    return SearchPolicy("mmseqs2", "15.6f452", _DIGEST, _OTHER_DIGEST, 1000, 10000)


def _stage(stage_id: str, dependencies: tuple[str, ...] = ()) -> PlannedStage:
    return PlannedStage(
        StageInput(stage_id, StageKind.FEATURIZE, (), f"ns/{stage_id}", _DIGEST),
        dependencies,
    )


@pytest.mark.parametrize(
    "value",
    [
        "sha256:" + "0" * 64,
        "sha256:" + "f" * 64,
        "sha256:" + "0123456789abcdef" * 4,
    ],
)
def test_require_sha256_accepts_canonical_digests(value: str) -> None:
    # Returning at all is the observable behaviour: the function signals only by raising.
    require_sha256(value, "config_digest")


@pytest.mark.parametrize(
    "value",
    [
        "",
        "sha256:",  # algorithm named, nothing digested
        "a" * 64,  # bare hex, no algorithm — ambiguous once a second algorithm exists
        "sha256:" + "a" * 63,  # one nibble short
        "sha256:" + "a" * 65,  # one nibble long
        "sha-256:" + "a" * 64,
        "SHA256:" + "a" * 64,  # the prefix comparison is case sensitive
        " sha256:" + "a" * 64,  # leading whitespace is not stripped
        "sha256:" + "a" * 64 + " ",
    ],
)
def test_require_sha256_rejects_non_canonical_digests(value: str) -> None:
    # The field name has to reach the message: these digests arrive from six different contract
    # fields, and "must be canonical sha256 digest" alone does not say which one was wrong.
    with pytest.raises(ValueError, match="config_digest must be canonical sha256 digest"):
        require_sha256(value, "config_digest")


def test_require_sha256_does_not_check_the_digest_alphabet() -> None:
    """CURRENT BEHAVIOUR. The check is prefix-and-length only, so a 64-character non-hex body
    passes. A digest that is accepted here and rejected by a downstream hex decoder fails far
    from the caller that supplied it."""
    require_sha256("sha256:" + "z" * 64, "config_digest")
    require_sha256("sha256:" + "A" * 64, "config_digest")


@pytest.mark.parametrize(
    "entity_type",
    [EntityType.PROTEIN, EntityType.RNA, EntityType.DNA, EntityType.LIGAND],
)
def test_entity_accepts_every_declared_type_and_digests_it_canonically(entity_type: str) -> None:
    # Ties EntityType to Entity's accepted set: adding a constant without widening the guard (or
    # the reverse) shows up here rather than at the first unclassifiable input.
    entity = Entity(entity_type, "MKVLIA")
    assert entity.entity_type == entity_type
    require_sha256(entity.digest, "digest")


def test_entity_rejects_an_unsupported_type() -> None:
    with pytest.raises(ValueError, match="unsupported entity type"):
        Entity("peptide", "MKVLIA")


@pytest.mark.parametrize("canonical", ["", " ", "   ", "\n\t "])
def test_entity_rejects_a_blank_canonical_representation(canonical: str) -> None:
    # Whitespace is not a molecule. An all-blank canonical form would still hash to a stable
    # digest, so without this guard every empty input would share one cache entry.
    with pytest.raises(ValueError, match="canonical entity representation is empty"):
        Entity(EntityType.PROTEIN, canonical)


def test_entity_digest_pins_the_cache_key_framing() -> None:
    # Golden value, deliberately. Preprocessing artifacts are addressed by this digest, so the
    # framing — "<type>\0<canonical>", sha256, "sha256:" prefix — is a compatibility surface. A
    # refactor that drops the NUL separator or reorders the fields keeps every existing test
    # green while silently orphaning every cached artifact; this one goes red.
    assert (
        Entity(EntityType.PROTEIN, "MKVLIA").digest
        == "sha256:404ff25fde15f129646d8610323d88a192c93640b2932596b60fe17efefcbb55"
    )


def test_entity_digest_covers_both_fields() -> None:
    protein = Entity(EntityType.PROTEIN, "MKVLIA")
    assert protein.digest == Entity(EntityType.PROTEIN, "MKVLIA").digest
    assert protein.digest != Entity(EntityType.RNA, "MKVLIA").digest
    assert protein.digest != Entity(EntityType.PROTEIN, "MKVLIB").digest


def test_entity_is_hashable_by_value() -> None:
    # Frozen-ness is not decoration: these are used as dictionary keys while a plan is walked.
    # A mutable dataclass would be unhashable, so the round trip below is the immutability test.
    cache = {Entity(EntityType.PROTEIN, "MKVLIA"): "hit"}
    assert cache[Entity(EntityType.PROTEIN, "MKVLIA")] == "hit"
    assert Entity(EntityType.RNA, "MKVLIA") not in cache


@pytest.mark.parametrize(
    ("field", "value"),
    [
        ("digest", "a" * 64),  # no algorithm prefix
        ("digest", "md5:" + "a" * 32),  # wrong algorithm
        ("size_bytes", -1),  # a payload cannot have negative length
        ("media_type", "octet-stream"),  # not a type/subtype pair
        ("logical_kind", ""),  # unnamed role in the DAG
        ("schema_version", 0),  # schema versions start at 1
        ("schema_version", -3),
    ],
)
def test_artifact_ref_rejects_malformed_fields(field: str, value: object) -> None:
    with pytest.raises(ValueError, match="invalid artifact reference"):
        dataclasses.replace(_artifact(), **{field: value})


def test_artifact_ref_accepts_its_boundary_values() -> None:
    # An empty artifact is a real result (a search that found nothing), and 1 is the first legal
    # schema version, so neither boundary may be rejected by the guard above.
    ref = dataclasses.replace(_artifact(), size_bytes=0, schema_version=1)
    assert (ref.size_bytes, ref.schema_version) == (0, 1)


def test_artifact_ref_digest_validation_stops_at_the_prefix() -> None:
    """CURRENT BEHAVIOUR. `ArtifactRef` checks the "sha256:" prefix and nothing else, while
    `require_sha256` — which lives in this same package and is imported by nothing in it —
    would reject the same value. Two validators disagreeing about one field is how a truncated
    digest reaches a content-addressed store."""
    ref = dataclasses.replace(_artifact(), digest="sha256:")
    assert ref.digest == "sha256:"
    with pytest.raises(ValueError, match="digest must be canonical sha256 digest"):
        require_sha256(ref.digest, "digest")


def test_stage_kind_values_are_the_wire_strings() -> None:
    # `compile_plan` serializes the kind with `str()`, and the descriptor leaves Python. Renaming
    # a member is free; changing its value is a wire break, so the values are pinned here.
    assert {kind.name: str(kind) for kind in StageKind} == {
        "ENTITY_CANONICALIZE": "entity_canonicalize",
        "MSA_SEARCH": "msa_search",
        "TEMPLATE_SEARCH": "template_search",
        "MSA_PAIR": "msa_pair",
        "LIGAND_PREPARE": "ligand_prepare",
        "FEATURIZE": "featurize",
    }


def test_stage_input_carries_optional_provenance_and_validates_nothing() -> None:
    """CURRENT BEHAVIOUR, and the reason `PipelinePlan.validate` exists. `StageInput` has no
    `__post_init__`: an empty stage id and a junk config digest both construct. Nothing between
    the caller and the plan validator will object, so a plan that is never validated is not
    checked at all."""
    spec = StageInput("", StageKind.MSA_SEARCH, (), "", "not-a-digest")
    assert spec.reference_snapshot_digest is None
    assert spec.parameters is None
    assert spec.config_digest == "not-a-digest"


def test_plan_accepts_a_diamond_and_revalidates_cleanly() -> None:
    plan = PipelinePlan(
        (
            _stage("canonicalize"),
            _stage("msa", ("canonicalize",)),
            _stage("ligands", ("canonicalize",)),
            _stage("features", ("msa", "ligands")),
        )
    )
    plan.validate()
    # Called twice on purpose: the compiler validates a plan the planner already validated. The
    # traversal state is per call, and would have to stay that way for the second call to agree.
    plan.validate()


def test_plan_rejects_a_duplicate_stage_id() -> None:
    # Stage ids are the resume key. Two stages sharing one would make "already done" ambiguous.
    with pytest.raises(ValueError, match="duplicate preprocessing stage id"):
        PipelinePlan((_stage("msa"), _stage("msa"))).validate()


def test_plan_rejects_a_dependency_that_is_not_in_the_plan() -> None:
    with pytest.raises(ValueError, match="unknown stage dependency canonicalize"):
        PipelinePlan((_stage("msa", ("canonicalize",)),)).validate()


@pytest.mark.parametrize(
    "stages",
    [
        (_stage("a", ("a",)),),  # self dependency
        (_stage("a", ("b",)), _stage("b", ("a",))),  # two-node cycle
        # A cycle that excludes the entry point: traversal has to start from every node, not
        # only from the first, or this plan validates and then deadlocks the executor.
        (_stage("a", ("b",)), _stage("b", ("c",)), _stage("c", ("b",))),
    ],
)
def test_plan_rejects_cycles(stages: tuple[PlannedStage, ...]) -> None:
    with pytest.raises(ValueError, match="preprocessing pipeline contains a cycle"):
        PipelinePlan(stages).validate()


def test_empty_plan_is_accepted() -> None:
    """CURRENT BEHAVIOUR. An empty DAG is well formed by this validator's own rules, so "did I
    plan anything" is the caller's question, not the contract's."""
    PipelinePlan(()).validate()


def test_planned_stage_defaults_to_no_dependencies() -> None:
    # The default is an immutable empty tuple rather than a list, so a root stage in one plan
    # cannot acquire edges through a shared default held by another.
    assert _stage("root").dependencies == ()


@pytest.mark.parametrize(
    ("field", "value"),
    [
        ("tool", ""),  # an unattributable search result
        ("tool_version", ""),  # not reproducible against a specific binary
        ("maximum_hits", 0),
        ("maximum_hits", -1),
        ("maximum_sequences", 0),
        ("maximum_sequences", -5),
    ],
)
def test_search_policy_rejects_unbounded_or_unattributable_search(
    field: str, value: object
) -> None:
    # A non-positive maximum is an unbounded search — the repository rule that every queue,
    # buffer and spool carries a bound applies to an MSA hit list as much as to a channel.
    with pytest.raises(ValueError, match="invalid search policy"):
        dataclasses.replace(_policy(), **{field: value})


def test_search_policy_accepts_the_smallest_legal_bounds() -> None:
    policy = dataclasses.replace(_policy(), maximum_hits=1, maximum_sequences=1)
    assert (policy.maximum_hits, policy.maximum_sequences) == (1, 1)


def test_search_policy_does_not_validate_its_digests() -> None:
    """CURRENT BEHAVIOUR. The snapshot and parameter digests are the fields that make a search
    reproducible, and the guard covers everything except them."""
    policy = dataclasses.replace(
        _policy(), database_snapshot_digest="", parameters_digest="not-a-digest"
    )
    assert policy.database_snapshot_digest == ""


@pytest.mark.parametrize(
    ("exit_code", "succeeded"), [(0, True), (1, False), (127, False), (-9, False)]
)
def test_tool_result_success_is_exactly_a_zero_exit(exit_code: int, succeeded: bool) -> None:
    # -9 is a SIGKILL'd tool — an OOM kill during an MSA search looks like this, and it must not
    # read as success.
    result = ToolResult("mmseqs2", "15.6f452", _DIGEST, None, None, (), exit_code, 12)
    assert result.succeeded is succeeded


def test_tool_result_with_outputs_still_fails_on_a_nonzero_exit() -> None:
    # A crashed search often leaves a partial output file behind. Presence of outputs is not
    # evidence of success; the exit code is the only signal.
    result = ToolResult("mmseqs2", "15.6f452", _DIGEST, None, None, (_artifact(),), 1, 12)
    assert result.outputs == (_artifact(),)
    assert result.succeeded is False


def test_feature_bundle_optional_digest_groups_default_to_empty() -> None:
    # A bundle built without MSAs or templates is a legitimate result (a ligand-only job), and
    # it must be distinguishable from one that has them rather than defaulting to a shared list.
    bundle = FeatureBundle(_artifact(), "featurized/v1", "structure_model/v1", (_DIGEST,))
    assert (bundle.msa_digests, bundle.template_digests, bundle.reference_snapshot_digests) == (
        (),
        (),
        (),
    )
    assert bundle != dataclasses.replace(bundle, msa_digests=(_OTHER_DIGEST,))


def test_feature_bundle_is_hashable_by_value() -> None:
    bundle = FeatureBundle(_artifact(), "featurized/v1", "structure_model/v1", (_DIGEST,))
    twin = FeatureBundle(_artifact(), "featurized/v1", "structure_model/v1", (_DIGEST,))
    assert hash(bundle) == hash(twin)
    assert {bundle: "hit"}[twin] == "hit"
