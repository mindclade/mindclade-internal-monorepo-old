# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

import json
import sys
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[3]
ANALYSIS = ROOT / "tools/analysis"
if str(ANALYSIS) not in sys.path:
    sys.path.insert(0, str(ANALYSIS))

import check_affected_presubmit  # noqa: E402
import workflow_yaml  # noqa: E402

from ci.common.affected_contract import GlobalInputContract  # noqa: E402
from ci.nightly import pipeline as nightly_pipeline  # noqa: E402
from ci.presubmit import pipeline as presubmit_pipeline  # noqa: E402


def _contract() -> GlobalInputContract:
    return GlobalInputContract(
        exact_paths=frozenset({"MODULE.bazel"}),
        prefixes=("ci/",),
        review_boundaries=(
            ("", ("ci", "tools")),
            ("tools", ("analysis",)),
        ),
    )


def test_reviewed_authority_inventory_passes() -> None:
    assert not check_affected_presubmit._review_boundary_errors(
        _contract(),
        ("ci/common/affected.py", "tools/analysis/check_affected_presubmit.py"),
    )


def test_new_top_level_authority_fails_closed() -> None:
    errors = check_affected_presubmit._review_boundary_errors(
        _contract(),
        (
            "build-support/config.json",
            "ci/common/affected.py",
            "tools/analysis/check_affected_presubmit.py",
        ),
    )
    assert errors == ["[AFFECTED-GLOBAL-006] an unreviewed repository authority exists"]


def test_new_tools_authority_fails_closed() -> None:
    errors = check_affected_presubmit._review_boundary_errors(
        _contract(),
        (
            "ci/common/affected.py",
            "tools/analysis/check_affected_presubmit.py",
            "tools/graph/config.json",
        ),
    )
    assert errors == ["[AFFECTED-GLOBAL-006] an unreviewed repository authority exists"]


def test_graph_native_activation_cannot_bypass_evidence(tmp_path: Path) -> None:
    source = ROOT / "ci/common/affected_global_inputs.json"
    payload = json.loads(source.read_text(encoding="utf-8"))
    payload["activation"]["state"] = "active"
    candidate = tmp_path / "affected_global_inputs.json"
    candidate.write_text(json.dumps(payload), encoding="utf-8")
    assert check_affected_presubmit._activation_errors(candidate) == [
        "[AFFECTED-GLOBAL-009] graph-native activation must remain blocked pending evidence"
    ]


def _presubmit_workflow(*, mode: str = "auto", comment_only_contract: bool = False) -> str:
    governed_command = (
        "/nix/var/nix/profiles/default/bin/nix develop .#ci-bazel "
        "--command python3 -I ci/presubmit/pipeline.py "
        f'--bazel-only --mode {mode} --base "${{PR_BASE_SHA}}" '
        '--event "${GITHUB_EVENT_NAME}" --ref "${GITHUB_REF}" '
        '--head "${GITHUB_SHA}" '
        '--evidence-dir "${RUNNER_TEMP}/bazel-evidence" '
        '--job-started-at-file "${RUNNER_TEMP}/bazel-job-started"'
    )
    run = (
        f"# {governed_command}\n          echo unsafe"
        if comment_only_contract
        else governed_command
    )
    return f"""---
name: Presubmit
on:
  push:
    branches: [main]
  pull_request:
  merge_group:
permissions:
  contents: read
concurrency:
  group: ${{{{ github.workflow }}}}-${{{{ github.ref }}}}
  cancel-in-progress: true
jobs:
  bazel:
    name: bazel / verdict
    runs-on: ubuntu-24.04
    timeout-minutes: 90
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1
        with:
          fetch-depth: 0
          persist-credentials: false
      - name: Run event-governed Bazel validation
        env:
          BASH_ENV: ""
          PR_BASE_SHA: ${{{{ github.event.pull_request.base.sha }}}}
        run: >-
          {run}
      - name: Upload Bazel performance evidence
        if: always()
        uses: actions/upload-artifact@ea165f8d65b6e75b540449e92b4886f43607fa02
        with:
          name: bazel-performance-${{{{ github.run_id }}}}-${{{{ github.run_attempt }}}}
          path: ${{{{ runner.temp }}}}/bazel-evidence/*
          if-no-files-found: warn
          retention-days: 35
      - name: Upload Bazel latency metric
        if: always()
        uses: actions/upload-artifact@ea165f8d65b6e75b540449e92b4886f43607fa02
        with:
          name: bazel-metrics-${{{{ github.run_id }}}}-${{{{ github.run_attempt }}}}
          path: ${{{{ runner.temp }}}}/bazel-evidence/run-metrics.json
          if-no-files-found: ignore
          retention-days: 35
"""


def test_structural_workflow_parser_accepts_governed_event_routing() -> None:
    workflow = workflow_yaml.parse_workflow_text(_presubmit_workflow())
    assert check_affected_presubmit._presubmit_workflow_errors(workflow) == []


def test_workflow_comment_cannot_satisfy_governed_command() -> None:
    workflow = workflow_yaml.parse_workflow_text(_presubmit_workflow(comment_only_contract=True))
    assert "[AFFECTED-WORKFLOW-005] governed Bazel command is invalid" in (
        check_affected_presubmit._presubmit_workflow_errors(workflow)
    )


def test_workflow_rejects_alternate_pull_request_mode() -> None:
    workflow = workflow_yaml.parse_workflow_text(_presubmit_workflow(mode="full"))
    assert "[AFFECTED-WORKFLOW-005] governed Bazel command is invalid" in (
        check_affected_presubmit._presubmit_workflow_errors(workflow)
    )


def test_workflow_parser_rejects_duplicate_keys() -> None:
    with pytest.raises(workflow_yaml.WorkflowYamlError) as captured:
        workflow_yaml.parse_workflow_text("name: first\nname: second\n")
    assert captured.value.code == "AFFECTED-WORKFLOW-002"


def test_workflow_parser_rejects_quoted_key_alias() -> None:
    with pytest.raises(workflow_yaml.WorkflowYamlError) as captured:
        workflow_yaml.parse_workflow_text('permissions:\n  contents: read\n"permissions": {}\n')
    assert captured.value.code == "AFFECTED-WORKFLOW-001"


@pytest.mark.parametrize("scope", ["job", "governed-step"])
def test_presubmit_rejects_expression_continue_on_error(scope: str) -> None:
    workflow = workflow_yaml.parse_workflow_text(_presubmit_workflow())
    job = workflow["jobs"]["bazel"]
    if scope == "job":
        job["continue-on-error"] = "${{ true }}"
    else:
        step = next(
            item
            for item in job["steps"]
            if item.get("name") == "Run event-governed Bazel validation"
        )
        step["continue-on-error"] = "${{ true }}"
    assert check_affected_presubmit._presubmit_workflow_errors(workflow)


def test_presubmit_rejects_disabled_evidence_upload() -> None:
    workflow = workflow_yaml.parse_workflow_text(_presubmit_workflow())
    upload = next(
        item
        for item in workflow["jobs"]["bazel"]["steps"]
        if item.get("name") == "Upload Bazel performance evidence"
    )
    upload["if"] = "${{ false }}"
    assert "[AFFECTED-WORKFLOW-006] Bazel evidence retention is invalid" in (
        check_affected_presubmit._presubmit_workflow_errors(workflow)
    )


def test_presubmit_rejects_governed_step_shell_override() -> None:
    workflow = workflow_yaml.parse_workflow_text(_presubmit_workflow())
    step = next(
        item
        for item in workflow["jobs"]["bazel"]["steps"]
        if item.get("name") == "Run event-governed Bazel validation"
    )
    step["shell"] = "bash -c 'exit 0; #' {0}"
    assert "[AFFECTED-WORKFLOW-005] governed Bazel command is invalid" in (
        check_affected_presubmit._presubmit_workflow_errors(workflow)
    )


@pytest.mark.parametrize("field", ["defaults", "env", "needs", "permissions"])
def test_presubmit_rejects_bazel_job_control_overrides(field: str) -> None:
    workflow = workflow_yaml.parse_workflow_text(_presubmit_workflow())
    workflow["jobs"]["bazel"][field] = {}
    assert "[AFFECTED-WORKFLOW-004] presubmit Bazel job is invalid" in (
        check_affected_presubmit._presubmit_workflow_errors(workflow)
    )


def test_presubmit_rejects_duplicate_or_disabled_checkout() -> None:
    workflow = workflow_yaml.parse_workflow_text(_presubmit_workflow())
    job = workflow["jobs"]["bazel"]
    checkout = next(item for item in job["steps"] if "uses" in item and "checkout@" in item["uses"])
    checkout["if"] = "${{ false }}"
    job["steps"].insert(
        -3,
        {
            "uses": check_affected_presubmit.CHECKOUT_ACTION,
            "with": {"persist-credentials": False, "fetch-depth": 0},
        },
    )
    assert "[AFFECTED-WORKFLOW-004] presubmit checkout is incomplete" in (
        check_affected_presubmit._presubmit_workflow_errors(workflow)
    )


def test_workflow_parser_redacts_invalid_utf8(tmp_path: Path) -> None:
    path = tmp_path / "workflow.yml"
    path.write_bytes(b"\xffsecret-workflow-content")
    with pytest.raises(workflow_yaml.WorkflowYamlError) as captured:
        workflow_yaml.parse_workflow(path)
    assert captured.value.code == "AFFECTED-WORKFLOW-001"
    assert "secret-workflow-content" not in str(captured.value)


def test_selection_policy_behavior_is_governed() -> None:
    assert check_affected_presubmit._selection_policy_errors() == []


def test_selection_policy_behavior_rejects_mutated_resolver(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setattr(
        check_affected_presubmit.affected,
        "resolve_selection_mode",
        lambda _mode, **_kwargs: "affected",
    )
    assert check_affected_presubmit._selection_policy_errors() == [
        "[AFFECTED-WORKFLOW-008] selection event policy is invalid"
    ]


def test_nightly_target_contract_rejects_duplicate_keys(tmp_path: Path) -> None:
    path = tmp_path / "targets.yaml"
    path.write_text(
        '{"schema_version":1,"mode":"full","mode":"full",'
        '"analysis_targets":["//..."],"test_targets":["//..."]}',
        encoding="utf-8",
    )
    assert check_affected_presubmit._nightly_target_errors(path) == [
        "[AFFECTED-WORKFLOW-007] nightly target contract is unreadable"
    ]


def _nightly_workflow() -> dict[str, object]:
    return {
        "name": "CPU nightly",
        "on": {
            "schedule": [{"cron": "17 5 * * *"}],
            "workflow_dispatch": {},
        },
        "permissions": {"actions": "read", "contents": "read"},
        "concurrency": {"group": "cpu-nightly-bazel", "cancel-in-progress": False},
        "jobs": {
            "bazel-nightly": {
                "name": "nightly Bazel / verdict",
                "if": "github.ref == 'refs/heads/main'",
                "runs-on": "ubuntu-24.04",
                "timeout-minutes": 90,
                "steps": [
                    {
                        "uses": check_affected_presubmit.CHECKOUT_ACTION,
                        "with": {"persist-credentials": False},
                    },
                    {
                        "name": "Analyze and test the complete configured graph",
                        "env": {"BASH_ENV": ""},
                        "run": " ".join(check_affected_presubmit.NIGHTLY_BAZEL_COMMAND),
                    },
                    {
                        "name": "Upload nightly Bazel evidence",
                        "if": "always()",
                        "uses": check_affected_presubmit.UPLOAD_ARTIFACT_ACTION,
                        "with": {
                            "name": "bazel-nightly-${{ github.run_id }}-${{ github.run_attempt }}",
                            "path": "${{ runner.temp }}/bazel-evidence/*",
                            "if-no-files-found": "warn",
                            "retention-days": 35,
                        },
                    },
                ],
            }
        },
    }


def test_nightly_workflow_contract_is_full_and_non_bypassable() -> None:
    assert check_affected_presubmit._nightly_workflow_errors(_nightly_workflow()) == []


def test_nightly_rejects_expression_continue_on_error() -> None:
    workflow = _nightly_workflow()
    job = workflow["jobs"]["bazel-nightly"]
    governed_step = next(
        item
        for item in job["steps"]
        if item.get("name") == "Analyze and test the complete configured graph"
    )
    governed_step["continue-on-error"] = "${{ true }}"
    assert "[AFFECTED-WORKFLOW-005] nightly Bazel command is invalid" in (
        check_affected_presubmit._nightly_workflow_errors(workflow)
    )


def test_pipeline_orchestration_is_exercised_behaviorally() -> None:
    assert check_affected_presubmit._presubmit_orchestration_errors() == []
    assert check_affected_presubmit._nightly_orchestration_errors() == []


def test_static_guard_rejects_noop_presubmit_main(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setattr(presubmit_pipeline, "main", lambda: 0)
    assert check_affected_presubmit._presubmit_orchestration_errors() == [
        "[AFFECTED-CODE-006] presubmit orchestration contract is invalid"
    ]


def test_static_guard_rejects_noop_nightly_main(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setattr(nightly_pipeline, "main", lambda: 0)
    assert check_affected_presubmit._nightly_orchestration_errors() == [
        "[AFFECTED-CODE-007] nightly orchestration contract is invalid"
    ]
