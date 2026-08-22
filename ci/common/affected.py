# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Fail-closed affected Bazel selection and evidence-producing execution.

Bazel's post-loading graph is the dependency authority. This module maps changed
files to conservative owning-package seeds, asks Bazel for reverse dependencies,
and executes configured analysis and tests over the resulting target files.
"""

from __future__ import annotations

import argparse
import gzip
import json
import os
import subprocess
import sys
import time
from collections.abc import Callable, Iterable, Sequence
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path, PurePosixPath
from typing import Any

ROOT = Path(__file__).resolve().parents[2]
SCHEMA_VERSION = 1
FULL_TARGET = "//..."
LATENCY_SLO_SECONDS = 30 * 60

GLOBAL_EXACT_PATHS = frozenset(
    {
        ".bazelrc",
        ".bazelversion",
        ".buildifier.json",
        "BUILD",
        "BUILD.bazel",
        "Cargo.lock",
        "Cargo.toml",
        "MODULE.bazel",
        "MODULE.bazel.lock",
        "REPO.bazel",
        "WORKSPACE",
        "WORKSPACE.bazel",
        "bazel_downloader.cfg",
        "components.toml",
        "deny.toml",
        "flake.lock",
        "flake.nix",
        "go.mod",
        "go.sum",
        "maturity.toml",
        "package.json",
        "pnpm-lock.yaml",
        "pnpm-workspace.yaml",
        "pyproject.toml",
        "uv.lock",
    }
)
GLOBAL_PREFIXES = (
    ".buildkite/",
    ".github/",
    "architecture/",
    "ci/",
    "protocols/",
    "qualification/",
    "tools/build/",
    "tools/dev/bazel-repo-bin/",
    "tools/qualification/",
)
STRUCTURAL_STATUSES = frozenset({"C", "D", "R", "T", "U", "X", "B"})

RUST_PREFIXES = (
    "libs/rust/",
    "protocols/rust/",
    "protocols/proto/mindclade/runtime/",
    "services/runtime_gateway/",
    "services/runtime_host/",
    "services/artifact_proxy/",
    "services/node_agent/",
    "services/workers/ingestion/",
    "serving/runtime/",
    "Cargo.toml",
    "Cargo.lock",
    "deny.toml",
    "security/rust-supply-chain.toml",
    "tools/qualification/rust/",
    "tools/build/nix/",
    "flake.nix",
    "flake.lock",
)


class SelectionError(RuntimeError):
    """The affected set could not be established authoritatively."""


@dataclass(frozen=True)
class Change:
    status: str
    path: str
    old_path: str | None = None

    def as_dict(self) -> dict[str, str]:
        payload = {"status": self.status, "path": self.path}
        if self.old_path is not None:
            payload["old_path"] = self.old_path
        return payload


@dataclass(frozen=True)
class Selection:
    mode: str
    reason: str
    changes: tuple[Change, ...]
    seeds: tuple[str, ...]
    analysis_targets: tuple[str, ...]
    test_targets: tuple[str, ...]
    base_sha: str | None
    head_sha: str
    event: str
    analysis_query: str | None = None
    test_query: str | None = None

    def as_dict(self) -> dict[str, Any]:
        return {
            "schema_version": SCHEMA_VERSION,
            "base_sha": self.base_sha,
            "head_sha": self.head_sha,
            "event": self.event,
            "mode": self.mode,
            "reason": self.reason,
            "changed_paths": [change.as_dict() for change in self.changes],
            "seeds": list(self.seeds),
            "analysis_targets": list(self.analysis_targets),
            "test_targets": list(self.test_targets),
            "counts": {
                "changed_paths": len(self.changes),
                "seeds": len(self.seeds),
                "analysis_targets": len(self.analysis_targets),
                "test_targets": len(self.test_targets),
            },
            "queries": {"analysis": self.analysis_query, "test": self.test_query},
        }


Query = Callable[[str], Sequence[str]]


def utc_now() -> str:
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


def _normalize_path(raw: str) -> str:
    path = PurePosixPath(raw)
    if path.is_absolute() or not raw or ".." in path.parts:
        raise SelectionError(f"unsafe changed path: {raw!r}")
    normalized = path.as_posix()
    if normalized == ".":
        raise SelectionError(f"unsafe changed path: {raw!r}")
    return normalized


def _normalize_changes(changes: Iterable[Change | str]) -> tuple[Change, ...]:
    normalized: list[Change] = []
    for item in changes:
        change = item if isinstance(item, Change) else Change(status="M", path=item)
        normalized.append(
            Change(
                status=change.status[:1].upper(),
                path=_normalize_path(change.path),
                old_path=_normalize_path(change.old_path) if change.old_path else None,
            )
        )
    return tuple(normalized)


def rust_qualification_required(changed: Iterable[Change | str]) -> bool:
    changes = _normalize_changes(changed)
    return not changes or any(
        change.path == prefix or change.path.startswith(prefix)
        for change in changes
        for prefix in RUST_PREFIXES
    )


def _run_git(args: Sequence[str], *, root: Path = ROOT) -> subprocess.CompletedProcess[bytes]:
    return subprocess.run(["git", *args], cwd=root, check=False, capture_output=True)


def git_revision(revision: str, *, root: Path = ROOT) -> str:
    result = _run_git(["rev-parse", "--verify", f"{revision}^{{commit}}"], root=root)
    if result.returncode:
        detail = result.stderr.decode(errors="replace").strip()
        raise SelectionError(f"invalid git revision {revision!r}: {detail}")
    return result.stdout.decode().strip()


def git_changed(base: str, *, root: Path = ROOT) -> tuple[Change, ...]:
    """Return an authoritative, rename-aware diff from ``base`` to ``HEAD``."""

    base_sha = git_revision(base, root=root)
    head_sha = git_revision("HEAD", root=root)
    ancestor = _run_git(["merge-base", "--is-ancestor", base_sha, head_sha], root=root)
    if ancestor.returncode:
        raise SelectionError(f"base {base_sha} is not an ancestor of HEAD {head_sha}")
    result = _run_git(
        ["diff", "--name-status", "-z", "--find-renames", f"{base_sha}...{head_sha}"],
        root=root,
    )
    if result.returncode:
        detail = result.stderr.decode(errors="replace").strip()
        raise SelectionError(f"git diff failed: {detail}")

    fields = result.stdout.split(b"\0")
    if fields and fields[-1] == b"":
        fields.pop()
    changes: list[Change] = []
    index = 0
    while index < len(fields):
        status_text = fields[index].decode("utf-8", errors="strict")
        index += 1
        status = status_text[:1].upper()
        if status in {"C", "R"}:
            if index + 1 >= len(fields):
                raise SelectionError("malformed rename/copy record from git diff")
            old_path = fields[index].decode("utf-8", errors="strict")
            new_path = fields[index + 1].decode("utf-8", errors="strict")
            index += 2
            changes.append(Change(status=status, path=new_path, old_path=old_path))
        else:
            if index >= len(fields):
                raise SelectionError("malformed path record from git diff")
            path = fields[index].decode("utf-8", errors="strict")
            index += 1
            changes.append(Change(status=status, path=path))
    return _normalize_changes(changes)


def _global_reason(change: Change) -> str | None:
    if change.status in STRUCTURAL_STATUSES:
        return f"structural_{change.status.lower()}"
    path = change.path
    if path in GLOBAL_EXACT_PATHS:
        return f"global_path:{path}"
    if path.endswith(".bzl"):
        return f"starlark:{path}"
    for prefix in GLOBAL_PREFIXES:
        if path.startswith(prefix):
            return f"global_prefix:{prefix}"
    return None


def _package_for(path: str, *, root: Path = ROOT) -> Path | None:
    candidate = root / path
    current = candidate if candidate.is_dir() else candidate.parent
    while True:
        if (current / "BUILD.bazel").is_file() or (current / "BUILD").is_file():
            return current
        if current == root:
            return None
        if root not in current.parents:
            return None
        current = current.parent


def _package_pattern(package: Path, *, root: Path = ROOT) -> str:
    relative = package.relative_to(root).as_posix()
    return "//:*" if relative == "." else f"//{relative}:*"


def _query_expression(seeds: Sequence[str], *, tests: bool) -> str:
    seed_set = "set(" + " ".join(json.dumps(seed) for seed in seeds) + ")"
    affected = f"rdeps(//..., {seed_set})"
    manual_pattern = json.dumps(r"[\[ ]manual[,\]]")
    selected = "tests($affected)" if tests else 'kind(".* rule", $affected)'
    return (
        f"let affected = {affected} in "
        f"({selected} except attr(\"tags\", {manual_pattern}, $affected))"
    )


def bazel_query(
    expression: str,
    *,
    root: Path = ROOT,
    bazel: Path | None = None,
) -> tuple[str, ...]:
    launcher = bazel or root / "tools/dev/bazelw"
    result = subprocess.run(
        [
            str(launcher),
            "query",
            expression,
            "--config=ci",
            "--output=label",
            "--order_output=no",
        ],
        cwd=root,
        text=True,
        capture_output=True,
        check=False,
    )
    if result.returncode:
        if result.stdout:
            print(result.stdout, end="", file=sys.stderr)
        if result.stderr:
            print(result.stderr, end="", file=sys.stderr)
        detail = (result.stderr or result.stdout).strip()
        if len(detail) > 4000:
            detail = detail[-4000:]
        raise SelectionError(f"Bazel query failed with exit {result.returncode}: {detail}")
    return tuple(sorted(set(line.strip() for line in result.stdout.splitlines() if line.strip())))


def select(
    changed: Iterable[Change | str],
    *,
    mode: str = "affected",
    base_sha: str | None = None,
    head_sha: str | None = None,
    event: str = "local",
    root: Path = ROOT,
    query: Query | None = None,
) -> Selection:
    if mode not in {"affected", "full"}:
        raise SelectionError(f"unsupported selection mode: {mode}")
    changes = _normalize_changes(changed)
    head = head_sha or git_revision("HEAD", root=root)
    if mode == "full":
        return Selection(
            mode="full",
            reason="explicit_full",
            changes=changes,
            seeds=(FULL_TARGET,),
            analysis_targets=(FULL_TARGET,),
            test_targets=(FULL_TARGET,),
            base_sha=base_sha,
            head_sha=head,
            event=event,
        )

    for change in changes:
        reason = _global_reason(change)
        if reason is not None:
            return Selection(
                mode="full",
                reason=reason,
                changes=changes,
                seeds=(FULL_TARGET,),
                analysis_targets=(FULL_TARGET,),
                test_targets=(FULL_TARGET,),
                base_sha=base_sha,
                head_sha=head,
                event=event,
            )
        if not (root / change.path).exists():
            return Selection(
                mode="full",
                reason=f"missing_changed_path:{change.path}",
                changes=changes,
                seeds=(FULL_TARGET,),
                analysis_targets=(FULL_TARGET,),
                test_targets=(FULL_TARGET,),
                base_sha=base_sha,
                head_sha=head,
                event=event,
            )

    seeds: set[str] = set()
    for change in changes:
        package = _package_for(change.path, root=root)
        if package is None:
            return Selection(
                mode="full",
                reason=f"unowned_path:{change.path}",
                changes=changes,
                seeds=(FULL_TARGET,),
                analysis_targets=(FULL_TARGET,),
                test_targets=(FULL_TARGET,),
                base_sha=base_sha,
                head_sha=head,
                event=event,
            )
        seeds.add(_package_pattern(package, root=root))

    ordered_seeds = tuple(sorted(seeds))
    if not ordered_seeds:
        return Selection(
            mode="affected",
            reason="no_changed_paths",
            changes=changes,
            seeds=(),
            analysis_targets=(),
            test_targets=(),
            base_sha=base_sha,
            head_sha=head,
            event=event,
        )
    analysis_expression = _query_expression(ordered_seeds, tests=False)
    test_expression = _query_expression(ordered_seeds, tests=True)
    run_query = query or (lambda expression: bazel_query(expression, root=root))
    analysis_targets = tuple(sorted(set(run_query(analysis_expression))))
    test_targets = tuple(sorted(set(run_query(test_expression))))
    return Selection(
        mode="affected",
        reason="bazel_reverse_dependencies",
        changes=changes,
        seeds=ordered_seeds,
        analysis_targets=analysis_targets,
        test_targets=test_targets,
        base_sha=base_sha,
        head_sha=head,
        event=event,
        analysis_query=analysis_expression,
        test_query=test_expression,
    )


def _safe_evidence_dir(path: Path, *, root: Path = ROOT) -> Path:
    if path.exists() and path.is_symlink():
        raise SelectionError(f"evidence directory must not be a symbolic link: {path}")
    resolved = path.resolve()
    try:
        resolved.relative_to(root.resolve())
    except ValueError:
        pass
    else:
        raise SelectionError("evidence directory must be outside the source checkout")
    resolved.mkdir(parents=True, exist_ok=True)
    return resolved


def _write_json(path: Path, payload: dict[str, Any]) -> None:
    path.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n", encoding="utf-8")


def _write_targets(path: Path, targets: Sequence[str]) -> None:
    path.write_text("".join(f"{target}\n" for target in targets), encoding="utf-8")


def _append_step_summary(path: Path) -> None:
    summary = os.environ.get("GITHUB_STEP_SUMMARY")
    if not summary or not path.is_file():
        return
    with Path(summary).open("a", encoding="utf-8") as destination:
        destination.write(path.read_text(encoding="utf-8"))
        destination.write("\n")


def _compress(path: Path) -> None:
    if not path.is_file() or not path.stat().st_size:
        return
    compressed = path.with_name(path.name + ".gz")
    compressed.write_bytes(gzip.compress(path.read_bytes(), compresslevel=9, mtime=0))
    path.unlink()


def _run_phase(
    phase: str,
    targets: Sequence[str],
    *,
    evidence_dir: Path,
    root: Path = ROOT,
) -> dict[str, Any]:
    started_at = utc_now()
    started = time.monotonic()
    if not targets:
        return {
            "phase": phase,
            "status": "skipped",
            "exit_code": 0,
            "reason": "no_targets",
            "started_at": started_at,
            "elapsed_seconds": 0.0,
        }

    target_file = evidence_dir / f"{phase}.targets"
    bep = evidence_dir / f"{phase}.bep.json"
    profile = evidence_dir / f"{phase}.profile.json.gz"
    _write_targets(target_file, targets)
    verb = "build" if phase == "analysis" else "test"
    command = [
        str(root / "tools/dev/bazelw"),
        verb,
        "--config=ci",
        f"--target_pattern_file={target_file}",
        f"--build_event_json_file={bep}",
        f"--profile={profile}",
    ]
    if phase == "analysis":
        command.insert(2, "--nobuild")
    environment = os.environ.copy()
    environment.pop("BAZELISK_GITHUB_TOKEN", None)
    result = subprocess.run(command, cwd=root, env=environment, check=False)

    summary_status = 0
    if bep.is_file() and bep.stat().st_size:
        summary_json = evidence_dir / f"{phase}.summary.json"
        summary_markdown = evidence_dir / f"{phase}.summary.md"
        summary = subprocess.run(
            [
                sys.executable,
                str(root / "tools/analysis/summarize_bazel_bep.py"),
                "--bep",
                str(bep),
                "--label",
                "configured analysis" if phase == "analysis" else "Bazel test",
                "--json-output",
                str(summary_json),
                "--markdown-output",
                str(summary_markdown),
            ],
            cwd=root,
            check=False,
        )
        summary_status = summary.returncode
        _append_step_summary(summary_markdown)
        _compress(bep)

    exit_code = result.returncode or summary_status
    return {
        "phase": phase,
        "status": "passed" if exit_code == 0 else "failed",
        "exit_code": exit_code,
        "bazel_exit_code": result.returncode,
        "summary_exit_code": summary_status,
        "started_at": started_at,
        "elapsed_seconds": round(time.monotonic() - started, 3),
        "target_count": len(targets),
        "command": command,
    }


def write_failure_evidence(
    evidence_dir: Path,
    *,
    mode: str,
    event: str,
    base_sha: str | None,
    error: Exception,
    root: Path = ROOT,
) -> None:
    directory = _safe_evidence_dir(evidence_dir, root=root)
    _write_json(
        directory / "selection.json",
        {
            "schema_version": SCHEMA_VERSION,
            "base_sha": base_sha,
            "event": event,
            "mode": mode,
            "reason": "selection_failed",
            "error": {"type": type(error).__name__, "message": str(error)[-4000:]},
        },
    )


def execute_selection(
    selection: Selection,
    evidence_dir: Path,
    *,
    job_started_epoch: float | None = None,
    root: Path = ROOT,
) -> int:
    directory = _safe_evidence_dir(evidence_dir, root=root)
    payload = selection.as_dict()
    _write_json(directory / "selection.json", payload)

    execution: list[dict[str, Any]] = []
    analysis = _run_phase(
        "analysis", selection.analysis_targets, evidence_dir=directory, root=root
    )
    execution.append(analysis)
    if analysis["exit_code"] == 0:
        execution.append(
            _run_phase("test", selection.test_targets, evidence_dir=directory, root=root)
        )
    else:
        execution.append(
            {
                "phase": "test",
                "status": "skipped",
                "exit_code": analysis["exit_code"],
                "reason": "analysis_failed",
                "started_at": utc_now(),
                "elapsed_seconds": 0.0,
            }
        )

    total_elapsed = None
    if job_started_epoch is not None:
        total_elapsed = round(max(0.0, time.time() - job_started_epoch), 3)
    payload["execution"] = execution
    payload["completed_at"] = utc_now()
    payload["job_elapsed_seconds"] = total_elapsed
    payload["latency_slo_seconds"] = LATENCY_SLO_SECONDS
    payload["latency_slo_met"] = (
        None
        if total_elapsed is None or selection.mode != "affected"
        else total_elapsed <= LATENCY_SLO_SECONDS
    )
    _write_json(directory / "selection.json", payload)
    _write_json(
        directory / "run-metrics.json",
        {
            "schema_version": SCHEMA_VERSION,
            "event": selection.event,
            "mode": selection.mode,
            "reason": selection.reason,
            "head_sha": selection.head_sha,
            "completed_at": payload["completed_at"],
            "job_elapsed_seconds": total_elapsed,
            "latency_slo_seconds": LATENCY_SLO_SECONDS,
            "latency_slo_met": payload["latency_slo_met"],
            "analysis_target_count": len(selection.analysis_targets),
            "test_target_count": len(selection.test_targets),
        },
    )
    return next((item["exit_code"] for item in execution if item["exit_code"]), 0)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("paths", nargs="*")
    parser.add_argument("--base")
    parser.add_argument("--mode", choices=("affected", "full"), default="affected")
    parser.add_argument("--event", default=os.environ.get("GITHUB_EVENT_NAME", "local"))
    parser.add_argument("--format", choices=("lines", "json"), default="lines")
    parser.add_argument("--kind", choices=("analysis", "test"), default="test")
    args = parser.parse_args()
    if args.mode == "affected" and not args.paths and not args.base:
        parser.error("affected mode requires paths or --base")
    changes = (
        tuple(Change(status="M", path=path) for path in args.paths)
        if args.paths
        else git_changed(args.base)
        if args.mode == "affected"
        else ()
    )
    base_sha = git_revision(args.base) if args.base else None
    selection = select(changes, mode=args.mode, base_sha=base_sha, event=args.event)
    if args.format == "json":
        print(json.dumps(selection.as_dict(), indent=2, sort_keys=True))
    else:
        targets = (
            selection.analysis_targets if args.kind == "analysis" else selection.test_targets
        )
        print("\n".join(targets))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
