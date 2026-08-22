#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import ast
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


def _top_level_symbols(path: Path) -> set[str]:
    tree = ast.parse(path.read_text(encoding="utf-8"), filename=str(path))
    return {
        node.name
        for node in tree.body
        if isinstance(node, (ast.ClassDef, ast.FunctionDef, ast.AsyncFunctionDef))
    }


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


def check(root: Path) -> list[str]:
    errors: list[str] = []
    affected_path = root / "ci/common/affected.py"
    pipeline_path = root / "ci/presubmit/pipeline.py"
    affected_symbols = _top_level_symbols(affected_path)
    pipeline_symbols = _top_level_symbols(pipeline_path)
    for symbol in (
        "Change",
        "Selection",
        "SelectionError",
        "bazel_query",
        "execute_selection",
        "git_changed",
        "rust_qualification_required",
        "select",
        "write_failure_evidence",
    ):
        if symbol not in affected_symbols:
            errors.append(f"affected selector missing top-level {symbol}")
    if "re" in _imports(affected_path):
        errors.append("affected selector must not parse BUILD files with regular expressions")
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
            "--mode \"${mode}\"",
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

    nightly_path = root / ".github/workflows/nightly.yml"
    if not nightly_path.is_file():
        errors.append("CPU nightly workflow is missing")
    else:
        nightly = nightly_path.read_text(encoding="utf-8")
        nightly_job = _job_block(nightly, "bazel-nightly")
        for contract in (
            'cron: "17 5 * * *"',
            "workflow_dispatch:",
            "permissions:\n  contents: read",
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
                    "permissions:\n  contents: read",
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
