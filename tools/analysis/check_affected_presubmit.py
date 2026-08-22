#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

import ast
import contextlib
import io
import json
import shlex
import subprocess
import sys
from pathlib import Path
from typing import Any
from unittest import mock

ROOT = Path(__file__).resolve().parents[2]
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))

from workflow_yaml import WorkflowYamlError, parse_workflow  # noqa: E402

from ci.common import affected  # noqa: E402
from ci.common.affected_contract import (  # noqa: E402
    ContractError,
    GlobalInputContract,
    load_global_input_contract,
    load_global_input_payload,
)
from ci.nightly import pipeline as nightly_pipeline  # noqa: E402
from ci.presubmit import pipeline as presubmit_pipeline  # noqa: E402

PRESUBMIT_EVENTS = frozenset({"merge_group", "pull_request", "push"})
NIGHTLY_EVENTS = frozenset({"schedule", "workflow_dispatch"})
PRESUBMIT_BAZEL_COMMAND = (
    "/nix/var/nix/profiles/default/bin/nix",
    "develop",
    ".#ci-bazel",
    "--command",
    "python3",
    "-I",
    "ci/presubmit/pipeline.py",
    "--bazel-only",
    "--mode",
    "auto",
    "--base",
    "${PR_BASE_SHA}",
    "--event",
    "${GITHUB_EVENT_NAME}",
    "--ref",
    "${GITHUB_REF}",
    "--head",
    "${GITHUB_SHA}",
    "--evidence-dir",
    "${RUNNER_TEMP}/bazel-evidence",
    "--job-started-at-file",
    "${RUNNER_TEMP}/bazel-job-started",
)
NIGHTLY_BAZEL_COMMAND = (
    "/nix/var/nix/profiles/default/bin/nix",
    "develop",
    ".#ci-bazel",
    "--command",
    "python3",
    "-I",
    "ci/nightly/pipeline.py",
    "--event",
    "${GITHUB_EVENT_NAME}",
    "--ref",
    "${GITHUB_REF}",
    "--head",
    "${GITHUB_SHA}",
    "--evidence-dir",
    "${RUNNER_TEMP}/bazel-evidence",
    "--job-started-at-file",
    "${RUNNER_TEMP}/bazel-job-started",
)
UPLOAD_ARTIFACT_ACTION = "actions/upload-artifact@ea165f8d65b6e75b540449e92b4886f43607fa02"
CHECKOUT_ACTION = "actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1"


def _error(code: str, message: str) -> str:
    return f"[{code}] {message}"


def _top_level_symbols(path: Path) -> set[str]:
    tree = ast.parse(path.read_text(encoding="utf-8"), filename=str(path))
    return {
        node.name
        for node in tree.body
        if isinstance(node, (ast.ClassDef, ast.FunctionDef, ast.AsyncFunctionDef))
    }


def _top_level_assignments(path: Path) -> set[str]:
    tree = ast.parse(path.read_text(encoding="utf-8"), filename=str(path))
    names: set[str] = set()
    for node in tree.body:
        if isinstance(node, ast.Assign):
            targets = node.targets
        elif isinstance(node, ast.AnnAssign):
            targets = [node.target]
        else:
            continue
        names.update(target.id for target in targets if isinstance(target, ast.Name))
    return names


def _imports(path: Path) -> set[str]:
    tree = ast.parse(path.read_text(encoding="utf-8"), filename=str(path))
    names: set[str] = set()
    for node in ast.walk(tree):
        if isinstance(node, ast.Import):
            names.update(alias.name for alias in node.names)
        elif isinstance(node, ast.ImportFrom) and node.module:
            names.add(node.module)
    return names


def _tracked_paths(root: Path) -> tuple[str, ...]:
    try:
        result = subprocess.run(
            ["git", "ls-files", "--cached", "--others", "--exclude-standard", "-z"],
            cwd=root,
            capture_output=True,
            check=False,
        )
    except OSError as error:
        raise ContractError("AFFECTED-GLOBAL-008", "tracked-path inventory failed") from error
    if result.returncode:
        raise ContractError("AFFECTED-GLOBAL-008", "tracked-path inventory failed")
    try:
        return tuple(
            sorted(
                field.decode("utf-8", errors="strict")
                for field in result.stdout.split(b"\0")
                if field
            )
        )
    except UnicodeError as error:
        raise ContractError("AFFECTED-GLOBAL-008", "tracked-path inventory failed") from error


def _boundary_entries(paths: tuple[str, ...], boundary: str) -> set[str]:
    if not boundary:
        return {path.split("/", 1)[0] for path in paths}
    prefix = f"{boundary}/"
    return {
        path[len(prefix) :].split("/", 1)[0]
        for path in paths
        if path.startswith(prefix) and path != prefix
    }


def _review_boundary_errors(contract: GlobalInputContract, paths: tuple[str, ...]) -> list[str]:
    errors: list[str] = []
    for boundary, expected_entries in contract.review_boundaries:
        expected = set(expected_entries)
        actual = _boundary_entries(paths, boundary)
        if actual - expected:
            errors.append(
                _error("AFFECTED-GLOBAL-006", "an unreviewed repository authority exists")
            )
        if expected - actual:
            errors.append(_error("AFFECTED-GLOBAL-007", "a reviewed authority is stale"))
    return errors


def _activation_errors(path: Path) -> list[str]:
    try:
        payload = load_global_input_payload(path)
    except ContractError as error:
        return [str(error)]
    activation = payload.get("activation")
    if not isinstance(activation, dict) or set(activation) != {
        "blockers",
        "release",
        "state",
        "tool",
    }:
        return [_error("AFFECTED-GLOBAL-009", "graph-native activation evidence is invalid")]
    errors: list[str] = []
    if activation.get("state") != "blocked":
        errors.append(
            _error(
                "AFFECTED-GLOBAL-009",
                "graph-native activation must remain blocked pending evidence",
            )
        )
    expected_blockers = [
        "bazel_version_parse_not_qualified",
        "full_graph_linux_not_qualified",
        "remote_cache_not_qualified",
        "workspace_restoration_not_hardened",
    ]
    if activation.get("blockers") != expected_blockers:
        errors.append(_error("AFFECTED-GLOBAL-009", "graph-native blockers are invalid"))
    release = activation.get("release")
    if not isinstance(release, dict) or set(release) != {"assets", "commit", "license", "tag"}:
        errors.append(_error("AFFECTED-GLOBAL-010", "graph-native release pin is invalid"))
        return errors
    if (
        activation.get("tool") != "bazel-contrib/target-determinator"
        or release.get("tag") != "v0.34.0"
        or release.get("commit") != "d4b6125546979713431e63b5c3e65810fa989446"
        or release.get("license") != "Apache-2.0"
    ):
        errors.append(_error("AFFECTED-GLOBAL-010", "graph-native release identity drifted"))
    assets = release.get("assets")
    expected_assets = {
        "aarch64-darwin": (
            "target-determinator.darwin.arm64",
            "1405ff844db1255fc1e10f28c04ed72ced648822c2a5d39a393a4d6a6b7b890d",
        ),
        "aarch64-linux": (
            "target-determinator.linux.arm64",
            "e818a59b1813ba4053eb0011a5302932cdc32a7879ae019ac4ef8f879c3953a9",
        ),
        "x86_64-linux": (
            "target-determinator.linux.amd64",
            "115e1c63d39e2cd0d0b011c9fadc80f059f021176a4ae0de2232cdd83b1f8011",
        ),
    }
    if not isinstance(assets, dict) or set(assets) != set(expected_assets):
        errors.append(_error("AFFECTED-GLOBAL-010", "graph-native release assets drifted"))
        return errors
    for system, (expected_name, expected_digest) in expected_assets.items():
        asset = assets.get(system)
        if (
            not isinstance(asset, dict)
            or set(asset) != {"name", "sha256"}
            or asset.get("name") != expected_name
            or asset.get("sha256") != expected_digest
        ):
            errors.append(_error("AFFECTED-GLOBAL-010", "a release asset pin is invalid"))
    return errors


def _mapping(value: Any) -> dict[str, Any] | None:
    if not isinstance(value, dict) or not all(isinstance(key, str) for key in value):
        return None
    return value


def _named_step(job: dict[str, Any], name: str) -> dict[str, Any] | None:
    steps = job.get("steps")
    if not isinstance(steps, list):
        return None
    matches = [step for step in steps if isinstance(step, dict) and step.get("name") == name]
    if len(matches) != 1:
        return None
    return matches[0]


def _command(value: Any) -> tuple[str, ...] | None:
    if not isinstance(value, str):
        return None
    try:
        return tuple(shlex.split(value, comments=False, posix=True))
    except ValueError:
        return None


def _checkout_is_complete(job: dict[str, Any], *, full_history: bool, before_step: str) -> bool:
    steps = job.get("steps")
    if not isinstance(steps, list):
        return False
    checkout_indices = [
        index
        for index, step in enumerate(steps)
        if isinstance(step, dict)
        and isinstance(step.get("uses"), str)
        and step["uses"].startswith("actions/checkout@")
    ]
    governed_indices = [
        index
        for index, step in enumerate(steps)
        if isinstance(step, dict) and step.get("name") == before_step
    ]
    if len(checkout_indices) != 1 or len(governed_indices) != 1:
        return False
    checkout_index = checkout_indices[0]
    checkout = steps[checkout_index]
    expected_configuration = {"persist-credentials": False}
    if full_history:
        expected_configuration["fetch-depth"] = 0
    return (
        checkout_index < governed_indices[0]
        and set(checkout) == {"uses", "with"}
        and checkout.get("uses") == CHECKOUT_ACTION
        and _mapping(checkout.get("with")) == expected_configuration
    )


def _uploads_are_governed(job: dict[str, Any], expected: dict[str, dict[str, Any]]) -> bool:
    for name, expected_configuration in expected.items():
        step = _named_step(job, name)
        configuration = _mapping(step.get("with")) if step is not None else None
        if (
            step is None
            or set(step) != {"if", "name", "uses", "with"}
            or step.get("if") != "always()"
            or "continue-on-error" in step
            or step.get("uses") != UPLOAD_ARTIFACT_ACTION
            or configuration != expected_configuration
        ):
            return False
    return True


def _presubmit_workflow_errors(workflow: dict[str, Any]) -> list[str]:
    errors: list[str] = []
    if set(workflow) != {"concurrency", "jobs", "name", "on", "permissions"}:
        errors.append(_error("AFFECTED-WORKFLOW-004", "presubmit workflow keys are invalid"))
    events = _mapping(workflow.get("on"))
    if events is None or set(events) != PRESUBMIT_EVENTS:
        errors.append(_error("AFFECTED-WORKFLOW-003", "presubmit event contract is invalid"))
    else:
        push = _mapping(events.get("push"))
        if (
            push is None
            or push.get("branches") != ["main"]
            or events.get("pull_request") != {}
            or events.get("merge_group") != {}
        ):
            errors.append(_error("AFFECTED-WORKFLOW-003", "presubmit event routing is invalid"))
    if workflow.get("permissions") != {"contents": "read"}:
        errors.append(_error("AFFECTED-WORKFLOW-004", "presubmit permissions are invalid"))
    jobs = _mapping(workflow.get("jobs"))
    bazel_job = _mapping(jobs.get("bazel")) if jobs is not None else None
    if (
        bazel_job is None
        or set(bazel_job) != {"name", "runs-on", "steps", "timeout-minutes"}
        or bazel_job.get("name") != "bazel / verdict"
        or bazel_job.get("runs-on") != "ubuntu-24.04"
        or bazel_job.get("timeout-minutes") != 90
    ):
        return [*errors, _error("AFFECTED-WORKFLOW-004", "presubmit Bazel job is invalid")]
    if not _checkout_is_complete(
        bazel_job,
        full_history=True,
        before_step="Run event-governed Bazel validation",
    ):
        errors.append(_error("AFFECTED-WORKFLOW-004", "presubmit checkout is incomplete"))
    step = _named_step(bazel_job, "Run event-governed Bazel validation")
    if step is None:
        errors.append(_error("AFFECTED-WORKFLOW-005", "governed Bazel step is missing"))
    elif (
        set(step) != {"env", "name", "run"}
        or step.get("env")
        != {
            "BASH_ENV": "",
            "PR_BASE_SHA": "${{ github.event.pull_request.base.sha }}",
        }
        or _command(step.get("run")) != PRESUBMIT_BAZEL_COMMAND
    ):
        errors.append(_error("AFFECTED-WORKFLOW-005", "governed Bazel command is invalid"))
    if not _uploads_are_governed(
        bazel_job,
        {
            "Upload Bazel performance evidence": {
                "name": "bazel-performance-${{ github.run_id }}-${{ github.run_attempt }}",
                "path": "${{ runner.temp }}/bazel-evidence/*",
                "if-no-files-found": "warn",
                "retention-days": 35,
            },
            "Upload Bazel latency metric": {
                "name": "bazel-metrics-${{ github.run_id }}-${{ github.run_attempt }}",
                "path": "${{ runner.temp }}/bazel-evidence/run-metrics.json",
                "if-no-files-found": "ignore",
                "retention-days": 35,
            },
        },
    ):
        errors.append(_error("AFFECTED-WORKFLOW-006", "Bazel evidence retention is invalid"))
    return errors


def _nightly_target_errors(path: Path) -> list[str]:
    def unique_object(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
        result: dict[str, Any] = {}
        for key, value in pairs:
            if key in result:
                raise ValueError("duplicate key")
            result[key] = value
        return result

    try:
        lines = [
            line
            for line in path.read_text(encoding="utf-8").splitlines()
            if line.strip() and not line.lstrip().startswith("#") and line.strip() != "---"
        ]
        payload = json.loads(
            "\n".join(lines),
            object_pairs_hook=unique_object,
            parse_constant=lambda _value: (_ for _ in ()).throw(ValueError("constant")),
        )
    except (OSError, UnicodeError, json.JSONDecodeError, RecursionError, ValueError):
        return [_error("AFFECTED-WORKFLOW-007", "nightly target contract is unreadable")]
    if payload != {
        "schema_version": 1,
        "mode": "full",
        "analysis_targets": ["//..."],
        "test_targets": ["//..."],
    }:
        return [_error("AFFECTED-WORKFLOW-007", "nightly target contract is not full graph")]
    return []


def _nightly_workflow_errors(workflow: dict[str, Any]) -> list[str]:
    errors: list[str] = []
    if set(workflow) != {"concurrency", "jobs", "name", "on", "permissions"}:
        errors.append(_error("AFFECTED-WORKFLOW-004", "nightly workflow keys are invalid"))
    events = _mapping(workflow.get("on"))
    schedule = events.get("schedule") if events is not None else None
    if (
        events is None
        or set(events) != NIGHTLY_EVENTS
        or events.get("workflow_dispatch") != {}
        or schedule != [{"cron": "17 5 * * *"}]
    ):
        errors.append(_error("AFFECTED-WORKFLOW-003", "nightly event contract is invalid"))
    if workflow.get("permissions") != {"actions": "read", "contents": "read"}:
        errors.append(_error("AFFECTED-WORKFLOW-004", "nightly permissions are invalid"))
    jobs = _mapping(workflow.get("jobs"))
    job = _mapping(jobs.get("bazel-nightly")) if jobs is not None else None
    if (
        job is None
        or set(job) != {"if", "name", "runs-on", "steps", "timeout-minutes"}
        or job.get("name") != "nightly Bazel / verdict"
        or job.get("runs-on") != "ubuntu-24.04"
        or job.get("timeout-minutes") != 90
    ):
        return [*errors, _error("AFFECTED-WORKFLOW-004", "nightly Bazel job is invalid")]
    if job.get("if") != "github.ref == 'refs/heads/main'" or not _checkout_is_complete(
        job,
        full_history=False,
        before_step="Analyze and test the complete configured graph",
    ):
        errors.append(_error("AFFECTED-WORKFLOW-004", "nightly Bazel job can be bypassed"))
    step = _named_step(job, "Analyze and test the complete configured graph")
    if (
        step is None
        or set(step) != {"env", "name", "run"}
        or step.get("env") != {"BASH_ENV": ""}
        or _command(step.get("run")) != NIGHTLY_BAZEL_COMMAND
    ):
        errors.append(_error("AFFECTED-WORKFLOW-005", "nightly Bazel command is invalid"))
    if not _uploads_are_governed(
        job,
        {
            "Upload nightly Bazel evidence": {
                "name": "bazel-nightly-${{ github.run_id }}-${{ github.run_attempt }}",
                "path": "${{ runner.temp }}/bazel-evidence/*",
                "if-no-files-found": "warn",
                "retention-days": 35,
            }
        },
    ):
        errors.append(_error("AFFECTED-WORKFLOW-006", "nightly evidence retention is invalid"))
    return errors


def _selection_policy_errors() -> list[str]:
    cases = (
        ("pull_request", "refs/pull/1/merge", "0" * 40, "affected"),
        ("merge_group", "refs/heads/gh-readonly-queue/main/pr-1", None, "full"),
        ("push", "refs/heads/main", None, "full"),
        ("schedule", "refs/heads/main", None, "full"),
        ("workflow_dispatch", "refs/heads/main", None, "full"),
    )
    for event, ref, base_sha, expected_mode in cases:
        try:
            actual_mode = affected.resolve_selection_mode(
                "auto",
                event=event,
                ref=ref,
                base_sha=base_sha,
            )
        except affected.SelectionError:
            return [_error("AFFECTED-WORKFLOW-008", "selection event policy is invalid")]
        if actual_mode != expected_mode:
            return [_error("AFFECTED-WORKFLOW-008", "selection event policy is invalid")]

        alternate_mode = "full" if expected_mode == "affected" else "affected"
        try:
            affected.resolve_selection_mode(
                alternate_mode,
                event=event,
                ref=ref,
                base_sha=base_sha,
            )
        except affected.SelectionError as error:
            if error.code != "AFFECTED-SELECT-010":
                return [_error("AFFECTED-WORKFLOW-008", "selection event policy is invalid")]
        else:
            return [_error("AFFECTED-WORKFLOW-008", "selection event policy is invalid")]
    return []


def _presubmit_orchestration_errors() -> list[str]:
    cases = (
        ("pull_request", "refs/pull/1/merge", "0" * 40, "affected"),
        ("merge_group", "refs/heads/gh-readonly-queue/main/pr-1", "", "full"),
        ("push", "refs/heads/main", "", "full"),
    )
    evidence = Path("/tmp/mindclade-affected-orchestration")
    head = "1" * 40
    try:
        for event, ref, base_sha, expected_mode in cases:
            changes = (
                (affected.Change(status="M", path="pkg/source.py"),)
                if expected_mode == "affected"
                else ()
            )
            canonical_base = "2" * 40 if expected_mode == "affected" else None
            selection = mock.Mock(
                mode=expected_mode,
                reason="orchestration_contract",
                analysis_targets=(),
                test_targets=(),
            )
            resolver = mock.Mock(return_value=expected_mode)
            clean_checkout = mock.Mock()
            bazelrc_contract = mock.Mock()
            revision = mock.Mock(return_value=canonical_base)
            changed = mock.Mock(return_value=changes)
            selector = mock.Mock(return_value=selection)
            executor = mock.Mock(return_value=0)
            failure_writer = mock.Mock()
            argv = [
                "pipeline.py",
                "--bazel-only",
                "--mode",
                "auto",
                "--base",
                base_sha,
                "--event",
                event,
                "--ref",
                ref,
                "--head",
                head,
                "--evidence-dir",
                str(evidence),
            ]
            with (
                mock.patch.object(sys, "argv", argv),
                mock.patch.object(
                    presubmit_pipeline.affected,
                    "resolve_selection_mode",
                    resolver,
                ),
                mock.patch.object(
                    presubmit_pipeline.affected,
                    "assert_clean_checkout",
                    clean_checkout,
                ),
                mock.patch.object(
                    presubmit_pipeline.affected,
                    "assert_bazelrc_contract",
                    bazelrc_contract,
                ),
                mock.patch.object(presubmit_pipeline.affected, "git_revision", revision),
                mock.patch.object(presubmit_pipeline.affected, "git_changed", changed),
                mock.patch.object(presubmit_pipeline.affected, "select", selector),
                mock.patch.object(
                    presubmit_pipeline.affected,
                    "execute_selection",
                    executor,
                ),
                mock.patch.object(
                    presubmit_pipeline.affected,
                    "write_failure_evidence",
                    failure_writer,
                ),
                contextlib.redirect_stdout(io.StringIO()),
            ):
                status = presubmit_pipeline.main()
            if status != 0:
                raise AssertionError("status")
            resolver.assert_called_once_with("auto", event=event, ref=ref, base_sha=base_sha)
            clean_checkout.assert_called_once_with(head)
            bazelrc_contract.assert_called_once_with(event)
            if expected_mode == "affected":
                revision.assert_called_once_with(base_sha)
                changed.assert_called_once_with(canonical_base)
            else:
                revision.assert_not_called()
                changed.assert_not_called()
            selector.assert_called_once_with(
                changes,
                mode=expected_mode,
                base_sha=canonical_base,
                event=event,
            )
            executor.assert_called_once_with(
                selection,
                evidence,
                job_started_epoch=None,
            )
            failure_writer.assert_not_called()

        failure_writer = mock.Mock()
        argv = [
            "pipeline.py",
            "--bazel-only",
            "--mode",
            "auto",
            "--base",
            "0" * 40,
            "--event",
            "pull_request",
            "--ref",
            "refs/pull/1/merge",
            "--head",
            head,
            "--evidence-dir",
            str(evidence),
        ]
        with (
            mock.patch.object(sys, "argv", argv),
            mock.patch.object(
                presubmit_pipeline.affected,
                "resolve_selection_mode",
                side_effect=affected.SelectionError(
                    "AFFECTED-SELECT-010", "selection mode conflicts with workflow policy"
                ),
            ),
            mock.patch.object(
                presubmit_pipeline.affected,
                "write_failure_evidence",
                failure_writer,
            ),
            contextlib.redirect_stderr(io.StringIO()),
        ):
            status = presubmit_pipeline.main()
        if status != 2 or failure_writer.call_count != 1:
            raise AssertionError("failure evidence")
    except Exception:
        return [_error("AFFECTED-CODE-006", "presubmit orchestration contract is invalid")]
    return []


def _nightly_orchestration_errors() -> list[str]:
    evidence = Path("/tmp/mindclade-nightly-orchestration")
    head = "1" * 40
    contract = nightly_pipeline.NightlyContract(
        mode="full",
        analysis_targets=("//...",),
        test_targets=("//...",),
    )
    try:
        for event in ("schedule", "workflow_dispatch"):
            selection = mock.Mock(
                analysis_targets=("//...",),
                test_targets=("//...",),
            )
            loader = mock.Mock(return_value=contract)
            resolver = mock.Mock(return_value="full")
            clean_checkout = mock.Mock()
            bazelrc_contract = mock.Mock()
            selector = mock.Mock(return_value=selection)
            executor = mock.Mock(return_value=0)
            failure_writer = mock.Mock()
            argv = [
                "pipeline.py",
                "--event",
                event,
                "--ref",
                "refs/heads/main",
                "--head",
                head,
                "--evidence-dir",
                str(evidence),
            ]
            with (
                mock.patch.object(sys, "argv", argv),
                mock.patch.object(nightly_pipeline, "load_contract", loader),
                mock.patch.object(
                    nightly_pipeline.affected,
                    "resolve_selection_mode",
                    resolver,
                ),
                mock.patch.object(
                    nightly_pipeline.affected,
                    "assert_clean_checkout",
                    clean_checkout,
                ),
                mock.patch.object(
                    nightly_pipeline.affected,
                    "assert_bazelrc_contract",
                    bazelrc_contract,
                ),
                mock.patch.object(nightly_pipeline.affected, "select", selector),
                mock.patch.object(
                    nightly_pipeline.affected,
                    "execute_selection",
                    executor,
                ),
                mock.patch.object(
                    nightly_pipeline.affected,
                    "write_failure_evidence",
                    failure_writer,
                ),
                contextlib.redirect_stdout(io.StringIO()),
            ):
                status = nightly_pipeline.main()
            if status != 0:
                raise AssertionError("status")
            resolver.assert_called_once_with(
                "full", event=event, ref="refs/heads/main", base_sha=None
            )
            clean_checkout.assert_called_once_with(head)
            bazelrc_contract.assert_called_once_with(event)
            selector.assert_called_once_with([], mode="full", event=event)
            executor.assert_called_once_with(
                selection,
                evidence,
                job_started_epoch=None,
            )
            failure_writer.assert_not_called()
    except Exception:
        return [_error("AFFECTED-CODE-007", "nightly orchestration contract is invalid")]
    return []


def check(root: Path) -> list[str]:
    errors: list[str] = []
    affected_path = root / "ci/common/affected.py"
    pipeline_path = root / "ci/presubmit/pipeline.py"
    try:
        affected_symbols = _top_level_symbols(affected_path)
        affected_assignments = _top_level_assignments(affected_path)
        affected_imports = _imports(affected_path)
        pipeline_symbols = _top_level_symbols(pipeline_path)
    except (OSError, UnicodeError, SyntaxError):
        return [_error("AFFECTED-CODE-001", "affected-selection source is unreadable")]
    for symbol in (
        "Change",
        "Selection",
        "SelectionError",
        "assert_bazelrc_contract",
        "assert_clean_checkout",
        "bazel_query",
        "execute_selection",
        "git_changed",
        "load_global_input_contract",
        "resolve_selection_mode",
        "rust_qualification_required",
        "select",
        "write_failure_evidence",
    ):
        if symbol not in affected_symbols:
            errors.append(_error("AFFECTED-CODE-002", "affected selector interface is incomplete"))
            break
    if "re" in affected_imports:
        errors.append(_error("AFFECTED-CODE-003", "affected selector uses a forbidden parser"))
    if {"GLOBAL_EXACT_PATHS", "GLOBAL_PREFIXES"} & affected_assignments:
        errors.append(_error("AFFECTED-CODE-004", "selector embeds mutable global inputs"))
    if "main" not in pipeline_symbols:
        errors.append(_error("AFFECTED-CODE-005", "presubmit pipeline entry point is missing"))

    contract_path = root / "ci/common/affected_global_inputs.json"
    try:
        contract = load_global_input_contract(contract_path)
        errors.extend(_review_boundary_errors(contract, _tracked_paths(root)))
    except ContractError as error:
        errors.append(str(error))
    errors.extend(_activation_errors(contract_path))

    try:
        presubmit = parse_workflow(root / ".github/workflows/presubmit.yml")
        errors.extend(_presubmit_workflow_errors(presubmit))
        nightly = parse_workflow(root / ".github/workflows/nightly.yml")
        errors.extend(_nightly_workflow_errors(nightly))
    except WorkflowYamlError as error:
        errors.append(str(error))
    errors.extend(_nightly_target_errors(root / "ci/nightly/targets.yaml"))
    errors.extend(_selection_policy_errors())
    errors.extend(_presubmit_orchestration_errors())
    errors.extend(_nightly_orchestration_errors())
    return errors


def main() -> int:
    errors = check(ROOT)
    for error in errors:
        print(error)
    if errors:
        return 1
    print("affected presubmit contract passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
