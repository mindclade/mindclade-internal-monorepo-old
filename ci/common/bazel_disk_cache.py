#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

import argparse
import json
import math
import os
import re
import shutil
import stat
import subprocess
import sys
import time
from pathlib import Path
from typing import NoReturn

MAX_SIZE_BYTES = 1024**3
MAX_SIZE_FLAG = "1G"
MAX_SIZE_MIB = MAX_SIZE_BYTES // 1024**2
GC_IDLE_DELAY = "1s"
SHUTDOWN_TIMEOUT_SECONDS = 120
FULL_SHA = re.compile(r"^[0-9a-f]{40}$")
FINGERPRINT = re.compile(r"^[0-9a-f]{64}$")
KEY_COMPONENT_PATTERN = r"[A-Za-z0-9][A-Za-z0-9._-]{0,31}"
KEY_COMPONENT = re.compile(rf"^{KEY_COMPONENT_PATTERN}$")
PULL_REQUEST_REF = re.compile(r"^refs/pull/[1-9][0-9]*/merge$")
CACHE_NAMESPACE = "bazel-disk-v2"
CACHE_RESTORE_PREFIX = re.compile(
    rf"^{CACHE_NAMESPACE}-{KEY_COMPONENT_PATTERN}-{KEY_COMPONENT_PATTERN}-[0-9a-f]{{64}}-$"
)
MAIN_REF = "refs/heads/main"


class CacheContractError(ValueError):
    pass


def _fail(message: str) -> NoReturn:
    raise CacheContractError(message)


def _validate_single_line(label: str, value: str) -> str:
    if "\n" in value or "\r" in value:
        _fail(f"{label} must be a single line")
    return value


def _require_main_base(label: str, value: str) -> None:
    if value not in {"main", MAIN_REF}:
        _fail(f"{label} must target the protected main branch")


def _active_lines(path: Path) -> set[str]:
    return {
        line
        for raw_line in path.read_text(encoding="utf-8").splitlines()
        if (line := raw_line.strip()) and not line.startswith("#")
    }


def cache_keys(
    *, runner_os: str, runner_arch: str, fingerprint: str, revision: str
) -> tuple[str, str]:
    for label, value in {"runner OS": runner_os, "runner architecture": runner_arch}.items():
        if KEY_COMPONENT.fullmatch(value) is None:
            _fail(f"{label} is not a safe cache-key component")
    if FINGERPRINT.fullmatch(fingerprint) is None:
        _fail("Bazel cache toolchain fingerprint is invalid")
    if FULL_SHA.fullmatch(revision) is None:
        _fail("trusted Bazel cache revision is not a full commit SHA")

    restore_prefix = f"{CACHE_NAMESPACE}-{runner_os}-{runner_arch}-{fingerprint}-"
    return f"{restore_prefix}{revision}", restore_prefix


def select_trust(
    *,
    workflow: str,
    event: str,
    ref: str,
    ref_protected: str,
    head_sha: str,
    fingerprint: str,
    runner_os: str,
    runner_arch: str,
    pull_request_base_sha: str = "",
    pull_request_base_ref: str = "",
    merge_group_base_sha: str = "",
    merge_group_base_ref: str = "",
) -> dict[str, str]:
    if ref_protected not in {"true", "false"}:
        _fail("GitHub protected-ref state must be true or false")
    if workflow == "presubmit":
        if event == "pull_request":
            if PULL_REQUEST_REF.fullmatch(ref) is None:
                _fail("pull-request Bazel cache requires a GitHub pull-request merge ref")
            _require_main_base("pull request", pull_request_base_ref)
            revision, role = pull_request_base_sha, "reader"
        elif event == "merge_group":
            _require_main_base("merge group", merge_group_base_ref)
            if not ref.startswith("refs/heads/gh-readonly-queue/main/"):
                _fail("merge-group Bazel cache requires the protected main queue ref")
            revision, role = merge_group_base_sha, "reader"
        elif event == "push" and ref == MAIN_REF:
            if ref_protected != "true":
                _fail("Bazel cache writes require protected main")
            revision, role = head_sha, "writer"
        elif event == "push":
            _fail("Bazel cache writes require a protected main-branch push")
        else:
            _fail(f"unsupported presubmit Bazel cache event: {event}")
    elif workflow == "nightly":
        if ref != MAIN_REF:
            _fail("nightly Bazel cache requires the main branch")
        if ref_protected != "true":
            _fail("nightly Bazel cache requires protected main")
        if event not in {"schedule", "workflow_dispatch"}:
            _fail("nightly Bazel cache requires schedule or workflow_dispatch")
        revision, role = head_sha, "writer"
    else:
        _fail(f"unsupported Bazel cache workflow: {workflow}")

    primary_key, restore_prefix = cache_keys(
        runner_os=runner_os,
        runner_arch=runner_arch,
        fingerprint=fingerprint,
        revision=revision,
    )
    return {
        "revision": revision,
        "role": role,
        "fingerprint": fingerprint,
        "primary-key": primary_key,
        "restore-prefix": restore_prefix,
    }


def append_github_output(path: Path, values: dict[str, str]) -> None:
    with path.open("a", encoding="utf-8") as stream:
        for key, value in values.items():
            stream.write(f"{key}={_validate_single_line(key, value)}\n")


def configure(
    *,
    cache_dir: Path,
    workspace: Path,
    bazelrc: Path,
    role: str,
    github_env: Path,
    restore_outcome: str,
) -> None:
    if role not in {"reader", "writer"}:
        _fail(f"invalid Bazel cache role: {role}")
    if not cache_dir.is_absolute() or any(character.isspace() for character in str(cache_dir)):
        _fail("Bazel disk cache must be an absolute whitespace-free path")
    if restore_outcome not in {"success", "failure", "cancelled"}:
        _fail("configure requires a completed cache-restore outcome")

    if cache_dir.is_symlink():
        _fail("Bazel disk cache root must not be a symbolic link")

    resolved_workspace = workspace.resolve(strict=True)
    resolved_cache = cache_dir.resolve(strict=False)
    try:
        resolved_cache.relative_to(resolved_workspace)
    except ValueError:
        pass
    else:
        _fail("Bazel disk cache must remain outside the source checkout")

    expected_bazelrc = resolved_workspace / "user.bazelrc"
    if bazelrc.resolve(strict=False) != expected_bazelrc:
        _fail("Bazel disk cache configuration must use workspace user.bazelrc")
    if "try-import %workspace%/user.bazelrc" not in _active_lines(resolved_workspace / ".bazelrc"):
        _fail("workspace .bazelrc must try-import user.bazelrc")
    ignored = _active_lines(resolved_workspace / ".gitignore")
    if "user.bazelrc" not in ignored or "!user.bazelrc" in ignored:
        _fail("user.bazelrc must remain gitignored")

    if restore_outcome != "success" and cache_dir.exists():
        shutil.rmtree(cache_dir)
        print("::warning::Bazel cache restore failed; continuing with an empty cold cache")
    cache_dir.mkdir(mode=0o700, parents=True, exist_ok=True)
    cache_dir.chmod(0o700)
    upload = "true" if role == "writer" else "false"
    contents = (
        "# Generated by CI; contains no credentials.\n"
        f"build --disk_cache={resolved_cache}\n"
        f"build --remote_upload_local_results={upload}\n"
        "build --noremote_cache_async\n"
        "build --remote_verify_downloads\n"
        "build --remote_cache_compression\n"
        f"build --experimental_disk_cache_gc_max_size={MAX_SIZE_FLAG}\n"
        f"build --experimental_disk_cache_gc_idle_delay={GC_IDLE_DELAY}\n"
    )
    temporary = bazelrc.with_name(f"{bazelrc.name}.tmp")
    temporary.write_text(contents, encoding="utf-8")
    temporary.chmod(0o600)
    os.replace(temporary, bazelrc)
    with github_env.open("a", encoding="utf-8") as stream:
        stream.write("BAZELISK_GITHUB_TOKEN=\n")


def directory_size(path: Path) -> int:
    if not path.exists():
        return 0
    if path.is_symlink():
        _fail("Bazel disk cache root must not be a symbolic link")
    if not path.is_dir():
        _fail("Bazel disk cache root must be a directory")
    total = 0
    for candidate in path.rglob("*"):
        metadata = candidate.lstat()
        if stat.S_ISLNK(metadata.st_mode):
            _fail(f"Bazel disk cache contains a symbolic link: {candidate}")
        if stat.S_ISREG(metadata.st_mode):
            total += metadata.st_size
        elif not stat.S_ISDIR(metadata.st_mode):
            _fail(f"Bazel disk cache contains a special file: {candidate}")
    return total


def quiesce_bazel(
    *, cache_dir: Path, workspace: Path, bazel_wrapper: Path, wait_seconds: float
) -> None:
    if not math.isfinite(wait_seconds) or wait_seconds < 0 or wait_seconds > 30:
        _fail("GC wait must be between zero and thirty seconds")

    resolved_workspace = workspace.resolve(strict=True)
    expected_wrapper = resolved_workspace / "tools/dev/bazelw"
    if bazel_wrapper.resolve(strict=True) != expected_wrapper or expected_wrapper.is_symlink():
        _fail("Bazel shutdown must use the repository wrapper")
    if not expected_wrapper.is_file() or not os.access(expected_wrapper, os.X_OK):
        _fail("repository Bazel wrapper must be an executable file")

    resolved_cache = cache_dir.resolve(strict=True)
    active_bazelrc = _active_lines(resolved_workspace / "user.bazelrc")
    required_options = {
        f"build --disk_cache={resolved_cache}",
        "build --noremote_cache_async",
        f"build --experimental_disk_cache_gc_max_size={MAX_SIZE_FLAG}",
        f"build --experimental_disk_cache_gc_idle_delay={GC_IDLE_DELAY}",
    }
    if not required_options.issubset(active_bazelrc):
        _fail("user.bazelrc does not contain the quiescence contract")

    time.sleep(wait_seconds)
    environment = os.environ.copy()
    environment.pop("BAZELISK_GITHUB_TOKEN", None)
    try:
        result = subprocess.run(
            [str(expected_wrapper), "shutdown"],
            cwd=resolved_workspace,
            env=environment,
            check=False,
            timeout=SHUTDOWN_TIMEOUT_SECONDS,
        )
    except subprocess.TimeoutExpired:
        _fail("Bazel shutdown timed out before disk-cache persistence")
    if result.returncode != 0:
        _fail("Bazel shutdown failed before disk-cache persistence")


def measure(*, cache_dir: Path) -> dict[str, str]:
    size_bytes = directory_size(cache_dir)
    within_limit = size_bytes <= MAX_SIZE_BYTES
    if not within_limit:
        print(
            "::warning::Bazel disk cache is "
            f"{size_bytes} bytes; skipping persistence above the {MAX_SIZE_MIB} MiB ceiling"
        )
    return {
        "size-bytes": str(size_bytes),
        "within-limit": str(within_limit).lower(),
    }


def quiesce_and_measure(
    *, cache_dir: Path, workspace: Path, bazel_wrapper: Path, wait_seconds: float
) -> dict[str, str]:
    quiesce_bazel(
        cache_dir=cache_dir,
        workspace=workspace,
        bazel_wrapper=bazel_wrapper,
        wait_seconds=wait_seconds,
    )
    return measure(cache_dir=cache_dir)


def record_metrics(
    *,
    evidence_dir: Path,
    summary: Path,
    role: str,
    trusted_revision: str,
    primary_key: str,
    matched_key: str,
    exact_hit: str,
    save_outcome: str,
    size_bytes: int,
    within_limit: str,
    restore_prefix: str,
    restore_outcome: str,
    measure_outcome: str,
) -> dict[str, object]:
    role = role or "unknown"
    if role not in {"reader", "writer", "unknown"}:
        _fail(f"invalid metrics cache role: {role}")
    for label, value in {
        "primary key": primary_key,
        "matched key": matched_key,
        "restore prefix": restore_prefix,
    }.items():
        _validate_single_line(label, value)
    if exact_hit not in {"", "true", "false"}:
        _fail("exact-hit metric must be true, false, or empty")
    if within_limit not in {"", "true", "false"}:
        _fail("within-limit metric must be true, false, or empty")
    if save_outcome not in {"", "success", "failure", "cancelled", "skipped"}:
        _fail("save outcome is invalid")
    if restore_outcome not in {"", "success", "failure", "cancelled", "skipped"}:
        _fail("restore outcome is invalid")
    if measure_outcome not in {"", "success", "failure", "cancelled", "skipped"}:
        _fail("measurement outcome is invalid")
    if size_bytes < 0:
        _fail("cache size must not be negative")
    if within_limit and (within_limit == "true") != (size_bytes <= MAX_SIZE_BYTES):
        _fail("cache size and within-limit metric disagree")
    if measure_outcome == "success" and not within_limit:
        _fail("successful cache measurement must report its size limit result")
    if measure_outcome != "success" and within_limit:
        _fail("unsuccessful cache measurement must not report a size limit result")

    if role == "unknown":
        if any((trusted_revision, primary_key, matched_key, restore_prefix)):
            _fail("unknown cache role must not carry trusted cache metadata")
    else:
        if FULL_SHA.fullmatch(trusted_revision) is None:
            _fail("metrics trusted revision is invalid")
        expected_primary = f"{restore_prefix}{trusted_revision}"
        if CACHE_RESTORE_PREFIX.fullmatch(restore_prefix) is None:
            _fail("metrics restore prefix is invalid")
        if primary_key != expected_primary:
            _fail("metrics primary key does not match the trusted revision")
        if matched_key and (
            not matched_key.startswith(restore_prefix)
            or FULL_SHA.fullmatch(matched_key.removeprefix(restore_prefix)) is None
        ):
            _fail("metrics matched key is outside the trusted cache namespace")
        if exact_hit == "true" and matched_key != primary_key:
            _fail("exact-hit metric does not match the restored key")
        if restore_outcome != "success" and (matched_key or exact_hit == "true"):
            _fail("unsuccessful restore cannot report a cache match")
        if role == "reader" and save_outcome == "success":
            _fail("read-only cache role cannot report a successful save")

    if restore_outcome == "success":
        if exact_hit == "true":
            restore_state = "exact"
        elif matched_key:
            restore_state = "prefix"
        else:
            restore_state = "not-restored"
    elif restore_outcome in {"failure", "cancelled"}:
        restore_state = "error"
    elif restore_outcome == "skipped":
        restore_state = "skipped"
    else:
        restore_state = "unavailable"

    normalized_save_outcome = save_outcome or "skipped"
    if save_outcome == "success":
        save_state = "attempted-unverified"
    elif save_outcome in {"failure", "cancelled"}:
        save_state = "error"
    else:
        save_state = "skipped"

    if measure_outcome == "success":
        measure_state = "measured"
    elif measure_outcome in {"failure", "cancelled"}:
        measure_state = "error"
    elif measure_outcome == "skipped":
        measure_state = "skipped"
    else:
        measure_state = "unavailable"

    measurement_succeeded = measure_outcome == "success"
    payload: dict[str, object] = {
        "schema_version": 1,
        "cache_kind": "bazel-disk-cache",
        "transport": "github-actions-cache",
        "remote_cache": False,
        "role": role,
        "trusted_revision": trusted_revision,
        "primary_key": primary_key,
        "matched_key": matched_key,
        "restore_state": restore_state,
        "restore_step_outcome": restore_outcome or "unavailable",
        "restore_found": bool(matched_key),
        "restore_exact_hit": exact_hit == "true",
        "restore_verified": bool(matched_key),
        "save_state": save_state,
        "save_step_outcome": normalized_save_outcome,
        "save_verified": False,
        "measurement_state": measure_state,
        "measurement_step_outcome": measure_outcome or "unavailable",
        "size_verified": measurement_succeeded,
        "size_bytes": size_bytes if measurement_succeeded else None,
        "max_size_bytes": MAX_SIZE_BYTES,
        "within_limit": within_limit == "true" if measurement_succeeded else None,
    }
    evidence_dir.mkdir(mode=0o700, parents=True, exist_ok=True)
    destination = evidence_dir / "cache-metrics.json"
    temporary = destination.with_name(f"{destination.name}.tmp")
    temporary.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    temporary.chmod(0o600)
    os.replace(temporary, destination)

    with summary.open("a", encoding="utf-8") as stream:
        stream.write("### Bazel persistent action cache\n\n")
        stream.write(
            "GitHub Actions transports a bounded `--disk_cache`; this is not remote REAPI/GCS.\n\n"
        )
        stream.write("| Metric | Value |\n| --- | --- |\n")
        stream.write(f"| Role | `{role}` |\n")
        stream.write(f"| Restore | `{restore_state}` |\n")
        stream.write(f"| Save attempt | `{save_state}` (step: `{normalized_save_outcome}`) |\n")
        stream.write(f"| Measurement | `{measure_state}` |\n")
        if measure_state == "measured":
            stream.write(f"| Size | `{size_bytes / 1024**2:.1f} MiB` / `{MAX_SIZE_MIB} MiB` |\n")
        else:
            stream.write("| Size | `unavailable` |\n")
    return payload


def _nonnegative_int(value: str) -> int:
    parsed = int(value or 0)
    if parsed < 0:
        raise argparse.ArgumentTypeError("must not be negative")
    return parsed


def parser() -> argparse.ArgumentParser:
    root = argparse.ArgumentParser(description="Govern the GitHub-transported Bazel disk cache")
    commands = root.add_subparsers(dest="command", required=True)

    trust = commands.add_parser("select-trust")
    trust.add_argument("--workflow", choices=("presubmit", "nightly"), required=True)
    trust.add_argument("--event", required=True)
    trust.add_argument("--ref", required=True)
    trust.add_argument("--ref-protected", required=True)
    trust.add_argument("--head-sha", required=True)
    trust.add_argument("--fingerprint", required=True)
    trust.add_argument("--runner-os", required=True)
    trust.add_argument("--runner-arch", required=True)
    trust.add_argument("--pull-request-base-sha", default="")
    trust.add_argument("--pull-request-base-ref", default="")
    trust.add_argument("--merge-group-base-sha", default="")
    trust.add_argument("--merge-group-base-ref", default="")
    trust.add_argument("--github-output", type=Path, required=True)

    configure_parser = commands.add_parser("configure")
    configure_parser.add_argument("--cache-dir", type=Path, required=True)
    configure_parser.add_argument("--workspace", type=Path, required=True)
    configure_parser.add_argument("--bazelrc", type=Path, required=True)
    configure_parser.add_argument("--role", required=True)
    configure_parser.add_argument("--github-env", type=Path, required=True)
    configure_parser.add_argument("--restore-outcome", required=True)

    measure_parser = commands.add_parser("measure")
    measure_parser.add_argument("--cache-dir", type=Path, required=True)
    measure_parser.add_argument("--gc-wait-seconds", type=float, default=3)
    measure_parser.add_argument("--workspace", type=Path, required=True)
    measure_parser.add_argument("--bazel-wrapper", type=Path, required=True)
    measure_parser.add_argument("--github-output", type=Path, required=True)

    metrics = commands.add_parser("record-metrics")
    metrics.add_argument("--evidence-dir", type=Path, required=True)
    metrics.add_argument("--summary", type=Path, required=True)
    metrics.add_argument("--role", default="")
    metrics.add_argument("--trusted-revision", default="")
    metrics.add_argument("--primary-key", default="")
    metrics.add_argument("--matched-key", default="")
    metrics.add_argument("--exact-hit", default="")
    metrics.add_argument("--save-outcome", default="")
    metrics.add_argument("--size-bytes", type=_nonnegative_int, default=0)
    metrics.add_argument("--within-limit", default="")
    metrics.add_argument("--restore-prefix", default="")
    metrics.add_argument("--restore-outcome", default="")
    metrics.add_argument("--measure-outcome", default="")
    return root


def main() -> int:
    args = parser().parse_args()
    try:
        if args.command == "select-trust":
            values = select_trust(
                workflow=args.workflow,
                event=args.event,
                ref=args.ref,
                ref_protected=args.ref_protected,
                head_sha=args.head_sha,
                fingerprint=args.fingerprint,
                runner_os=args.runner_os,
                runner_arch=args.runner_arch,
                pull_request_base_sha=args.pull_request_base_sha,
                pull_request_base_ref=args.pull_request_base_ref,
                merge_group_base_sha=args.merge_group_base_sha,
                merge_group_base_ref=args.merge_group_base_ref,
            )
            append_github_output(args.github_output, values)
        elif args.command == "configure":
            configure(
                cache_dir=args.cache_dir,
                workspace=args.workspace,
                bazelrc=args.bazelrc,
                role=args.role,
                github_env=args.github_env,
                restore_outcome=args.restore_outcome,
            )
        elif args.command == "measure":
            append_github_output(
                args.github_output,
                quiesce_and_measure(
                    cache_dir=args.cache_dir,
                    workspace=args.workspace,
                    bazel_wrapper=args.bazel_wrapper,
                    wait_seconds=args.gc_wait_seconds,
                ),
            )
        else:
            record_metrics(
                evidence_dir=args.evidence_dir,
                summary=args.summary,
                role=args.role,
                trusted_revision=args.trusted_revision,
                primary_key=args.primary_key,
                matched_key=args.matched_key,
                exact_hit=args.exact_hit,
                save_outcome=args.save_outcome,
                size_bytes=args.size_bytes,
                within_limit=args.within_limit,
                restore_prefix=args.restore_prefix,
                restore_outcome=args.restore_outcome,
                measure_outcome=args.measure_outcome,
            )
    except (CacheContractError, OSError) as error:
        print(f"Bazel disk-cache contract failed: {error}", file=sys.stderr)
        return 2
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
