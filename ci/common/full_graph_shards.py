# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Deterministic partitions of the complete non-manual Bazel repository graph."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import stat
import sys
import tomllib
from collections.abc import Callable, Sequence
from dataclasses import dataclass
from datetime import datetime
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[2]
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))

from ci.common import affected  # noqa: E402

SCHEMA_VERSION = 2
DEFAULT_CONTRACT = ROOT / "ci/bazel/full_graph_shards.toml"
MANUAL_TAG_PATTERN = "manual"
ANALYSIS_QUERY = (
    "let universe = //... in "
    f'let manual = attr("tags", {json.dumps(MANUAL_TAG_PATTERN)}, $universe) in '
    '(kind(".* rule", $universe) except $manual except tests($universe))'
)
TEST_QUERY = (
    "let universe = //... in "
    f'let manual = attr("tags", {json.dumps(MANUAL_TAG_PATTERN)}, $universe) in '
    "(tests($universe) except $manual)"
)


class ShardContractError(RuntimeError):
    """The shard contract or computed partition is not authoritative."""


@dataclass(frozen=True)
class TestWeight:
    label: str
    median_duration_ms: int
    minimum_duration_ms: int
    maximum_duration_ms: int
    observations: int


@dataclass(frozen=True)
class EvidenceSample:
    artifact_id: int
    artifact_name: str
    artifact_digest: str
    artifact_size_bytes: int
    workflow_run_id: int
    workflow_event: str
    workflow_status: str
    workflow_conclusion: str
    run_attempt: int
    head_sha: str
    bazel_job_id: int
    bazel_job_name: str
    bazel_job_status: str
    bazel_job_conclusion: str
    test_total: int
    test_passed: int
    test_failed: int
    qualification_path_ms: int
    test_wall_ms: int
    test_bep_critical_path_ms: int
    test_bep_action_cache_misses: int
    failed_test_labels: tuple[str, ...]


@dataclass(frozen=True)
class Evidence:
    generated_at: str
    samples: tuple[EvidenceSample, ...]
    qualification_path_median_ms: int
    test_wall_median_ms: int
    test_bep_critical_path_median_ms: int
    test_bep_action_cache_miss_median: int

    @property
    def successful_sample_count(self) -> int:
        return sum(sample.bazel_job_conclusion == "success" for sample in self.samples)


@dataclass(frozen=True)
class ShardContract:
    schema_version: int
    shard_count: int
    default_test_duration_ms: int
    evidence: Evidence
    test_weights: tuple[TestWeight, ...]
    source_sha256: str

    def durations(self) -> dict[str, int]:
        return {weight.label: weight.median_duration_ms for weight in self.test_weights}


@dataclass(frozen=True)
class Shard:
    index: int
    analysis_targets: tuple[str, ...]
    test_targets: tuple[str, ...]
    estimated_test_duration_ms: int

    def as_manifest(self) -> dict[str, int | str]:
        return {
            "index": self.index,
            "analysis_target_count": len(self.analysis_targets),
            "analysis_targets_sha256": _target_digest(self.analysis_targets),
            "test_target_count": len(self.test_targets),
            "test_targets_sha256": _target_digest(self.test_targets),
            "estimated_test_duration_ms": self.estimated_test_duration_ms,
        }


@dataclass(frozen=True)
class FullGraphPlan:
    contract_sha256: str
    analysis_targets: tuple[str, ...]
    test_targets: tuple[str, ...]
    weighted_test_target_count: int
    shards: tuple[Shard, ...]

    def __post_init__(self) -> None:
        if len(self.shards) < 2:
            raise ShardContractError("a full-graph plan requires at least two shards")
        if tuple(shard.index for shard in self.shards) != tuple(range(len(self.shards))):
            raise ShardContractError("full-graph shard indexes are not contiguous")
        analysis_parts = tuple(target for shard in self.shards for target in shard.analysis_targets)
        test_parts = tuple(target for shard in self.shards for target in shard.test_targets)
        if len(analysis_parts) != len(set(analysis_parts)):
            raise ShardContractError("analysis partitions overlap")
        if len(test_parts) != len(set(test_parts)):
            raise ShardContractError("test partitions overlap")
        if set(analysis_parts) != set(self.analysis_targets):
            raise ShardContractError("analysis partitions do not cover the complete graph")
        if set(test_parts) != set(self.test_targets):
            raise ShardContractError("test partitions do not cover the complete graph")
        if set(self.analysis_targets) & set(self.test_targets):
            raise ShardContractError("analysis and test universes overlap")

    def manifest(self, *, selected_shard: int | None = None) -> dict[str, Any]:
        payload: dict[str, Any] = {
            "schema_version": SCHEMA_VERSION,
            "contract_sha256": self.contract_sha256,
            "shard_count": len(self.shards),
            "analysis_query": ANALYSIS_QUERY,
            "analysis_target_count": len(self.analysis_targets),
            "analysis_graph_sha256": _target_digest(self.analysis_targets),
            "test_query": TEST_QUERY,
            "test_target_count": len(self.test_targets),
            "test_graph_sha256": _target_digest(self.test_targets),
            "weighted_test_target_count": self.weighted_test_target_count,
            "shards": [shard.as_manifest() for shard in self.shards],
        }
        if selected_shard is not None:
            payload["selected_shard"] = self.shard(selected_shard).as_manifest()
        return payload

    def shard(self, index: int) -> Shard:
        if index < 0 or index >= len(self.shards):
            raise ShardContractError(f"shard index {index} is outside [0, {len(self.shards) - 1}]")
        return self.shards[index]


Query = Callable[[str], Sequence[str]]


def _only_fields(payload: dict[str, Any], allowed: set[str], *, label: str) -> None:
    unknown = set(payload) - allowed
    missing = allowed - set(payload)
    if unknown:
        raise ShardContractError(f"{label} has unknown fields: {sorted(unknown)}")
    if missing:
        raise ShardContractError(f"{label} is missing fields: {sorted(missing)}")


def _positive_integer(value: object, *, label: str, minimum: int = 1) -> int:
    if type(value) is not int or value < minimum:
        raise ShardContractError(f"{label} must be an integer >= {minimum}")
    return value


def _canonical_label(value: object, *, label: str) -> str:
    if (
        not isinstance(value, str)
        or not value.startswith("//")
        or any(character.isspace() for character in value)
    ):
        raise ShardContractError(f"{label} must be a canonical absolute Bazel label")
    if value in {"//", "//..."}:
        raise ShardContractError(f"{label} must name one repository target")
    return value


def _canonical_label_list(value: object, *, label: str) -> tuple[str, ...]:
    if not isinstance(value, list):
        raise ShardContractError(f"{label} must be a list")
    labels = tuple(
        _canonical_label(item, label=f"{label}[{index}]") for index, item in enumerate(value)
    )
    if tuple(sorted(set(labels))) != labels:
        raise ShardContractError(f"{label} must be unique and sorted")
    return labels


def _lower_hex(value: object, *, length: int, label: str) -> str:
    if (
        not isinstance(value, str)
        or len(value) != length
        or any(character not in "0123456789abcdef" for character in value)
    ):
        raise ShardContractError(f"{label} must be {length} lowercase hexadecimal characters")
    return value


def _choice(value: object, choices: set[str], *, label: str) -> str:
    if not isinstance(value, str) or value not in choices:
        raise ShardContractError(f"{label} must be one of {sorted(choices)}")
    return value


def _parse_evidence_sample(payload: object, *, index: int) -> EvidenceSample:
    label = f"evidence.sample[{index}]"
    if not isinstance(payload, dict):
        raise ShardContractError(f"{label} must be a table")
    fields = {
        "artifact_id",
        "artifact_name",
        "artifact_digest",
        "artifact_size_bytes",
        "workflow_run_id",
        "workflow_event",
        "workflow_status",
        "workflow_conclusion",
        "run_attempt",
        "head_sha",
        "bazel_job_id",
        "bazel_job_name",
        "bazel_job_status",
        "bazel_job_conclusion",
        "test_total",
        "test_passed",
        "test_failed",
        "qualification_path_ms",
        "test_wall_ms",
        "test_bep_critical_path_ms",
        "test_bep_action_cache_misses",
        "failed_test_labels",
    }
    _only_fields(payload, fields, label=label)
    workflow_run_id = _positive_integer(
        payload["workflow_run_id"], label=f"{label}.workflow_run_id"
    )
    run_attempt = _positive_integer(payload["run_attempt"], label=f"{label}.run_attempt")
    artifact_name = payload["artifact_name"]
    expected_artifact_name = f"bazel-performance-{workflow_run_id}-{run_attempt}"
    if artifact_name != expected_artifact_name:
        raise ShardContractError(f"{label}.artifact_name is not aligned with its workflow run")
    artifact_digest = payload["artifact_digest"]
    if not isinstance(artifact_digest, str) or not artifact_digest.startswith("sha256:"):
        raise ShardContractError(f"{label}.artifact_digest must be a GitHub SHA-256 digest")
    _lower_hex(artifact_digest.removeprefix("sha256:"), length=64, label=f"{label}.artifact_digest")
    workflow_status = _choice(
        payload["workflow_status"], {"completed"}, label=f"{label}.workflow_status"
    )
    bazel_job_status = _choice(
        payload["bazel_job_status"], {"completed"}, label=f"{label}.bazel_job_status"
    )
    workflow_conclusion = _choice(
        payload["workflow_conclusion"], {"failure", "success"}, label=f"{label}.workflow_conclusion"
    )
    bazel_job_conclusion = _choice(
        payload["bazel_job_conclusion"],
        {"failure", "success"},
        label=f"{label}.bazel_job_conclusion",
    )
    if workflow_conclusion != bazel_job_conclusion:
        raise ShardContractError(f"{label} workflow and Bazel job conclusions must match")
    bazel_job_name = payload["bazel_job_name"]
    if bazel_job_name != "bazel / verdict":
        raise ShardContractError(f"{label}.bazel_job_name is not the governed Bazel context")
    failed_test_labels = _canonical_label_list(
        payload["failed_test_labels"], label=f"{label}.failed_test_labels"
    )
    test_total = _positive_integer(payload["test_total"], label=f"{label}.test_total")
    test_passed = _positive_integer(payload["test_passed"], label=f"{label}.test_passed", minimum=0)
    test_failed = _positive_integer(payload["test_failed"], label=f"{label}.test_failed", minimum=0)
    if test_total != test_passed + test_failed or test_failed != len(failed_test_labels):
        raise ShardContractError(f"{label} test outcome counts are not aligned")
    expected_conclusion = "success" if test_failed == 0 else "failure"
    if bazel_job_conclusion != expected_conclusion:
        raise ShardContractError(f"{label} Bazel conclusion is not aligned with test outcomes")
    return EvidenceSample(
        artifact_id=_positive_integer(payload["artifact_id"], label=f"{label}.artifact_id"),
        artifact_name=artifact_name,
        artifact_digest=artifact_digest,
        artifact_size_bytes=_positive_integer(
            payload["artifact_size_bytes"], label=f"{label}.artifact_size_bytes"
        ),
        workflow_run_id=workflow_run_id,
        workflow_event=_choice(
            payload["workflow_event"],
            {"merge_group", "pull_request", "push", "schedule", "workflow_dispatch"},
            label=f"{label}.workflow_event",
        ),
        workflow_status=workflow_status,
        workflow_conclusion=workflow_conclusion,
        run_attempt=run_attempt,
        head_sha=_lower_hex(payload["head_sha"], length=40, label=f"{label}.head_sha"),
        bazel_job_id=_positive_integer(payload["bazel_job_id"], label=f"{label}.bazel_job_id"),
        bazel_job_name=bazel_job_name,
        bazel_job_status=bazel_job_status,
        bazel_job_conclusion=bazel_job_conclusion,
        test_total=test_total,
        test_passed=test_passed,
        test_failed=test_failed,
        qualification_path_ms=_positive_integer(
            payload["qualification_path_ms"], label=f"{label}.qualification_path_ms"
        ),
        test_wall_ms=_positive_integer(payload["test_wall_ms"], label=f"{label}.test_wall_ms"),
        test_bep_critical_path_ms=_positive_integer(
            payload["test_bep_critical_path_ms"], label=f"{label}.test_bep_critical_path_ms"
        ),
        test_bep_action_cache_misses=_positive_integer(
            payload["test_bep_action_cache_misses"],
            label=f"{label}.test_bep_action_cache_misses",
            minimum=0,
        ),
        failed_test_labels=failed_test_labels,
    )


def _parse_evidence(payload: object) -> Evidence:
    if not isinstance(payload, dict):
        raise ShardContractError("evidence must be a table")
    fields = {
        "generated_at",
        "sample",
        "qualification_path_median_ms",
        "test_wall_median_ms",
        "test_bep_critical_path_median_ms",
        "test_bep_action_cache_miss_median",
    }
    _only_fields(payload, fields, label="evidence")
    generated_at = payload["generated_at"]
    if not isinstance(generated_at, str) or not generated_at.endswith("Z"):
        raise ShardContractError("evidence.generated_at must be a UTC RFC3339 string")
    try:
        datetime.fromisoformat(generated_at.removesuffix("Z") + "+00:00")
    except ValueError as error:
        raise ShardContractError("evidence.generated_at must be valid RFC3339") from error
    raw_samples = payload["sample"]
    if not isinstance(raw_samples, list) or len(raw_samples) < 3:
        raise ShardContractError("evidence.sample must contain at least three retained runs")
    samples = tuple(
        _parse_evidence_sample(sample, index=index) for index, sample in enumerate(raw_samples)
    )
    if tuple(sample.workflow_run_id for sample in samples) != tuple(
        sorted(sample.workflow_run_id for sample in samples)
    ):
        raise ShardContractError("evidence.sample must be sorted by workflow_run_id")
    for field in ("artifact_id", "artifact_digest", "workflow_run_id", "bazel_job_id"):
        values = tuple(getattr(sample, field) for sample in samples)
        if len(values) != len(set(values)):
            raise ShardContractError(f"evidence.sample {field} values must be unique")
    if not any(sample.bazel_job_conclusion == "success" for sample in samples):
        raise ShardContractError("evidence.sample must include a successful complete Bazel run")
    if len(samples) % 2 == 0:
        raise ShardContractError(
            "evidence.sample count must be odd for integer median verification"
        )
    median_index = len(samples) // 2
    aggregate_fields = {
        "qualification_path_median_ms": "qualification_path_ms",
        "test_wall_median_ms": "test_wall_ms",
        "test_bep_critical_path_median_ms": "test_bep_critical_path_ms",
        "test_bep_action_cache_miss_median": "test_bep_action_cache_misses",
    }
    aggregates: dict[str, int] = {}
    for aggregate_field, sample_field in aggregate_fields.items():
        aggregate = _positive_integer(payload[aggregate_field], label=f"evidence.{aggregate_field}")
        calculated = sorted(getattr(sample, sample_field) for sample in samples)[median_index]
        if aggregate != calculated:
            raise ShardContractError(
                f"evidence.{aggregate_field} does not match the retained sample median"
            )
        aggregates[aggregate_field] = aggregate
    return Evidence(
        generated_at=generated_at,
        samples=samples,
        qualification_path_median_ms=aggregates["qualification_path_median_ms"],
        test_wall_median_ms=aggregates["test_wall_median_ms"],
        test_bep_critical_path_median_ms=aggregates["test_bep_critical_path_median_ms"],
        test_bep_action_cache_miss_median=aggregates["test_bep_action_cache_miss_median"],
    )


def _parse_test_weights(payload: object) -> tuple[TestWeight, ...]:
    if not isinstance(payload, list) or not payload:
        raise ShardContractError("test_weight must contain at least one retained observation")
    fields = {
        "label",
        "median_duration_ms",
        "minimum_duration_ms",
        "maximum_duration_ms",
        "observations",
    }
    weights: list[TestWeight] = []
    for index, item in enumerate(payload):
        if not isinstance(item, dict):
            raise ShardContractError(f"test_weight[{index}] must be a table")
        _only_fields(item, fields, label=f"test_weight[{index}]")
        minimum = _positive_integer(
            item["minimum_duration_ms"], label=f"test_weight[{index}].minimum_duration_ms"
        )
        median = _positive_integer(
            item["median_duration_ms"], label=f"test_weight[{index}].median_duration_ms"
        )
        maximum = _positive_integer(
            item["maximum_duration_ms"], label=f"test_weight[{index}].maximum_duration_ms"
        )
        if not minimum <= median <= maximum:
            raise ShardContractError(f"test_weight[{index}] duration order is invalid")
        weights.append(
            TestWeight(
                label=_canonical_label(item["label"], label=f"test_weight[{index}].label"),
                median_duration_ms=median,
                minimum_duration_ms=minimum,
                maximum_duration_ms=maximum,
                observations=_positive_integer(
                    item["observations"], label=f"test_weight[{index}].observations", minimum=3
                ),
            )
        )
    labels = tuple(weight.label for weight in weights)
    if tuple(sorted(set(labels))) != labels:
        raise ShardContractError("test_weight entries must be unique and sorted by label")
    return tuple(weights)


def load_contract(path: Path = DEFAULT_CONTRACT) -> ShardContract:
    descriptor = -1
    try:
        if not path.is_absolute() or path.resolve(strict=True) != path:
            raise OSError("noncanonical shard contract path")
        descriptor = os.open(path, os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0))
        with os.fdopen(descriptor, "rb") as stream:
            descriptor = -1
            before = os.fstat(stream.fileno())
            if not stat.S_ISREG(before.st_mode) or before.st_mode & 0o022:
                raise OSError("unsafe shard contract")
            source = stream.read(1024 * 1024 + 1)
            after = os.fstat(stream.fileno())
        current = path.lstat()
    except OSError as error:
        if descriptor >= 0:
            os.close(descriptor)
        raise ShardContractError(f"cannot read shard contract {path}: {error}") from error
    identities = {
        (metadata.st_dev, metadata.st_ino, metadata.st_mode, metadata.st_size, metadata.st_mtime_ns)
        for metadata in (before, after, current)
    }
    if len(source) > 1024 * 1024 or len(identities) != 1:
        raise ShardContractError("shard contract changed while it was read")
    try:
        payload = tomllib.loads(source.decode("utf-8"))
    except (UnicodeDecodeError, tomllib.TOMLDecodeError) as error:
        raise ShardContractError(f"cannot parse shard contract {path}: {error}") from error
    fields = {
        "schema_version",
        "shard_count",
        "default_test_duration_ms",
        "evidence",
        "test_weight",
    }
    _only_fields(payload, fields, label="shard contract")
    schema_version = _positive_integer(payload["schema_version"], label="schema_version")
    if schema_version != SCHEMA_VERSION:
        raise ShardContractError(
            f"unsupported shard contract schema {schema_version}; expected {SCHEMA_VERSION}"
        )
    evidence = _parse_evidence(payload["evidence"])
    test_weights = _parse_test_weights(payload["test_weight"])
    failed_test_labels = {
        label for sample in evidence.samples for label in sample.failed_test_labels
    }
    retained_failed_labels = sorted(
        weight.label for weight in test_weights if weight.label in failed_test_labels
    )
    if retained_failed_labels:
        raise ShardContractError(
            "test_weight includes labels with failed retained observations: "
            + ", ".join(retained_failed_labels)
        )
    if any(weight.observations != len(evidence.samples) for weight in test_weights):
        raise ShardContractError("test_weight observations must match the retained sample count")
    return ShardContract(
        schema_version=schema_version,
        shard_count=_positive_integer(payload["shard_count"], label="shard_count", minimum=2),
        default_test_duration_ms=_positive_integer(
            payload["default_test_duration_ms"], label="default_test_duration_ms"
        ),
        evidence=evidence,
        test_weights=test_weights,
        source_sha256=hashlib.sha256(source).hexdigest(),
    )


def _normalize_targets(targets: Sequence[str], *, label: str) -> tuple[str, ...]:
    normalized = tuple(sorted({_canonical_label(target, label=label) for target in targets}))
    if not normalized:
        raise ShardContractError(f"{label} query returned no targets")
    return normalized


def _target_digest(targets: Sequence[str]) -> str:
    encoded = "".join(f"{target}\n" for target in sorted(targets)).encode()
    return hashlib.sha256(encoded).hexdigest()


def _label_order(label: str) -> tuple[bytes, str]:
    return hashlib.sha256(label.encode()).digest(), label


def plan(
    contract: ShardContract,
    analysis_targets: Sequence[str],
    test_targets: Sequence[str],
) -> FullGraphPlan:
    analysis = _normalize_targets(analysis_targets, label="analysis")
    tests = _normalize_targets(test_targets, label="test")
    if set(analysis) & set(tests):
        raise ShardContractError("analysis and test query results overlap")
    durations = contract.durations()
    stale_weights = sorted(set(durations) - set(tests))
    if stale_weights:
        raise ShardContractError(
            "retained test weights no longer resolve in the queried repository graph: "
            + ", ".join(stale_weights)
        )

    analysis_parts: list[list[str]] = [[] for _ in range(contract.shard_count)]
    for position, target in enumerate(sorted(analysis, key=_label_order)):
        analysis_parts[position % contract.shard_count].append(target)

    test_parts: list[list[str]] = [[] for _ in range(contract.shard_count)]
    estimated = [0 for _ in range(contract.shard_count)]
    weighted_tests = sorted(
        tests,
        key=lambda target: (
            -durations.get(target, contract.default_test_duration_ms),
            *_label_order(target),
        ),
    )
    for target in weighted_tests:
        index = min(range(contract.shard_count), key=lambda shard: (estimated[shard], shard))
        test_parts[index].append(target)
        estimated[index] += durations.get(target, contract.default_test_duration_ms)

    shards = tuple(
        Shard(
            index=index,
            analysis_targets=tuple(sorted(analysis_parts[index])),
            test_targets=tuple(sorted(test_parts[index])),
            estimated_test_duration_ms=estimated[index],
        )
        for index in range(contract.shard_count)
    )
    return FullGraphPlan(
        contract_sha256=contract.source_sha256,
        analysis_targets=analysis,
        test_targets=tests,
        weighted_test_target_count=len(durations),
        shards=shards,
    )


def plan_from_bazel(
    contract: ShardContract,
    *,
    root: Path = ROOT,
    query: Query | None = None,
) -> FullGraphPlan:
    run_query = query or (lambda expression: affected.bazel_query(expression, root=root))
    return plan(contract, run_query(ANALYSIS_QUERY), run_query(TEST_QUERY))


def selection_for_shard(
    graph: FullGraphPlan,
    index: int,
    *,
    event: str,
    head_sha: str,
) -> affected.Selection:
    shard = graph.shard(index)
    return affected.Selection(
        mode="full",
        reason=f"complete_partition:{index + 1}_of_{len(graph.shards)}",
        changes=(),
        seeds=(affected.FULL_TARGET,),
        analysis_targets=shard.analysis_targets,
        test_targets=shard.test_targets,
        base_sha=None,
        head_sha=head_sha,
        event=event,
        analysis_query=ANALYSIS_QUERY,
        test_query=TEST_QUERY,
        partition=graph.manifest(selected_shard=index),
    )


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--contract", type=Path, default=DEFAULT_CONTRACT)
    parser.add_argument("--shard-index", type=int, required=True)
    parser.add_argument("--shard-count", type=int, required=True)
    parser.add_argument("--json-output", type=Path)
    args = parser.parse_args()
    try:
        contract = load_contract(args.contract)
        if args.shard_count != contract.shard_count:
            raise ShardContractError(
                f"runtime shard count {args.shard_count} does not match contract "
                f"{contract.shard_count}"
            )
        graph = plan_from_bazel(contract)
        manifest = graph.manifest(selected_shard=args.shard_index)
    except ShardContractError as error:
        print(f"full-graph shard planning failed: {error}", file=sys.stderr)
        return 2
    encoded = json.dumps(manifest, indent=2, sort_keys=True) + "\n"
    if args.json_output is not None:
        args.json_output.parent.mkdir(parents=True, exist_ok=True)
        args.json_output.write_text(encoded, encoding="utf-8")
    else:
        print(encoded, end="")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
