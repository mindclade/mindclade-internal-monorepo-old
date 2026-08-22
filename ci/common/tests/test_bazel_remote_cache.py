# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

import copy
import importlib.util
import io
import json
import subprocess
import sys
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[3]
PROVIDER = (
    "projects/123456789/locations/global/workloadIdentityPools/github/providers/gh-bazel-cache"
)


def load_module():
    path = ROOT / "ci/common/bazel_remote_cache.py"
    spec = importlib.util.spec_from_file_location("bazel_remote_cache", path)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"unable to load {path}")
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


remote = load_module()


def blocked() -> dict[str, object]:
    return json.loads((ROOT / "ci/bazel_cache/activation.json").read_text(encoding="utf-8"))


def qualified() -> dict[str, object]:
    value = blocked()
    value["state"] = "qualified-v1"
    value["bucket"] = "mc-common-ci-bazel-cache"
    value["module_release"]["status"] = "published"
    for key in value["qualification"]:
        value["qualification"][key] = True
    value["qualification"].update(
        {
            "evidence_object_generation": (
                "gs://mc-production-qualification-evidence/bazel-cache/cache.json#42"
            ),
            "evidence_sha256": "sha256:" + "a" * 64,
            "reviewed_at": "2026-08-22T12:00:00Z",
            "reviewer": "reviewer-one",
        }
    )
    return value


def write_contract(tmp_path: Path, payload: dict[str, object]) -> Path:
    path = tmp_path / "activation.json"
    path.write_text(json.dumps(payload), encoding="utf-8")
    return path


def test_committed_contract_is_blocked_and_valid() -> None:
    value = remote.load_contract(ROOT / "ci/bazel_cache/activation.json")
    assert value["state"] == "blocked"
    assert value["bucket"] is None


def test_blocked_source_disables_missing_or_blocked_repository_state() -> None:
    for state in ("", "blocked"):
        result = remote.select_activation(
            contract=blocked(),
            repository_state=state,
            workflow="presubmit",
            event="pull_request",
            ref="refs/pull/7/merge",
            ref_protected="false",
            project_id="",
            pull_request_base_ref="main",
        )
        assert result["enabled"] == "false"
        assert result["role"] == "disabled"


def test_server_side_block_wins_over_proposed_qualified_source() -> None:
    result = remote.select_activation(
        contract=qualified(),
        repository_state="blocked",
        workflow="presubmit",
        event="push",
        ref="refs/heads/main",
        ref_protected="true",
        project_id="mc-common-ci",
    )
    assert result == {
        "enabled": "false",
        "reason": "server_side_blocked",
        "role": "disabled",
        "upload": "false",
    }


@pytest.mark.parametrize(
    ("workflow", "event", "ref", "protected", "pr_base", "merge_base", "role"),
    [
        ("presubmit", "pull_request", "refs/pull/8/merge", "false", "main", "", "reader"),
        ("presubmit", "push", "refs/heads/main", "true", "", "", "writer"),
        (
            "presubmit",
            "merge_group",
            "refs/heads/gh-readonly-queue/main/pr-8-deadbeef",
            "true",
            "",
            "refs/heads/main",
            "writer",
        ),
        ("nightly", "schedule", "refs/heads/main", "true", "", "", "writer"),
    ],
)
def test_qualified_routes_are_exact(
    workflow: str,
    event: str,
    ref: str,
    protected: str,
    pr_base: str,
    merge_base: str,
    role: str,
) -> None:
    result = remote.select_activation(
        contract=qualified(),
        repository_state="qualified-v1",
        workflow=workflow,
        event=event,
        ref=ref,
        ref_protected=protected,
        project_id="mc-common-ci",
        pull_request_base_ref=pr_base,
        merge_group_base_ref=merge_base,
    )
    assert result["enabled"] == "true"
    assert result["role"] == role
    assert result["upload"] == str(role == "writer").lower()
    assert result["bucket"] == "mc-common-ci-bazel-cache"


def test_manual_nightly_never_receives_remote_cache_credentials() -> None:
    result = remote.select_activation(
        contract=qualified(),
        repository_state="qualified-v1",
        workflow="nightly",
        event="workflow_dispatch",
        ref="refs/heads/main",
        ref_protected="true",
        project_id="mc-common-ci",
    )
    assert result["enabled"] == "false"
    assert result["reason"] == "manual_dispatch_not_authorized"


def test_unprotected_merge_group_cannot_write() -> None:
    with pytest.raises(remote.RemoteCacheContractError, match="protected main queue"):
        remote.select_activation(
            contract=qualified(),
            repository_state="qualified-v1",
            workflow="presubmit",
            event="merge_group",
            ref="refs/heads/gh-readonly-queue/main/pr-8-deadbeef",
            ref_protected="false",
            project_id="mc-common-ci",
            merge_group_base_ref="refs/heads/main",
        )


@pytest.mark.parametrize(
    "mutation",
    [
        lambda value: value.update(state="active"),
        lambda value: value.update(bucket="wrong-bucket"),
        lambda value: value["module_release"].update(status="unpublished"),
        lambda value: value["qualification"].update(cold_rebuild=False),
        lambda value: value["qualification"].update(evidence_sha256="sha256:bad"),
        lambda value: value["qualification"].update(
            evidence_object_generation="gs://bucket/no-generation"
        ),
        lambda value: value["qualification"].update(reviewed_at="yesterday"),
    ],
)
def test_malformed_or_incomplete_qualification_fails(tmp_path: Path, mutation) -> None:
    value = qualified()
    mutation(value)
    with pytest.raises(remote.RemoteCacheContractError):
        remote.load_contract(write_contract(tmp_path, value))


def test_blocked_contract_cannot_claim_evidence(tmp_path: Path) -> None:
    value = copy.deepcopy(blocked())
    value["qualification"]["cold_rebuild"] = True
    with pytest.raises(remote.RemoteCacheContractError, match="must not claim"):
        remote.load_contract(write_contract(tmp_path, value))


def test_start_configuration_is_loopback_and_create_only(tmp_path: Path, monkeypatch) -> None:
    workspace = tmp_path / "workspace"
    workspace.mkdir()
    (workspace / ".bazelrc").write_text("try-import %workspace%/user.bazelrc\n", encoding="utf-8")
    (workspace / ".gitignore").write_text("user.bazelrc\n", encoding="utf-8")
    binary = tmp_path / "gateway"
    binary.write_text("#!/bin/sh\nexit 0\n", encoding="utf-8")
    binary.chmod(0o500)
    github_env = tmp_path / "github-env"
    github_env.touch()
    credentials = workspace / "gha-creds-0123456789abcdef.json"
    credentials.write_text(
        json.dumps(
            {
                "type": "external_account",
                "audience": f"//iam.googleapis.com/{PROVIDER}",
                "subject_token_type": "urn:ietf:params:oauth:token-type:jwt",
                "token_url": "https://sts.googleapis.com/v1/token",
                "credential_source": {
                    "url": "https://vstoken.actions.githubusercontent.com/path?audience=test",
                    "headers": {"Authorization": "Bearer request-token"},
                    "format": {
                        "type": "json",
                        "subject_token_field_name": "value",
                    },
                },
                "service_account_impersonation_url": (
                    "https://iamcredentials.googleapis.com/v1/projects/-/serviceAccounts/"
                    "bazel-cache-reader@mc-common-ci.iam.gserviceaccount.com:generateAccessToken"
                ),
            }
        ),
        encoding="utf-8",
    )
    credentials.chmod(0o600)

    class Process:
        pid = 4242

        def poll(self):
            return None

        def terminate(self):
            return None

    captured: list[str] = []
    process_environment: dict[str, str] = {}

    def popen(command, **kwargs):
        captured.extend(command)
        process_environment.update(kwargs["env"])
        ready_index = command.index("--ready-file") + 1
        Path(command[ready_index]).write_text("ready", encoding="utf-8")
        return Process()

    monkeypatch.setattr(remote.subprocess, "Popen", popen)
    monkeypatch.setattr(remote, "endpoint_ready", lambda: True)
    remote.configure_and_start(
        binary=binary,
        workspace=workspace,
        bazelrc=workspace / "user.bazelrc",
        runtime_dir=tmp_path / "runtime",
        github_env=github_env,
        bucket="mc-common-ci-bazel-cache",
        role="reader",
        project_id="mc-common-ci",
        provider=PROVIDER,
        service_account="bazel-cache-reader@mc-common-ci.iam.gserviceaccount.com",
        credentials_file=credentials,
        timeout_seconds=1,
    )
    bazelrc = (workspace / "user.bazelrc").read_text(encoding="utf-8")
    assert "--remote_cache=http://127.0.0.1:8085" in bazelrc
    assert "--remote_upload_local_results=false" in bazelrc
    assert "--noremote_cache_async" in bazelrc
    assert "--disk_cache" not in bazelrc
    assert captured[captured.index("--mode") + 1] == "read"
    assert captured[captured.index("--prefix") + 1] == "bazel-http-cache/v1"
    assert captured[captured.index("--maximum-body-bytes") + 1] == str(1024**3)
    assert not credentials.exists()
    private_credentials = tmp_path / "runtime/google-credentials.json"
    assert private_credentials.is_file()
    assert process_environment["GOOGLE_APPLICATION_CREDENTIALS"] == str(private_credentials)
    assert "GITHUB_TOKEN" not in process_environment
    assert remote.SHA256.fullmatch(
        (tmp_path / "runtime/gateway-binary.sha256").read_text(encoding="utf-8").strip()
    )


def test_start_failure_stops_gateway_and_removes_private_credentials(
    tmp_path: Path, monkeypatch
) -> None:
    workspace = tmp_path / "workspace"
    workspace.mkdir()
    (workspace / ".bazelrc").write_text("try-import %workspace%/user.bazelrc\n", encoding="utf-8")
    (workspace / ".gitignore").write_text("user.bazelrc\n", encoding="utf-8")
    binary = tmp_path / "gateway"
    binary.write_text("#!/bin/sh\nexit 0\n", encoding="utf-8")
    binary.chmod(0o500)
    github_env = tmp_path / "github-env"
    github_env.touch()
    credentials = workspace / "gha-creds-0123456789abcdef.json"
    credentials.write_text(
        json.dumps(
            {
                "type": "external_account",
                "audience": f"//iam.googleapis.com/{PROVIDER}",
                "subject_token_type": "urn:ietf:params:oauth:token-type:jwt",
                "token_url": "https://sts.googleapis.com/v1/token",
                "credential_source": {
                    "url": "https://vstoken.actions.githubusercontent.com/path?audience=test",
                    "headers": {"Authorization": "Bearer request-token"},
                    "format": {"type": "json", "subject_token_field_name": "value"},
                },
                "service_account_impersonation_url": (
                    "https://iamcredentials.googleapis.com/v1/projects/-/serviceAccounts/"
                    "bazel-cache-reader@mc-common-ci.iam.gserviceaccount.com:generateAccessToken"
                ),
            }
        ),
        encoding="utf-8",
    )
    credentials.chmod(0o600)

    class Process:
        pid = 4242
        terminated = False
        killed = False

        def poll(self):
            return None

        def terminate(self):
            self.terminated = True

        def wait(self, timeout):
            if not self.killed:
                raise subprocess.TimeoutExpired("gateway", timeout)
            return 0

        def kill(self):
            self.killed = True

    process = Process()

    def popen(command, **kwargs):
        return process

    monkeypatch.setattr(remote.subprocess, "Popen", popen)
    monkeypatch.setattr(remote, "endpoint_ready", lambda: False)
    monkeypatch.setattr(remote.time, "sleep", lambda _: None)

    with pytest.raises(remote.RemoteCacheContractError, match="readiness timed out"):
        remote.configure_and_start(
            binary=binary,
            workspace=workspace,
            bazelrc=workspace / "user.bazelrc",
            runtime_dir=tmp_path / "runtime",
            github_env=github_env,
            bucket="mc-common-ci-bazel-cache",
            role="reader",
            project_id="mc-common-ci",
            provider=PROVIDER,
            service_account="bazel-cache-reader@mc-common-ci.iam.gserviceaccount.com",
            credentials_file=credentials,
            timeout_seconds=0.001,
        )

    assert process.terminated
    assert process.killed
    assert not (tmp_path / "runtime/google-credentials.json").exists()


def test_metrics_failure_still_stops_gateway(tmp_path: Path, monkeypatch) -> None:
    runtime = tmp_path / "runtime"
    runtime.mkdir()
    stopped: list[Path] = []

    def unavailable(*args, **kwargs):
        raise OSError("offline")

    monkeypatch.setattr(remote.urllib.request, "urlopen", unavailable)
    monkeypatch.setattr(remote, "stop_gateway", stopped.append)

    with pytest.raises(remote.RemoteCacheContractError, match="metrics are unavailable"):
        remote.record_and_stop(
            runtime_dir=runtime,
            evidence_dir=tmp_path / "evidence",
            summary=tmp_path / "summary",
            role="reader",
        )

    assert stopped == [runtime]


def test_metrics_record_binary_identity_and_redacted_gateway_log(
    tmp_path: Path, monkeypatch
) -> None:
    runtime = tmp_path / "runtime"
    runtime.mkdir()
    (runtime / "gateway-binary.sha256").write_text("sha256:" + "b" * 64 + "\n", encoding="utf-8")
    (runtime / "gateway.log").write_text(
        '{"msg":"cache gateway ready","mode":"read"}\n', encoding="utf-8"
    )
    metrics = {
        "get_hit": 7,
        "get_miss": 2,
        "head_hit": 1,
        "head_miss": 0,
        "immutable_collision": 0,
        "mode": "read",
        "protocol": "bazel-http-cache-v1",
        "put_created": 0,
        "put_idempotent": 0,
        "put_rejected": 1,
        "read_bytes": 1024,
        "request_error": 0,
        "schema_version": 1,
        "written_bytes": 0,
    }

    class Response(io.BytesIO):
        status = 200

        def __enter__(self):
            return self

        def __exit__(self, *_):
            self.close()

    monkeypatch.setattr(
        remote.urllib.request,
        "urlopen",
        lambda *args, **kwargs: Response(json.dumps(metrics).encode("utf-8")),
    )
    stopped: list[Path] = []
    monkeypatch.setattr(remote, "stop_gateway", stopped.append)
    evidence = tmp_path / "evidence"
    summary = tmp_path / "summary"
    payload = remote.record_and_stop(
        runtime_dir=runtime,
        evidence_dir=evidence,
        summary=summary,
        role="reader",
    )
    assert stopped == [runtime]
    assert payload["gateway_binary_sha256"] == "sha256:" + "b" * 64
    assert payload["transport"] == "loopback-gateway"
    assert (evidence / "cache-gateway.log").read_text(encoding="utf-8") == (
        '{"msg":"cache gateway ready","mode":"read"}\n'
    )
    archived = json.loads((evidence / "cache-metrics.json").read_text(encoding="utf-8"))
    assert archived == payload
    assert "Bazel GCS remote action cache" in summary.read_text(encoding="utf-8")
