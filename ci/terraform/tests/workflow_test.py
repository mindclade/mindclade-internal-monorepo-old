# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import json
import pathlib
import re
import shlex

import yaml

ROOT = pathlib.Path(__file__).resolve().parents[3]
PRESUBMIT = (ROOT / ".github/workflows/presubmit.yml").read_text(encoding="utf-8")
NIGHTLY = (ROOT / ".github/workflows/nightly.yml").read_text(encoding="utf-8")
SECURITY = (ROOT / ".github/workflows/security.yml").read_text(encoding="utf-8")
CACHE_SHA = "55cc8345863c7cc4c66a329aec7e433d2d1c52a9"
REUSABLE_SHA = "7e4b7a873fc9312c2985ed262b251455c71756fe"
AUTH_SHA = "7c6bc770dae815cd3e89ee6cdf493a5fab2cc093"
AUTH_ACTION = f"google-github-actions/auth@{AUTH_SHA}"
PULL_REQUEST_CACHE_BASE_REF = (
    "${{ github.event.pull_request.stack.base.ref || github.event.pull_request.base.ref }}"
)
PULL_REQUEST_CACHE_BASE_SHA = (
    "${{ github.event.pull_request.stack.base.sha || github.event.pull_request.base.sha }}"
)
PULL_REQUEST_SELECTION_BASE_SHA = "${{ github.event.pull_request.base.sha }}"
PERSISTENT_CACHE_MEASURE_IF = (
    "always() && steps.bazel-remote-cache.outputs.enabled != 'true' "
    "&& steps.bazel-cache-trust.outcome == 'success'"
)
REMOTE_CACHE_ACTIVATION = json.loads(
    (ROOT / "ci/bazel_cache/activation.json").read_text(encoding="utf-8")
)
PRESUBMIT_WORKFLOW = yaml.safe_load(PRESUBMIT)
NIGHTLY_WORKFLOW = yaml.safe_load(NIGHTLY)


def workflow_job(workflow: object, name: str) -> dict[str, object]:
    assert isinstance(workflow, dict), "workflow must be a mapping"
    jobs = workflow.get("jobs")
    assert isinstance(jobs, dict), "workflow jobs must be a mapping"
    job = jobs.get(name)
    assert isinstance(job, dict), f"workflow job {name!r} must be a mapping"
    return job


def workflow_steps(job: dict[str, object]) -> list[dict[str, object]]:
    raw_steps = job.get("steps")
    assert isinstance(raw_steps, list), "workflow job steps must be a list"
    assert all(isinstance(step, dict) for step in raw_steps), "workflow steps must be mappings"
    return raw_steps


def workflow_step(
    job: dict[str, object], *, step_id: str | None = None, name: str | None = None
) -> dict[str, object]:
    assert step_id is not None or name is not None, "step lookup requires an id or name"
    matches = [
        step
        for step in workflow_steps(job)
        if (step_id is None or step.get("id") == step_id)
        and (name is None or step.get("name") == name)
    ]
    assert len(matches) == 1, f"expected one workflow step id={step_id!r} name={name!r}"
    return matches[0]


def python_invocation(step: dict[str, object]) -> list[str]:
    script = step.get("run")
    assert isinstance(script, str), "Python workflow step must have a run script"
    tokens = shlex.split(script.replace("\\\n", " "), comments=True, posix=True)
    starts = [index for index, token in enumerate(tokens) if token == "python3"]
    assert len(starts) == 1, "remote-cache workflow step must invoke Python exactly once"
    return tokens[starts[0] :]


def assert_remote_cache_command(
    step: dict[str, object], command: str, arguments: list[str]
) -> None:
    assert python_invocation(step) == [
        "python3",
        "ci/common/bazel_remote_cache.py",
        command,
        *arguments,
    ]


def assert_auth_step(
    job: dict[str, object], *, step_id: str, name: str, role: str, service_account: str
) -> None:
    step = workflow_step(job, step_id=step_id)
    assert step == {
        "name": name,
        "id": step_id,
        "if": (
            "steps.bazel-remote-cache.outputs.enabled == 'true' && "
            f"steps.bazel-remote-cache.outputs.role == '{role}'"
        ),
        "uses": AUTH_ACTION,
        "with": {
            "cleanup_credentials": True,
            "create_credentials_file": True,
            "export_environment_variables": False,
            "project_id": "${{ vars.CI_PROJECT_ID }}",
            "service_account": service_account,
            "workload_identity_provider": "${{ vars.WIF_PROVIDER_BAZEL_CACHE }}",
        },
    }


def assert_only_expected_auth_steps(job: dict[str, object], expected_ids: set[str]) -> None:
    auth_steps = []
    for step in workflow_steps(job):
        action = step.get("uses")
        if isinstance(action, str) and action.partition("@")[0] == "google-github-actions/auth":
            auth_steps.append(step)
    assert {step.get("id") for step in auth_steps} == expected_ids
    assert all(step.get("uses") == AUTH_ACTION for step in auth_steps)


for job in ("architecture", "lint", "terraform"):
    pattern = rf"(?m)^  {job}:\n    name: {job}$"
    assert re.search(pattern, PRESUBMIT), f"{job} must have a stable id and display name"

terraform_job = PRESUBMIT.split("\n  terraform:\n", maxsplit=1)[1]
terraform_job = terraform_job.split("\n  # " + "-" * 10, maxsplit=1)[0]
assert "fetch-depth: 0" in terraform_job
assert (
    'TERRAFORM_INTERFACE_BASE_REF: "${{ github.event.pull_request.base.sha || github.event.before }}"'
    in terraform_job
)

assert terraform_job.count(f"actions/cache/restore@{CACHE_SHA}") == 1
assert terraform_job.count(f"actions/cache/save@{CACHE_SHA}") == 1
assert "restore-keys:" not in terraform_job

cache_keys = re.findall(r"(?m)^          key: (tf-google-.+)$", terraform_job)
assert len(cache_keys) == 2 and cache_keys[0] == cache_keys[1]
assert "runner.os" in cache_keys[0] and "runner.arch" in cache_keys[0]
assert "provider-compatibility.toml" in cache_keys[0]
assert ".terraform.lock.hcl" in cache_keys[0]

save_step = PRESUBMIT.split("- name: Save trusted Terraform provider cache", maxsplit=1)[1]
save_step = save_step.split("\n      - name:", maxsplit=1)[0]
assert "github.event_name == 'push'" in save_step
assert "github.ref == 'refs/heads/main'" in save_step
assert "cache-hit != 'true'" in save_step

for workflow in (
    "reusable-go-ci.yml",
    "reusable-rust-ci.yml",
    "reusable-uv-ci.yml",
):
    assert f"{workflow}@{REUSABLE_SHA}" in PRESUBMIT
assert f"reusable-codeql.yml@{REUSABLE_SHA}" in SECURITY
assert "mindclade-org/.github" not in PRESUBMIT
assert "mindclade-org/.github" not in SECURITY
assert "build-mode: autobuild" in SECURITY
assert "@v1.1.0" not in PRESUBMIT
assert "@v1.1.0" not in SECURITY

python_audit_job = SECURITY.split("\n  python-dependency-audit:\n", maxsplit=1)[1]
python_audit_job = python_audit_job.split("\n  go-vulnerability:\n", maxsplit=1)[0]
assert python_audit_job.count("pip-audit==2.10.1") == 2
assert python_audit_job.count("--require-hashes") == 2
assert python_audit_job.count("--requirement requirements.lock.txt") == 2
assert python_audit_job.count("--vulnerability-service osv") == 1
assert "torch's Linux +cpu wheel" in python_audit_job

bazel_job = PRESUBMIT.split("\n  bazel:\n", maxsplit=1)[1]
bazel_job = bazel_job.split("\n  # Stable, always-reported", maxsplit=1)[0]
assert "timeout-minutes: 90" in bazel_job
affected_executor = (ROOT / "ci/common/affected.py").read_text(encoding="utf-8")
assert bazel_job.count(f"actions/cache/restore@{CACHE_SHA}") == 1
assert bazel_job.count(f"actions/cache/save@{CACHE_SHA}") == 1
for start, end in (
    ("- name: Restore trusted Bazel", "- name: Configure bounded Bazel"),
    ("- name: Measure bounded Bazel", "- name: Save trusted Bazel"),
    ("- name: Save trusted Bazel", "- name: Record Bazel"),
):
    assert (
        "continue-on-error: true" in bazel_job.split(start, maxsplit=1)[1].split(end, maxsplit=1)[0]
    )
assert "--build_event_json_file=" in affected_executor
assert "bazel-performance-${{ github.run_id }}-${{ github.run_attempt }}" in bazel_job
assert "tools/dev/bazelw query '//...' --config=ci" in bazel_job
assert re.search(r"smoke_test --config=ci\s*$", bazel_job, re.MULTILINE)

bazel_cache_keys = re.findall(
    r"(?m)^          key: (\$\{\{ steps\.bazel-cache-trust.+)$", bazel_job
)
assert len(bazel_cache_keys) == 2 and bazel_cache_keys[0] == bazel_cache_keys[1]
assert "bazel-cache-trust.outputs.primary-key" in bazel_cache_keys[0]
assert "restore-keys:" in bazel_job
for locked_input in (
    ".bazelversion",
    "MODULE.bazel",
    "MODULE.bazel.lock",
    "REPO.bazel",
    "flake.lock",
    "tools/build/nix/toolchain-manifest.json",
):
    assert locked_input in bazel_job
for trusted_base in (
    "github.event.pull_request.base.sha",
    "github.event.pull_request.base.ref",
    "github.event.merge_group.base_sha",
    "github.event.merge_group.base_ref",
):
    assert trusted_base in bazel_job
assert "github.event_name == 'push'" in bazel_job
assert "github.ref == 'refs/heads/main'" in bazel_job
assert "github.ref_protected == true" in bazel_job
for command in ("select-trust", "configure", "measure", "record-metrics"):
    assert f"ci/common/bazel_disk_cache.py {command}" in bazel_job
assert "steps.bazel-cache-size.outputs.within-limit == 'true'" in bazel_job
assert "steps.bazel-disk-cache.outcome" in bazel_job
assert "steps.bazel-cache-size.outcome" in bazel_job
assert "--bazel-wrapper" in bazel_job
assert REMOTE_CACHE_ACTIVATION["state"] == "blocked"
bazel_workflow_job = workflow_job(PRESUBMIT_WORKFLOW, "bazel")
assert bazel_workflow_job["permissions"] == {"contents": "read"}

presubmit_selector = workflow_step(bazel_workflow_job, step_id="bazel-remote-cache")
assert {key: value for key, value in presubmit_selector.items() if key != "run"} == {
    "name": "Select qualified Bazel remote-cache route",
    "id": "bazel-remote-cache",
    "env": {
        "BAZEL_REMOTE_CACHE_STATE": "${{ vars.BAZEL_REMOTE_CACHE_STATE }}",
        "CI_PROJECT_ID": "${{ vars.CI_PROJECT_ID }}",
        "MERGE_GROUP_BASE_REF": "${{ github.event.merge_group.base_ref }}",
        "PR_BASE_REF": PULL_REQUEST_CACHE_BASE_REF,
        "REF_PROTECTED": "${{ github.ref_protected }}",
    },
}
assert_remote_cache_command(
    presubmit_selector,
    "select",
    [
        "--contract",
        "ci/bazel_cache/activation.json",
        "--repository-state",
        "${BAZEL_REMOTE_CACHE_STATE}",
        "--workflow",
        "presubmit",
        "--event",
        "${GITHUB_EVENT_NAME}",
        "--ref",
        "${GITHUB_REF}",
        "--ref-protected",
        "${REF_PROTECTED}",
        "--project-id",
        "${CI_PROJECT_ID}",
        "--pull-request-base-ref",
        "${PR_BASE_REF}",
        "--merge-group-base-ref",
        "${MERGE_GROUP_BASE_REF}",
        "--github-output",
        "${GITHUB_OUTPUT}",
    ],
)

presubmit_disk_selector = workflow_step(bazel_workflow_job, step_id="bazel-cache-trust")
presubmit_disk_selector_env = presubmit_disk_selector.get("env")
assert isinstance(presubmit_disk_selector_env, dict)
assert presubmit_disk_selector_env["PR_BASE_REF"] == PULL_REQUEST_CACHE_BASE_REF
assert presubmit_disk_selector_env["PR_BASE_SHA"] == PULL_REQUEST_CACHE_BASE_SHA

presubmit_governed_run = workflow_step(
    bazel_workflow_job, name="Run event-governed Bazel validation"
)
presubmit_governed_env = presubmit_governed_run.get("env")
assert isinstance(presubmit_governed_env, dict)
assert presubmit_governed_env["PR_BASE_SHA"] == PULL_REQUEST_SELECTION_BASE_SHA

presubmit_cache_measure = workflow_step(bazel_workflow_job, step_id="bazel-cache-size")
assert presubmit_cache_measure.get("if") == PERSISTENT_CACHE_MEASURE_IF

assert_only_expected_auth_steps(
    bazel_workflow_job, {"bazel-cache-reader-auth", "bazel-cache-writer-auth"}
)
assert_auth_step(
    bazel_workflow_job,
    step_id="bazel-cache-reader-auth",
    name="Authenticate read-only Bazel cache route",
    role="reader",
    service_account="${{ vars.SA_BAZEL_CACHE_READER }}",
)
assert_auth_step(
    bazel_workflow_job,
    step_id="bazel-cache-writer-auth",
    name="Authenticate trusted Bazel cache writer route",
    role="writer",
    service_account="${{ vars.SA_BAZEL_CACHE_WRITER }}",
)

presubmit_start = workflow_step(bazel_workflow_job, step_id="bazel-remote-cache-start")
assert {key: value for key, value in presubmit_start.items() if key != "run"} == {
    "name": "Start loopback Bazel GCS cache gateway",
    "id": "bazel-remote-cache-start",
    "if": "steps.bazel-remote-cache.outputs.enabled == 'true'",
    "env": {
        "CACHE_BINARY": "${{ steps.bazel-remote-cache-binary.outputs.path }}",
        "CACHE_BUCKET": "${{ steps.bazel-remote-cache.outputs.bucket }}",
        "CACHE_CREDENTIALS_FILE": (
            "${{ steps.bazel-cache-reader-auth.outputs.credentials_file_path || "
            "steps.bazel-cache-writer-auth.outputs.credentials_file_path }}"
        ),
        "CACHE_ROLE": "${{ steps.bazel-remote-cache.outputs.role }}",
        "CACHE_SERVICE_ACCOUNT": (
            "${{ steps.bazel-remote-cache.outputs.role == 'reader' && "
            "vars.SA_BAZEL_CACHE_READER || vars.SA_BAZEL_CACHE_WRITER }}"
        ),
        "CI_PROJECT_ID": "${{ vars.CI_PROJECT_ID }}",
        "WIF_PROVIDER_BAZEL_CACHE": "${{ vars.WIF_PROVIDER_BAZEL_CACHE }}",
    },
}
assert_remote_cache_command(
    presubmit_start,
    "start",
    [
        "--binary",
        "${CACHE_BINARY}",
        "--workspace",
        "${GITHUB_WORKSPACE}",
        "--bazelrc",
        "${GITHUB_WORKSPACE}/user.bazelrc",
        "--runtime-dir",
        "${RUNNER_TEMP}/mindclade-bazel-remote-cache",
        "--github-env",
        "${GITHUB_ENV}",
        "--bucket",
        "${CACHE_BUCKET}",
        "--role",
        "${CACHE_ROLE}",
        "--project-id",
        "${CI_PROJECT_ID}",
        "--provider",
        "${WIF_PROVIDER_BAZEL_CACHE}",
        "--service-account",
        "${CACHE_SERVICE_ACCOUNT}",
        "--credentials-file",
        "${CACHE_CREDENTIALS_FILE}",
    ],
)

presubmit_record = workflow_step(
    bazel_workflow_job, name="Record Bazel GCS remote-cache metrics and stop gateway"
)
assert {key: value for key, value in presubmit_record.items() if key != "run"} == {
    "name": "Record Bazel GCS remote-cache metrics and stop gateway",
    "if": (
        "always() && steps.bazel-remote-cache.outputs.enabled == 'true' && "
        "steps.bazel-remote-cache-start.outcome == 'success'"
    ),
    "env": {"CACHE_ROLE": "${{ steps.bazel-remote-cache.outputs.role }}"},
}
assert_remote_cache_command(
    presubmit_record,
    "record-stop",
    [
        "--runtime-dir",
        "${RUNNER_TEMP}/mindclade-bazel-remote-cache",
        "--evidence-dir",
        "${RUNNER_TEMP}/bazel-evidence",
        "--summary",
        "${GITHUB_STEP_SUMMARY}",
        "--role",
        "${CACHE_ROLE}",
    ],
)

nightly_job = NIGHTLY.split("\n  bazel-nightly:\n", maxsplit=1)[1]
assert "if: github.ref == 'refs/heads/main'" in nightly_job
assert nightly_job.count(f"actions/cache/restore@{CACHE_SHA}") == 1
assert nightly_job.count(f"actions/cache/save@{CACHE_SHA}") == 1
for start, end in (
    ("- name: Restore trusted nightly Bazel", "- name: Configure bounded nightly Bazel"),
    ("- name: Measure bounded nightly Bazel", "- name: Save trusted nightly Bazel"),
    ("- name: Save trusted nightly Bazel", "- name: Record nightly Bazel"),
):
    assert (
        "continue-on-error: true"
        in nightly_job.split(start, maxsplit=1)[1].split(end, maxsplit=1)[0]
    )
nightly_cache_keys = re.findall(
    r"(?m)^          key: (\$\{\{ steps\.bazel-cache-trust.+)$", nightly_job
)
assert len(nightly_cache_keys) == 2 and nightly_cache_keys[0] == nightly_cache_keys[1]
assert "restore-keys:" in nightly_job
assert "github.event_name == 'schedule'" in nightly_job
assert "github.event_name == 'workflow_dispatch'" in nightly_job
assert "github.ref_protected == true" in nightly_job
assert "steps.bazel-cache-trust.outputs.role == 'writer'" in nightly_job
assert "steps.bazel-cache-size.outputs.within-limit == 'true'" in nightly_job
assert "steps.bazel-disk-cache.outcome" in nightly_job
assert "steps.bazel-cache-size.outcome" in nightly_job
assert "--bazel-wrapper" in nightly_job
for command in ("select-trust", "configure", "measure", "record-metrics"):
    assert f"ci/common/bazel_disk_cache.py {command}" in nightly_job
assert "ci/nightly/pipeline.py" in nightly_job
assert "--mode affected" not in nightly_job
nightly_workflow_job = workflow_job(NIGHTLY_WORKFLOW, "bazel-nightly")
assert nightly_workflow_job["if"] == "github.ref == 'refs/heads/main'"
assert nightly_workflow_job["permissions"] == {"actions": "read", "contents": "read"}

nightly_selector = workflow_step(nightly_workflow_job, step_id="bazel-remote-cache")
assert {key: value for key, value in nightly_selector.items() if key != "run"} == {
    "name": "Select qualified nightly Bazel remote-cache route",
    "id": "bazel-remote-cache",
    "env": {
        "BAZEL_REMOTE_CACHE_STATE": "${{ vars.BAZEL_REMOTE_CACHE_STATE }}",
        "CI_PROJECT_ID": "${{ vars.CI_PROJECT_ID }}",
        "REF_PROTECTED": "${{ github.ref_protected }}",
    },
}
assert_remote_cache_command(
    nightly_selector,
    "select",
    [
        "--contract",
        "ci/bazel_cache/activation.json",
        "--repository-state",
        "${BAZEL_REMOTE_CACHE_STATE}",
        "--workflow",
        "nightly",
        "--event",
        "${GITHUB_EVENT_NAME}",
        "--ref",
        "${GITHUB_REF}",
        "--ref-protected",
        "${REF_PROTECTED}",
        "--project-id",
        "${CI_PROJECT_ID}",
        "--github-output",
        "${GITHUB_OUTPUT}",
    ],
)

assert_only_expected_auth_steps(nightly_workflow_job, {"bazel-cache-writer-auth"})
assert_auth_step(
    nightly_workflow_job,
    step_id="bazel-cache-writer-auth",
    name="Authenticate scheduled Bazel cache writer route",
    role="writer",
    service_account="${{ vars.SA_BAZEL_CACHE_WRITER }}",
)

nightly_start = workflow_step(nightly_workflow_job, step_id="bazel-remote-cache-start")
assert {key: value for key, value in nightly_start.items() if key != "run"} == {
    "name": "Start scheduled loopback Bazel GCS cache gateway",
    "id": "bazel-remote-cache-start",
    "if": "steps.bazel-remote-cache.outputs.enabled == 'true'",
    "env": {
        "CACHE_BINARY": "${{ steps.bazel-remote-cache-binary.outputs.path }}",
        "CACHE_BUCKET": "${{ steps.bazel-remote-cache.outputs.bucket }}",
        "CACHE_CREDENTIALS_FILE": (
            "${{ steps.bazel-cache-writer-auth.outputs.credentials_file_path }}"
        ),
        "CACHE_ROLE": "${{ steps.bazel-remote-cache.outputs.role }}",
        "CI_PROJECT_ID": "${{ vars.CI_PROJECT_ID }}",
        "SA_BAZEL_CACHE_WRITER": "${{ vars.SA_BAZEL_CACHE_WRITER }}",
        "WIF_PROVIDER_BAZEL_CACHE": "${{ vars.WIF_PROVIDER_BAZEL_CACHE }}",
    },
}
assert_remote_cache_command(
    nightly_start,
    "start",
    [
        "--binary",
        "${CACHE_BINARY}",
        "--workspace",
        "${GITHUB_WORKSPACE}",
        "--bazelrc",
        "${GITHUB_WORKSPACE}/user.bazelrc",
        "--runtime-dir",
        "${RUNNER_TEMP}/mindclade-bazel-remote-cache",
        "--github-env",
        "${GITHUB_ENV}",
        "--bucket",
        "${CACHE_BUCKET}",
        "--role",
        "${CACHE_ROLE}",
        "--project-id",
        "${CI_PROJECT_ID}",
        "--provider",
        "${WIF_PROVIDER_BAZEL_CACHE}",
        "--service-account",
        "${SA_BAZEL_CACHE_WRITER}",
        "--credentials-file",
        "${CACHE_CREDENTIALS_FILE}",
    ],
)

nightly_record = workflow_step(
    nightly_workflow_job, name="Record nightly Bazel GCS remote-cache metrics and stop gateway"
)
assert {key: value for key, value in nightly_record.items() if key != "run"} == {
    "name": "Record nightly Bazel GCS remote-cache metrics and stop gateway",
    "if": (
        "always() && steps.bazel-remote-cache.outputs.enabled == 'true' && "
        "steps.bazel-remote-cache-start.outcome == 'success'"
    ),
    "env": {"CACHE_ROLE": "${{ steps.bazel-remote-cache.outputs.role }}"},
}
assert_remote_cache_command(
    nightly_record,
    "record-stop",
    [
        "--runtime-dir",
        "${RUNNER_TEMP}/mindclade-bazel-remote-cache",
        "--evidence-dir",
        "${RUNNER_TEMP}/bazel-evidence",
        "--summary",
        "${GITHUB_STEP_SUMMARY}",
        "--role",
        "${CACHE_ROLE}",
    ],
)

print("Terraform and Bazel workflow trust-boundary assertions passed.")
