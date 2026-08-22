# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Fail-closed aggregation for connected reference-training qualification evidence."""

from __future__ import annotations

import json
import os
import re
import stat
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Final, cast

from libs.python.identifiers import Digest
from libs.python.serialization import canonical_json_bytes

_DIGEST: Final = re.compile(r"^sha256:(?!0{64}$)[0-9a-f]{64}$")
_RUN_ID: Final = re.compile(r"^run_[0-9a-f]{32}$")

REQUIRED_ARTIFACT_KINDS: Final = (
    "alert_fire_resolve",
    "checkpoint_resume",
    "cost_qualification",
    "h100_1g_qualification",
    "h100_8g_qualification",
    "lineage",
    "numerical_qualification",
    "performance",
    "reliability_qualification",
    "rollback_drill",
    "scale",
    "security_qualification",
    "slo_approval",
    "vulnerability_scan",
)
_APPROVAL_TEAMS: Final = ("finops", "sre", "training-platform")
_OUTCOMES: Final = {
    "deadline_exceeded",
    "failed",
    "platform_failure",
    "pre_admission_rejected",
    "succeeded",
    "user_canceled",
}
_ROOT_FIELDS: Final = {
    "schema_version",
    "subject_digest",
    "cohort_digest",
    "policy_digest",
    "policy",
    "smoke_evidence_digest",
    "target_runs",
    "summary",
    "artifacts",
    "approvals",
}
_RUN_FIELDS: Final = {
    "run_id",
    "cohort_digest",
    "phase",
    "capacity_type",
    "attempt_count",
    "started_unix_millis",
    "terminal_unix_millis",
    "outcome",
    "evidence_digest",
}
MAXIMUM_QUALIFICATION_BYTES: Final = 16 * 1024 * 1024


@dataclass(frozen=True, slots=True)
class QualificationSummary:
    eligible_runs: int
    successful_runs: int
    excluded_pre_admission: int
    excluded_user_cancellations: int
    completion_ratio_ppm: int


def _unique_json_object(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    result: dict[str, Any] = {}
    for key, value in pairs:
        if key in result:
            raise ValueError(f"duplicate JSON key: {key}")
        result[key] = value
    return result


def load_qualification_set(path: Path) -> dict[str, Any]:
    """Load one bounded regular file without following a final symlink."""

    if not path.is_absolute():
        raise ValueError("qualification evidence path must be absolute")
    descriptor = os.open(path, os.O_RDONLY | os.O_CLOEXEC | os.O_NOFOLLOW)
    try:
        file_stat = os.fstat(descriptor)
        if (
            not stat.S_ISREG(file_stat.st_mode)
            or not 0 < file_stat.st_size <= MAXIMUM_QUALIFICATION_BYTES
        ):
            raise ValueError("qualification evidence must be a bounded regular file")
        chunks: list[bytes] = []
        remaining = MAXIMUM_QUALIFICATION_BYTES + 1
        while remaining:
            chunk = os.read(descriptor, remaining)
            if not chunk:
                break
            chunks.append(chunk)
            remaining -= len(chunk)
    finally:
        os.close(descriptor)
    raw = b"".join(chunks)
    if len(raw) > MAXIMUM_QUALIFICATION_BYTES:
        raise ValueError("qualification evidence exceeds its byte budget")
    value = json.loads(
        raw,
        object_pairs_hook=_unique_json_object,
        parse_constant=lambda token: (_ for _ in ()).throw(
            ValueError(f"non-finite JSON number: {token}")
        ),
    )
    validate_qualification_set(value)
    return cast(dict[str, Any], value)


def _digest(value: Any, field: str) -> str:
    if not isinstance(value, str) or _DIGEST.fullmatch(value) is None:
        raise ValueError(f"{field} must be a nonzero canonical sha256 digest")
    return value


def _positive_integer(value: Any, field: str, maximum: int = (1 << 63) - 1) -> int:
    if isinstance(value, bool) or not isinstance(value, int) or not 1 <= value <= maximum:
        raise ValueError(f"{field} must be a positive bounded integer")
    return value


def validate_qualification_set(value: Any) -> QualificationSummary:
    """Validate a structural projection; typed artifact resolution remains external."""

    if not isinstance(value, dict) or set(value) != _ROOT_FIELDS:
        raise ValueError("training qualification evidence fields are incomplete")
    if value["schema_version"] != "mindclade.dev/training-platform-qualification/v1":
        raise ValueError("training qualification evidence schema is unsupported")
    subject_digest = _digest(value["subject_digest"], "subject_digest")
    cohort_digest = _digest(value["cohort_digest"], "cohort_digest")
    policy_digest = _digest(value["policy_digest"], "policy_digest")
    smoke_digest = _digest(value["smoke_evidence_digest"], "smoke_evidence_digest")

    policy = value["policy"]
    if not isinstance(policy, dict) or set(policy) != {
        "minimum_target_runs",
        "completion_objective_ppm",
        "approved_thresholds_digest",
    }:
        raise ValueError("qualification policy fields are invalid")
    minimum_runs = _positive_integer(policy["minimum_target_runs"], "minimum_target_runs", 10_000)
    if minimum_runs < 30:
        raise ValueError("qualification policy must require at least 30 target runs")
    objective_ppm = _positive_integer(
        policy["completion_objective_ppm"], "completion_objective_ppm", 1_000_000
    )
    thresholds_digest = _digest(policy["approved_thresholds_digest"], "approved_thresholds_digest")
    if Digest.of(canonical_json_bytes(policy)).text != policy_digest:
        raise ValueError("qualification policy digest does not match its canonical projection")

    artifacts = value["artifacts"]
    if not isinstance(artifacts, dict) or set(artifacts) != set(REQUIRED_ARTIFACT_KINDS):
        raise ValueError("qualification artifacts must contain the exact required kinds")
    for kind, digest in artifacts.items():
        _digest(digest, f"artifacts[{kind}]")
    if artifacts["h100_1g_qualification"] != smoke_digest:
        raise ValueError("one-GPU smoke artifact does not match smoke evidence")

    approvals = value["approvals"]
    if not isinstance(approvals, list) or len(approvals) != len(_APPROVAL_TEAMS):
        raise ValueError("qualification requires Training Platform, SRE, and FinOps approvals")
    observed_teams: list[str] = []
    observed_decisions: set[str] = set()
    for approval in approvals:
        if not isinstance(approval, dict) or set(approval) != {
            "team",
            "decision",
            "subject_digest",
            "policy_digest",
            "approved_thresholds_digest",
            "decision_digest",
        }:
            raise ValueError("qualification approval fields are invalid")
        observed_teams.append(str(approval["team"]))
        if approval["decision"] != "approved":
            raise ValueError("qualification approval decision must be approved")
        if (
            approval["subject_digest"] != subject_digest
            or approval["policy_digest"] != policy_digest
            or approval["approved_thresholds_digest"] != thresholds_digest
        ):
            raise ValueError("qualification approval is not bound to the subject and policy")
        decision_digest = _digest(approval["decision_digest"], "approval decision_digest")
        if decision_digest in observed_decisions:
            raise ValueError("qualification approvals must reference independent decisions")
        observed_decisions.add(decision_digest)
    if tuple(observed_teams) != _APPROVAL_TEAMS:
        raise ValueError("qualification approvals must be exact and sorted")

    runs = value["target_runs"]
    if not isinstance(runs, list) or not runs or len(runs) > 10_000:
        raise ValueError("target_runs must be a bounded nonempty list")
    seen: set[str] = set()
    seen_evidence: set[str] = set()
    eligible = successful = pre_admission = user_cancellations = 0
    for run in runs:
        if not isinstance(run, dict) or set(run) != _RUN_FIELDS:
            raise ValueError("target run fields are invalid")
        run_id = run["run_id"]
        if not isinstance(run_id, str) or _RUN_ID.fullmatch(run_id) is None or run_id in seen:
            raise ValueError("target run IDs must be unique canonical run IDs")
        seen.add(run_id)
        if (
            run["cohort_digest"] != cohort_digest
            or run["phase"] != "h100-8g-ddp-dcp"
            or run["capacity_type"] != "on-demand"
        ):
            raise ValueError("target run does not match the immutable eight-GPU cohort")
        _positive_integer(run["attempt_count"], "attempt_count", 100)
        terminal = _positive_integer(run["terminal_unix_millis"], "terminal_unix_millis")
        outcome = run["outcome"]
        if outcome not in _OUTCOMES:
            raise ValueError("target run outcome is invalid")
        started = run["started_unix_millis"]
        if outcome == "pre_admission_rejected":
            if started is not None:
                raise ValueError("pre-admission rejection must not claim execution start")
            pre_admission += 1
        else:
            started_value = _positive_integer(started, "started_unix_millis")
            if terminal < started_value:
                raise ValueError("target run terminal time predates its start")
            if outcome == "user_canceled":
                user_cancellations += 1
            else:
                eligible += 1
                successful += int(outcome == "succeeded")
        evidence_digest = _digest(run["evidence_digest"], "target run evidence_digest")
        if evidence_digest in seen_evidence:
            raise ValueError("target runs must reference independent evidence artifacts")
        seen_evidence.add(evidence_digest)

    if eligible < minimum_runs:
        raise ValueError("qualification has fewer eligible target runs than approved policy")
    if successful * 1_000_000 < objective_ppm * eligible:
        raise ValueError("observed completion ratio is below the approved objective")
    completion_ratio_ppm = (successful * 1_000_000) // eligible
    expected = QualificationSummary(
        eligible_runs=eligible,
        successful_runs=successful,
        excluded_pre_admission=pre_admission,
        excluded_user_cancellations=user_cancellations,
        completion_ratio_ppm=completion_ratio_ppm,
    )
    summary = value["summary"]
    if not isinstance(summary, dict) or set(summary) != {
        "eligible_runs",
        "successful_runs",
        "excluded_pre_admission",
        "excluded_user_cancellations",
        "completion_ratio_ppm",
        "subject_digest",
    }:
        raise ValueError("qualification summary fields are invalid")
    if summary["subject_digest"] != subject_digest:
        raise ValueError("qualification summary is not bound to the subject")
    observed = (
        summary["eligible_runs"],
        summary["successful_runs"],
        summary["excluded_pre_admission"],
        summary["excluded_user_cancellations"],
    )
    calculated = (
        expected.eligible_runs,
        expected.successful_runs,
        expected.excluded_pre_admission,
        expected.excluded_user_cancellations,
    )
    ratio_ppm = summary["completion_ratio_ppm"]
    if observed != calculated or ratio_ppm != completion_ratio_ppm:
        raise ValueError("qualification summary does not match authoritative run outcomes")
    return expected
