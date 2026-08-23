# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

from collections.abc import Callable
from pathlib import Path

import pytest

from ci.common import full_graph_shards


def _sample(index: int) -> full_graph_shards.EvidenceSample:
    workflow_run_id = 100 + index
    return full_graph_shards.EvidenceSample(
        artifact_id=10 + index,
        artifact_name=f"bazel-performance-{workflow_run_id}-1",
        artifact_digest=f"sha256:{index:064x}",
        artifact_size_bytes=1000 + index,
        workflow_run_id=workflow_run_id,
        workflow_event="pull_request",
        workflow_status="completed",
        workflow_conclusion="success",
        run_attempt=1,
        head_sha=f"{index:040x}",
        bazel_job_id=1000 + index,
        bazel_job_name="bazel / verdict",
        bazel_job_status="completed",
        bazel_job_conclusion="success",
        test_total=1,
        test_passed=1,
        test_failed=0,
        qualification_path_ms=100,
        test_wall_ms=80,
        test_bep_critical_path_ms=20,
        test_bep_action_cache_misses=10,
        failed_test_labels=(),
    )


def _contract(**overrides: object) -> full_graph_shards.ShardContract:
    values: dict[str, object] = {
        "schema_version": 2,
        "shard_count": 4,
        "default_test_duration_ms": 1000,
        "evidence": full_graph_shards.Evidence(
            generated_at="2026-08-22T12:00:00Z",
            samples=tuple(_sample(index) for index in range(3)),
            qualification_path_median_ms=100,
            test_wall_median_ms=80,
            test_bep_critical_path_median_ms=20,
            test_bep_action_cache_miss_median=10,
        ),
        "test_weights": (full_graph_shards.TestWeight("//tests:slow", 8000, 7000, 9000, 3),),
        "source_sha256": "a" * 64,
    }
    values.update(overrides)
    return full_graph_shards.ShardContract(**values)  # type: ignore[arg-type]


def test_committed_contract_is_strict_and_evidence_backed() -> None:
    contract = full_graph_shards.load_contract()
    assert contract.schema_version == 2
    assert contract.shard_count == 4
    assert contract.default_test_duration_ms == 5000
    assert len(contract.evidence.samples) == 5
    assert contract.evidence.successful_sample_count == 1
    assert [sample.bazel_job_conclusion for sample in contract.evidence.samples] == [
        "failure",
        "failure",
        "success",
        "failure",
        "failure",
    ]
    assert contract.evidence.samples[0].artifact_digest == (
        "sha256:86bf431a81f455d3745a0dc9f8c64a02dcfbea08b5368b21555c6d0ec6ff8d8e"
    )
    assert contract.evidence.samples[2].head_sha == "732bb8ffb32b086a31261baf00be398fa9df5fa9"
    assert len(contract.test_weights) == 48
    assert contract.test_weights[0].label == "//data/loaders/packing/tests:test_packing"
    assert contract.test_weights[-1].label == "//training/tasks/tests:test_tasks"
    assert "//services/workers/training/tests:test_reference_affine" not in contract.durations()


def test_partition_is_deterministic_disjoint_complete_and_balanced() -> None:
    analysis = tuple(f"//pkg{index}:library" for index in range(17))
    tests = ("//tests:slow", *(f"//tests:test_{index}" for index in range(24)))
    graph = full_graph_shards.plan(_contract(), analysis, tests)
    repeated = full_graph_shards.plan(
        _contract(), tuple(reversed(analysis)), tuple(reversed(tests))
    )
    assert graph == repeated
    assert set().union(*(set(shard.analysis_targets) for shard in graph.shards)) == set(analysis)
    assert set().union(*(set(shard.test_targets) for shard in graph.shards)) == set(tests)
    assert sum(len(shard.analysis_targets) for shard in graph.shards) == len(analysis)
    assert sum(len(shard.test_targets) for shard in graph.shards) == len(tests)
    assert (
        max(len(shard.analysis_targets) for shard in graph.shards)
        - min(len(shard.analysis_targets) for shard in graph.shards)
        <= 1
    )
    assert (
        max(shard.estimated_test_duration_ms for shard in graph.shards)
        - min(shard.estimated_test_duration_ms for shard in graph.shards)
        <= 1000
    )


def test_stale_observed_test_weight_fails_closed() -> None:
    with pytest.raises(full_graph_shards.ShardContractError, match="no longer resolve"):
        full_graph_shards.plan(_contract(), ("//pkg:library",), ("//tests:other",))


def test_query_contract_excludes_manual_rules_and_separates_tests() -> None:
    expressions: list[str] = []

    def query(expression: str) -> tuple[str, ...]:
        expressions.append(expression)
        return ("//pkg:library",) if "kind" in expression else ("//tests:slow",)

    graph = full_graph_shards.plan_from_bazel(_contract(), query=query)
    assert expressions == [full_graph_shards.ANALYSIS_QUERY, full_graph_shards.TEST_QUERY]
    assert graph.analysis_targets == ("//pkg:library",)
    assert graph.test_targets == ("//tests:slow",)
    assert full_graph_shards.MANUAL_TAG_PATTERN == "manual"
    assert all(
        'attr("tags", "manual"' in expression
        for expression in (full_graph_shards.ANALYSIS_QUERY, full_graph_shards.TEST_QUERY)
    )
    assert "except tests($universe)" in full_graph_shards.ANALYSIS_QUERY


def test_selection_carries_auditable_partition_manifest() -> None:
    graph = full_graph_shards.plan(
        _contract(),
        tuple(f"//pkg{index}:library" for index in range(8)),
        ("//tests:slow", "//tests:fast_a", "//tests:fast_b", "//tests:fast_c"),
    )
    selection = full_graph_shards.selection_for_shard(
        graph, 0, event="merge_group", head_sha="1" * 40
    )
    assert selection.reason == "complete_partition:1_of_4"
    assert selection.partition is not None
    assert selection.partition["shard_count"] == 4
    assert selection.partition["selected_shard"]["index"] == 0
    assert selection.as_dict()["partition"] == selection.partition


def test_contract_rejects_unknown_and_unsorted_entries(tmp_path: Path) -> None:
    source = full_graph_shards.DEFAULT_CONTRACT.read_text(encoding="utf-8")
    unknown = tmp_path / "unknown.toml"
    unknown.write_text(source.replace("schema_version = 2", "schema_version = 2\nunknown = true"))
    with pytest.raises(full_graph_shards.ShardContractError, match="unknown"):
        full_graph_shards.load_contract(unknown)

    blocks = source.split("[[test_weight]]")
    assert len(blocks) > 3
    unsorted = tmp_path / "unsorted.toml"
    unsorted.write_text(
        blocks[0]
        + "".join("[[test_weight]]" + block for block in (blocks[2], blocks[1], *blocks[3:])),
        encoding="utf-8",
    )
    with pytest.raises(full_graph_shards.ShardContractError, match="sorted"):
        full_graph_shards.load_contract(unsorted)


@pytest.mark.parametrize(
    ("mutate", "message"),
    [
        (
            lambda source: source.replace(
                "sha256:86bf431a81f455d3745a0dc9f8c64a02dcfbea08b5368b21555c6d0ec6ff8d8e",
                "sha256:not-a-digest",
                1,
            ),
            "artifact_digest",
        ),
        (
            lambda source: source.replace(
                'head_sha = "1adeb702dafa5b13cd429231ccc93b0a2dee6716"',
                'head_sha = "ABC"',
                1,
            ),
            "head_sha",
        ),
        (
            lambda source: source.replace(
                'workflow_conclusion = "failure"',
                'workflow_conclusion = "success"',
                1,
            ),
            "conclusions must match",
        ),
        (
            lambda source: source.replace("test_failed = 3", "test_failed = 2", 1),
            "outcome counts",
        ),
        (
            lambda source: source.replace(
                "qualification_path_median_ms = 4220338",
                "qualification_path_median_ms = 4220339",
                1,
            ),
            "retained sample median",
        ),
    ],
)
def test_contract_rejects_unaligned_evidence_metadata(
    tmp_path: Path, mutate: Callable[[str], str], message: str
) -> None:
    source = full_graph_shards.DEFAULT_CONTRACT.read_text(encoding="utf-8")
    path = tmp_path / "invalid-evidence.toml"
    path.write_text(mutate(source), encoding="utf-8")
    with pytest.raises(full_graph_shards.ShardContractError, match=message):
        full_graph_shards.load_contract(path)


def test_contract_rejects_failed_or_partial_weight_samples(tmp_path: Path) -> None:
    source = full_graph_shards.DEFAULT_CONTRACT.read_text(encoding="utf-8")
    failed_weight = """
[[test_weight]]
label = "//services/workers/training/tests:test_reference_affine"
median_duration_ms = 16189
minimum_duration_ms = 11459
maximum_duration_ms = 36191
observations = 5

"""
    anchor = '[[test_weight]]\nlabel = "//services/workers/training/tests:test_smoke"'
    failed = tmp_path / "failed-weight.toml"
    failed.write_text(source.replace(anchor, failed_weight + anchor), encoding="utf-8")
    with pytest.raises(full_graph_shards.ShardContractError, match="failed retained"):
        full_graph_shards.load_contract(failed)

    partial = tmp_path / "partial-weight.toml"
    partial.write_text(source.replace("observations = 5", "observations = 4", 1), encoding="utf-8")
    with pytest.raises(full_graph_shards.ShardContractError, match="sample count"):
        full_graph_shards.load_contract(partial)


def test_contract_loader_rejects_symlinked_or_relative_authority(tmp_path: Path) -> None:
    source = tmp_path / "source.toml"
    source.write_bytes(full_graph_shards.DEFAULT_CONTRACT.read_bytes())
    alias = tmp_path / "alias.toml"
    alias.symlink_to(source)
    with pytest.raises(full_graph_shards.ShardContractError, match="cannot read"):
        full_graph_shards.load_contract(alias)
    with pytest.raises(full_graph_shards.ShardContractError, match="cannot read"):
        full_graph_shards.load_contract(Path("relative.toml"))


def test_overlap_and_invalid_shard_index_fail_closed() -> None:
    with pytest.raises(full_graph_shards.ShardContractError, match="overlap"):
        full_graph_shards.plan(_contract(test_weights=()), ("//pkg:same",), ("//pkg:same",))
    graph = full_graph_shards.plan(_contract(), ("//pkg:library",), ("//tests:slow",))
    with pytest.raises(full_graph_shards.ShardContractError, match="outside"):
        graph.shard(4)
