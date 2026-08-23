# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

import json
import stat
import subprocess
from pathlib import Path

import pytest

from ci.common import bazel_disk_cache

SHA = "a" * 40
BASE_SHA = "b" * 40
FINGERPRINT = "c" * 64


@pytest.mark.parametrize(
    ("event", "ref", "base_arguments"),
    (
        (
            "pull_request",
            "refs/pull/17/merge",
            {"pull_request_base_sha": BASE_SHA, "pull_request_base_ref": "main"},
        ),
        (
            "merge_group",
            "refs/heads/gh-readonly-queue/main/pr-17-abcdef",
            {"merge_group_base_sha": BASE_SHA, "merge_group_base_ref": "refs/heads/main"},
        ),
    ),
)
def test_presubmit_untrusted_events_are_read_only(
    event: str, ref: str, base_arguments: dict[str, str]
) -> None:
    values = bazel_disk_cache.select_trust(
        workflow="presubmit",
        event=event,
        ref=ref,
        ref_protected="false",
        head_sha=SHA,
        fingerprint=FINGERPRINT,
        runner_os="Linux",
        runner_arch="X64",
        **base_arguments,
    )
    assert values == {
        "revision": BASE_SHA,
        "role": "reader",
        "fingerprint": FINGERPRINT,
        "primary-key": f"bazel-disk-v2-Linux-X64-{FINGERPRINT}-{BASE_SHA}",
        "restore-prefix": f"bazel-disk-v2-Linux-X64-{FINGERPRINT}-",
    }


def test_protected_main_push_is_writer() -> None:
    values = bazel_disk_cache.select_trust(
        workflow="presubmit",
        event="push",
        ref="refs/heads/main",
        ref_protected="true",
        head_sha=SHA,
        fingerprint=FINGERPRINT,
        runner_os="Linux",
        runner_arch="ARM64",
    )
    assert values["revision"] == SHA
    assert values["role"] == "writer"
    assert values["primary-key"].endswith(SHA)


@pytest.mark.parametrize(
    "arguments",
    (
        {"workflow": "presubmit", "event": "push", "ref": "refs/heads/topic"},
        {
            "workflow": "presubmit",
            "event": "push",
            "ref": "refs/heads/main",
            "ref_protected": "false",
        },
        {"workflow": "presubmit", "event": "pull_request", "ref": "refs/pull/1/merge"},
        {
            "workflow": "presubmit",
            "event": "merge_group",
            "ref": "refs/heads/gh-readonly-queue/topic/pr-1-abcdef",
            "merge_group_base_sha": BASE_SHA,
            "merge_group_base_ref": "refs/heads/topic",
        },
        {"workflow": "nightly", "event": "schedule", "ref": "refs/heads/topic"},
        {
            "workflow": "nightly",
            "event": "schedule",
            "ref": "refs/heads/main",
            "ref_protected": "false",
        },
        {"workflow": "nightly", "event": "push", "ref": "refs/heads/main"},
    ),
)
def test_untrusted_or_incomplete_writer_contracts_fail(arguments: dict[str, str]) -> None:
    call_arguments = arguments.copy()
    ref_protected = call_arguments.pop("ref_protected", "false")
    with pytest.raises(bazel_disk_cache.CacheContractError):
        bazel_disk_cache.select_trust(
            head_sha=SHA,
            ref_protected=ref_protected,
            fingerprint=FINGERPRINT,
            runner_os="Linux",
            runner_arch="X64",
            **call_arguments,
        )


@pytest.mark.parametrize("event", ("schedule", "workflow_dispatch"))
def test_main_nightly_is_writer(event: str) -> None:
    values = bazel_disk_cache.select_trust(
        workflow="nightly",
        event=event,
        ref="refs/heads/main",
        ref_protected="true",
        head_sha=SHA,
        fingerprint=FINGERPRINT,
        runner_os="Linux",
        runner_arch="X64",
    )
    assert values["role"] == "writer"
    assert values["revision"] == SHA


@pytest.mark.parametrize("component", ("", "Linux arm", "../../Linux", "a" * 33))
def test_cache_keys_reject_unsafe_platform_components(component: str) -> None:
    with pytest.raises(bazel_disk_cache.CacheContractError):
        bazel_disk_cache.cache_keys(
            runner_os=component,
            runner_arch="X64",
            fingerprint=FINGERPRINT,
            revision=SHA,
        )


@pytest.mark.parametrize("role", ("reader", "writer"))
def test_configure_writes_bounded_secret_free_user_bazelrc(tmp_path: Path, role: str) -> None:
    workspace = tmp_path / "workspace"
    workspace.mkdir()
    (workspace / ".bazelrc").write_text("try-import %workspace%/user.bazelrc\n", encoding="utf-8")
    (workspace / ".gitignore").write_text("user.bazelrc\n", encoding="utf-8")
    github_env = tmp_path / "github-env"
    github_env.touch()
    cache_dir = tmp_path / "cache"

    bazel_disk_cache.configure(
        cache_dir=cache_dir,
        workspace=workspace,
        bazelrc=workspace / "user.bazelrc",
        role=role,
        github_env=github_env,
        restore_outcome="success",
    )

    contents = (workspace / "user.bazelrc").read_text(encoding="utf-8")
    assert f"build --disk_cache={cache_dir}" in contents
    assert (
        f"build --remote_upload_local_results={'true' if role == 'writer' else 'false'}" in contents
    )
    assert "build --noremote_cache_async" in contents
    assert "build --remote_verify_downloads" in contents
    assert "build --remote_cache_compression" in contents
    assert "build --experimental_disk_cache_gc_max_size=1G" in contents
    assert "build --experimental_disk_cache_gc_idle_delay=1s" in contents
    assert "token" not in contents.lower()
    assert stat.S_IMODE(cache_dir.stat().st_mode) == 0o700
    assert github_env.read_text(encoding="utf-8") == "BAZELISK_GITHUB_TOKEN=\n"


def test_configure_rejects_cache_inside_checkout(tmp_path: Path) -> None:
    (tmp_path / ".bazelrc").write_text("try-import %workspace%/user.bazelrc\n", encoding="utf-8")
    (tmp_path / ".gitignore").write_text("user.bazelrc\n", encoding="utf-8")
    with pytest.raises(bazel_disk_cache.CacheContractError, match="outside"):
        bazel_disk_cache.configure(
            cache_dir=tmp_path / "cache",
            workspace=tmp_path,
            bazelrc=tmp_path / "user.bazelrc",
            role="reader",
            github_env=tmp_path / "github-env",
            restore_outcome="success",
        )


def test_configure_requires_active_bazelrc_import(tmp_path: Path) -> None:
    (tmp_path / ".bazelrc").write_text("# try-import %workspace%/user.bazelrc\n", encoding="utf-8")
    (tmp_path / ".gitignore").write_text("user.bazelrc\n", encoding="utf-8")
    with pytest.raises(bazel_disk_cache.CacheContractError, match="try-import"):
        bazel_disk_cache.configure(
            cache_dir=tmp_path.parent / "cache",
            workspace=tmp_path,
            bazelrc=tmp_path / "user.bazelrc",
            role="reader",
            github_env=tmp_path / "github-env",
            restore_outcome="success",
        )


def test_measure_reports_size_and_enforces_ceiling(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    (tmp_path / "entry").write_bytes(b"cache")
    assert bazel_disk_cache.measure(cache_dir=tmp_path) == {
        "size-bytes": "5",
        "within-limit": "true",
    }
    monkeypatch.setattr(bazel_disk_cache, "MAX_SIZE_BYTES", 4)
    assert bazel_disk_cache.measure(cache_dir=tmp_path)["within-limit"] == "false"


def test_measure_rejects_cache_symlinks(tmp_path: Path) -> None:
    target = tmp_path / "target"
    target.write_bytes(b"cache")
    cache = tmp_path / "cache"
    cache.mkdir()
    (cache / "entry").symlink_to(target)
    with pytest.raises(bazel_disk_cache.CacheContractError, match="symbolic link"):
        bazel_disk_cache.measure(cache_dir=cache)


@pytest.mark.parametrize("wait_seconds", (-1, 31, float("nan"), float("inf")))
def test_measure_rejects_invalid_gc_wait(tmp_path: Path, wait_seconds: float) -> None:
    with pytest.raises(bazel_disk_cache.CacheContractError, match="GC wait"):
        bazel_disk_cache.quiesce_bazel(
            cache_dir=tmp_path,
            workspace=tmp_path,
            bazel_wrapper=tmp_path / "tools/dev/bazelw",
            wait_seconds=wait_seconds,
        )


def _configured_cache(tmp_path: Path) -> tuple[Path, Path]:
    workspace = tmp_path / "workspace"
    wrapper = workspace / "tools/dev/bazelw"
    wrapper.parent.mkdir(parents=True)
    wrapper.write_text("#!/usr/bin/env bash\nexit 0\n", encoding="utf-8")
    wrapper.chmod(0o700)
    (workspace / ".bazelrc").write_text("try-import %workspace%/user.bazelrc\n", encoding="utf-8")
    (workspace / ".gitignore").write_text("user.bazelrc\n", encoding="utf-8")
    github_env = tmp_path / "github-env"
    github_env.touch()
    cache = tmp_path / "cache"
    bazel_disk_cache.configure(
        cache_dir=cache,
        workspace=workspace,
        bazelrc=workspace / "user.bazelrc",
        role="writer",
        github_env=github_env,
        restore_outcome="success",
    )
    return workspace, cache


def test_quiesce_waits_then_shuts_down_without_bazelisk_token(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    workspace, cache = _configured_cache(tmp_path)
    events: list[object] = []
    monkeypatch.setenv("BAZELISK_GITHUB_TOKEN", "sensitive")
    monkeypatch.setattr(
        "ci.common.bazel_disk_cache.time.sleep", lambda seconds: events.append(seconds)
    )

    def fake_run(command: list[str], **kwargs: object) -> subprocess.CompletedProcess[bytes]:
        events.append(command)
        assert kwargs["cwd"] == workspace
        environment = kwargs["env"]
        assert isinstance(environment, dict)
        assert "BAZELISK_GITHUB_TOKEN" not in environment
        assert kwargs["timeout"] == bazel_disk_cache.SHUTDOWN_TIMEOUT_SECONDS
        return subprocess.CompletedProcess(command, 0)

    monkeypatch.setattr("ci.common.bazel_disk_cache.subprocess.run", fake_run)
    bazel_disk_cache.quiesce_bazel(
        cache_dir=cache,
        workspace=workspace,
        bazel_wrapper=workspace / "tools/dev/bazelw",
        wait_seconds=3,
    )
    assert events == [3, [str(workspace / "tools/dev/bazelw"), "shutdown"]]


def test_quiesce_fails_closed_on_shutdown_error(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    workspace, cache = _configured_cache(tmp_path)
    monkeypatch.setattr("ci.common.bazel_disk_cache.time.sleep", lambda _seconds: None)
    monkeypatch.setattr(
        "ci.common.bazel_disk_cache.subprocess.run",
        lambda command, **_kwargs: subprocess.CompletedProcess(command, 7),
    )
    with pytest.raises(bazel_disk_cache.CacheContractError, match="shutdown failed"):
        bazel_disk_cache.quiesce_bazel(
            cache_dir=cache,
            workspace=workspace,
            bazel_wrapper=workspace / "tools/dev/bazelw",
            wait_seconds=0,
        )


def test_quiesce_fails_closed_on_shutdown_timeout(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    workspace, cache = _configured_cache(tmp_path)
    monkeypatch.setattr("ci.common.bazel_disk_cache.time.sleep", lambda _seconds: None)

    def timeout(command: list[str], **_kwargs: object) -> subprocess.CompletedProcess[bytes]:
        raise subprocess.TimeoutExpired(command, 120)

    monkeypatch.setattr("ci.common.bazel_disk_cache.subprocess.run", timeout)
    with pytest.raises(bazel_disk_cache.CacheContractError, match="timed out"):
        bazel_disk_cache.quiesce_bazel(
            cache_dir=cache,
            workspace=workspace,
            bazel_wrapper=workspace / "tools/dev/bazelw",
            wait_seconds=0,
        )


def test_configure_discards_partial_cache_after_restore_failure(tmp_path: Path) -> None:
    workspace, cache = _configured_cache(tmp_path)
    partial = cache / "partial-entry"
    partial.write_bytes(b"incomplete")
    github_env = tmp_path / "second-github-env"
    github_env.touch()
    bazel_disk_cache.configure(
        cache_dir=cache,
        workspace=workspace,
        bazelrc=workspace / "user.bazelrc",
        role="writer",
        github_env=github_env,
        restore_outcome="failure",
    )
    assert cache.is_dir()
    assert not partial.exists()


def test_stable_measure_occurs_only_after_quiescence(monkeypatch: pytest.MonkeyPatch) -> None:
    order: list[str] = []

    def fake_measure(**_kwargs: object) -> dict[str, str]:
        order.append("measure")
        return {"size-bytes": "0", "within-limit": "true"}

    monkeypatch.setattr(
        bazel_disk_cache,
        "quiesce_bazel",
        lambda **_kwargs: order.append("quiesce"),
    )
    monkeypatch.setattr(bazel_disk_cache, "measure", fake_measure)
    assert bazel_disk_cache.quiesce_and_measure(
        cache_dir=Path("/cache"),
        workspace=Path("/workspace"),
        bazel_wrapper=Path("/workspace/tools/dev/bazelw"),
        wait_seconds=3,
    ) == {"size-bytes": "0", "within-limit": "true"}
    assert order == ["quiesce", "measure"]


def test_failed_quiescence_never_measures(monkeypatch: pytest.MonkeyPatch) -> None:
    def fail_quiescence(**_kwargs: object) -> None:
        raise bazel_disk_cache.CacheContractError("shutdown failed")

    def unexpected_measure(**_kwargs: object) -> dict[str, str]:
        pytest.fail("cache measurement must not run while Bazel can still mutate the cache")

    monkeypatch.setattr(bazel_disk_cache, "quiesce_bazel", fail_quiescence)
    monkeypatch.setattr(bazel_disk_cache, "measure", unexpected_measure)
    with pytest.raises(bazel_disk_cache.CacheContractError, match="shutdown failed"):
        bazel_disk_cache.quiesce_and_measure(
            cache_dir=Path("/cache"),
            workspace=Path("/workspace"),
            bazel_wrapper=Path("/workspace/tools/dev/bazelw"),
            wait_seconds=3,
        )


def test_record_metrics_writes_redacted_evidence_and_summary(tmp_path: Path) -> None:
    evidence = tmp_path / "evidence"
    summary = tmp_path / "summary.md"
    summary.touch()
    payload = bazel_disk_cache.record_metrics(
        evidence_dir=evidence,
        summary=summary,
        role="reader",
        trusted_revision=BASE_SHA,
        primary_key=f"bazel-disk-v2-Linux-ARM64-{FINGERPRINT}-{BASE_SHA}",
        matched_key=f"bazel-disk-v2-Linux-ARM64-{FINGERPRINT}-{SHA}",
        exact_hit="false",
        save_outcome="skipped",
        size_bytes=1024,
        within_limit="true",
        restore_prefix=f"bazel-disk-v2-Linux-ARM64-{FINGERPRINT}-",
        restore_outcome="success",
        measure_outcome="success",
    )
    assert payload["transport"] == "github-actions-cache"
    assert payload["remote_cache"] is False
    assert payload["restore_found"] is True
    assert payload["restore_exact_hit"] is False
    assert payload["restore_state"] == "prefix"
    assert payload["save_verified"] is False
    assert json.loads((evidence / "cache-metrics.json").read_text(encoding="utf-8")) == payload
    summary_text = summary.read_text(encoding="utf-8")
    assert "not remote REAPI/GCS" in summary_text
    assert "| Restore | `prefix` |" in summary_text


def test_record_metrics_rejects_cross_namespace_match(tmp_path: Path) -> None:
    with pytest.raises(bazel_disk_cache.CacheContractError, match="namespace"):
        bazel_disk_cache.record_metrics(
            evidence_dir=tmp_path / "evidence",
            summary=tmp_path / "summary.md",
            role="writer",
            trusted_revision=SHA,
            primary_key=f"bazel-disk-v2-Linux-X64-{FINGERPRINT}-{SHA}",
            matched_key=f"bazel-disk-v2-Linux-X64-other-{BASE_SHA}",
            exact_hit="false",
            save_outcome="skipped",
            size_bytes=0,
            within_limit="true",
            restore_prefix=f"bazel-disk-v2-Linux-X64-{FINGERPRINT}-",
            restore_outcome="success",
            measure_outcome="success",
        )


def test_record_metrics_distinguishes_restore_failure_from_not_restored(tmp_path: Path) -> None:
    payload = bazel_disk_cache.record_metrics(
        evidence_dir=tmp_path / "evidence",
        summary=tmp_path / "summary.md",
        role="writer",
        trusted_revision=SHA,
        primary_key=f"bazel-disk-v2-Linux-X64-{FINGERPRINT}-{SHA}",
        matched_key="",
        exact_hit="",
        save_outcome="skipped",
        size_bytes=0,
        within_limit="true",
        restore_prefix=f"bazel-disk-v2-Linux-X64-{FINGERPRINT}-",
        restore_outcome="failure",
        measure_outcome="success",
    )
    assert payload["restore_state"] == "error"
    assert payload["restore_step_outcome"] == "failure"


def test_record_metrics_marks_successful_save_step_unverified(tmp_path: Path) -> None:
    payload = bazel_disk_cache.record_metrics(
        evidence_dir=tmp_path / "evidence",
        summary=tmp_path / "summary.md",
        role="writer",
        trusted_revision=SHA,
        primary_key=f"bazel-disk-v2-Linux-X64-{FINGERPRINT}-{SHA}",
        matched_key="",
        exact_hit="false",
        save_outcome="success",
        size_bytes=0,
        within_limit="true",
        restore_prefix=f"bazel-disk-v2-Linux-X64-{FINGERPRINT}-",
        restore_outcome="success",
        measure_outcome="success",
    )
    assert payload["save_state"] == "attempted-unverified"
    assert payload["save_step_outcome"] == "success"
    assert payload["save_verified"] is False
    assert payload["restore_state"] == "not-restored"
    assert payload["restore_verified"] is False


def test_record_metrics_marks_failed_measurement_unavailable(tmp_path: Path) -> None:
    summary = tmp_path / "summary.md"
    payload = bazel_disk_cache.record_metrics(
        evidence_dir=tmp_path / "evidence",
        summary=summary,
        role="writer",
        trusted_revision=SHA,
        primary_key=f"bazel-disk-v2-Linux-X64-{FINGERPRINT}-{SHA}",
        matched_key="",
        exact_hit="false",
        save_outcome="skipped",
        size_bytes=0,
        within_limit="",
        restore_prefix=f"bazel-disk-v2-Linux-X64-{FINGERPRINT}-",
        restore_outcome="success",
        measure_outcome="failure",
    )
    assert payload["measurement_state"] == "error"
    assert payload["size_verified"] is False
    assert payload["size_bytes"] is None
    assert payload["within_limit"] is None
    assert "| Size | `unavailable` |" in summary.read_text(encoding="utf-8")
