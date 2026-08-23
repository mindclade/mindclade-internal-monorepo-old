# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

import json
import sys
from dataclasses import replace
from pathlib import Path

import pytest

from ci.common.bazel_verdict import (
    VerdictContractError,
    WorkerSelectionEvidence,
    evaluate,
    main,
    parse_expected_workers,
    redact_selection,
    verify_worker_selections,
)

HEAD_SHA = "1" * 40
ARTIFACT_PREFIX = "bazel-selection-123-1-"


def _execution() -> list[dict[str, object]]:
    return [
        {"phase": "analysis", "status": "passed", "exit_code": 0},
        {"phase": "test", "status": "passed", "exit_code": 0},
    ]


def _unsharded_selection() -> dict[str, object]:
    return {
        "schema_version": 1,
        "event": "pull_request",
        "head_sha": HEAD_SHA,
        "mode": "full",
        "partition": None,
        "execution": _execution(),
        "completed_at": "2026-08-22T12:00:00Z",
        "analysis_targets": ["//private:target"],
        "test_targets": ["//private:test"],
        "changed_paths": [{"path": "private/source.py"}],
        "queries": {"analysis": "private-query", "test": "private-query"},
    }


def _shards() -> list[dict[str, int | str]]:
    return [
        {
            "index": index,
            "analysis_target_count": 2,
            "analysis_targets_sha256": f"{index + 1:064x}",
            "test_target_count": 1,
            "test_targets_sha256": f"{index + 11:064x}",
            "estimated_test_duration_ms": 1000 + index,
        }
        for index in range(4)
    ]


def _sharded_selection(worker: int) -> dict[str, object]:
    shards = _shards()
    return {
        "schema_version": 1,
        "event": "merge_group",
        "head_sha": HEAD_SHA,
        "mode": "full",
        "partition": {
            "schema_version": 2,
            "contract_sha256": "a" * 64,
            "shard_count": 4,
            "analysis_query": "analysis-query",
            "analysis_target_count": 8,
            "analysis_graph_sha256": "b" * 64,
            "test_query": "test-query",
            "test_target_count": 4,
            "test_graph_sha256": "c" * 64,
            "weighted_test_target_count": 4,
            "shards": shards,
            "selected_shard": shards[worker],
        },
        "execution": _execution(),
        "completed_at": "2026-08-22T12:00:00Z",
        "analysis_targets": [f"//private:analysis_{worker}"],
        "test_targets": [f"//private:test_{worker}"],
    }


def _sharded_evidence(worker: int) -> WorkerSelectionEvidence:
    return redact_selection(
        _sharded_selection(worker),
        worker=worker,
        topology_mode="full-sharded",
        event="merge_group",
        head_sha=HEAD_SHA,
        shard_count=4,
    )


def _write_artifacts(root: Path, evidence: tuple[WorkerSelectionEvidence, ...]) -> None:
    root.mkdir()
    for item in evidence:
        directory = root / f"{ARTIFACT_PREFIX}{item.worker}"
        directory.mkdir()
        (directory / "worker-selection.json").write_text(
            json.dumps(item.as_dict()), encoding="utf-8"
        )


@pytest.mark.parametrize(
    "lane,event",
    [
        ("presubmit", "pull_request"),
        ("presubmit", "merge_group"),
        ("presubmit", "push"),
        ("nightly", "schedule"),
        ("nightly", "workflow_dispatch"),
    ],
)
def test_expected_lane_results_pass(lane: str, event: str) -> None:
    assert not evaluate(lane=lane, event=event, plan="success", workers="success")


@pytest.mark.parametrize("result", ["failure", "cancelled", "skipped"])
def test_required_worker_failure_is_rejected(result: str) -> None:
    errors = evaluate(
        lane="presubmit",
        event="merge_group",
        plan="success",
        workers=result,
    )
    assert [error.code for error in errors] == ["BAZEL_VERDICT_WORKERS_SUCCESS_REQUIRED"]


def test_failed_plan_and_skipped_workers_are_both_rejected() -> None:
    errors = evaluate(
        lane="presubmit",
        event="pull_request",
        plan="failure",
        workers="skipped",
    )
    assert [error.code for error in errors] == [
        "BAZEL_VERDICT_PLAN_SUCCESS_REQUIRED",
        "BAZEL_VERDICT_WORKERS_SUCCESS_REQUIRED",
    ]


def test_unknown_event_and_result_fail_closed_without_echoing_values() -> None:
    event_errors = evaluate(
        lane="presubmit",
        event="repository_dispatch",
        plan="success",
        workers="success",
    )
    assert event_errors[0].code == "BAZEL_VERDICT_UNEXPECTED_EVENT"
    result_errors = evaluate(
        lane="nightly",
        event="schedule",
        plan="success",
        workers="secret-shaped-value",
    )
    assert result_errors[0].code == "BAZEL_VERDICT_INVALID_RESULT"
    assert "secret-shaped-value" not in result_errors[0].message


def test_worker_selection_artifact_is_redacted() -> None:
    evidence = redact_selection(
        _unsharded_selection(),
        worker=-1,
        topology_mode="presubmit-auto",
        event="pull_request",
        head_sha=HEAD_SHA,
        shard_count=4,
    )
    encoded = json.dumps(evidence.as_dict(), sort_keys=True)
    assert evidence.worker == -1
    assert evidence.contract_sha256 is None
    assert "//private" not in encoded
    assert "private/source.py" not in encoded
    assert "private-query" not in encoded
    assert "analysis_targets" not in encoded
    assert "execution" not in encoded


def test_redact_cli_writes_exclusive_private_artifact(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    source = tmp_path / "selection.json"
    source.write_text(json.dumps(_unsharded_selection()), encoding="utf-8")
    output = tmp_path / "bazel-worker-selection" / "worker-selection.json"
    monkeypatch.setattr(
        sys,
        "argv",
        [
            "bazel_verdict.py",
            "redact-selection",
            "--source",
            str(source),
            "--output",
            str(output),
            "--runner-temp",
            str(tmp_path),
            "--worker",
            "-1",
            "--topology-mode",
            "presubmit-auto",
            "--event",
            "pull_request",
            "--head-sha",
            HEAD_SHA,
            "--shard-count",
            "4",
        ],
    )
    assert main() == 0
    assert output.stat().st_mode & 0o777 == 0o600
    assert "//private" not in output.read_text(encoding="utf-8")
    assert main() == 2


def test_sharded_selection_attests_the_complete_partition() -> None:
    evidence = _sharded_evidence(2)
    assert evidence.worker == 2
    assert evidence.selected_shard_index == 2
    assert evidence.contract_sha256 == "a" * 64
    assert evidence.analysis_graph_sha256 == "b" * 64
    assert evidence.test_graph_sha256 == "c" * 64
    assert evidence.partition_manifest_sha256 is not None


def test_failed_execution_cannot_emit_worker_evidence() -> None:
    selection = _sharded_selection(0)
    selection["execution"] = [
        {"phase": "analysis", "status": "failed", "exit_code": 1},
        {"phase": "test", "status": "skipped", "exit_code": 1},
    ]
    with pytest.raises(VerdictContractError) as captured:
        redact_selection(
            selection,
            worker=0,
            topology_mode="full-sharded",
            event="merge_group",
            head_sha=HEAD_SHA,
            shard_count=4,
        )
    assert captured.value.code == "BAZEL_VERDICT_SELECTION_INVALID"


def test_pull_request_cannot_claim_full_sharded_topology() -> None:
    selection = _sharded_selection(0)
    selection["event"] = "pull_request"
    with pytest.raises(VerdictContractError) as captured:
        redact_selection(
            selection,
            worker=0,
            topology_mode="full-sharded",
            event="pull_request",
            head_sha=HEAD_SHA,
            shard_count=4,
        )
    assert captured.value.code == "BAZEL_VERDICT_TOPOLOGY_INVALID"


def test_complete_sharded_worker_artifacts_pass_central_verification(tmp_path: Path) -> None:
    root = tmp_path / "selections"
    _write_artifacts(root, tuple(_sharded_evidence(index) for index in range(4)))
    verify_worker_selections(
        root,
        artifact_prefix=ARTIFACT_PREFIX,
        expected_workers=(0, 1, 2, 3),
        topology_mode="full-sharded",
        event="merge_group",
        head_sha=HEAD_SHA,
        shard_count=4,
    )


def test_verify_cli_requires_and_accepts_complete_current_attempt(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    root = tmp_path / "selections"
    _write_artifacts(root, tuple(_sharded_evidence(index) for index in range(4)))
    monkeypatch.setattr(
        sys,
        "argv",
        [
            "bazel_verdict.py",
            "verify",
            "--lane",
            "presubmit",
            "--event",
            "merge_group",
            "--head-sha",
            HEAD_SHA,
            "--plan-result",
            "success",
            "--workers-result",
            "success",
            "--topology-mode",
            "full-sharded",
            "--expected-workers",
            "[0,1,2,3]",
            "--shard-count",
            "4",
            "--selection-root",
            str(root),
            "--artifact-prefix",
            ARTIFACT_PREFIX,
        ],
    )
    assert main() == 0


def test_missing_shard_artifact_fails_closed(tmp_path: Path) -> None:
    root = tmp_path / "selections"
    _write_artifacts(root, tuple(_sharded_evidence(index) for index in range(3)))
    with pytest.raises(VerdictContractError) as captured:
        verify_worker_selections(
            root,
            artifact_prefix=ARTIFACT_PREFIX,
            expected_workers=(0, 1, 2, 3),
            topology_mode="full-sharded",
            event="merge_group",
            head_sha=HEAD_SHA,
            shard_count=4,
        )
    assert captured.value.code == "BAZEL_VERDICT_ARTIFACT_SET_INVALID"


def test_worker_graph_digest_disagreement_fails_closed(tmp_path: Path) -> None:
    evidence = [_sharded_evidence(index) for index in range(4)]
    evidence[3] = replace(evidence[3], analysis_graph_sha256="d" * 64)
    root = tmp_path / "selections"
    _write_artifacts(root, tuple(evidence))
    with pytest.raises(VerdictContractError) as captured:
        verify_worker_selections(
            root,
            artifact_prefix=ARTIFACT_PREFIX,
            expected_workers=(0, 1, 2, 3),
            topology_mode="full-sharded",
            event="merge_group",
            head_sha=HEAD_SHA,
            shard_count=4,
        )
    assert captured.value.code == "BAZEL_VERDICT_PARTITION_MISMATCH"


def test_worker_cannot_claim_another_shard_index(tmp_path: Path) -> None:
    evidence = [_sharded_evidence(index) for index in range(4)]
    evidence[3] = replace(evidence[3], selected_shard_index=2)
    root = tmp_path / "selections"
    _write_artifacts(root, tuple(evidence))
    with pytest.raises(VerdictContractError) as captured:
        verify_worker_selections(
            root,
            artifact_prefix=ARTIFACT_PREFIX,
            expected_workers=(0, 1, 2, 3),
            topology_mode="full-sharded",
            event="merge_group",
            head_sha=HEAD_SHA,
            shard_count=4,
        )
    assert captured.value.code == "BAZEL_VERDICT_SELECTION_IDENTITY_MISMATCH"


def test_symlinked_artifact_is_rejected_without_path_disclosure(tmp_path: Path) -> None:
    root = tmp_path / "selections"
    root.mkdir()
    secret = tmp_path / "secret"
    secret.mkdir()
    (root / f"{ARTIFACT_PREFIX}0").symlink_to(secret, target_is_directory=True)
    for worker in range(1, 4):
        directory = root / f"{ARTIFACT_PREFIX}{worker}"
        directory.mkdir()
        (directory / "worker-selection.json").write_text(
            json.dumps(_sharded_evidence(worker).as_dict()), encoding="utf-8"
        )
    with pytest.raises(VerdictContractError) as captured:
        verify_worker_selections(
            root,
            artifact_prefix=ARTIFACT_PREFIX,
            expected_workers=(0, 1, 2, 3),
            topology_mode="full-sharded",
            event="merge_group",
            head_sha=HEAD_SHA,
            shard_count=4,
        )
    assert str(secret) not in str(captured.value)


@pytest.mark.parametrize(
    "value",
    ["", "[0,0]", "[true]", "[0,NaN]", '{"worker": 0}'],
)
def test_expected_worker_matrix_parser_fails_closed(value: str) -> None:
    with pytest.raises(VerdictContractError) as captured:
        parse_expected_workers(value)
    assert captured.value.code == "BAZEL_VERDICT_TOPOLOGY_INVALID"
