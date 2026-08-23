#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

import ast
import contextlib
import hashlib
import io
import json
import shlex
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
PRESUBMIT_BAZEL_COMMAND = (
    "/nix/var/nix/profiles/default/bin/nix",
    "develop",
    ".#ci-bazel",
    "--command",
    "python3",
    "-B",
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
    "--runner-temp",
    "${RUNNER_TEMP}",
    "--cache-mode",
    "${BAZEL_CACHE_MODE}",
    "--cache-role",
    "${BAZEL_CACHE_ROLE}",
)
NIGHTLY_BAZEL_COMMAND = (
    "/nix/var/nix/profiles/default/bin/nix",
    "develop",
    ".#ci-bazel",
    "--command",
    "python3",
    "-B",
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
    "--runner-temp",
    "${RUNNER_TEMP}",
    "--cache-mode",
    "${BAZEL_CACHE_MODE}",
    "--cache-role",
    "${BAZEL_CACHE_ROLE}",
)
UPLOAD_ARTIFACT_ACTION = "actions/upload-artifact@ea165f8d65b6e75b540449e92b4886f43607fa02"
CHECKOUT_ACTION = "actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1"
PRESUBMIT_BAZEL_STEP_CONTRACT = (
    (
        "name:Record Bazel verdict start",
        "6e3581f87afe2f1357d9389beee1612d32a39123b48a1f7112e11a70865bfd16",
    ),
    (
        "uses:actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1",
        "3facce523aa8a0e29a937485282e18e07e2907c45914b4ce7745b674c765f3b7",
    ),
    (
        "name:Prepare GitHub-hosted runner disk for Nix",
        "c8ce580064e9a2ca3c9dabcdac62b53fe14d8ce7b17fa00de32d1a1e97915267",
    ),
    (
        "uses:bazel-contrib/setup-bazel@4fd964a13a440a8aeb0be47350db2fc640f19ca8",
        "5c825b313c6e81fad2e993ae09ce267a40dd48b30816e4e151764b96273d16f2",
    ),
    (
        "uses:cachix/install-nix-action@630ae543ea3a38a9a4166f03376c02c50f408342",
        "889e811758a8eb0bfe298bec1412219d252c18e4946c086c793465172a6a4513",
    ),
    (
        "name:Select qualified Bazel remote-cache route",
        "17f1e826fbd763e44b11c1d9b11e8f21b62c8fde215ec4e63810bc6ad168473c",
    ),
    (
        "name:Select trusted Bazel cache revision",
        "bea656ab62c9f9a001e3c1ad62af6470630387cdc39aa490946410a3d9d6ba8e",
    ),
    (
        "name:Restore trusted Bazel persistent action cache",
        "2451db34483041c5d9a402a684b0a8b25e60f3e47d3c2dadbe50a72f260d032f",
    ),
    (
        "name:Configure bounded Bazel persistent action cache",
        "f6ba8b633e0fd50ca9c98a0220064a0a071e0b4210ace41aa0b14e8c3307d25a",
    ),
    (
        "name:Build qualified Bazel GCS cache gateway",
        "6f1f4320ac9b5bd8d49ac0784f852d711aea478def27cebd4104d687909e8096",
    ),
    (
        "name:Authenticate read-only Bazel cache route",
        "0744449797a1f59706bfd676fb0b6b408d366731a605eee8127922ce210f5931",
    ),
    (
        "name:Authenticate trusted Bazel cache writer route",
        "4f3a50497ca60210af3343e78880a5de32eae5b0921bf639674deb75f525548b",
    ),
    (
        "name:Start loopback Bazel GCS cache gateway",
        "9c57f2444e9db26b2f8814163b2314a7bdc8c366b9c6711c9083e574254a26c4",
    ),
    (
        "name:buildifier",
        "59abfcb3d0cfb35ea31cf7fd1c3e9bb1fd11b9123984cd69f4b3e082cf2cb07b",
    ),
    (
        "name:Every BUILD file loads and every label resolves",
        "f32d7fa54fe7ab8d7d88953737d06603c6eb7930af8688c2964967cac33d362d",
    ),
    (
        "name:Prove affected selection against the real Bazel graph",
        "8716a67b6b12c8ac097dc79c44522c0381bca16125dd523b4bcccbf1559dd51d",
    ),
    (
        "name:Enforce Bazel dependency layers",
        "ef215db3e1f7e9ec15044631aa83aa653631597805864debda741ff267f05792",
    ),
    (
        "name:Validate and resolve the registered C/C++ toolchain",
        "c9ab1c7b5e17c4594d37e9aaa4348bf4c2d83bb0d994fbd022341069d81a2a70",
    ),
    (
        "name:Run event-governed Bazel validation",
        "c3f73f3be957151c5b1d2b7b5d108f00d382f62044913eedcf7d77733299d923",
    ),
    (
        "name:Measure bounded Bazel persistent action cache",
        "95b92bedabf6707c2764881bd036ab7fd73f3e26b4e6258de880830752acdfd5",
    ),
    (
        "name:Save trusted Bazel persistent action cache",
        "dcbadf057eab254e9a46988c53ee666724ba6afc007b26e6a977efbf00e81127",
    ),
    (
        "name:Record Bazel persistent action cache metrics",
        "9b4261daaa5be664481d8a50a5c9875daf0d72f466c70b9f1d9244c91f4b1875",
    ),
    (
        "name:Record Bazel GCS remote-cache metrics and stop gateway",
        "48b22f3adde66695cd963bf0bb50b07f7f35e9beebef3562f6e330ba380abe63",
    ),
    (
        "name:Upload Bazel performance evidence",
        "993670b32d2cd1be75d6dccbdc68e0aa9afcddb4d85adbcd39b27642dbadb4d3",
    ),
    (
        "name:Upload Bazel latency metric",
        "e6641e5b4f5e36548083e59a30bc606ee9355d00549af926b9ea907c61ff75f2",
    ),
)
NIGHTLY_BAZEL_STEP_CONTRACT = (
    (
        "name:Record nightly verdict start",
        "63dc8127ef8e2ef48145a82df11a5b8f74628c9e38fb1f9fe1b005cde7c96e1c",
    ),
    (
        "uses:actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1",
        "69983d424f70d56e16aca2c5bc441464578ac832b548ce342069dd3e5a1ca9e1",
    ),
    (
        "name:Prepare GitHub-hosted runner disk for Nix",
        "c8ce580064e9a2ca3c9dabcdac62b53fe14d8ce7b17fa00de32d1a1e97915267",
    ),
    (
        "uses:bazel-contrib/setup-bazel@4fd964a13a440a8aeb0be47350db2fc640f19ca8",
        "5c825b313c6e81fad2e993ae09ce267a40dd48b30816e4e151764b96273d16f2",
    ),
    (
        "uses:cachix/install-nix-action@630ae543ea3a38a9a4166f03376c02c50f408342",
        "889e811758a8eb0bfe298bec1412219d252c18e4946c086c793465172a6a4513",
    ),
    (
        "name:Select qualified nightly Bazel remote-cache route",
        "f946134ef78e00c5703c21fd217855b331e8d1c6a6ef166800cad24bbd95a21c",
    ),
    (
        "name:Select trusted nightly Bazel cache revision",
        "8a11170e45d5ba4e1ab10f13a6032f173057e060766aa8895f18b7f242514a7b",
    ),
    (
        "name:Restore trusted nightly Bazel persistent action cache",
        "d2f8fb316c6fd8485dea77c8bd7c4f356376a70fcac19485c3ce23384e0341b0",
    ),
    (
        "name:Configure bounded nightly Bazel persistent action cache",
        "c307e45367643c715bd84ce724a8364413b4f9367948e1f2925bf23b2103066c",
    ),
    (
        "name:Build qualified nightly Bazel GCS cache gateway",
        "5d490c127bcbe8e961d15be535f78e809bcaf6fee8e64d4af32ed7cffe7c0ea0",
    ),
    (
        "name:Authenticate scheduled Bazel cache writer route",
        "1e6a054411a5caed2fc04fb952c2cdf918c403f9c0871622f14f7c31a621bd81",
    ),
    (
        "name:Start scheduled loopback Bazel GCS cache gateway",
        "5d87963363e71b5606287182b94caa97a9c4f41ff1a1db20066529df80a4e518",
    ),
    (
        "name:Validate complete loading, formatting, and layer policy",
        "ab588d08973519ca09fa2a82320527065ebff415a66539e61f41c0d33e0e59d6",
    ),
    (
        "name:Validate and resolve the registered C/C++ toolchain",
        "c9ab1c7b5e17c4594d37e9aaa4348bf4c2d83bb0d994fbd022341069d81a2a70",
    ),
    (
        "name:Analyze and test the complete configured graph",
        "a33877886c60363097f440573f52eb502fc01703bb02c750740893d3ea05ad48",
    ),
    (
        "name:Qualify the rolling affected-presubmit latency SLO",
        "ab6a33e357da78ffe4b4966ab7c3bad9eeec3b6f2e192d39c8f8a8374adb07fe",
    ),
    (
        "name:Measure bounded nightly Bazel persistent action cache",
        "867a6253a4d0c3d2996066a6d842fae33252c4fdf4c4e8e2478d75ffd6ff9c17",
    ),
    (
        "name:Save trusted nightly Bazel persistent action cache",
        "628d8743e71c0cdda2bde68e2f1c24f0f7cea9f6d60b9b11d8d464302ec0c2a3",
    ),
    (
        "name:Record nightly Bazel persistent action cache metrics",
        "d7997e4788ffd0e339395d1d154a5dfaca8cab0559f0c26ac234ea54944c3298",
    ),
    (
        "name:Record nightly Bazel GCS remote-cache metrics and stop gateway",
        "f6f3004b9f1f49492a6bb1d030bc0d0518145a09709e3f4a3824dd465edd5112",
    ),
    (
        "name:Upload nightly Bazel evidence",
        "73dd5401be7731758e82064e0fef2fbd8d4d0b5e475b7fa9cd74d01bb54390fb",
    ),
)


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
        result = affected.run_git(
            ["ls-files", "--cached", "--others", "--exclude-standard", "-z"],
            root=root,
        )
    except affected.SelectionError as error:
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
        "external_required_workflow_not_active",
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


def _step_contract_is_exact(job: dict[str, Any], expected: tuple[tuple[str, str], ...]) -> bool:
    steps = job.get("steps")
    if not isinstance(steps, list):
        return False
    actual: list[tuple[str, str]] = []
    try:
        for step in steps:
            if not isinstance(step, dict):
                return False
            name = step.get("name")
            uses = step.get("uses")
            if isinstance(name, str) and name:
                identity = f"name:{name}"
            elif isinstance(uses, str) and uses:
                identity = f"uses:{uses}"
            else:
                return False
            canonical = json.dumps(
                step,
                ensure_ascii=True,
                separators=(",", ":"),
                sort_keys=True,
            ).encode("ascii")
            actual.append((identity, hashlib.sha256(canonical).hexdigest()))
    except (TypeError, UnicodeError, ValueError):
        return False
    return tuple(actual) == expected


def _verdict_job_is_unique(
    jobs: dict[str, Any] | None, *, expected_id: str, expected_name: str
) -> bool:
    if jobs is None:
        return False
    matching_ids = [
        job_id
        for job_id, candidate in jobs.items()
        if isinstance(candidate, dict) and candidate.get("name") == expected_name
    ]
    return matching_ids == [expected_id]


def _verdict_context_errors(workflows: dict[str, dict[str, Any]]) -> list[str]:
    protected_names = {"bazel / verdict", "nightly Bazel / verdict"}
    expected = {
        (".github/workflows/nightly.yml", "bazel-nightly", "nightly Bazel / verdict"),
        (".github/workflows/presubmit.yml", "bazel", "bazel / verdict"),
    }
    observed: set[tuple[str, str, str]] = set()
    for path, workflow in workflows.items():
        jobs = _mapping(workflow.get("jobs"))
        if jobs is None:
            continue
        for job_id, candidate in jobs.items():
            job = _mapping(candidate)
            name = job.get("name") if job is not None else None
            if isinstance(name, str) and "${{" in name:
                return [_error("AFFECTED-WORKFLOW-010", "Bazel verdict context is ambiguous")]
            if name in protected_names:
                observed.add((path, job_id, name))
    if observed != expected:
        return [_error("AFFECTED-WORKFLOW-010", "Bazel verdict context is ambiguous")]
    return []


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


def _presubmit_cache_routing_is_governed(job: dict[str, Any]) -> bool:
    remote_route = _named_step(job, "Select qualified Bazel remote-cache route")
    disk_route = _named_step(job, "Select trusted Bazel cache revision")
    governed_run = _named_step(job, "Run event-governed Bazel validation")
    cache_measure = _named_step(job, "Measure bounded Bazel persistent action cache")
    remote_env = _mapping(remote_route.get("env")) if remote_route is not None else None
    disk_env = _mapping(disk_route.get("env")) if disk_route is not None else None
    governed_env = _mapping(governed_run.get("env")) if governed_run is not None else None
    return (
        remote_env is not None
        and remote_env.get("PR_BASE_REF") == PULL_REQUEST_CACHE_BASE_REF
        and disk_env is not None
        and disk_env.get("PR_BASE_REF") == PULL_REQUEST_CACHE_BASE_REF
        and disk_env.get("PR_BASE_SHA") == PULL_REQUEST_CACHE_BASE_SHA
        and governed_env is not None
        and governed_env.get("PR_BASE_SHA") == PULL_REQUEST_SELECTION_BASE_SHA
        and cache_measure is not None
        and cache_measure.get("if") == PERSISTENT_CACHE_MEASURE_IF
    )


def _checkout_python_bytecode_is_disabled(job: dict[str, Any], *, through_step: str) -> bool:
    steps = job.get("steps")
    if not isinstance(steps, list):
        return False
    boundaries = [
        index
        for index, step in enumerate(steps)
        if isinstance(step, dict) and step.get("name") == through_step
    ]
    if len(boundaries) != 1:
        return False
    observed = False
    for step in steps[: boundaries[0] + 1]:
        if not isinstance(step, dict) or "run" not in step:
            continue
        command = _command(step.get("run"))
        if command is None:
            return False
        for index, token in enumerate(command):
            if token != "python3":
                continue
            observed = True
            if index + 1 >= len(command) or command[index + 1] != "-B":
                return False
    return observed


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
    if not _verdict_job_is_unique(jobs, expected_id="bazel", expected_name="bazel / verdict"):
        errors.append(_error("AFFECTED-WORKFLOW-004", "presubmit verdict job is ambiguous"))
    bazel_job = _mapping(jobs.get("bazel")) if jobs is not None else None
    if (
        bazel_job is None
        or set(bazel_job) != {"name", "permissions", "runs-on", "steps", "timeout-minutes"}
        or bazel_job.get("name") != "bazel / verdict"
        or bazel_job.get("permissions") != {"contents": "read"}
        or bazel_job.get("runs-on") != "ubuntu-24.04"
        or bazel_job.get("timeout-minutes") != 90
    ):
        return [*errors, _error("AFFECTED-WORKFLOW-004", "presubmit Bazel job is invalid")]
    if not _step_contract_is_exact(bazel_job, PRESUBMIT_BAZEL_STEP_CONTRACT):
        errors.append(_error("AFFECTED-WORKFLOW-009", "presubmit Bazel steps drifted"))
    if not _presubmit_cache_routing_is_governed(bazel_job):
        errors.append(_error("AFFECTED-WORKFLOW-011", "presubmit cache routing is invalid"))
    if not _checkout_python_bytecode_is_disabled(
        bazel_job,
        through_step="Run event-governed Bazel validation",
    ):
        errors.append(_error("AFFECTED-WORKFLOW-012", "presubmit Python launch is invalid"))
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
            "BAZEL_CACHE_MODE": (
                "${{ steps.bazel-remote-cache.outputs.enabled == 'true' && 'remote' || 'disk' }}"
            ),
            "BAZEL_CACHE_ROLE": (
                "${{ steps.bazel-remote-cache.outputs.enabled == 'true' "
                "&& steps.bazel-remote-cache.outputs.role "
                "|| steps.bazel-cache-trust.outputs.role }}"
            ),
            "PR_BASE_SHA": PULL_REQUEST_SELECTION_BASE_SHA,
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
    if not _verdict_job_is_unique(
        jobs,
        expected_id="bazel-nightly",
        expected_name="nightly Bazel / verdict",
    ):
        errors.append(_error("AFFECTED-WORKFLOW-004", "nightly verdict job is ambiguous"))
    job = _mapping(jobs.get("bazel-nightly")) if jobs is not None else None
    if (
        job is None
        or set(job)
        != {
            "if",
            "name",
            "permissions",
            "runs-on",
            "steps",
            "timeout-minutes",
        }
        or job.get("name") != "nightly Bazel / verdict"
        or job.get("permissions") != {"actions": "read", "contents": "read"}
        or job.get("runs-on") != "ubuntu-24.04"
        or job.get("timeout-minutes") != 90
    ):
        return [*errors, _error("AFFECTED-WORKFLOW-004", "nightly Bazel job is invalid")]
    if not _step_contract_is_exact(job, NIGHTLY_BAZEL_STEP_CONTRACT):
        errors.append(_error("AFFECTED-WORKFLOW-009", "nightly Bazel steps drifted"))
    if not _checkout_python_bytecode_is_disabled(
        job,
        through_step="Analyze and test the complete configured graph",
    ):
        errors.append(_error("AFFECTED-WORKFLOW-012", "nightly Python launch is invalid"))
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
        or step.get("env")
        != {
            "BASH_ENV": "",
            "BAZEL_CACHE_MODE": (
                "${{ steps.bazel-remote-cache.outputs.enabled == 'true' && 'remote' || 'disk' }}"
            ),
            "BAZEL_CACHE_ROLE": (
                "${{ steps.bazel-remote-cache.outputs.enabled == 'true' "
                "&& steps.bazel-remote-cache.outputs.role "
                "|| steps.bazel-cache-trust.outputs.role }}"
            ),
        }
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
        ("pull_request", "refs/pull/1/merge", "0" * 40, "full"),
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
        ("pull_request", "refs/pull/1/merge", "0" * 40, "full", "disk", "reader"),
        ("pull_request", "refs/pull/1/merge", "0" * 40, "full", "remote", "reader"),
        (
            "merge_group",
            "refs/heads/gh-readonly-queue/main/pr-1",
            "",
            "full",
            "disk",
            "reader",
        ),
        (
            "merge_group",
            "refs/heads/gh-readonly-queue/main/pr-1",
            "",
            "full",
            "remote",
            "writer",
        ),
        ("push", "refs/heads/main", "", "full", "disk", "writer"),
        ("push", "refs/heads/main", "", "full", "remote", "writer"),
    )
    evidence = Path("/tmp/mindclade-affected-orchestration")
    runner_temp = Path("/tmp/mindclade-affected-runner")
    started_file = runner_temp / "bazel-job-started"
    started_epoch = 123
    head = "1" * 40
    try:
        for event, ref, base_sha, expected_mode, cache_mode, cache_role in cases:
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
            bazelrc_authority = object()
            clean_checkout = mock.Mock(return_value=bazelrc_authority)
            started_loader = mock.Mock(return_value=started_epoch)
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
                "--job-started-at-file",
                str(started_file),
                "--runner-temp",
                str(runner_temp),
                "--cache-mode",
                cache_mode,
                "--cache-role",
                cache_role,
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
                    "load_job_started_epoch",
                    started_loader,
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
            clean_checkout.assert_called_once_with(
                head,
                event=event,
                runner_temp=runner_temp,
                cache_mode=cache_mode,
                cache_role=cache_role,
            )
            started_loader.assert_called_once_with(started_file, runner_temp=runner_temp)
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
                bazelrc_authority=bazelrc_authority,
                job_started_epoch=started_epoch,
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
    runner_temp = Path("/tmp/mindclade-nightly-runner")
    started_file = runner_temp / "bazel-job-started"
    started_epoch = 123
    head = "1" * 40
    contract = nightly_pipeline.NightlyContract(
        mode="full",
        analysis_targets=("//...",),
        test_targets=("//...",),
    )
    try:
        for event, cache_mode, cache_role in (
            ("schedule", "disk", "writer"),
            ("schedule", "remote", "writer"),
            ("workflow_dispatch", "disk", "writer"),
        ):
            selection = mock.Mock(
                analysis_targets=("//...",),
                test_targets=("//...",),
            )
            loader = mock.Mock(return_value=contract)
            resolver = mock.Mock(return_value="full")
            bazelrc_authority = object()
            clean_checkout = mock.Mock(return_value=bazelrc_authority)
            started_loader = mock.Mock(return_value=started_epoch)
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
                "--job-started-at-file",
                str(started_file),
                "--runner-temp",
                str(runner_temp),
                "--cache-mode",
                cache_mode,
                "--cache-role",
                cache_role,
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
                    "load_job_started_epoch",
                    started_loader,
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
            clean_checkout.assert_called_once_with(
                head,
                event=event,
                runner_temp=runner_temp,
                cache_mode=cache_mode,
                cache_role=cache_role,
            )
            started_loader.assert_called_once_with(started_file, runner_temp=runner_temp)
            selector.assert_called_once_with([], mode="full", event=event)
            executor.assert_called_once_with(
                selection,
                evidence,
                bazelrc_authority=bazelrc_authority,
                job_started_epoch=started_epoch,
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
        "load_job_started_epoch",
        "resolve_selection_mode",
        "run_git",
        "rust_qualification_required",
        "select",
        "trusted_git_launcher",
        "write_failure_evidence",
    ):
        if symbol not in affected_symbols:
            errors.append(_error("AFFECTED-CODE-002", "affected selector interface is incomplete"))
            break
    if "re" in affected_imports:
        errors.append(_error("AFFECTED-CODE-003", "affected selector uses a forbidden parser"))
    if {"GLOBAL_EXACT_PATHS", "GLOBAL_PREFIXES"} & affected_assignments:
        errors.append(_error("AFFECTED-CODE-004", "selector embeds mutable global inputs"))
    if affected.GRAPH_NATIVE_AFFECTED_ACTIVE is not False:
        errors.append(_error("AFFECTED-CODE-008", "affected workflow activation is premature"))
    if "main" not in pipeline_symbols:
        errors.append(_error("AFFECTED-CODE-005", "presubmit pipeline entry point is missing"))

    contract_path = root / "ci/common/affected_global_inputs.json"
    try:
        contract = load_global_input_contract(contract_path)
        errors.extend(_review_boundary_errors(contract, _tracked_paths(root)))
    except ContractError as error:
        errors.append(str(error))
    errors.extend(_activation_errors(contract_path))

    workflows: dict[str, dict[str, Any]] = {}
    try:
        for path in sorted((root / ".github/workflows").glob("*.y*ml")):
            workflows[path.relative_to(root).as_posix()] = parse_workflow(path)
        presubmit = workflows[".github/workflows/presubmit.yml"]
        errors.extend(_presubmit_workflow_errors(presubmit))
        nightly = workflows[".github/workflows/nightly.yml"]
        errors.extend(_nightly_workflow_errors(nightly))
        errors.extend(_verdict_context_errors(workflows))
    except (KeyError, WorkflowYamlError) as error:
        if isinstance(error, WorkflowYamlError):
            errors.append(str(error))
        else:
            errors.append(_error("AFFECTED-WORKFLOW-001", "workflow source is unreadable"))
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
