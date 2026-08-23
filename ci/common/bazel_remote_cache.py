#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import shutil
import signal
import stat
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from datetime import datetime
from pathlib import Path
from typing import NoReturn

MAIN_REF = "refs/heads/main"
PULL_REQUEST_REF = re.compile(r"^refs/pull/[1-9][0-9]*/merge$")
MERGE_GROUP_REF = re.compile(r"^refs/heads/gh-readonly-queue/main/")
PROJECT_ID = re.compile(r"^[a-z][a-z0-9-]{0,18}[a-z0-9]-common-ci$")
SHA256 = re.compile(r"^sha256:[0-9a-f]{64}$")
EVIDENCE_OBJECT = re.compile(
    r"^gs://mc-production-qualification-evidence/bazel-cache/[^#]+#[1-9][0-9]*$"
)
WIF_PROVIDER = re.compile(
    r"^projects/[1-9][0-9]*/locations/global/workloadIdentityPools/github/providers/gh-bazel-cache$"
)
AUTH_CREDENTIAL_NAME = re.compile(r"^gha-creds-[a-z0-9]{16}\.json$")
GATEWAY_TARGET = "//tools/build/bazel/cache_gateway/cmd:cache_gateway"
LOOPBACK_ENDPOINT = "http://127.0.0.1:8085"
ALLOWED_STATES = {"blocked", "qualified-v1"}
MAXIMUM_BODY_BYTES = 1024**3
MAXIMUM_CONCURRENT_STAGING = 2
MAXIMUM_CREDENTIAL_BYTES = 64 * 1024
MAXIMUM_GATEWAY_LOG_BYTES = 16 * 1024**2
QUALIFICATION_FIELDS = {
    "access_logging",
    "bucket_retention",
    "bounded_staging_load",
    "cache_loss_rebuild",
    "cas_integrity",
    "cmek_encryption",
    "cold_rebuild",
    "corrupt_download_rejected",
    "duplicate_write_idempotent",
    "evidence_object_generation",
    "evidence_sha256",
    "immutable_collision_rejected",
    "negative_route_matrix",
    "object_versioning",
    "public_access_prevention",
    "reader_write_denied",
    "reviewed_at",
    "reviewer",
    "soft_delete_recovery",
    "warm_cache_hit",
    "writer_delete_denied",
}


class RemoteCacheContractError(ValueError):
    pass


def _fail(message: str) -> NoReturn:
    raise RemoteCacheContractError(message)


def _single_line(label: str, value: str) -> str:
    if not value or "\n" in value or "\r" in value:
        _fail(f"{label} must be a non-empty single line")
    return value


def load_contract(path: Path) -> dict[str, object]:
    if path.is_symlink() or not path.is_file():
        _fail("remote-cache activation contract must be a regular non-symlink file")
    try:
        payload = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as error:
        raise RemoteCacheContractError("remote-cache activation contract is unreadable") from error
    expected = {
        "backend",
        "bucket",
        "gateway_target",
        "module_release",
        "protocol",
        "qualification",
        "schema_version",
        "state",
    }
    if not isinstance(payload, dict) or set(payload) != expected:
        _fail("remote-cache activation fields must be exact")
    if payload["schema_version"] != 1:
        _fail("remote-cache activation schema_version must be 1")
    if payload["backend"] != "gcs-generation-zero" or payload["protocol"] != "bazel-http-cache-v1":
        _fail("remote-cache backend and protocol must be immutable GCS Bazel HTTP v1")
    if payload["gateway_target"] != GATEWAY_TARGET:
        _fail("remote-cache gateway target is not canonical")
    state = payload["state"]
    if state not in ALLOWED_STATES:
        _fail("remote-cache activation state is invalid")

    release = payload["module_release"]
    if not isinstance(release, dict) or set(release) != {"status", "version"}:
        _fail("remote-cache module release fields must be exact")
    if release["version"] != "v0.4.0" or release["status"] not in {"unpublished", "published"}:
        _fail("remote-cache module release must name v0.4.0 and a valid status")
    qualification = payload["qualification"]
    if not isinstance(qualification, dict) or set(qualification) != QUALIFICATION_FIELDS:
        _fail("remote-cache qualification fields must be exact")

    if state == "blocked":
        if payload["bucket"] is not None:
            _fail("blocked remote cache must not claim an applied bucket")
        if any(value not in {False, None} for value in qualification.values()):
            _fail("blocked remote cache must not claim qualification evidence")
        return payload

    bucket = payload["bucket"]
    if (
        not isinstance(bucket, str)
        or not valid_bucket(bucket)
        or not bucket.endswith("-common-ci-bazel-cache")
    ):
        _fail("qualified remote cache requires an exact bucket")
    if release["status"] != "published":
        _fail("qualified remote cache requires published module v0.4.0")
    boolean_fields = QUALIFICATION_FIELDS - {
        "evidence_object_generation",
        "evidence_sha256",
        "reviewed_at",
        "reviewer",
    }
    if any(qualification[field] is not True for field in boolean_fields):
        _fail("qualified remote cache requires every behavioral check")
    if (
        not isinstance(qualification["evidence_object_generation"], str)
        or EVIDENCE_OBJECT.fullmatch(qualification["evidence_object_generation"]) is None
    ):
        _fail("qualified remote cache requires a restricted evidence object generation")
    if (
        not isinstance(qualification["evidence_sha256"], str)
        or SHA256.fullmatch(qualification["evidence_sha256"]) is None
    ):
        _fail("qualified remote cache requires an evidence SHA-256")
    reviewer = qualification["reviewer"]
    if not isinstance(reviewer, str) or not re.fullmatch(
        r"[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?", reviewer
    ):
        _fail("qualified remote cache requires a GitHub reviewer login")
    reviewed_at = qualification["reviewed_at"]
    if not isinstance(reviewed_at, str) or not valid_timestamp(reviewed_at):
        _fail("qualified remote cache requires an RFC3339 UTC review timestamp")
    return payload


def valid_bucket(value: str) -> bool:
    return (
        3 <= len(value) <= 63 and re.fullmatch(r"[a-z0-9][a-z0-9._-]*[a-z0-9]", value) is not None
    )


def valid_timestamp(value: str) -> bool:
    if re.fullmatch(r"[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z", value) is None:
        return False
    try:
        datetime.fromisoformat(value.removesuffix("Z") + "+00:00")
    except ValueError:
        return False
    return True


def select_activation(
    *,
    contract: dict[str, object],
    repository_state: str,
    workflow: str,
    event: str,
    ref: str,
    ref_protected: str,
    project_id: str,
    pull_request_base_ref: str = "",
    merge_group_base_ref: str = "",
) -> dict[str, str]:
    if repository_state == "":
        repository_state = "blocked"
    if repository_state not in ALLOWED_STATES:
        _fail("repository remote-cache state is invalid")
    if contract["state"] != repository_state:
        if repository_state == "blocked":
            return {
                "enabled": "false",
                "reason": "server_side_blocked",
                "role": "disabled",
                "upload": "false",
            }
        _fail("qualified repository state does not match reviewed source contract")
    if repository_state == "blocked":
        return {
            "enabled": "false",
            "reason": "source_blocked",
            "role": "disabled",
            "upload": "false",
        }
    if ref_protected not in {"true", "false"}:
        _fail("GitHub protected-ref state must be true or false")
    if PROJECT_ID.fullmatch(project_id) is None:
        _fail("qualified remote cache requires the applied common-CI project ID")
    expected_bucket = f"{project_id}-bazel-cache"
    if contract["bucket"] != expected_bucket:
        _fail("qualified remote-cache bucket does not match the common-CI project")

    role = ""
    if workflow == "presubmit" and event == "pull_request":
        if PULL_REQUEST_REF.fullmatch(ref) is None or pull_request_base_ref != "main":
            _fail("pull-request remote cache requires the main merge ref")
        role = "reader"
    elif workflow == "presubmit" and event == "merge_group":
        if (
            MERGE_GROUP_REF.match(ref) is None
            or merge_group_base_ref != "refs/heads/main"
            or ref_protected != "true"
        ):
            _fail("merge-group remote cache requires the protected main queue")
        role = "writer"
    elif workflow == "presubmit" and event == "push":
        if ref != MAIN_REF or ref_protected != "true":
            _fail("remote-cache writes require protected main")
        role = "writer"
    elif workflow == "nightly" and event == "schedule":
        if ref != MAIN_REF or ref_protected != "true":
            _fail("scheduled remote cache requires protected main")
        role = "writer"
    elif workflow == "nightly" and event == "workflow_dispatch":
        return {
            "enabled": "false",
            "reason": "manual_dispatch_not_authorized",
            "role": "disabled",
            "upload": "false",
        }
    else:
        _fail("unsupported remote-cache workflow route")
    return {
        "bucket": expected_bucket,
        "enabled": "true",
        "reason": "qualified",
        "role": role,
        "upload": str(role == "writer").lower(),
    }


def append_outputs(path: Path, values: dict[str, str]) -> None:
    with path.open("a", encoding="utf-8") as stream:
        for key, value in values.items():
            stream.write(f"{key}={_single_line(key, value)}\n")


def configure_and_start(
    *,
    binary: Path,
    workspace: Path,
    bazelrc: Path,
    runtime_dir: Path,
    github_env: Path,
    bucket: str,
    role: str,
    project_id: str,
    provider: str,
    service_account: str,
    credentials_file: Path,
    timeout_seconds: float,
) -> None:
    if role not in {"reader", "writer"} or PROJECT_ID.fullmatch(project_id) is None:
        _fail("gateway start requires an exact role and common-CI project")
    if bucket != f"{project_id}-bazel-cache":
        _fail("gateway bucket does not match the common-CI project")
    if WIF_PROVIDER.fullmatch(provider) is None:
        _fail("gateway start requires the dedicated Bazel-cache WIF provider")
    expected_service_account = f"bazel-cache-{role}@{project_id}.iam.gserviceaccount.com"
    if service_account != expected_service_account:
        _fail("gateway service account does not match the selected cache role")
    if timeout_seconds <= 0 or timeout_seconds > 120:
        _fail("gateway start timeout must be between zero and 120 seconds")
    resolved_workspace = workspace.resolve(strict=True)
    if bazelrc.resolve(strict=False) != resolved_workspace / "user.bazelrc":
        _fail("remote-cache configuration must use workspace user.bazelrc")
    if "try-import %workspace%/user.bazelrc" not in active_lines(resolved_workspace / ".bazelrc"):
        _fail("workspace .bazelrc must try-import user.bazelrc")
    ignored = active_lines(resolved_workspace / ".gitignore")
    if "user.bazelrc" not in ignored or "!user.bazelrc" in ignored:
        _fail("user.bazelrc must remain gitignored")
    binary_metadata = binary.stat()
    if (
        not stat.S_ISREG(binary_metadata.st_mode)
        or binary.is_symlink()
        or not os.access(binary, os.X_OK)
    ):
        _fail("cache gateway binary must be an executable regular file")
    if binary_metadata.st_mode & 0o022:
        _fail("cache gateway binary must not be group- or world-writable")

    resolved_runtime = runtime_dir.resolve(strict=False)
    try:
        resolved_runtime.relative_to(resolved_workspace)
    except ValueError:
        pass
    else:
        _fail("cache gateway runtime must remain outside the source checkout")
    if runtime_dir.is_symlink():
        _fail("cache gateway runtime must not be a symbolic link")
    runtime_dir.mkdir(mode=0o700, parents=True, exist_ok=False)
    runtime_dir.chmod(0o700)
    private_credentials = stage_credentials(
        source=credentials_file,
        destination=runtime_dir / "google-credentials.json",
        workspace=resolved_workspace,
        provider=provider,
        service_account=service_account,
    )
    staging = runtime_dir / "staging"
    staging.mkdir(mode=0o700)
    ready = runtime_dir / "ready"
    log = runtime_dir / "gateway.log"
    pid_path = runtime_dir / "gateway.pid"
    mode = "write" if role == "writer" else "read"
    process_environment = {
        key: os.environ[key]
        for key in (
            "GODEBUG",
            "GRPC_DEFAULT_SSL_ROOTS_FILE_PATH",
            "HOME",
            "HTTPS_PROXY",
            "HTTP_PROXY",
            "NO_PROXY",
            "PATH",
            "SSL_CERT_DIR",
            "SSL_CERT_FILE",
            "TMPDIR",
        )
        if key in os.environ
    }
    process_environment["GOOGLE_APPLICATION_CREDENTIALS"] = str(private_credentials)
    process: subprocess.Popen[bytes] | None = None
    try:
        with log.open("xb") as log_stream:
            process = subprocess.Popen(
                [
                    str(binary),
                    "--bucket",
                    bucket,
                    "--listen-address",
                    "127.0.0.1:8085",
                    "--maximum-body-bytes",
                    str(MAXIMUM_BODY_BYTES),
                    "--maximum-concurrent-staging",
                    str(MAXIMUM_CONCURRENT_STAGING),
                    "--mode",
                    mode,
                    "--prefix",
                    "bazel-http-cache/v1",
                    "--ready-file",
                    str(ready),
                    "--temporary-directory",
                    str(staging),
                ],
                cwd=resolved_workspace,
                stdin=subprocess.DEVNULL,
                stdout=log_stream,
                stderr=subprocess.STDOUT,
                env=process_environment,
                start_new_session=True,
                close_fds=True,
            )
        pid_path.write_text(f"{process.pid}\n", encoding="utf-8")
        pid_path.chmod(0o600)
        binary_digest_path = runtime_dir / "gateway-binary.sha256"
        binary_digest_path.write_text(f"sha256:{sha256_file(binary)}\n", encoding="utf-8")
        binary_digest_path.chmod(0o600)
        deadline = time.monotonic() + timeout_seconds
        while time.monotonic() < deadline:
            if process.poll() is not None:
                _fail("cache gateway exited before readiness")
            if ready.is_file() and endpoint_ready():
                break
            time.sleep(0.2)
        else:
            _fail("cache gateway readiness timed out")

        upload = "true" if role == "writer" else "false"
        contents = (
            "# Generated by CI; contains no credentials.\n"
            f"build --remote_cache={LOOPBACK_ENDPOINT}\n"
            f"build --remote_upload_local_results={upload}\n"
            "build --remote_verify_downloads\n"
            "build --noremote_cache_async\n"
            "build --remote_timeout=60s\n"
            "build --remote_retries=3\n"
        )
        temporary = bazelrc.with_name(f"{bazelrc.name}.tmp")
        temporary.write_text(contents, encoding="utf-8")
        temporary.chmod(0o600)
        os.replace(temporary, bazelrc)
        with github_env.open("a", encoding="utf-8") as stream:
            stream.write("BAZELISK_GITHUB_TOKEN=\n")
            stream.write("MINDCLADE_BAZEL_REMOTE_CACHE=qualified-v1\n")
            stream.write(f"MINDCLADE_BAZEL_REMOTE_CACHE_ROLE={role}\n")
    except Exception:
        if process is not None and process.poll() is None:
            process.terminate()
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait(timeout=10)
        private_credentials.unlink(missing_ok=True)
        raise


def stage_credentials(
    *,
    source: Path,
    destination: Path,
    workspace: Path,
    provider: str,
    service_account: str,
) -> Path:
    if source.is_symlink() or AUTH_CREDENTIAL_NAME.fullmatch(source.name) is None:
        _fail("Google auth credentials path is not canonical")
    resolved = source.resolve(strict=True)
    if resolved.parent != workspace:
        _fail("Google auth credentials must originate at the checked-out workspace root")
    metadata = resolved.stat()
    if not stat.S_ISREG(metadata.st_mode) or metadata.st_size > MAXIMUM_CREDENTIAL_BYTES:
        _fail("Google auth credentials must be a bounded regular file")
    if metadata.st_mode & 0o077:
        _fail("Google auth credentials must be private to the runner account")
    try:
        payload = json.loads(resolved.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as error:
        raise RemoteCacheContractError("Google auth credentials are unreadable") from error
    expected_fields = {
        "audience",
        "credential_source",
        "service_account_impersonation_url",
        "subject_token_type",
        "token_url",
        "type",
    }
    if not isinstance(payload, dict) or set(payload) != expected_fields:
        _fail("Google auth credentials fields are not exact")
    expected_audience = f"//iam.googleapis.com/{provider}"
    expected_impersonation = (
        "https://iamcredentials.googleapis.com/v1/projects/-/serviceAccounts/"
        f"{service_account}:generateAccessToken"
    )
    if (
        payload["type"] != "external_account"
        or payload["audience"] != expected_audience
        or payload["subject_token_type"] != "urn:ietf:params:oauth:token-type:jwt"
        or payload["token_url"] != "https://sts.googleapis.com/v1/token"
        or payload["service_account_impersonation_url"] != expected_impersonation
    ):
        _fail("Google auth credentials do not match the selected WIF route")
    credential_source = payload["credential_source"]
    if not isinstance(credential_source, dict) or set(credential_source) != {
        "format",
        "headers",
        "url",
    }:
        _fail("Google auth credential source is not exact")
    parsed_url = urllib.parse.urlsplit(str(credential_source["url"]))
    if (
        parsed_url.scheme != "https"
        or parsed_url.username is not None
        or parsed_url.password is not None
        or parsed_url.hostname is None
        or not parsed_url.hostname.endswith(".actions.githubusercontent.com")
    ):
        _fail("Google auth credential source is not GitHub OIDC")
    if credential_source["format"] != {
        "type": "json",
        "subject_token_field_name": "value",
    }:
        _fail("Google auth credential source format is not exact")
    headers = credential_source["headers"]
    if (
        not isinstance(headers, dict)
        or set(headers) != {"Authorization"}
        or not isinstance(headers["Authorization"], str)
        or not headers["Authorization"].startswith("Bearer ")
        or len(headers["Authorization"]) <= len("Bearer ")
    ):
        _fail("Google auth credential source authorization is invalid")
    with destination.open("xb") as stream:
        stream.write(resolved.read_bytes())
    destination.chmod(0o600)
    resolved.unlink()
    return destination


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def endpoint_ready() -> bool:
    try:
        with urllib.request.urlopen(f"{LOOPBACK_ENDPOINT}/readyz", timeout=1) as response:
            return response.status == 200
    except OSError:
        return False


def record_and_stop(
    *, runtime_dir: Path, evidence_dir: Path, summary: Path, role: str
) -> dict[str, object]:
    if role not in {"reader", "writer"}:
        _fail("remote-cache metrics require reader or writer role")
    try:
        metrics_path = evidence_dir / "cache-metrics.json"
        try:
            with urllib.request.urlopen(f"{LOOPBACK_ENDPOINT}/metrics", timeout=5) as response:
                payload = json.load(response)
        except (OSError, urllib.error.URLError, json.JSONDecodeError) as error:
            raise RemoteCacheContractError("cache gateway metrics are unavailable") from error
        expected = {
            "get_hit",
            "get_miss",
            "head_hit",
            "head_miss",
            "immutable_collision",
            "maximum_concurrent_staging",
            "mode",
            "protocol",
            "put_created",
            "put_idempotent",
            "put_rejected",
            "read_bytes",
            "request_error",
            "schema_version",
            "staging_active",
            "staging_peak",
            "staging_wait",
            "staging_wait_canceled",
            "written_bytes",
        }
        if not isinstance(payload, dict) or set(payload) != expected:
            _fail("cache gateway metrics fields are not exact")
        if payload["schema_version"] != 2 or payload["protocol"] != "bazel-http-cache-v1":
            _fail("cache gateway metrics protocol is invalid")
        expected_mode = "write" if role == "writer" else "read"
        if payload["mode"] != expected_mode:
            _fail("cache gateway metrics mode does not match the WIF route")
        for field in expected - {"mode", "protocol"}:
            if field == "schema_version":
                continue
            if (
                not isinstance(payload[field], int)
                or isinstance(payload[field], bool)
                or payload[field] < 0
            ):
                _fail("cache gateway metrics counters must be non-negative integers")
        if payload["maximum_concurrent_staging"] != MAXIMUM_CONCURRENT_STAGING:
            _fail("cache gateway staging limit does not match the qualified launcher")
        if payload["staging_active"] != 0:
            _fail("cache gateway staging must be idle before evidence collection")
        if payload["staging_peak"] > payload["maximum_concurrent_staging"]:
            _fail("cache gateway staging peak exceeds its configured bound")
        payload.update(
            {
                "backend": "gcs-generation-zero",
                "cache_kind": "bazel-http-remote-cache",
                "gateway_binary_sha256": read_binary_digest(runtime_dir),
                "remote_cache": True,
                "role": role,
                "transport": "loopback-gateway",
            }
        )
        evidence_dir.mkdir(mode=0o700, parents=True, exist_ok=True)
        temporary = metrics_path.with_name(f"{metrics_path.name}.tmp")
        temporary.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n", encoding="utf-8")
        temporary.chmod(0o600)
        os.replace(temporary, metrics_path)
        with summary.open("a", encoding="utf-8") as stream:
            stream.write("### Bazel GCS remote action cache\n\n")
            stream.write("| Metric | Value |\n| --- | ---: |\n")
            stream.write(f"| WIF role | `{role}` |\n")
            stream.write(f"| GET hits / misses | {payload['get_hit']} / {payload['get_miss']} |\n")
            stream.write(
                f"| PUT created / idempotent | {payload['put_created']} / {payload['put_idempotent']} |\n"
            )
            stream.write(
                f"| Rejected writes / collisions | {payload['put_rejected']} / {payload['immutable_collision']} |\n"
            )
            stream.write(
                f"| Bytes read / written | {payload['read_bytes']} / {payload['written_bytes']} |\n"
            )
            stream.write(
                "| Staging peak / limit / waits | "
                f"{payload['staging_peak']} / {payload['maximum_concurrent_staging']} / "
                f"{payload['staging_wait']} |\n"
            )
    finally:
        stop_gateway(runtime_dir)
    copy_gateway_log(runtime_dir, evidence_dir)
    return payload


def read_binary_digest(runtime_dir: Path) -> str:
    path = runtime_dir / "gateway-binary.sha256"
    value = path.read_text(encoding="utf-8").strip()
    if SHA256.fullmatch(value) is None:
        _fail("cache gateway binary digest is invalid")
    return value


def copy_gateway_log(runtime_dir: Path, evidence_dir: Path) -> None:
    source = runtime_dir / "gateway.log"
    if source.is_symlink():
        _fail("cache gateway log must not be a symbolic link")
    metadata = source.stat()
    if not stat.S_ISREG(metadata.st_mode) or metadata.st_size > MAXIMUM_GATEWAY_LOG_BYTES:
        _fail("cache gateway log must be a bounded regular file")
    destination = evidence_dir / "cache-gateway.log"
    with source.open("rb") as input_stream, destination.open("xb") as output_stream:
        shutil.copyfileobj(input_stream, output_stream)
    destination.chmod(0o600)


def stop_gateway(runtime_dir: Path) -> None:
    pid_path = runtime_dir / "gateway.pid"
    try:
        pid = int(pid_path.read_text(encoding="utf-8").strip())
    except (OSError, ValueError) as error:
        raise RemoteCacheContractError("cache gateway PID is unavailable") from error
    if pid <= 1:
        _fail("cache gateway PID is invalid")
    try:
        try:
            os.kill(pid, signal.SIGTERM)
        except ProcessLookupError:
            _fail("cache gateway exited before evidence collection")
        deadline = time.monotonic() + 30
        while time.monotonic() < deadline:
            try:
                os.kill(pid, 0)
            except ProcessLookupError:
                return
            time.sleep(0.2)
        os.kill(pid, signal.SIGKILL)
        _fail("cache gateway did not terminate cleanly")
    finally:
        (runtime_dir / "google-credentials.json").unlink(missing_ok=True)


def active_lines(path: Path) -> set[str]:
    return {
        line.strip()
        for line in path.read_text(encoding="utf-8").splitlines()
        if line.strip() and not line.lstrip().startswith("#")
    }


def parser() -> argparse.ArgumentParser:
    root = argparse.ArgumentParser(description="Govern the immutable Bazel GCS cache gateway")
    commands = root.add_subparsers(dest="command", required=True)
    select = commands.add_parser("select")
    select.add_argument("--contract", type=Path, required=True)
    select.add_argument("--repository-state", default="")
    select.add_argument("--workflow", choices=("presubmit", "nightly"), required=True)
    select.add_argument("--event", required=True)
    select.add_argument("--ref", required=True)
    select.add_argument("--ref-protected", required=True)
    select.add_argument("--project-id", default="")
    select.add_argument("--pull-request-base-ref", default="")
    select.add_argument("--merge-group-base-ref", default="")
    select.add_argument("--github-output", type=Path, required=True)
    start = commands.add_parser("start")
    start.add_argument("--binary", type=Path, required=True)
    start.add_argument("--workspace", type=Path, required=True)
    start.add_argument("--bazelrc", type=Path, required=True)
    start.add_argument("--runtime-dir", type=Path, required=True)
    start.add_argument("--github-env", type=Path, required=True)
    start.add_argument("--bucket", required=True)
    start.add_argument("--role", required=True)
    start.add_argument("--project-id", required=True)
    start.add_argument("--provider", required=True)
    start.add_argument("--service-account", required=True)
    start.add_argument("--credentials-file", type=Path, required=True)
    start.add_argument("--timeout-seconds", type=float, default=60)
    record = commands.add_parser("record-stop")
    record.add_argument("--runtime-dir", type=Path, required=True)
    record.add_argument("--evidence-dir", type=Path, required=True)
    record.add_argument("--summary", type=Path, required=True)
    record.add_argument("--role", required=True)
    return root


def main() -> int:
    args = parser().parse_args()
    try:
        if args.command == "select":
            append_outputs(
                args.github_output,
                select_activation(
                    contract=load_contract(args.contract),
                    repository_state=args.repository_state,
                    workflow=args.workflow,
                    event=args.event,
                    ref=args.ref,
                    ref_protected=args.ref_protected,
                    project_id=args.project_id,
                    pull_request_base_ref=args.pull_request_base_ref,
                    merge_group_base_ref=args.merge_group_base_ref,
                ),
            )
        elif args.command == "start":
            configure_and_start(
                binary=args.binary,
                workspace=args.workspace,
                bazelrc=args.bazelrc,
                runtime_dir=args.runtime_dir,
                github_env=args.github_env,
                bucket=args.bucket,
                role=args.role,
                project_id=args.project_id,
                provider=args.provider,
                service_account=args.service_account,
                credentials_file=args.credentials_file,
                timeout_seconds=args.timeout_seconds,
            )
        else:
            record_and_stop(
                runtime_dir=args.runtime_dir,
                evidence_dir=args.evidence_dir,
                summary=args.summary,
                role=args.role,
            )
    except (RemoteCacheContractError, OSError, subprocess.SubprocessError) as error:
        print(f"Bazel remote-cache contract failed: {error}", file=sys.stderr)
        return 2
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
