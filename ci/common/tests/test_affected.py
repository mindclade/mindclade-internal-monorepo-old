# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

import json
import os
import shlex
import shutil
import subprocess
from pathlib import Path

import pytest

from ci.common import affected


@pytest.fixture(autouse=True)
def _trusted_git_fixture(
    tmp_path_factory: pytest.TempPathFactory, monkeypatch: pytest.MonkeyPatch
) -> None:
    if os.environ.get("MINDCLADE_GIT"):
        return
    source = shutil.which("git")
    assert source is not None
    store = tmp_path_factory.mktemp("nix") / "store"
    launcher = store / "fixture-git/bin/git"
    launcher.parent.mkdir(parents=True)
    launcher.write_text(f'#!/bin/sh\nexec {shlex.quote(source)} "$@"\n', encoding="utf-8")
    launcher.chmod(0o555)
    launcher.parent.chmod(0o555)
    launcher.parent.parent.chmod(0o555)
    monkeypatch.setattr(affected, "NIX_STORE_ROOT", store)
    monkeypatch.setenv("MINDCLADE_GIT", str(launcher))


def _workspace(tmp_path: Path) -> Path:
    tmp_path.mkdir(parents=True, exist_ok=True)
    (tmp_path / "BUILD.bazel").write_text("exports_files([])\n", encoding="utf-8")
    package = tmp_path / "pkg"
    package.mkdir()
    (package / "BUILD.bazel").write_text(
        'py_library(name = "library", srcs = ["library.py"])\n', encoding="utf-8"
    )
    (package / "library.py").write_text("VALUE = 1\n", encoding="utf-8")
    return tmp_path


def _select(
    root: Path,
    changes: list[affected.Change | str],
    query,
) -> affected.Selection:
    return affected.select(
        changes,
        root=root,
        head_sha="1" * 40,
        base_sha="0" * 40,
        event="pull_request",
        query=query,
    )


@pytest.mark.parametrize(
    "path,reason",
    [
        ("Cargo.toml", "global_path:Cargo.toml"),
        ("components.toml", "global_path:components.toml"),
        ("architecture/component_ownership.toml", "global_prefix:architecture/"),
        ("tools/build/rules.bzl", "starlark:tools/build/rules.bzl"),
    ],
)
def test_global_changes_force_full_graph(tmp_path: Path, path: str, reason: str) -> None:
    selection = _select(tmp_path, [path], lambda _expression: pytest.fail("query ran"))
    assert selection.mode == "full"
    assert selection.reason == reason
    assert selection.analysis_targets == ("//...",)
    assert selection.test_targets == ("//...",)


def test_global_input_contract_covers_dependency_and_workspace_inputs() -> None:
    contract = affected.load_global_input_contract()
    assert {
        ".bazelignore",
        "MODULE.bazel.lock",
        "nix.conf",
        "requirements.darwin.lock.txt",
        "requirements.lock.txt",
        "rust-toolchain.toml",
        "tools/analysis/check_affected_presubmit.py",
        "tools/analysis/run_architecture_checks.py",
        "tools/analysis/workflow_yaml.py",
        "tools/dev/bazelw",
        "tools/dev/nixw",
    } <= contract.exact_paths
    assert "tools/build/" in contract.prefixes


def test_global_input_contract_rejects_unordered_values(tmp_path: Path) -> None:
    path = tmp_path / "affected_global_inputs.json"
    path.write_text(
        json.dumps(
            {
                "activation": {},
                "schema_version": 1,
                "exact_paths": ["z", "a"],
                "prefixes": ["ci/"],
                "review_boundaries": {"": ["ci"]},
            }
        ),
        encoding="utf-8",
    )
    with pytest.raises(affected.SelectionError, match=r"\[AFFECTED-GLOBAL-003\]"):
        affected.load_global_input_contract(path)


def test_missing_global_input_contract_fails_closed(tmp_path: Path) -> None:
    with pytest.raises(affected.SelectionError, match=r"\[AFFECTED-GLOBAL-001\]"):
        affected.load_global_input_contract(tmp_path / "missing.json")


def test_global_input_contract_rejects_duplicate_json_keys(tmp_path: Path) -> None:
    path = tmp_path / "affected_global_inputs.json"
    path.write_text('{"schema_version": 1, "schema_version": 1}', encoding="utf-8")
    with pytest.raises(affected.SelectionError, match=r"\[AFFECTED-GLOBAL-011\]"):
        affected.load_global_input_contract(path)


def test_global_input_contract_redacts_invalid_utf8(tmp_path: Path) -> None:
    path = tmp_path / "affected_global_inputs.json"
    path.write_bytes(b"\xffsecret-contract-content")
    with pytest.raises(affected.SelectionError) as captured:
        affected.load_global_input_contract(path)
    assert captured.value.code == "AFFECTED-GLOBAL-001"
    assert "secret-contract-content" not in str(captured.value)


def test_global_input_contract_rejects_removed_immutable_anchor(tmp_path: Path) -> None:
    source = affected.GLOBAL_INPUT_CONTRACT_PATH
    payload = json.loads(source.read_text(encoding="utf-8"))
    payload["exact_paths"].remove("tools/dev/bazelw")
    path = tmp_path / "affected_global_inputs.json"
    path.write_text(json.dumps(payload), encoding="utf-8")
    with pytest.raises(affected.SelectionError, match=r"\[AFFECTED-GLOBAL-012\]"):
        affected.load_global_input_contract(path)


@pytest.mark.parametrize("boundary", ["", "tools"])
def test_global_input_contract_rejects_removed_review_boundary(
    tmp_path: Path, boundary: str
) -> None:
    source = affected.GLOBAL_INPUT_CONTRACT_PATH
    payload = json.loads(source.read_text(encoding="utf-8"))
    del payload["review_boundaries"][boundary]
    path = tmp_path / "affected_global_inputs.json"
    path.write_text(json.dumps(payload), encoding="utf-8")
    with pytest.raises(affected.SelectionError, match=r"\[AFFECTED-GLOBAL-013\]"):
        affected.load_global_input_contract(path)


@pytest.mark.parametrize(
    "boundary,entry",
    [("", "CLAUDE.md"), ("", "services"), ("tools", "analysis")],
)
def test_global_input_contract_rejects_removed_review_boundary_anchor(
    tmp_path: Path, boundary: str, entry: str
) -> None:
    source = affected.GLOBAL_INPUT_CONTRACT_PATH
    payload = json.loads(source.read_text(encoding="utf-8"))
    payload["review_boundaries"][boundary].remove(entry)
    path = tmp_path / "affected_global_inputs.json"
    path.write_text(json.dumps(payload), encoding="utf-8")
    with pytest.raises(affected.SelectionError, match=r"\[AFFECTED-GLOBAL-013\]"):
        affected.load_global_input_contract(path)


@pytest.mark.parametrize("status", ["C", "D", "R", "T", "U", "X", "B"])
def test_structural_changes_force_full_graph(tmp_path: Path, status: str) -> None:
    selection = _select(
        tmp_path,
        [affected.Change(status=status, path="pkg/library.py")],
        lambda _expression: pytest.fail("query ran"),
    )
    assert selection.mode == "full"
    assert selection.reason == f"structural_{status.lower()}"


@pytest.mark.parametrize(
    "status,reason,build_filename",
    [
        ("A", "package_boundary_added", "BUILD"),
        ("A", "package_boundary_added", "BUILD.bazel"),
        ("D", "package_boundary_deleted", "BUILD"),
        ("D", "package_boundary_deleted", "BUILD.bazel"),
    ],
)
def test_nested_package_boundary_changes_force_full_graph(
    tmp_path: Path, status: str, reason: str, build_filename: str
) -> None:
    root = _workspace(tmp_path)
    nested = root / "pkg/nested"
    nested.mkdir()
    build_file = nested / build_filename
    build_file.write_text("exports_files([])\n", encoding="utf-8")
    if status == "D":
        build_file.unlink()
    selection = _select(
        root,
        [affected.Change(status=status, path=f"pkg/nested/{build_filename}")],
        lambda _expression: pytest.fail("query ran"),
    )
    assert selection.mode == "full"
    assert selection.reason == reason


@pytest.mark.parametrize(
    "path",
    [
        "tools/analysis/check_affected_presubmit.py",
        "tools/analysis/run_architecture_checks.py",
        "tools/analysis/workflow_yaml.py",
        "tools/dev/bazelw",
        "tools/dev/nixw",
    ],
)
def test_direct_global_control_inputs_force_full_graph(tmp_path: Path, path: str) -> None:
    selection = _select(tmp_path, [path], lambda _expression: pytest.fail("query ran"))
    assert selection.mode == "full"
    assert selection.reason == f"global_path:{path}"


def test_explicit_full_mode_does_not_query(tmp_path: Path) -> None:
    selection = affected.select(
        [],
        mode="full",
        root=tmp_path,
        head_sha="1" * 40,
        event="merge_group",
        query=lambda _expression: pytest.fail("query ran"),
    )
    assert selection.reason == "explicit_full"
    assert selection.test_targets == ("//...",)


def test_missing_or_unowned_paths_fall_back_full(tmp_path: Path) -> None:
    root = _workspace(tmp_path)
    missing = _select(root, ["pkg/missing.py"], lambda _expression: pytest.fail("query ran"))
    assert missing.reason == "missing_changed_path:pkg/missing.py"

    unowned = root / "unowned.txt"
    unowned.write_text("documentation\n", encoding="utf-8")
    # The root BUILD owns root files, so use a directory outside that workspace shape.
    (root / "BUILD.bazel").unlink()
    selection = _select(root, ["unowned.txt"], lambda _expression: pytest.fail("query ran"))
    assert selection.reason == "unowned_path:unowned.txt"


def test_affected_selection_uses_bazel_rdeps_and_tests(tmp_path: Path) -> None:
    root = _workspace(tmp_path)
    expressions: list[str] = []

    def query(expression: str) -> tuple[str, ...]:
        expressions.append(expression)
        if "tests($affected)" in expression:
            return ("//consumer:library_test", "//pkg:library_test")
        return ("//pkg:library", "//consumer:binary", "//pkg:library")

    selection = _select(root, ["pkg/library.py"], query)

    assert selection.mode == "affected"
    assert selection.reason == "bazel_reverse_dependencies"
    assert selection.seeds == ("//pkg:*",)
    assert selection.analysis_targets == ("//consumer:binary", "//pkg:library")
    assert selection.test_targets == (
        "//:gazelle_check",
        "//consumer:library_test",
        "//pkg:library_test",
    )
    assert len(expressions) == 2
    assert all('rdeps(//..., set("//pkg:*"))' in expression for expression in expressions)
    assert all('attr("tags", "manual"' in expression for expression in expressions)


def test_build_file_change_seeds_owning_package(tmp_path: Path) -> None:
    root = _workspace(tmp_path)
    selection = _select(root, ["pkg/BUILD.bazel"], lambda _expression: ())
    assert selection.mode == "affected"
    assert selection.seeds == ("//pkg:*",)


def test_no_changed_paths_is_authoritative_empty_selection(tmp_path: Path) -> None:
    selection = _select(tmp_path, [], lambda _expression: pytest.fail("query ran"))
    assert selection.reason == "no_changed_paths"
    assert selection.analysis_targets == ()
    assert selection.test_targets == ()


def test_query_failure_is_not_converted_to_empty_selection(tmp_path: Path) -> None:
    root = _workspace(tmp_path)

    def fail(_expression: str):
        raise affected.SelectionError("AFFECTED-SELECT-007", "Bazel graph query failed")

    with pytest.raises(affected.SelectionError, match=r"\[AFFECTED-SELECT-007\]"):
        _select(root, ["pkg/library.py"], fail)


def test_bazel_query_failure_is_redacted_and_removes_launcher_token(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
    capsys: pytest.CaptureFixture[str],
) -> None:
    observed_environment: dict[str, str] = {}

    def fail(_command, **kwargs):
        observed_environment.update(kwargs["env"])
        return subprocess.CompletedProcess(
            _command,
            1,
            stdout="secret-query-output",
            stderr="secret-query-error",
        )

    monkeypatch.setenv("BAZELISK_GITHUB_TOKEN", "secret-launcher-token")
    monkeypatch.setattr(affected.subprocess, "run", fail)
    with pytest.raises(affected.SelectionError) as captured:
        affected.bazel_query("//...", root=tmp_path, bazel=tmp_path / "bazel")
    assert captured.value.code == "AFFECTED-SELECT-007"
    assert "secret" not in str(captured.value)
    assert "BAZELISK_GITHUB_TOKEN" not in observed_environment
    output = capsys.readouterr()
    assert output.out == ""
    assert output.err == ""


def test_rust_qualification_is_affected() -> None:
    assert affected.rust_qualification_required(["libs/rust/runtime_core/src/lib.rs"])
    assert affected.rust_qualification_required(["Cargo.toml"])
    assert not affected.rust_qualification_required(["control/artifacts/gc.go"])


def test_git_changed_is_rename_aware_and_validates_base(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    base = "0" * 40
    head = "1" * 40

    def fake_run_git(
        args: list[str], *, root: Path = affected.ROOT
    ) -> subprocess.CompletedProcess[bytes]:
        assert root == tmp_path
        command = tuple(args)
        if command == ("rev-parse", "--verify", "--end-of-options", "base^{commit}"):
            return subprocess.CompletedProcess(args, 0, f"{base}\n".encode(), b"")
        if command == ("rev-parse", "--verify", "--end-of-options", "HEAD^{commit}"):
            return subprocess.CompletedProcess(args, 0, f"{head}\n".encode(), b"")
        if command == ("merge-base", "--is-ancestor", base, head):
            return subprocess.CompletedProcess(args, 0, b"", b"")
        if command == (
            "diff",
            "--no-ext-diff",
            "--no-textconv",
            "--name-status",
            "-z",
            "--find-renames",
            f"{base}...{head}",
        ):
            return subprocess.CompletedProcess(args, 0, b"R100\0original.txt\0renamed.txt\0", b"")
        if command == (
            "rev-parse",
            "--verify",
            "--end-of-options",
            "does-not-exist^{commit}",
        ):
            return subprocess.CompletedProcess(args, 128, b"", b"unknown revision")
        raise AssertionError(f"unexpected git command: {command}")

    monkeypatch.setattr(affected, "run_git", fake_run_git)
    changes = affected.git_changed("base", root=tmp_path)
    assert changes == (affected.Change(status="R", path="renamed.txt", old_path="original.txt"),)
    with pytest.raises(affected.SelectionError, match=r"\[AFFECTED-SELECT-003\]"):
        affected.git_changed("does-not-exist", root=tmp_path)


def test_selection_evidence_is_versioned_and_outside_checkout(tmp_path: Path) -> None:
    root = _workspace(tmp_path / "repo")
    evidence = tmp_path / "evidence"
    selection = affected.Selection(
        mode="affected",
        reason="no_changed_paths",
        changes=(),
        seeds=(),
        analysis_targets=(),
        test_targets=(),
        base_sha="0" * 40,
        head_sha="1" * 40,
        event="local",
    )
    assert affected.execute_selection(selection, evidence, root=root, job_started_epoch=0) == 0
    payload = json.loads((evidence / "selection.json").read_text(encoding="utf-8"))
    assert payload["schema_version"] == 1
    assert payload["execution"][0]["reason"] == "no_targets"
    assert payload["latency_slo_met"] is False

    with pytest.raises(affected.SelectionError, match=r"\[AFFECTED-SELECT-008\]"):
        affected.execute_selection(selection, root / "evidence", root=root)


def test_evidence_phases_do_not_inherit_bazelisk_token(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    root = _workspace(tmp_path / "repo")
    evidence = tmp_path / "evidence"
    evidence.mkdir()
    commands: list[list[str]] = []
    environments: list[dict[str, str]] = []

    def fake_run(command, **kwargs):
        if command[0] == str(root / "tools/dev/bazelw"):
            commands.append(command)
            environments.append(kwargs["env"])
        return subprocess.CompletedProcess(command, 0)

    monkeypatch.setenv("BAZELISK_GITHUB_TOKEN", "must-not-reach-bazel")
    monkeypatch.setattr(affected.subprocess, "run", fake_run)

    for phase in ("analysis", "test"):
        result = affected._run_phase(
            phase,
            ("//pkg:library",),
            evidence_dir=evidence,
            root=root,
        )
        assert result["status"] == "passed"

    assert len(environments) == 2
    assert all("BAZELISK_GITHUB_TOKEN" not in environment for environment in environments)
    assert [command[2] for command in commands] == ["build", "test"]
    assert all(command[1] == "--nohome_rc" for command in commands)
    for command in commands:
        config_index = command.index("--config=ci")
        assert command[config_index + 1] == "--skip_incompatible_explicit_targets"
        assert command[config_index + 2].startswith("--target_pattern_file=")
    assert all(
        not any(
            argument.startswith(
                ("--disk_cache=", "--remote_cache=", "--remote_upload_local_results=")
            )
            for argument in command
        )
        for command in commands
    )
    assert all(
        any(argument.startswith("--build_event_json_file=") for argument in command)
        for command in commands
    )


def test_unsafe_changed_path_is_rejected(tmp_path: Path) -> None:
    with pytest.raises(affected.SelectionError, match=r"\[AFFECTED-SELECT-002\]"):
        _select(tmp_path, ["../outside"], lambda _expression: ())


@pytest.mark.parametrize(
    "event,ref,base,expected",
    [
        ("pull_request", "refs/pull/1/merge", "0" * 40, "full"),
        ("merge_group", "refs/heads/gh-readonly-queue/main/pr-1", None, "full"),
        ("push", "refs/heads/main", None, "full"),
        ("schedule", "refs/heads/main", None, "full"),
        ("workflow_dispatch", "refs/heads/main", None, "full"),
    ],
)
def test_protected_events_have_one_selection_mode(
    event: str, ref: str, base: str | None, expected: str
) -> None:
    assert affected.resolve_selection_mode("auto", event=event, ref=ref, base_sha=base) == expected


@pytest.mark.parametrize(
    "event,ref,base,alternate",
    [
        ("pull_request", "refs/pull/1/merge", "0" * 40, "affected"),
        ("merge_group", "refs/heads/gh-readonly-queue/main/pr-1", None, "affected"),
        ("push", "refs/heads/main", None, "affected"),
        ("schedule", "refs/heads/main", None, "affected"),
        ("workflow_dispatch", "refs/heads/main", None, "affected"),
    ],
)
def test_protected_events_reject_alternate_selection_modes(
    event: str, ref: str, base: str | None, alternate: str
) -> None:
    with pytest.raises(affected.SelectionError, match=r"\[AFFECTED-SELECT-010\]"):
        affected.resolve_selection_mode(alternate, event=event, ref=ref, base_sha=base)


def test_pull_request_affected_mode_requires_explicit_activation(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setattr(affected, "GRAPH_NATIVE_AFFECTED_ACTIVE", True)
    assert (
        affected.resolve_selection_mode(
            "auto",
            event="pull_request",
            ref="refs/pull/1/merge",
            base_sha="0" * 40,
        )
        == "affected"
    )
    with pytest.raises(affected.SelectionError, match=r"\[AFFECTED-SELECT-010\]"):
        affected.resolve_selection_mode(
            "full",
            event="pull_request",
            ref="refs/pull/1/merge",
            base_sha="0" * 40,
        )


def test_protected_event_rejects_wrong_ref_and_local_affected_requires_base() -> None:
    with pytest.raises(affected.SelectionError, match=r"\[AFFECTED-SELECT-009\]"):
        affected.resolve_selection_mode(
            "auto", event="push", ref="refs/heads/feature", base_sha=None
        )
    with pytest.raises(affected.SelectionError, match=r"\[AFFECTED-SELECT-011\]"):
        affected.resolve_selection_mode("affected", event="local", ref=None, base_sha=None)


def test_failure_evidence_redacts_untrusted_exception_content(tmp_path: Path) -> None:
    root = _workspace(tmp_path / "repo")
    evidence = tmp_path / "evidence"
    affected.write_failure_evidence(
        evidence,
        mode="affected",
        event="pull_request",
        base_sha=None,
        error=RuntimeError("secret-stdout secret-stderr /sensitive/path"),
        root=root,
    )
    payload = json.loads((evidence / "selection.json").read_text(encoding="utf-8"))
    assert payload["error"] == {
        "code": "AFFECTED-SELECT-999",
        "message": "affected selection failed",
    }
    assert "secret" not in json.dumps(payload)


def test_checkout_integrity_rejects_dirty_or_wrong_head(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    calls = 0

    def fake_run_git(args, *, root):
        nonlocal calls
        calls += 1
        if args[0] == "rev-parse":
            revision = "1" * 40 if calls == 1 else "2" * 40
            return subprocess.CompletedProcess(args, 0, f"{revision}\n".encode(), b"")
        return subprocess.CompletedProcess(args, 0, b" M secret.py\0", b"")

    monkeypatch.setattr(affected, "run_git", fake_run_git)
    monkeypatch.setattr(affected, "assert_bazelrc_contract", lambda *args, **kwargs: None)
    with pytest.raises(affected.SelectionError) as captured:
        affected.assert_clean_checkout(
            "expected",
            event="pull_request",
            runner_temp=tmp_path,
            cache_mode="disk",
            cache_role="reader",
            root=tmp_path,
        )
    assert captured.value.code == "AFFECTED-SELECT-019"
    assert "secret" not in str(captured.value)


def _initialized_git_repo(path: Path) -> tuple[Path, str]:
    path.mkdir()
    for command in (
        ["init", "--initial-branch=main"],
        ["config", "user.email", "ci@mindclade.invalid"],
        ["config", "user.name", "Mindclade CI"],
    ):
        result = affected.run_git(command, root=path)
        assert result.returncode == 0
    (path / "tracked.txt").write_text("trusted\n", encoding="utf-8")
    (path / ".bazelrc").write_text("try-import %workspace%/user.bazelrc\n", encoding="utf-8")
    (path / ".gitignore").write_text(
        "user.bazelrc\nbazel-*\n*.auto.tfvars\n.venv/\n", encoding="utf-8"
    )
    assert (
        affected.run_git(["add", "tracked.txt", ".bazelrc", ".gitignore"], root=path).returncode
        == 0
    )
    assert affected.run_git(["commit", "-m", "fixture"], root=path).returncode == 0
    return path, affected.git_revision("HEAD", root=path)


def _disk_checkout_arguments(root: Path, *, event: str = "pull_request") -> dict[str, object]:
    runner_temp = root.parent / f"{root.name}-runner-temp"
    runner_temp.mkdir(exist_ok=True)
    cache = runner_temp / "mindclade-bazel-disk-cache"
    cache.mkdir(exist_ok=True)
    role = affected.CACHE_ROLE_BY_ROUTE[("disk", event)]
    lines = (
        affected.BAZELRC_HEADER,
        f"build --disk_cache={cache}",
        f"build --remote_upload_local_results={'true' if role == 'writer' else 'false'}",
        *affected.DISK_BAZELRC_FIXED_LINES,
    )
    (root / "user.bazelrc").write_text("\n".join(lines) + "\n", encoding="utf-8")
    (root / "user.bazelrc").chmod(0o600)
    return {
        "event": event,
        "runner_temp": runner_temp,
        "cache_mode": "disk",
        "cache_role": role,
        "root": root,
    }


def test_checkout_integrity_compares_head_index_and_worktree(tmp_path: Path) -> None:
    root, head = _initialized_git_repo(tmp_path / "repo")
    arguments = _disk_checkout_arguments(root)
    affected.assert_clean_checkout(head, **arguments)

    (root / "tracked.txt").write_text("worktree drift\n", encoding="utf-8")
    with pytest.raises(affected.SelectionError, match=r"\[AFFECTED-SELECT-019\]"):
        affected.assert_clean_checkout(head, **arguments)

    assert affected.run_git(["checkout", "--", "tracked.txt"], root=root).returncode == 0
    (root / "tracked.txt").write_text("index drift\n", encoding="utf-8")
    assert affected.run_git(["add", "tracked.txt"], root=root).returncode == 0
    with pytest.raises(affected.SelectionError, match=r"\[AFFECTED-SELECT-019\]"):
        affected.assert_clean_checkout(head, **arguments)

    assert affected.run_git(["reset", "--hard", "HEAD"], root=root).returncode == 0
    (root / "untracked.txt").write_text("untracked\n", encoding="utf-8")
    with pytest.raises(affected.SelectionError, match=r"\[AFFECTED-SELECT-019\]"):
        affected.assert_clean_checkout(head, **arguments)


@pytest.mark.parametrize("metadata_kind", ["gitfile", "symlink"])
def test_checkout_integrity_rejects_redirected_git_metadata(
    tmp_path: Path, metadata_kind: str
) -> None:
    root, head = _initialized_git_repo(tmp_path / "repo")
    arguments = _disk_checkout_arguments(root)
    metadata = root / ".git"
    external = tmp_path / "external-git"
    metadata.rename(external)
    if metadata_kind == "gitfile":
        metadata.write_text(f"gitdir: {external}\n", encoding="utf-8")
    else:
        metadata.symlink_to(external, target_is_directory=True)
    with pytest.raises(affected.SelectionError, match=r"\[AFFECTED-SELECT-019\]"):
        affected.assert_clean_checkout(head, **arguments)


def test_checkout_integrity_rejects_symlinked_worktree_root(tmp_path: Path) -> None:
    root, head = _initialized_git_repo(tmp_path / "repo")
    arguments = _disk_checkout_arguments(root)
    alias = tmp_path / "repo-alias"
    alias.symlink_to(root, target_is_directory=True)
    with pytest.raises(affected.SelectionError, match=r"\[AFFECTED-SELECT-019\]"):
        affected.assert_clean_checkout(head, **{**arguments, "root": alias})


def _create_canonical_bazel_symlinks(root: Path, output_root: Path) -> None:
    execroot = output_root / "_bazel_ci" / ("a" * 32) / "execroot" / "_main"
    configuration = execroot / "bazel-out" / "linux-fastbuild"
    (configuration / "bin").mkdir(parents=True)
    (configuration / "testlogs").mkdir()
    for name, target in {
        f"bazel-{root.name}": execroot,
        "bazel-out": execroot / "bazel-out",
        "bazel-bin": configuration / "bin",
        "bazel-testlogs": configuration / "testlogs",
    }.items():
        (root / name).symlink_to(target)


def test_checkout_integrity_allows_only_canonical_generated_bazel_state(
    tmp_path: Path,
) -> None:
    root, head = _initialized_git_repo(tmp_path / "repo")
    arguments = _disk_checkout_arguments(root)
    _create_canonical_bazel_symlinks(root, tmp_path / "output")
    affected.assert_clean_checkout(head, **arguments)


@pytest.mark.parametrize(
    "relative",
    ["secret.auto.tfvars", ".venv/secret.py", ".ignored/secret.txt"],
)
def test_checkout_integrity_rejects_every_other_ignored_file(tmp_path: Path, relative: str) -> None:
    root, head = _initialized_git_repo(tmp_path / "repo")
    if relative.startswith(".ignored/"):
        with (root / ".gitignore").open("a", encoding="utf-8") as stream:
            stream.write(".ignored/\n")
        assert affected.run_git(["add", ".gitignore"], root=root).returncode == 0
        assert affected.run_git(["commit", "-m", "ignore fixture"], root=root).returncode == 0
        head = affected.git_revision("HEAD", root=root)
    candidate = root / relative
    candidate.parent.mkdir(parents=True, exist_ok=True)
    candidate.write_text("secret\n", encoding="utf-8")
    with pytest.raises(affected.SelectionError, match=r"\[AFFECTED-SELECT-019\]"):
        affected.assert_clean_checkout(head, **_disk_checkout_arguments(root))


@pytest.mark.parametrize("relative", ["empty-untracked", ".venv"])
def test_checkout_integrity_rejects_empty_untracked_directory(
    tmp_path: Path, relative: str
) -> None:
    root, head = _initialized_git_repo(tmp_path / "repo")
    (root / relative).mkdir()
    with pytest.raises(affected.SelectionError, match=r"\[AFFECTED-SELECT-019\]"):
        affected.assert_clean_checkout(head, **_disk_checkout_arguments(root))


def test_checkout_integrity_rejects_tracked_user_bazelrc(tmp_path: Path) -> None:
    root, _head = _initialized_git_repo(tmp_path / "repo")
    arguments = _disk_checkout_arguments(root)
    assert affected.run_git(["add", "--force", "user.bazelrc"], root=root).returncode == 0
    assert affected.run_git(["commit", "-m", "track generated rc"], root=root).returncode == 0
    head = affected.git_revision("HEAD", root=root)
    with pytest.raises(affected.SelectionError, match=r"\[AFFECTED-SELECT-019\]"):
        affected.assert_clean_checkout(head, **arguments)


def test_checkout_integrity_rejects_unsafe_bazel_output_symlink(tmp_path: Path) -> None:
    root, head = _initialized_git_repo(tmp_path / "repo")
    (root / "bazel-out").symlink_to(tmp_path)
    with pytest.raises(affected.SelectionError, match=r"\[AFFECTED-SELECT-019\]"):
        affected.assert_clean_checkout(head, **_disk_checkout_arguments(root))


@pytest.mark.parametrize("flag", ["--assume-unchanged", "--skip-worktree"])
def test_checkout_integrity_rejects_hidden_index_flags(tmp_path: Path, flag: str) -> None:
    root, head = _initialized_git_repo(tmp_path / "repo")
    arguments = _disk_checkout_arguments(root)
    assert affected.run_git(["update-index", flag, "tracked.txt"], root=root).returncode == 0
    with pytest.raises(affected.SelectionError, match=r"\[AFFECTED-SELECT-019\]"):
        affected.assert_clean_checkout(head, **arguments)


def test_git_metadata_ignores_inherited_repository_overrides(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    root, head = _initialized_git_repo(tmp_path / "repo")
    arguments = _disk_checkout_arguments(root)
    for name, value in {
        "GIT_CONFIG_COUNT": "1",
        "GIT_CONFIG_KEY_0": "core.bare",
        "GIT_CONFIG_VALUE_0": "true",
        "GIT_DIR": str(tmp_path / "spoofed-git-dir"),
        "GIT_INDEX_FILE": str(tmp_path / "spoofed-index"),
        "GIT_WORK_TREE": str(tmp_path / "spoofed-worktree"),
    }.items():
        monkeypatch.setenv(name, value)
    affected.assert_clean_checkout(head, **arguments)


def test_git_metadata_binds_the_validated_worktree(
    tmp_path: Path,
) -> None:
    root, head = _initialized_git_repo(tmp_path / "repo")
    arguments = _disk_checkout_arguments(root)
    decoy = tmp_path / "decoy"
    shutil.copytree(root, decoy, ignore=shutil.ignore_patterns(".git"))
    assert affected.run_git(["config", "core.worktree", str(decoy)], root=root).returncode == 0

    (root / "tracked.txt").write_text("untrusted worktree drift\n", encoding="utf-8")
    with pytest.raises(affected.SelectionError, match=r"\[AFFECTED-SELECT-019\]"):
        affected.assert_clean_checkout(head, **arguments)


def test_git_context_supports_a_valid_linked_worktree(tmp_path: Path) -> None:
    root, head = _initialized_git_repo(tmp_path / "repo")
    linked = tmp_path / "linked"
    assert (
        affected.run_git(
            ["worktree", "add", "-b", "linked-fixture", str(linked), head],
            root=root,
        ).returncode
        == 0
    )
    workspace, git_dir = affected._canonical_git_context(linked)
    assert workspace == linked
    assert git_dir.is_dir()
    assert affected.git_revision("HEAD", root=linked) == head


def test_governed_checkout_rejects_an_otherwise_valid_linked_worktree(tmp_path: Path) -> None:
    root, head = _initialized_git_repo(tmp_path / "repo")
    linked = tmp_path / "linked"
    assert (
        affected.run_git(
            ["worktree", "add", "-b", "linked-fixture", str(linked), head],
            root=root,
        ).returncode
        == 0
    )
    arguments = _disk_checkout_arguments(linked)

    with pytest.raises(affected.SelectionError) as captured:
        affected.assert_clean_checkout(head, **arguments)
    assert captured.value.code == "AFFECTED-SELECT-019"
    assert str(linked) not in str(captured.value)


def test_git_context_rejects_symlinked_linked_worktree_registry(tmp_path: Path) -> None:
    root, head = _initialized_git_repo(tmp_path / "repo")
    linked = tmp_path / "linked"
    assert (
        affected.run_git(
            ["worktree", "add", "-b", "linked-fixture", str(linked), head],
            root=root,
        ).returncode
        == 0
    )
    dot_git = linked / ".git"
    git_dir = Path(dot_git.read_text(encoding="utf-8").strip().removeprefix("gitdir: "))
    alias = tmp_path / "gitdir-alias"
    alias.symlink_to(git_dir, target_is_directory=True)
    dot_git.write_text(f"gitdir: {alias}\n", encoding="utf-8")

    with pytest.raises(affected.SelectionError, match=r"\[AFFECTED-SELECT-003\]"):
        affected._canonical_git_context(linked)


def test_git_context_rejects_forged_reciprocal_metadata_pair(tmp_path: Path) -> None:
    root, _head = _initialized_git_repo(tmp_path / "repo")
    common_dir = tmp_path / "forged" / ".git"
    git_dir = common_dir / "registrations" / "linked"
    git_dir.mkdir(parents=True)
    dot_git = root / ".git"
    shutil.rmtree(dot_git)
    dot_git.write_text(f"gitdir: {git_dir}\n", encoding="utf-8")
    (git_dir / "gitdir").write_text(f"{dot_git}\n", encoding="utf-8")
    (git_dir / "commondir").write_text("../..\n", encoding="utf-8")

    with pytest.raises(affected.SelectionError, match=r"\[AFFECTED-SELECT-003\]"):
        affected._canonical_git_context(root)


@pytest.mark.parametrize(
    "payload",
    [
        b"",
        b"a" * 40,
        b"A" * 40 + b"\n",
        b"g" * 40 + b"\n",
        b"a" * 39 + b"\n",
        b"a" * 41 + b"\n",
        b"a" * 40 + b"\n" + b"b" * 40 + b"\n",
    ],
)
def test_git_revision_rejects_noncanonical_output(
    payload: bytes, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setattr(
        affected,
        "run_git",
        lambda *args, **kwargs: subprocess.CompletedProcess(args, 0, payload, b"secret-stderr"),
    )
    with pytest.raises(affected.SelectionError) as captured:
        affected.git_revision("HEAD")
    assert captured.value.code == "AFFECTED-SELECT-003"
    assert "secret" not in str(captured.value)


@pytest.mark.parametrize("value", [None, "git", "/tmp/missing-mindclade-git"])
def test_trusted_git_launcher_rejects_missing_or_unpinned_paths(
    monkeypatch: pytest.MonkeyPatch, value: str | None
) -> None:
    if value is None:
        monkeypatch.delenv("MINDCLADE_GIT", raising=False)
    else:
        monkeypatch.setenv("MINDCLADE_GIT", value)
    with pytest.raises(affected.SelectionError) as captured:
        affected.trusted_git_launcher()
    assert captured.value.code == "AFFECTED-SELECT-022"
    assert value is None or value not in str(captured.value)


def test_trusted_git_launcher_rejects_mutable_binary(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    source = affected.trusted_git_launcher()
    store = tmp_path / "nix/store"
    mutable = store / "fixture-git/bin/git"
    mutable.parent.mkdir(parents=True)
    shutil.copy2(source, mutable)
    mutable.chmod(0o755)
    monkeypatch.setattr(affected, "NIX_STORE_ROOT", store)
    monkeypatch.setenv("MINDCLADE_GIT", str(mutable))
    with pytest.raises(affected.SelectionError, match=r"\[AFFECTED-SELECT-022\]"):
        affected.trusted_git_launcher()


@pytest.mark.parametrize(
    "cache_mode,event,role",
    [
        ("disk", "pull_request", "reader"),
        ("disk", "merge_group", "reader"),
        ("disk", "push", "writer"),
        ("disk", "schedule", "writer"),
        ("disk", "workflow_dispatch", "writer"),
        ("remote", "pull_request", "reader"),
        ("remote", "merge_group", "writer"),
        ("remote", "push", "writer"),
        ("remote", "schedule", "writer"),
    ],
)
def test_bazelrc_runtime_contract_is_exact(
    tmp_path: Path, cache_mode: str, event: str, role: str
) -> None:
    root = _workspace(tmp_path / "repo")
    (root / ".bazelrc").write_text(
        "build:ci --keep_going\ntry-import %workspace%/user.bazelrc\n",
        encoding="utf-8",
    )
    runner_temp = tmp_path / "runner-temp"
    runner_temp.mkdir()
    cache = runner_temp / "mindclade-bazel-disk-cache"
    cache.mkdir()
    cache_line = (
        f"build --disk_cache={cache}"
        if cache_mode == "disk"
        else f"build --remote_cache={affected.REMOTE_CACHE_ENDPOINT}"
    )
    fixed_lines = (
        affected.DISK_BAZELRC_FIXED_LINES
        if cache_mode == "disk"
        else affected.REMOTE_BAZELRC_FIXED_LINES
    )
    lines = (
        affected.BAZELRC_HEADER,
        cache_line,
        f"build --remote_upload_local_results={'true' if role == 'writer' else 'false'}",
        *fixed_lines,
    )
    (root / "user.bazelrc").write_text("\n".join(lines) + "\n", encoding="utf-8")
    (root / "user.bazelrc").chmod(0o600)
    affected.assert_bazelrc_contract(
        event,
        runner_temp,
        cache_mode=cache_mode,
        cache_role=role,
        root=root,
    )

    (root / "user.bazelrc").write_text(
        "\n".join((*lines, "build --nobuild secret-option")) + "\n",
        encoding="utf-8",
    )
    with pytest.raises(affected.SelectionError) as captured:
        affected.assert_bazelrc_contract(
            event,
            runner_temp,
            cache_mode=cache_mode,
            cache_role=role,
            root=root,
        )
    assert captured.value.code == "AFFECTED-SELECT-020"
    assert "secret-option" not in str(captured.value)

    wrong_lines = (lines[0], "build --remote_cache=http://127.0.0.1:9999", *lines[2:])
    (root / "user.bazelrc").write_text("\n".join(wrong_lines) + "\n", encoding="utf-8")
    with pytest.raises(affected.SelectionError, match=r"\[AFFECTED-SELECT-020\]"):
        affected.assert_bazelrc_contract(
            event,
            runner_temp,
            cache_mode=cache_mode,
            cache_role=role,
            root=root,
        )


def test_remote_manual_dispatch_is_never_authorized(tmp_path: Path) -> None:
    root = _workspace(tmp_path / "repo")
    (root / ".bazelrc").write_text("try-import %workspace%/user.bazelrc\n", encoding="utf-8")
    runner_temp = tmp_path / "runner-temp"
    runner_temp.mkdir()
    lines = (
        affected.BAZELRC_HEADER,
        f"build --remote_cache={affected.REMOTE_CACHE_ENDPOINT}",
        "build --remote_upload_local_results=true",
        *affected.REMOTE_BAZELRC_FIXED_LINES,
    )
    (root / "user.bazelrc").write_text("\n".join(lines) + "\n", encoding="utf-8")
    (root / "user.bazelrc").chmod(0o600)
    with pytest.raises(affected.SelectionError, match=r"\[AFFECTED-SELECT-020\]"):
        affected.assert_bazelrc_contract(
            "workflow_dispatch",
            runner_temp,
            cache_mode="remote",
            cache_role="writer",
            root=root,
        )


@pytest.mark.parametrize(
    "workspace_configuration",
    [
        "build --remote_cache=https://untrusted.invalid\ntry-import %workspace%/user.bazelrc\n",
        "build --disk_cache=/tmp/untrusted-cache\ntry-import %workspace%/user.bazelrc\n",
        "try-import %workspace%/user.bazelrc\nbuild --nobuild\n",
        "try-import %workspace%/other.bazelrc\ntry-import %workspace%/user.bazelrc\n",
    ],
)
def test_bazelrc_runtime_contract_rejects_tracked_authority_drift(
    tmp_path: Path, workspace_configuration: str
) -> None:
    root = _workspace(tmp_path / "repo")
    runner_temp = tmp_path / "runner-temp"
    runner_temp.mkdir()
    cache = runner_temp / "mindclade-bazel-disk-cache"
    cache.mkdir()
    (root / ".bazelrc").write_text(workspace_configuration, encoding="utf-8")
    lines = (
        affected.BAZELRC_HEADER,
        f"build --disk_cache={cache}",
        "build --remote_upload_local_results=false",
        *affected.DISK_BAZELRC_FIXED_LINES,
    )
    (root / "user.bazelrc").write_text("\n".join(lines) + "\n", encoding="utf-8")
    (root / "user.bazelrc").chmod(0o600)

    with pytest.raises(affected.SelectionError) as captured:
        affected.assert_bazelrc_contract(
            "pull_request",
            runner_temp,
            cache_mode="disk",
            cache_role="reader",
            root=root,
        )
    assert captured.value.code == "AFFECTED-SELECT-020"
    assert "untrusted" not in str(captured.value)


@pytest.mark.parametrize("relative", [".bazelrc", "user.bazelrc"])
def test_bazel_execution_rejects_bazelrc_mutation_after_checkout_validation(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
    relative: str,
) -> None:
    root, head = _initialized_git_repo(tmp_path / "repo")
    arguments = _disk_checkout_arguments(root)
    authority = affected.assert_clean_checkout(head, **arguments)
    evidence = tmp_path / "evidence"
    evidence.mkdir()
    candidate = root / relative
    candidate.write_text(candidate.read_text(encoding="utf-8") + "# mutated\n", encoding="utf-8")
    if relative == "user.bazelrc":
        candidate.chmod(0o600)
    launched = False

    def fake_run(*_args, **_kwargs):
        nonlocal launched
        launched = True
        return subprocess.CompletedProcess([], 0)

    monkeypatch.setattr(affected.subprocess, "run", fake_run)
    with pytest.raises(affected.SelectionError) as captured:
        affected._run_phase(
            "analysis",
            ("//pkg:library",),
            evidence_dir=evidence,
            bazelrc_authority=authority,
            root=root,
        )
    assert captured.value.code == "AFFECTED-SELECT-020"
    assert not launched


@pytest.mark.parametrize("replacement", ["deleted", "symlink"])
def test_bazel_execution_redacts_bazelrc_replacement_after_validation(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
    replacement: str,
) -> None:
    root, head = _initialized_git_repo(tmp_path / "repo")
    arguments = _disk_checkout_arguments(root)
    authority = affected.assert_clean_checkout(head, **arguments)
    evidence = tmp_path / "evidence"
    evidence.mkdir()
    candidate = root / "user.bazelrc"
    candidate.unlink()
    if replacement == "symlink":
        secret = tmp_path / "secret-bazelrc"
        secret.write_text("secret runtime configuration\n", encoding="utf-8")
        candidate.symlink_to(secret)
    launched = False

    def fake_run(*_args, **_kwargs):
        nonlocal launched
        launched = True
        return subprocess.CompletedProcess([], 0)

    monkeypatch.setattr(affected.subprocess, "run", fake_run)
    with pytest.raises(affected.SelectionError) as captured:
        affected._run_phase(
            "analysis",
            ("//pkg:library",),
            evidence_dir=evidence,
            bazelrc_authority=authority,
            root=root,
        )
    assert captured.value.code == "AFFECTED-SELECT-020"
    assert "secret" not in str(captured.value)
    assert not launched


def test_job_started_epoch_is_exact_positive_integer_seconds(tmp_path: Path) -> None:
    path = tmp_path / "bazel-job-started"
    path.write_text("1700000000\n", encoding="utf-8")
    assert (
        affected.load_job_started_epoch(
            path,
            runner_temp=tmp_path,
            now_epoch=1700000001,
        )
        == 1700000000
    )

    alternate = tmp_path / "alternate-job-started"
    alternate.write_text("1700000000\n", encoding="utf-8")
    with pytest.raises(affected.SelectionError) as captured:
        affected.load_job_started_epoch(
            alternate,
            runner_temp=tmp_path,
            now_epoch=1700000001,
        )
    assert captured.value.code == "AFFECTED-SELECT-014"
    assert str(captured.value) == "[AFFECTED-SELECT-014] job-start timestamp is invalid"


@pytest.mark.parametrize(
    "payload",
    [
        "",
        "0\n",
        "-1\n",
        "+1\n",
        "01\n",
        "1.0\n",
        "1e3\n",
        "nan\n",
        "inf\n",
        " 1\n",
        "1 \n",
        "1\n\n",
        "1700000002\n",
    ],
)
def test_job_started_epoch_rejects_noncanonical_or_future_values(
    tmp_path: Path, payload: str
) -> None:
    path = tmp_path / "job-started"
    path.write_text(payload, encoding="utf-8")
    with pytest.raises(affected.SelectionError) as captured:
        affected.load_job_started_epoch(path, now_epoch=1700000001)
    assert captured.value.code == "AFFECTED-SELECT-014"
    assert str(captured.value) == "[AFFECTED-SELECT-014] job-start timestamp is invalid"


def test_job_started_epoch_redacts_read_and_unicode_errors(tmp_path: Path) -> None:
    for path in (tmp_path / "missing", tmp_path / "invalid"):
        if path.name == "invalid":
            path.write_bytes(b"\xffsecret-timestamp")
        with pytest.raises(affected.SelectionError) as captured:
            affected.load_job_started_epoch(path, now_epoch=1700000001)
        assert captured.value.code == "AFFECTED-SELECT-014"
        assert "secret-timestamp" not in str(captured.value)


def test_execution_oserror_is_redacted_and_evidenced(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    root = _workspace(tmp_path / "repo")
    evidence = tmp_path / "evidence"
    selection = affected.Selection(
        mode="full",
        reason="explicit_full",
        changes=(),
        seeds=("//...",),
        analysis_targets=("//...",),
        test_targets=("//...",),
        base_sha=None,
        head_sha="1" * 40,
        event="push",
    )
    monkeypatch.setattr(
        affected.subprocess,
        "run",
        lambda *_args, **_kwargs: (_ for _ in ()).throw(
            OSError("secret-launcher-path secret-stdout secret-stderr")
        ),
    )
    with pytest.raises(affected.SelectionError) as captured:
        affected.execute_selection(selection, evidence, root=root)
    assert captured.value.code == "AFFECTED-SELECT-021"
    assert "secret" not in str(captured.value)

    affected.write_failure_evidence(
        evidence,
        mode="full",
        event="push",
        base_sha=None,
        error=captured.value,
        root=root,
    )
    payload = json.loads((evidence / "selection.json").read_text(encoding="utf-8"))
    assert payload["error"] == {
        "code": "AFFECTED-SELECT-021",
        "message": "Bazel execution failed",
    }
    assert "secret" not in json.dumps(payload)


def test_bazel_query_unicode_failure_is_redacted(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setattr(
        affected.subprocess,
        "run",
        lambda *_args, **_kwargs: (_ for _ in ()).throw(UnicodeError("secret-decoder-content")),
    )
    with pytest.raises(affected.SelectionError) as captured:
        affected.bazel_query("//...", root=tmp_path, bazel=tmp_path / "bazel")
    assert captured.value.code == "AFFECTED-SELECT-007"
    assert "secret-decoder-content" not in str(captured.value)
