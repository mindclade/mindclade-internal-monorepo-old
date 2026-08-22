# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

from copy import deepcopy
from pathlib import Path

import pytest

from configs.contract_validation import load_json, validate, validate_schema_subset
from libs.python.identifiers import Digest
from libs.python.serialization import canonical_json_bytes
from tools.qualification.training_gke.evidence import (
    REQUIRED_ARTIFACT_KINDS,
    QualificationSummary,
    validate_qualification_set,
)

DIGESTS = ["sha256:" + f"{index:064x}" for index in range(1, 100)]
SCHEMA = Path(__file__).resolve().parents[1] / "qualification-set.schema.json"


def _run(index: int, outcome: str = "succeeded") -> dict:
    started = None if outcome == "pre_admission_rejected" else 1_800_000_000_000 + index
    return {
        "run_id": f"run_{index + 1:032x}",
        "cohort_digest": DIGESTS[1],
        "phase": "h100-8g-ddp-dcp",
        "capacity_type": "on-demand",
        "attempt_count": 3 if index == 0 else 1,
        "started_unix_millis": started,
        "terminal_unix_millis": 1_800_000_100_000 + index,
        "outcome": outcome,
        "evidence_digest": DIGESTS[index + 20],
    }


def _document() -> dict:
    runs = [_run(index) for index in range(30)]
    runs.extend([_run(30, "pre_admission_rejected"), _run(31, "user_canceled")])
    policy = {
        "minimum_target_runs": 30,
        "completion_objective_ppm": 1_000_000,
        "approved_thresholds_digest": DIGESTS[3],
    }
    policy_digest = Digest.of(canonical_json_bytes(policy)).text
    return {
        "schema_version": "mindclade.dev/training-platform-qualification/v1",
        "subject_digest": DIGESTS[0],
        "cohort_digest": DIGESTS[1],
        "policy_digest": policy_digest,
        "policy": policy,
        "smoke_evidence_digest": DIGESTS[4],
        "target_runs": runs,
        "summary": {
            "eligible_runs": 30,
            "successful_runs": 30,
            "excluded_pre_admission": 1,
            "excluded_user_cancellations": 1,
            "completion_ratio_ppm": 1_000_000,
            "subject_digest": DIGESTS[0],
        },
        "artifacts": {
            kind: DIGESTS[4] if kind == "h100_1g_qualification" else DIGESTS[5]
            for kind in REQUIRED_ARTIFACT_KINDS
        },
        "approvals": [
            {
                "team": team,
                "decision": "approved",
                "subject_digest": DIGESTS[0],
                "policy_digest": policy_digest,
                "approved_thresholds_digest": DIGESTS[3],
                "decision_digest": DIGESTS[index + 6],
            }
            for index, team in enumerate(("finops", "sre", "training-platform"))
        ],
    }


def test_connected_set_counts_runs_not_attempts_and_excludes_only_defined_cases() -> None:
    document = _document()
    assert validate_qualification_set(document) == QualificationSummary(30, 30, 1, 1, 1_000_000)
    schema = load_json(SCHEMA)
    assert validate_schema_subset(schema) == ()
    assert validate(document, schema) == ()
    assert set(schema["required"]) == set(document)


def test_fewer_than_thirty_eligible_target_runs_fails_closed() -> None:
    document = _document()
    document["target_runs"] = document["target_runs"][:29]
    with pytest.raises(ValueError, match="fewer eligible"):
        validate_qualification_set(document)


def test_observed_completion_must_meet_owner_approved_objective() -> None:
    document = _document()
    document["target_runs"][0]["outcome"] = "platform_failure"
    document["summary"]["successful_runs"] = 29
    document["summary"]["completion_ratio_ppm"] = (29 * 1_000_000) // 30
    with pytest.raises(ValueError, match="below the approved objective"):
        validate_qualification_set(document)


def test_policy_projection_is_canonical_and_immutable() -> None:
    document = _document()
    document["policy"]["minimum_target_runs"] = 31
    with pytest.raises(ValueError, match="canonical projection"):
        validate_qualification_set(document)


def test_runs_cannot_cross_cohorts_or_be_duplicated() -> None:
    document = _document()
    document["target_runs"][0]["cohort_digest"] = DIGESTS[9]
    with pytest.raises(ValueError, match="immutable eight-GPU cohort"):
        validate_qualification_set(document)
    document = _document()
    document["target_runs"][1]["run_id"] = document["target_runs"][0]["run_id"]
    with pytest.raises(ValueError, match="unique canonical"):
        validate_qualification_set(document)
    document = _document()
    document["target_runs"][1]["evidence_digest"] = document["target_runs"][0]["evidence_digest"]
    with pytest.raises(ValueError, match="independent evidence"):
        validate_qualification_set(document)


def test_exact_artifacts_approvals_and_authoritative_summary_are_required() -> None:
    document = _document()
    del document["artifacts"]["rollback_drill"]
    with pytest.raises(ValueError, match="exact required"):
        validate_qualification_set(document)
    document = _document()
    document["approvals"][0]["decision"] = "pending"
    with pytest.raises(ValueError, match="must be approved"):
        validate_qualification_set(document)
    document = _document()
    document["approvals"][0]["policy_digest"] = DIGESTS[2]
    with pytest.raises(ValueError, match="not bound"):
        validate_qualification_set(document)
    document = _document()
    document["approvals"][1]["decision_digest"] = document["approvals"][0]["decision_digest"]
    with pytest.raises(ValueError, match="independent decisions"):
        validate_qualification_set(document)
    document = _document()
    inconsistent = deepcopy(document)
    inconsistent["summary"]["eligible_runs"] = 31
    with pytest.raises(ValueError, match="authoritative run outcomes"):
        validate_qualification_set(inconsistent)
