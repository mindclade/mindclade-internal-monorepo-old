#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Report branch and worktree preservation evidence without mutating the repository."""

from __future__ import annotations

import argparse
import html
import json
import os
import re
import subprocess
from pathlib import Path
from typing import Any

SCHEMA_VERSION = 1
SHA1 = re.compile(r"[0-9a-f]{40}")
BASELINE_REF = re.compile(r"refs/(?:heads|remotes/origin)/[A-Za-z0-9._/-]+")
MAX_COMMITS_PER_REF = 50


class HygieneReportError(RuntimeError):
    """Repository preservation evidence could not be established."""


def _git_binary() -> str:
    candidate = os.environ.get("MINDCLADE_GIT", "git")
    if candidate == "git":
        return candidate
    path = Path(candidate)
    try:
        resolved = path.resolve(strict=True)
    except OSError as error:
        raise HygieneReportError("configured git is unavailable") from error
    if (
        not path.is_absolute()
        or path.is_symlink()
        or not resolved.is_file()
        or not os.access(resolved, os.X_OK)
    ):
        raise HygieneReportError("configured git is invalid")
    return str(resolved)


def _git(root: Path, arguments: list[str], *, nul: bool = False) -> str | bytes:
    command = [_git_binary(), "-C", str(root), *arguments]
    try:
        result = subprocess.run(
            command,
            capture_output=True,
            check=False,
            text=not nul,
        )
    except OSError as error:
        raise HygieneReportError("git is unavailable") from error
    if result.returncode != 0:
        raise HygieneReportError("repository preservation evidence is unavailable")
    return result.stdout


def _canonical_root(root: Path) -> Path:
    value = _git(root, ["rev-parse", "--show-toplevel"])
    assert isinstance(value, str)
    try:
        canonical = Path(value.strip()).resolve(strict=True)
        requested = root.resolve(strict=True)
    except OSError as error:
        raise HygieneReportError("repository root is unavailable") from error
    if canonical != requested or not canonical.is_dir():
        raise HygieneReportError("repository root must be the canonical primary checkout")
    return canonical


def _worktree_records(root: Path) -> tuple[dict[str, str | bool], ...]:
    raw = _git(root, ["worktree", "list", "--porcelain", "-z"], nul=True)
    assert isinstance(raw, bytes)
    records: list[dict[str, str | bool]] = []
    current: dict[str, str | bool] = {}
    try:
        fields = raw.decode("utf-8").split("\0")
    except UnicodeError as error:
        raise HygieneReportError("worktree inventory is invalid") from error
    for field in fields:
        if not field:
            if current:
                records.append(current)
                current = {}
            continue
        key, separator, value = field.partition(" ")
        if key in current or key not in {
            "worktree",
            "HEAD",
            "branch",
            "detached",
            "locked",
            "prunable",
        }:
            raise HygieneReportError("worktree inventory is invalid")
        current[key] = value if separator else True
    if current:
        records.append(current)
    if not records:
        raise HygieneReportError("worktree inventory is empty")
    for record in records:
        if not isinstance(record.get("worktree"), str) or not isinstance(record.get("HEAD"), str):
            raise HygieneReportError("worktree inventory is incomplete")
        if SHA1.fullmatch(str(record["HEAD"])) is None:
            raise HygieneReportError("worktree HEAD is invalid")
    return tuple(records)


def _status(root: Path) -> list[dict[str, str]]:
    raw = _git(root, ["status", "--porcelain=v1", "-z", "--untracked-files=all"], nul=True)
    assert isinstance(raw, bytes)
    try:
        fields = raw.decode("utf-8", errors="strict").split("\0")
    except UnicodeError as error:
        raise HygieneReportError("worktree status is invalid") from error
    dirty: list[dict[str, str]] = []
    index = 0
    while index < len(fields):
        field = fields[index]
        index += 1
        if not field:
            continue
        if len(field) < 4 or field[2] != " ":
            raise HygieneReportError("worktree status is invalid")
        status = field[:2]
        entry = {"status": status, "path": field[3:]}
        if "R" in status or "C" in status:
            if index >= len(fields) or not fields[index]:
                raise HygieneReportError("worktree rename status is invalid")
            entry["original_path"] = fields[index]
            index += 1
        dirty.append(entry)
    return sorted(dirty, key=lambda item: (item["path"], item["status"]))


def _unique_commits(root: Path, baseline: str, revision: str) -> tuple[int, list[str]]:
    count_raw = _git(root, ["rev-list", "--count", f"{baseline}..{revision}"])
    assert isinstance(count_raw, str)
    try:
        count = int(count_raw.strip())
    except ValueError as error:
        raise HygieneReportError("unique commit count is invalid") from error
    hashes_raw = _git(
        root,
        ["rev-list", f"--max-count={MAX_COMMITS_PER_REF}", f"{baseline}..{revision}"],
    )
    assert isinstance(hashes_raw, str)
    hashes = [line for line in hashes_raw.splitlines() if line]
    if count < 0 or len(hashes) > count or any(SHA1.fullmatch(value) is None for value in hashes):
        raise HygieneReportError("unique commit inventory is invalid")
    return count, hashes


def _branch_records(root: Path, baseline: str) -> list[dict[str, Any]]:
    raw = _git(
        root,
        [
            "for-each-ref",
            "--format=%(refname)%00%(objectname)%00%(upstream:short)",
            "refs/heads",
            "refs/remotes/origin",
        ],
    )
    assert isinstance(raw, str)
    records = []
    for line in raw.splitlines():
        fields = line.split("\0")
        if len(fields) != 3:
            raise HygieneReportError("branch inventory is invalid")
        refname, head_sha, upstream = fields
        if refname == "refs/remotes/origin/HEAD":
            continue
        if SHA1.fullmatch(head_sha) is None:
            raise HygieneReportError("branch HEAD is invalid")
        count, commits = _unique_commits(root, baseline, refname)
        records.append(
            {
                "ref": refname,
                "head_sha": head_sha,
                "upstream": upstream or None,
                "unique_commit_count": count,
                "unique_commits": commits,
                "unique_commits_truncated": count > len(commits),
            }
        )
    return sorted(records, key=lambda item: item["ref"])


def build_report(root: Path, *, baseline: str = "refs/remotes/origin/main") -> dict[str, Any]:
    root = _canonical_root(root)
    if BASELINE_REF.fullmatch(baseline) is None or ".." in baseline or baseline.endswith("/"):
        raise HygieneReportError("baseline reference is invalid")
    baseline_sha_raw = _git(
        root,
        ["rev-parse", "--verify", "--end-of-options", f"{baseline}^{{commit}}"],
    )
    assert isinstance(baseline_sha_raw, str)
    baseline_sha = baseline_sha_raw.strip()
    if SHA1.fullmatch(baseline_sha) is None:
        raise HygieneReportError("baseline revision is invalid")

    worktrees = []
    for record in _worktree_records(root):
        path = Path(str(record["worktree"]))
        try:
            canonical = path.resolve(strict=True)
        except OSError as error:
            raise HygieneReportError("a registered worktree is unavailable") from error
        if path.is_symlink() or not canonical.is_dir():
            raise HygieneReportError("a registered worktree is unsafe")
        count, commits = _unique_commits(root, baseline, str(record["HEAD"]))
        worktrees.append(
            {
                "path": canonical.as_posix(),
                "head_sha": record["HEAD"],
                "branch": record.get("branch"),
                "detached": record.get("detached") is True,
                "locked": "locked" in record,
                "prunable": "prunable" in record,
                "dirty_files": _status(canonical),
                "unique_commit_count": count,
                "unique_commits": commits,
                "unique_commits_truncated": count > len(commits),
            }
        )
    branches = _branch_records(root, baseline)
    return {
        "schema_version": SCHEMA_VERSION,
        "mode": "report-only",
        "mutation_performed": False,
        "baseline": {"ref": baseline, "head_sha": baseline_sha},
        "summary": {
            "branch_count": len(branches),
            "worktree_count": len(worktrees),
            "dirty_worktree_count": sum(bool(item["dirty_files"]) for item in worktrees),
            "unmerged_branch_count": sum(item["unique_commit_count"] > 0 for item in branches),
            "unmerged_worktree_count": sum(item["unique_commit_count"] > 0 for item in worktrees),
        },
        "branches": branches,
        "worktrees": sorted(worktrees, key=lambda item: item["path"]),
    }


def render_html(payload: dict[str, Any]) -> str:
    branch_rows = "".join(
        "<tr>"
        f"<td><code>{html.escape(item['ref'])}</code></td>"
        f"<td><code>{html.escape(item['head_sha'])}</code></td>"
        f"<td>{item['unique_commit_count']}</td>"
        "</tr>"
        for item in payload["branches"]
    )
    worktree_rows = "".join(
        "<tr>"
        f"<td><code>{html.escape(item['path'])}</code></td>"
        f"<td>{len(item['dirty_files'])}</td>"
        f"<td>{item['unique_commit_count']}</td>"
        f"<td>{'yes' if item['locked'] else 'no'}</td>"
        "</tr>"
        for item in payload["worktrees"]
    )
    return f"""<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'">
<title>Repository hygiene report</title><style>body{{font:15px system-ui;max-width:1200px;margin:2rem auto;padding:0 1rem}}table{{border-collapse:collapse;width:100%;margin-bottom:2rem}}th,td{{border:1px solid #ccc;padding:.5rem;text-align:left}}code{{overflow-wrap:anywhere}}</style></head>
<body><main><h1>Repository hygiene report</h1><p>Report-only: no refs or worktrees were deleted or changed.</p>
<h2>Branches</h2><table><thead><tr><th>Ref</th><th>HEAD</th><th>Unique commits</th></tr></thead><tbody>{branch_rows}</tbody></table>
<h2>Worktrees</h2><table><thead><tr><th>Path</th><th>Dirty files</th><th>Unique commits</th><th>Locked</th></tr></thead><tbody>{worktree_rows}</tbody></table>
</main></body></html>\n"""


def _write(path: Path, content: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_name(f".{path.name}.tmp")
    temporary.write_text(content, encoding="utf-8")
    os.replace(temporary, path)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=Path.cwd())
    parser.add_argument("--baseline", default="refs/remotes/origin/main")
    parser.add_argument("--json-output", type=Path, required=True)
    parser.add_argument("--html-output", type=Path, required=True)
    args = parser.parse_args()
    try:
        payload = build_report(args.root, baseline=args.baseline)
    except HygieneReportError as error:
        print(f"GIT_HYGIENE_REPORT_FAILED: {error}", file=os.sys.stderr)
        return 2
    _write(args.json_output, json.dumps(payload, indent=2, sort_keys=True) + "\n")
    _write(args.html_output, render_html(payload))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
