# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

import copy
import json
import os
import shutil
import subprocess
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


def test_activation_contract_requires_external_workflow_blocker(tmp_path: Path) -> None:
    source = ROOT / "ci/common/affected_global_inputs.json"
    payload = json.loads(source.read_text(encoding="utf-8"))
    payload["activation"]["blockers"].remove("external_required_workflow_not_active")
    candidate = tmp_path / "affected_global_inputs.json"
    candidate.write_text(json.dumps(payload), encoding="utf-8")
    assert check_affected_presubmit._activation_errors(candidate) == [
        "[AFFECTED-GLOBAL-009] graph-native blockers are invalid"
    ]


def _presubmit_workflow() -> dict[str, object]:
    return workflow_yaml.parse_workflow(ROOT / ".github/workflows/presubmit.yml")


def test_structural_workflow_parser_accepts_governed_event_routing() -> None:
    assert check_affected_presubmit._presubmit_workflow_errors(_presubmit_workflow()) == []


def test_checkout_sanitizer_preserves_only_reviewed_authority(tmp_path: Path) -> None:
    nightly = workflow_yaml.parse_workflow(ROOT / ".github/workflows/nightly.yml")
    sanitizer_steps = [
        next(
            step
            for step in workflow["jobs"][job_id]["steps"]
            if step.get("name") == "Remove ignored checkout byproducts"
        )
        for workflow, job_id in (
            (_presubmit_workflow(), "bazel-workers"),
            (nightly, "bazel-nightly-workers"),
        )
    ]
    assert sanitizer_steps[0] == sanitizer_steps[1]
    assert set(sanitizer_steps[0]) == {"name", "run"}

    repository = tmp_path / "repository"
    outputs = tmp_path / "outputs"
    trusted_git_value = os.environ.get("MINDCLADE_GIT") or shutil.which("git")
    assert trusted_git_value is not None
    trusted_git = Path(trusted_git_value)
    environment = os.environ | {
        "PATH": f"{trusted_git.parent}:{os.environ.get('PATH', '')}",
    }
    repository.mkdir()
    outputs.mkdir()
    (repository / ".gitignore").write_text(
        "/user.bazelrc\n/bazel-*\n/ignored-cache/\n",
        encoding="utf-8",
    )
    subprocess.run(
        ["git", "init", "--quiet"],
        cwd=repository,
        env=environment,
        check=True,
    )
    for name in ("bazel-bin", "bazel-out", "bazel-testlogs", "bazel-repository"):
        (repository / name).symlink_to(outputs, target_is_directory=True)
    (repository / "ignored-cache").mkdir()
    (repository / "ignored-cache/cache-entry").write_text("ignored\n", encoding="utf-8")
    (repository / "user.bazelrc").write_text("build --disk_cache=/cache\n", encoding="utf-8")
    (repository / "unreviewed.txt").write_text("must remain visible\n", encoding="utf-8")

    subprocess.run(
        ["bash", "-c", sanitizer_steps[0]["run"]],
        cwd=repository,
        env=environment,
        check=True,
    )

    assert (repository / "user.bazelrc").is_file()
    assert (repository / "unreviewed.txt").is_file()
    assert not (repository / "ignored-cache").exists()
    assert not any(
        (repository / name).exists()
        for name in (
            "bazel-bin",
            "bazel-out",
            "bazel-testlogs",
            "bazel-repository",
        )
    )


def test_presubmit_worker_plan_uses_pinned_python_before_stdlib_selectors() -> None:
    workflow = _presubmit_workflow()
    plan = workflow["jobs"]["bazel-worker-plan"]
    steps = plan["steps"]
    setup_index = next(
        index
        for index, step in enumerate(steps)
        if str(step.get("uses", "")).startswith("actions/setup-python@")
    )
    selector_indices = [
        index
        for index, step in enumerate(steps)
        if step.get("name")
        in {
            "Select qualified Bazel remote-cache route for topology",
            "Select presubmit, fallback, or complete shard workers",
        }
    ]
    assert selector_indices and all(setup_index < index for index in selector_indices)
    assert all("nix " not in steps[index]["run"] for index in selector_indices)


def test_presubmit_worker_plan_rejects_selector_before_toolchain() -> None:
    workflow = _presubmit_workflow()
    steps = workflow["jobs"]["bazel-worker-plan"]["steps"]
    setup_index = next(
        index
        for index, step in enumerate(steps)
        if str(step.get("uses", "")).startswith("actions/setup-python@")
    )
    selector_index = next(
        index
        for index, step in enumerate(steps)
        if step.get("name") == "Select qualified Bazel remote-cache route for topology"
    )
    steps[setup_index], steps[selector_index] = steps[selector_index], steps[setup_index]
    assert "[AFFECTED-WORKFLOW-004] presubmit Bazel plan toolchain is invalid" in (
        check_affected_presubmit._presubmit_workflow_errors(workflow)
    )


def test_workflow_comment_cannot_satisfy_governed_command() -> None:
    workflow = _presubmit_workflow()
    step = next(
        item
        for item in workflow["jobs"]["bazel-workers"]["steps"]
        if item.get("name") == "Run event-governed Bazel validation"
    )
    step["run"] = f"# {step['run']}\necho unsafe"
    assert "[AFFECTED-WORKFLOW-009] presubmit Bazel worker steps drifted" in (
        check_affected_presubmit._presubmit_workflow_errors(workflow)
    )


def test_workflow_rejects_alternate_selection_mode() -> None:
    workflow = _presubmit_workflow()
    step = next(
        item
        for item in workflow["jobs"]["bazel-workers"]["steps"]
        if item.get("name") == "Run event-governed Bazel validation"
    )
    step["run"] = step["run"].replace("--mode auto", "--mode affected")
    assert "[AFFECTED-WORKFLOW-005] governed Bazel command is invalid" in (
        check_affected_presubmit._presubmit_workflow_errors(workflow)
    )


@pytest.mark.parametrize(
    ("field", "value"),
    [
        ("BAZEL_CACHE_MODE", "disk"),
        ("BAZEL_CACHE_ROLE", "${{ steps.bazel-remote-cache.outputs.role }}"),
    ],
)
def test_presubmit_rejects_ungoverned_cache_route(field: str, value: str) -> None:
    workflow = _presubmit_workflow()
    step = next(
        item
        for item in workflow["jobs"]["bazel-workers"]["steps"]
        if item.get("name") == "Run event-governed Bazel validation"
    )
    step["env"][field] = value
    assert "[AFFECTED-WORKFLOW-005] governed Bazel command is invalid" in (
        check_affected_presubmit._presubmit_workflow_errors(workflow)
    )


@pytest.mark.parametrize("flag", ["--cache-mode", "--cache-role"])
def test_presubmit_requires_cache_route_arguments(flag: str) -> None:
    workflow = _presubmit_workflow()
    step = next(
        item
        for item in workflow["jobs"]["bazel-workers"]["steps"]
        if item.get("name") == "Run event-governed Bazel validation"
    )
    argument = "${BAZEL_CACHE_MODE}" if flag == "--cache-mode" else "${BAZEL_CACHE_ROLE}"
    step["run"] = step["run"].replace(f' {flag} "{argument}"', "")
    assert "[AFFECTED-WORKFLOW-005] governed Bazel command is invalid" in (
        check_affected_presubmit._presubmit_workflow_errors(workflow)
    )


def test_presubmit_routes_native_stacks_through_ultimate_protected_base() -> None:
    workflow = _presubmit_workflow()
    plan_steps = workflow["jobs"]["bazel-worker-plan"]["steps"]
    worker_steps = workflow["jobs"]["bazel-workers"]["steps"]
    plan_remote_route = next(
        item
        for item in plan_steps
        if item.get("name") == "Select qualified Bazel remote-cache route for topology"
    )
    worker_remote_route = next(
        item
        for item in worker_steps
        if item.get("name") == "Select qualified Bazel remote-cache route"
    )
    disk_route = next(
        item for item in worker_steps if item.get("name") == "Select trusted Bazel cache revision"
    )
    governed_run = next(
        item for item in worker_steps if item.get("name") == "Run event-governed Bazel validation"
    )
    disk_restore = next(
        item
        for item in worker_steps
        if item.get("name") == "Restore trusted Bazel persistent action cache"
    )
    disk_configure = next(
        item
        for item in worker_steps
        if item.get("name") == "Configure bounded Bazel persistent action cache"
    )

    assert "if" not in plan_remote_route
    assert plan_remote_route["env"]["PR_BASE_REF"] == (
        check_affected_presubmit.PULL_REQUEST_CACHE_BASE_REF
    )
    assert "if" not in worker_remote_route
    assert worker_remote_route["env"]["PR_BASE_REF"] == (
        check_affected_presubmit.PULL_REQUEST_CACHE_BASE_REF
    )
    assert disk_route["if"] == check_affected_presubmit.PERSISTENT_CACHE_TRUST_IF
    assert disk_route["env"]["PR_BASE_REF"] == (
        check_affected_presubmit.PULL_REQUEST_CACHE_BASE_REF
    )
    assert disk_route["env"]["PR_BASE_SHA"] == (
        check_affected_presubmit.PULL_REQUEST_CACHE_BASE_SHA
    )
    assert governed_run["env"]["PR_BASE_SHA"] == (
        check_affected_presubmit.PULL_REQUEST_SELECTION_BASE_SHA
    )
    assert disk_restore["if"] == check_affected_presubmit.PERSISTENT_CACHE_RESTORE_IF
    assert disk_configure["env"]["BAZEL_CACHE_ROLE"] == (
        check_affected_presubmit.PERSISTENT_CACHE_ROLE_EXPRESSION
    )
    assert governed_run["env"]["BAZEL_CACHE_ROLE"] == (
        check_affected_presubmit.GOVERNED_CACHE_ROLE_EXPRESSION
    )
    assert governed_run["env"]["REMOTE_CACHE_ENABLED"] == (
        check_affected_presubmit.REMOTE_CACHE_ENABLED_EXPRESSION
    )


@pytest.mark.parametrize(
    ("job_id", "step_name", "field", "mutated_value"),
    [
        (
            "bazel-worker-plan",
            "Select qualified Bazel remote-cache route for topology",
            "PR_BASE_REF",
            "${{ github.event.pull_request.stack.base.ref }}",
        ),
        (
            "bazel-workers",
            "Select qualified Bazel remote-cache route",
            "PR_BASE_REF",
            "${{ github.event.pull_request.stack.base.ref }}",
        ),
        (
            "bazel-workers",
            "Select trusted Bazel cache revision",
            "PR_BASE_REF",
            "${{ github.event.pull_request.stack.base.ref }}",
        ),
        (
            "bazel-workers",
            "Select trusted Bazel cache revision",
            "PR_BASE_SHA",
            "${{ github.event.pull_request.stack.base.sha }}",
        ),
        (
            "bazel-workers",
            "Run event-governed Bazel validation",
            "PR_BASE_SHA",
            "${{ github.event.pull_request.stack.base.sha }}",
        ),
    ],
)
def test_presubmit_rejects_cache_or_selection_base_drift(
    job_id: str,
    step_name: str,
    field: str,
    mutated_value: str,
) -> None:
    workflow = _presubmit_workflow()
    step = next(item for item in workflow["jobs"][job_id]["steps"] if item.get("name") == step_name)
    step["env"][field] = mutated_value
    assert "[AFFECTED-WORKFLOW-011] presubmit cache routing is invalid" in (
        check_affected_presubmit._presubmit_workflow_errors(workflow)
    )


@pytest.mark.parametrize(
    ("job_id", "step_name", "field", "mutated_value"),
    [
        (
            "bazel-worker-plan",
            "Select qualified Bazel remote-cache route for topology",
            "if",
            "always()",
        ),
        (
            "bazel-worker-plan",
            "Select presubmit, fallback, or complete shard workers",
            "REMOTE_CACHE_ENABLED",
            "${{ steps.bazel-remote-cache.outputs.enabled }}",
        ),
        (
            "bazel-workers",
            "Select qualified Bazel remote-cache route",
            "if",
            "always()",
        ),
        (
            "bazel-workers",
            "Verify worker topology still matches cache qualification",
            "ACTUAL_REMOTE_CACHE_ENABLED",
            "${{ steps.bazel-remote-cache.outputs.enabled }}",
        ),
        (
            "bazel-workers",
            "Select trusted Bazel cache revision",
            "if",
            "steps.bazel-remote-cache.outputs.enabled != 'true' && "
            "(github.event_name != 'pull_request' || "
            "github.event.pull_request.base.ref == 'main')",
        ),
        (
            "bazel-workers",
            "Restore trusted Bazel persistent action cache",
            "if",
            "steps.bazel-remote-cache.outputs.enabled != 'true'",
        ),
        (
            "bazel-workers",
            "Configure bounded Bazel persistent action cache",
            "BAZEL_CACHE_ROLE",
            "${{ steps.bazel-cache-trust.outputs.role }}",
        ),
        (
            "bazel-workers",
            "Run event-governed Bazel validation",
            "BAZEL_CACHE_ROLE",
            "${{ steps.bazel-cache-trust.outputs.role }}",
        ),
        (
            "bazel-workers",
            "Run event-governed Bazel validation",
            "REMOTE_CACHE_ENABLED",
            "${{ steps.bazel-remote-cache.outputs.enabled }}",
        ),
    ],
)
def test_presubmit_rejects_native_stack_cache_trust_drift(
    job_id: str,
    step_name: str,
    field: str,
    mutated_value: str,
) -> None:
    workflow = _presubmit_workflow()
    step = next(item for item in workflow["jobs"][job_id]["steps"] if item.get("name") == step_name)
    if field == "if":
        step[field] = mutated_value
    else:
        step["env"][field] = mutated_value
    assert "[AFFECTED-WORKFLOW-011] presubmit cache routing is invalid" in (
        check_affected_presubmit._presubmit_workflow_errors(workflow)
    )


def test_presubmit_topology_output_fails_closed_when_remote_selection_is_skipped() -> None:
    workflow = _presubmit_workflow()
    outputs = workflow["jobs"]["bazel-worker-plan"]["outputs"]
    assert outputs["remote_cache_enabled"] == (
        check_affected_presubmit.REMOTE_CACHE_ENABLED_EXPRESSION
    )

    outputs["remote_cache_enabled"] = "${{ steps.bazel-remote-cache.outputs.enabled }}"
    assert "[AFFECTED-WORKFLOW-004] presubmit Bazel plan is invalid" in (
        check_affected_presubmit._presubmit_workflow_errors(workflow)
    )


def test_presubmit_skips_disk_cache_measurement_after_trust_rejection() -> None:
    workflow = _presubmit_workflow()
    step = next(
        item
        for item in workflow["jobs"]["bazel-workers"]["steps"]
        if item.get("name") == "Measure bounded Bazel persistent action cache"
    )
    assert step["if"] == check_affected_presubmit.PERSISTENT_CACHE_MEASURE_IF

    step["if"] = "always() && steps.bazel-remote-cache.outputs.enabled != 'true'"
    assert "[AFFECTED-WORKFLOW-011] presubmit cache routing is invalid" in (
        check_affected_presubmit._presubmit_workflow_errors(workflow)
    )


@pytest.mark.parametrize(
    "step_name",
    [
        "Select qualified Bazel remote-cache route",
        "Prove affected selection against the real Bazel graph",
        "Run event-governed Bazel validation",
    ],
)
def test_presubmit_rejects_checkout_local_python_bytecode(step_name: str) -> None:
    workflow = _presubmit_workflow()
    step = next(
        item for item in workflow["jobs"]["bazel-workers"]["steps"] if item.get("name") == step_name
    )
    step["run"] = step["run"].replace("python3 -B", "python3", 1)
    assert "[AFFECTED-WORKFLOW-012] presubmit Python launch is invalid" in (
        check_affected_presubmit._presubmit_workflow_errors(workflow)
    )


def test_workflow_parser_rejects_duplicate_keys() -> None:
    with pytest.raises(workflow_yaml.WorkflowYamlError) as captured:
        workflow_yaml.parse_workflow_text("name: first\nname: second\n")
    assert captured.value.code == "AFFECTED-WORKFLOW-002"


def test_workflow_parser_rejects_quoted_key_alias() -> None:
    with pytest.raises(workflow_yaml.WorkflowYamlError) as captured:
        workflow_yaml.parse_workflow_text('permissions:\n  contents: read\n"permissions": {}\n')
    assert captured.value.code == "AFFECTED-WORKFLOW-002"


def test_workflow_parser_preserves_block_scalar_indentation_and_content() -> None:
    shallow = workflow_yaml.parse_workflow_text(
        "jobs:\n  check:\n    steps:\n      - run: |\n          if true; then\n          echo unsafe # shell content\n\n"
    )
    nested = workflow_yaml.parse_workflow_text(
        "jobs:\n  check:\n    steps:\n      - run: |\n          if true; then\n            echo unsafe # shell content\n\n"
    )
    assert shallow["jobs"]["check"]["steps"][0]["run"] == (
        "if true; then\necho unsafe # shell content\n"
    )
    assert nested["jobs"]["check"]["steps"][0]["run"] == (
        "if true; then\n  echo unsafe # shell content\n"
    )


@pytest.mark.parametrize(
    "source",
    [
        "defaults: &defaults\n  shell: bash\njob: *defaults\n",
        "name: !!str governed\n",
    ],
)
def test_workflow_parser_rejects_yaml_indirection(source: str) -> None:
    with pytest.raises(workflow_yaml.WorkflowYamlError) as captured:
        workflow_yaml.parse_workflow_text(source)
    assert captured.value.code == "AFFECTED-WORKFLOW-001"


@pytest.mark.parametrize("scope", ["job", "governed-step"])
def test_presubmit_rejects_expression_continue_on_error(scope: str) -> None:
    workflow = _presubmit_workflow()
    job = workflow["jobs"]["bazel-workers"]
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
    workflow = _presubmit_workflow()
    upload = next(
        item
        for item in workflow["jobs"]["bazel-workers"]["steps"]
        if item.get("name") == "Upload Bazel performance evidence"
    )
    upload["if"] = "${{ false }}"
    assert "[AFFECTED-WORKFLOW-006] Bazel evidence retention is invalid" in (
        check_affected_presubmit._presubmit_workflow_errors(workflow)
    )


def test_presubmit_requires_redacted_selection_upload_and_central_download() -> None:
    workflow = _presubmit_workflow()
    workers = workflow["jobs"]["bazel-workers"]
    upload = next(
        item
        for item in workers["steps"]
        if item.get("name") == "Upload redacted Bazel worker selection"
    )
    redaction = next(
        item
        for item in workers["steps"]
        if item.get("name") == "Redact completed Bazel worker selection"
    )
    assert redaction["id"] == "bazel-selection-redact"
    assert redaction["if"] == "steps.bazel-validation.outcome == 'success'"
    assert upload["if"] == "steps.bazel-selection-redact.outcome == 'success'"
    assert upload["with"]["if-no-files-found"] == "error"
    assert upload["with"]["retention-days"] == 7
    verdict = workflow["jobs"]["bazel"]
    download = next(
        item
        for item in verdict["steps"]
        if item.get("name") == "Download redacted Bazel worker selections"
    )
    assert download["uses"] == check_affected_presubmit.DOWNLOAD_ARTIFACT_ACTION
    assert download["continue-on-error"] is True


@pytest.mark.parametrize("mutation", ["topology-output", "upload", "download", "aggregate"])
def test_presubmit_rejects_worker_evidence_route_drift(mutation: str) -> None:
    workflow = _presubmit_workflow()
    if mutation == "topology-output":
        del workflow["jobs"]["bazel-worker-plan"]["outputs"]["topology_mode"]
    elif mutation == "upload":
        upload = next(
            item
            for item in workflow["jobs"]["bazel-workers"]["steps"]
            if item.get("name") == "Upload redacted Bazel worker selection"
        )
        upload["with"]["if-no-files-found"] = "ignore"
    elif mutation == "download":
        download = next(
            item
            for item in workflow["jobs"]["bazel"]["steps"]
            if item.get("name") == "Download redacted Bazel worker selections"
        )
        download["with"]["pattern"] = "bazel-selection-*"
    else:
        aggregate = next(
            item
            for item in workflow["jobs"]["bazel"]["steps"]
            if item.get("name") == "Aggregate the complete Bazel verdict"
        )
        aggregate["run"] = "echo bypass"
    assert check_affected_presubmit._presubmit_workflow_errors(workflow)


def test_presubmit_rejects_governed_step_shell_override() -> None:
    workflow = _presubmit_workflow()
    step = next(
        item
        for item in workflow["jobs"]["bazel-workers"]["steps"]
        if item.get("name") == "Run event-governed Bazel validation"
    )
    step["shell"] = "bash -c 'exit 0; #' {0}"
    assert "[AFFECTED-WORKFLOW-005] governed Bazel command is invalid" in (
        check_affected_presubmit._presubmit_workflow_errors(workflow)
    )


@pytest.mark.parametrize("field", ["defaults", "env", "needs"])
def test_presubmit_rejects_bazel_job_control_overrides(field: str) -> None:
    workflow = _presubmit_workflow()
    workflow["jobs"]["bazel-workers"][field] = {}
    assert "[AFFECTED-WORKFLOW-004] presubmit Bazel workers are invalid" in (
        check_affected_presubmit._presubmit_workflow_errors(workflow)
    )


def test_presubmit_rejects_bazel_job_permission_escalation() -> None:
    workflow = _presubmit_workflow()
    workflow["jobs"]["bazel-workers"]["permissions"] = {
        "contents": "read",
        "id-token": "write",
    }
    assert "[AFFECTED-WORKFLOW-004] presubmit Bazel workers are invalid" in (
        check_affected_presubmit._presubmit_workflow_errors(workflow)
    )


def test_presubmit_rejects_duplicate_or_disabled_checkout() -> None:
    workflow = _presubmit_workflow()
    job = workflow["jobs"]["bazel-workers"]
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


@pytest.mark.parametrize("mutation", ["insert", "reorder", "action-sha", "duplicate-step"])
def test_presubmit_rejects_any_step_sequence_drift(mutation: str) -> None:
    workflow = _presubmit_workflow()
    steps = workflow["jobs"]["bazel-workers"]["steps"]
    governed_index = next(
        index
        for index, item in enumerate(steps)
        if item.get("name") == "Run event-governed Bazel validation"
    )
    if mutation == "insert":
        steps.insert(governed_index, {"name": "Unreviewed preparation", "run": "echo unsafe"})
    elif mutation == "reorder":
        steps[0], steps[1] = steps[1], steps[0]
    elif mutation == "action-sha":
        action = next(item for item in steps if str(item.get("uses", "")).startswith("actions/"))
        action["uses"] = "actions/checkout@0000000000000000000000000000000000000000"
    else:
        steps.insert(governed_index, copy.deepcopy(steps[governed_index]))
    assert "[AFFECTED-WORKFLOW-009] presubmit Bazel worker steps drifted" in (
        check_affected_presubmit._presubmit_workflow_errors(workflow)
    )


def test_presubmit_rejects_duplicate_or_spoofed_verdict_context() -> None:
    workflow = _presubmit_workflow()
    workflow["jobs"]["spoofed"] = {
        "name": "bazel / verdict",
        "runs-on": "ubuntu-24.04",
        "steps": [{"run": "true"}],
    }
    assert "[AFFECTED-WORKFLOW-004] presubmit verdict job is ambiguous" in (
        check_affected_presubmit._presubmit_workflow_errors(workflow)
    )


def test_repository_rejects_verdict_context_from_another_workflow() -> None:
    workflows = {
        ".github/workflows/presubmit.yml": _presubmit_workflow(),
        ".github/workflows/nightly.yml": _nightly_workflow(),
        ".github/workflows/spoof.yml": {
            "jobs": {"spoof": {"name": "bazel / verdict", "steps": [{"run": "true"}]}},
        },
    }
    assert check_affected_presubmit._verdict_context_errors(workflows) == [
        "[AFFECTED-WORKFLOW-010] Bazel verdict context is ambiguous"
    ]


def test_repository_rejects_dynamic_job_name_that_can_spoof_verdict_context() -> None:
    workflows = {
        ".github/workflows/presubmit.yml": _presubmit_workflow(),
        ".github/workflows/nightly.yml": _nightly_workflow(),
        ".github/workflows/spoof.yml": {
            "jobs": {
                "spoof": {
                    "name": "${{ format('{0} / verdict', 'bazel') }}",
                    "steps": [{"run": "true"}],
                }
            },
        },
    }
    assert check_affected_presubmit._verdict_context_errors(workflows) == [
        "[AFFECTED-WORKFLOW-010] Bazel verdict context is ambiguous"
    ]


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


def test_selection_policy_rejects_unauthorized_graph_native_activation(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setattr(check_affected_presubmit.affected, "GRAPH_NATIVE_AFFECTED_ACTIVE", True)
    assert check_affected_presubmit._selection_policy_errors() == [
        "[AFFECTED-WORKFLOW-008] selection event policy is invalid"
    ]


def test_nightly_target_contract_rejects_duplicate_keys(tmp_path: Path) -> None:
    path = tmp_path / "targets.yaml"
    path.write_text(
        '{"schema_version":2,"mode":"full","mode":"full",'
        '"shard_count":4,"partition_contract":"ci/bazel/full_graph_shards.toml"}',
        encoding="utf-8",
    )
    assert check_affected_presubmit._nightly_target_errors(path) == [
        "[AFFECTED-WORKFLOW-007] nightly target contract is unreadable"
    ]


def _nightly_workflow() -> dict[str, object]:
    return workflow_yaml.parse_workflow(ROOT / ".github/workflows/nightly.yml")


def test_nightly_workflow_contract_is_full_and_non_bypassable() -> None:
    assert check_affected_presubmit._nightly_workflow_errors(_nightly_workflow()) == []


def test_nightly_requires_redacted_selection_upload_and_central_download() -> None:
    workflow = _nightly_workflow()
    workers = workflow["jobs"]["bazel-nightly-workers"]
    upload = next(
        item
        for item in workers["steps"]
        if item.get("name") == "Upload redacted nightly Bazel worker selection"
    )
    assert upload["with"]["if-no-files-found"] == "error"
    verdict = workflow["jobs"]["bazel-nightly"]
    download = next(
        item
        for item in verdict["steps"]
        if item.get("name") == "Download redacted nightly Bazel worker selections"
    )
    assert download["uses"] == check_affected_presubmit.DOWNLOAD_ARTIFACT_ACTION
    assert download["continue-on-error"] is True


def test_nightly_worker_plan_uses_pinned_python_before_stdlib_selectors() -> None:
    workflow = _nightly_workflow()
    plan = workflow["jobs"]["bazel-nightly-plan"]
    steps = plan["steps"]
    setup_index = next(
        index
        for index, step in enumerate(steps)
        if str(step.get("uses", "")).startswith("actions/setup-python@")
    )
    selector_indices = [
        index
        for index, step in enumerate(steps)
        if step.get("name")
        in {
            "Select qualified nightly Bazel remote-cache route for topology",
            "Select fallback or complete shard workers",
        }
    ]
    assert selector_indices and all(setup_index < index for index in selector_indices)
    assert all("nix " not in steps[index]["run"] for index in selector_indices)


@pytest.mark.parametrize(
    ("field", "value"),
    [
        ("BAZEL_CACHE_MODE", "disk"),
        ("BAZEL_CACHE_ROLE", "${{ steps.bazel-remote-cache.outputs.role }}"),
    ],
)
def test_nightly_rejects_ungoverned_cache_route(field: str, value: str) -> None:
    workflow = _nightly_workflow()
    step = next(
        item
        for item in workflow["jobs"]["bazel-nightly-workers"]["steps"]
        if item.get("name") == "Analyze and test the selected repository-graph workload"
    )
    step["env"][field] = value
    assert "[AFFECTED-WORKFLOW-005] nightly Bazel command is invalid" in (
        check_affected_presubmit._nightly_workflow_errors(workflow)
    )


@pytest.mark.parametrize("flag", ["--cache-mode", "--cache-role"])
def test_nightly_requires_cache_route_arguments(flag: str) -> None:
    workflow = _nightly_workflow()
    step = next(
        item
        for item in workflow["jobs"]["bazel-nightly-workers"]["steps"]
        if item.get("name") == "Analyze and test the selected repository-graph workload"
    )
    argument = "${BAZEL_CACHE_MODE}" if flag == "--cache-mode" else "${BAZEL_CACHE_ROLE}"
    step["run"] = step["run"].replace(f' {flag} "{argument}"', "")
    assert "[AFFECTED-WORKFLOW-005] nightly Bazel command is invalid" in (
        check_affected_presubmit._nightly_workflow_errors(workflow)
    )


@pytest.mark.parametrize(
    "step_name",
    [
        "Select qualified nightly Bazel remote-cache route",
        "Validate complete loading, formatting, and layer policy",
        "Analyze and test the selected repository-graph workload",
    ],
)
def test_nightly_rejects_checkout_local_python_bytecode(step_name: str) -> None:
    workflow = _nightly_workflow()
    step = next(
        item
        for item in workflow["jobs"]["bazel-nightly-workers"]["steps"]
        if item.get("name") == step_name
    )
    step["run"] = step["run"].replace("python3 -B", "python3", 1)
    assert "[AFFECTED-WORKFLOW-012] nightly Python launch is invalid" in (
        check_affected_presubmit._nightly_workflow_errors(workflow)
    )


def test_nightly_rejects_expression_continue_on_error() -> None:
    workflow = _nightly_workflow()
    job = workflow["jobs"]["bazel-nightly-workers"]
    governed_step = next(
        item
        for item in job["steps"]
        if item.get("name") == "Analyze and test the selected repository-graph workload"
    )
    governed_step["continue-on-error"] = "${{ true }}"
    assert "[AFFECTED-WORKFLOW-005] nightly Bazel command is invalid" in (
        check_affected_presubmit._nightly_workflow_errors(workflow)
    )


@pytest.mark.parametrize("mutation", ["insert", "reorder", "action-sha", "duplicate-step"])
def test_nightly_rejects_any_step_sequence_drift(mutation: str) -> None:
    workflow = _nightly_workflow()
    steps = workflow["jobs"]["bazel-nightly-workers"]["steps"]
    governed_index = next(
        index
        for index, item in enumerate(steps)
        if item.get("name") == "Analyze and test the selected repository-graph workload"
    )
    if mutation == "insert":
        steps.insert(governed_index, {"name": "Unreviewed preparation", "run": "echo unsafe"})
    elif mutation == "reorder":
        steps[0], steps[1] = steps[1], steps[0]
    elif mutation == "action-sha":
        action = next(item for item in steps if str(item.get("uses", "")).startswith("actions/"))
        action["uses"] = "actions/checkout@0000000000000000000000000000000000000000"
    else:
        steps.insert(governed_index, copy.deepcopy(steps[governed_index]))
    assert "[AFFECTED-WORKFLOW-009] nightly Bazel worker steps drifted" in (
        check_affected_presubmit._nightly_workflow_errors(workflow)
    )


def test_nightly_rejects_permissions_or_spoofed_verdict_context() -> None:
    workflow = _nightly_workflow()
    workflow["permissions"] = {"contents": "read"}
    workflow["jobs"]["spoofed"] = {
        "name": "nightly Bazel / verdict",
        "runs-on": "ubuntu-24.04",
        "steps": [{"run": "true"}],
    }
    errors = check_affected_presubmit._nightly_workflow_errors(workflow)
    assert "[AFFECTED-WORKFLOW-004] nightly permissions are invalid" in errors
    assert "[AFFECTED-WORKFLOW-004] nightly verdict job is ambiguous" in errors


def test_nightly_rejects_bazel_job_permission_escalation() -> None:
    workflow = _nightly_workflow()
    workflow["jobs"]["bazel-nightly-workers"]["permissions"] = {
        "actions": "read",
        "contents": "read",
        "id-token": "write",
    }
    assert "[AFFECTED-WORKFLOW-004] nightly Bazel workers are invalid" in (
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
