#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import ast
import json
import re
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))

from ci.common.affected_contract import (  # noqa: E402
    ContractError,
    GlobalInputContract,
    load_global_input_contract,
)


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


def _job_block(workflow: str, job: str) -> str:
    match = re.search(rf"(?m)^  {re.escape(job)}:\s*$", workflow)
    if match is None:
        return ""
    following = workflow[match.end() :]
    next_job = re.search(r"(?m)^  [A-Za-z0-9_-]+:\s*$", following)
    return following[: next_job.start()] if next_job else following


def _tracked_paths(root: Path) -> tuple[str, ...]:
    result = subprocess.run(
        ["git", "ls-files", "--cached", "--others", "--exclude-standard", "-z"],
        cwd=root,
        capture_output=True,
        check=False,
    )
    if result.returncode:
        raise ContractError("[AFFECTED-GLOBAL-008] tracked-path inventory failed")
    return tuple(
        sorted(
            field.decode("utf-8", errors="strict") for field in result.stdout.split(b"\0") if field
        )
    )


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
        display = boundary or "root"
        for entry in sorted(actual - expected):
            errors.append(f"[AFFECTED-GLOBAL-006] {display} authority {entry!r} is not reviewed")
        for entry in sorted(expected - actual):
            errors.append(f"[AFFECTED-GLOBAL-007] {display} authority {entry!r} is stale")
    return errors


def _activation_errors(path: Path) -> list[str]:
    try:
        payload = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return ["[AFFECTED-GLOBAL-001] global-input contract is unreadable"]
    activation = payload.get("activation") if isinstance(payload, dict) else None
    if not isinstance(activation, dict):
        return ["[AFFECTED-GLOBAL-009] graph-native activation evidence is missing"]
    errors: list[str] = []
    if activation.get("state") != "blocked":
        errors.append(
            "[AFFECTED-GLOBAL-009] graph-native activation must remain blocked pending evidence"
        )
    blockers = activation.get("blockers")
    expected_blockers = [
        "bazel_version_parse_not_qualified",
        "full_graph_linux_not_qualified",
        "remote_cache_not_qualified",
        "workspace_restoration_not_hardened",
    ]
    if blockers != expected_blockers:
        errors.append("[AFFECTED-GLOBAL-009] graph-native activation blockers drifted")
    release = activation.get("release")
    if not isinstance(release, dict):
        errors.append("[AFFECTED-GLOBAL-010] graph-native release pin is missing")
        return errors
    if (
        activation.get("tool") != "bazel-contrib/target-determinator"
        or release.get("tag") != "v0.34.0"
        or release.get("commit") != "d4b6125546979713431e63b5c3e65810fa989446"
        or release.get("license") != "Apache-2.0"
    ):
        errors.append("[AFFECTED-GLOBAL-010] graph-native release identity drifted")
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
        errors.append("[AFFECTED-GLOBAL-010] graph-native release assets drifted")
        return errors
    for system, (expected_name, expected_digest) in expected_assets.items():
        asset = assets.get(system)
        digest = asset.get("sha256") if isinstance(asset, dict) else None
        if (
            not isinstance(asset, dict)
            or asset.get("name") != expected_name
            or digest != expected_digest
        ):
            errors.append(f"[AFFECTED-GLOBAL-010] invalid release asset for {system}")
    return errors


def check(root: Path) -> list[str]:
    errors: list[str] = []
    affected_path = root / "ci/common/affected.py"
    pipeline_path = root / "ci/presubmit/pipeline.py"
    affected_symbols = _top_level_symbols(affected_path)
    affected_assignments = _top_level_assignments(affected_path)
    pipeline_symbols = _top_level_symbols(pipeline_path)
    for symbol in (
        "Change",
        "Selection",
        "SelectionError",
        "bazel_query",
        "execute_selection",
        "git_changed",
        "load_global_input_contract",
        "rust_qualification_required",
        "select",
        "write_failure_evidence",
    ):
        if symbol not in affected_symbols:
            errors.append(f"affected selector missing top-level {symbol}")
    if "re" in _imports(affected_path):
        errors.append("affected selector must not parse BUILD files with regular expressions")
    for obsolete in ("GLOBAL_EXACT_PATHS", "GLOBAL_PREFIXES"):
        if obsolete in affected_assignments:
            errors.append(f"affected selector embeds obsolete global-input list {obsolete}")
    if "main" not in pipeline_symbols:
        errors.append("presubmit pipeline missing main entry point")

    pipeline = pipeline_path.read_text(encoding="utf-8")
    for contract in (
        "--static-only",
        "--bazel-only",
        "--mode",
        "--base",
        "--evidence-dir",
        "affected.git_changed",
        "affected.select",
        "affected.execute_selection",
    ):
        if contract not in pipeline:
            errors.append(f"presubmit pipeline missing contract {contract}")

    workflow = (root / ".github/workflows/presubmit.yml").read_text(encoding="utf-8")
    bazel_job = _job_block(workflow, "bazel")
    if not bazel_job:
        errors.append("presubmit workflow missing bazel job")
    else:
        for contract in (
            "name: bazel / verdict",
            "fetch-depth: 0",
            "ci/presubmit/pipeline.py",
            "--bazel-only",
            '--mode "${mode}"',
            "github.event.pull_request.base.sha",
            '[[ "${GITHUB_EVENT_NAME}" == "pull_request" ]]',
            "retention-days: 35",
        ):
            if contract not in bazel_job:
                errors.append(f"presubmit Bazel job missing {contract}")
        if "--static-only" in bazel_job:
            errors.append("presubmit Bazel job must not invoke the static-only pipeline path")
        for direct_full_command in ("bazelw build //...", "bazelw test //..."):
            if direct_full_command in bazel_job:
                errors.append(
                    f"presubmit Bazel job bypasses selection pipeline with {direct_full_command}"
                )
    for event in ("pull_request:", "merge_group:"):
        if not re.search(rf"(?m)^  {re.escape(event)}\s*$", workflow):
            errors.append(f"presubmit workflow must declare {event[:-1]}")

    contract_path = root / "ci/common/affected_global_inputs.json"
    try:
        contract = load_global_input_contract(contract_path)
        errors.extend(_review_boundary_errors(contract, _tracked_paths(root)))
    except ContractError as error:
        errors.append(str(error))
    errors.extend(_activation_errors(contract_path))

    nightly_path = root / ".github/workflows/nightly.yml"
    if not nightly_path.is_file():
        errors.append("CPU nightly workflow is missing")
    else:
        nightly = nightly_path.read_text(encoding="utf-8")
        nightly_job = _job_block(nightly, "bazel-nightly")
        for contract in (
            'cron: "17 5 * * *"',
            "workflow_dispatch:",
            "permissions:",
            "actions: read",
            "contents: read",
            "timeout-minutes: 90",
            "ci/nightly/pipeline.py",
            "retention-days: 35",
        ):
            source = (
                nightly
                if contract
                in {
                    'cron: "17 5 * * *"',
                    "workflow_dispatch:",
                    "permissions:",
                    "actions: read",
                    "contents: read",
                }
                else nightly_job or nightly
            )
            if contract not in source:
                errors.append(f"CPU nightly workflow missing {contract}")
    return errors


def main() -> int:
    errors = check(ROOT)
    [print(error) for error in errors]
    if errors:
        return 1
    print("affected presubmit contract passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
