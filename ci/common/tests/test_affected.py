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
    assert all('attr("tags", "[\\\\[ ]manual[,\\\\]]"' in expression for expression in expressions)


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
        if command == ("rev-parse", "--verify", "base^{commit}"):
            return subprocess.CompletedProcess(args, 0, f"{base}\n".encode(), b"")
        if command == ("rev-parse", "--verify", "HEAD^{commit}"):
            return subprocess.CompletedProcess(args, 0, f"{head}\n".encode(), b"")
        if command == ("merge-base", "--is-ancestor", base, head):
            return subprocess.CompletedProcess(args, 0, b"", b"")
        if command == (
            "diff",
            "--name-status",
            "-z",
            "--find-renames",
            f"{base}...{head}",
        ):
            return subprocess.CompletedProcess(args, 0, b"R100\0original.txt\0renamed.txt\0", b"")
        if command == ("rev-parse", "--verify", "does-not-exist^{commit}"):
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
    runtime_contract = affected.BazelRuntimeContract(
        disk_cache=tmp_path / "runner-cache",
        remote_upload_local_results=False,
    )

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
            runtime_contract=runtime_contract,
            root=root,
        )
        assert result["status"] == "passed"

    assert len(environments) == 2
    assert all("BAZELISK_GITHUB_TOKEN" not in environment for environment in environments)
    assert [command[2] for command in commands] == ["build", "test"]
    assert all(command[1] == "--nohome_rc" for command in commands)
    assert all(
        command[command.index("--config=ci") + 1 : command.index("--config=ci") + 3]
        == [
            f"--disk_cache={runtime_contract.disk_cache}",
            "--remote_upload_local_results=false",
        ]
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
        ("pull_request", "refs/pull/1/merge", "0" * 40, "affected"),
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
        ("pull_request", "refs/pull/1/merge", "0" * 40, "full"),
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
    with pytest.raises(affected.SelectionError) as captured:
        affected.assert_clean_checkout("expected", root=tmp_path)
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
    assert affected.run_git(["add", "tracked.txt"], root=path).returncode == 0
    assert affected.run_git(["commit", "-m", "fixture"], root=path).returncode == 0
    return path, affected.git_revision("HEAD", root=path)


def test_checkout_integrity_compares_head_index_and_worktree(tmp_path: Path) -> None:
    root, head = _initialized_git_repo(tmp_path / "repo")
    affected.assert_clean_checkout(head, root=root)

    (root / "tracked.txt").write_text("worktree drift\n", encoding="utf-8")
    with pytest.raises(affected.SelectionError, match=r"\[AFFECTED-SELECT-019\]"):
        affected.assert_clean_checkout(head, root=root)

    assert affected.run_git(["checkout", "--", "tracked.txt"], root=root).returncode == 0
    (root / "tracked.txt").write_text("index drift\n", encoding="utf-8")
    assert affected.run_git(["add", "tracked.txt"], root=root).returncode == 0
    with pytest.raises(affected.SelectionError, match=r"\[AFFECTED-SELECT-019\]"):
        affected.assert_clean_checkout(head, root=root)

    assert affected.run_git(["reset", "--hard", "HEAD"], root=root).returncode == 0
    (root / "untracked.txt").write_text("untracked\n", encoding="utf-8")
    with pytest.raises(affected.SelectionError, match=r"\[AFFECTED-SELECT-019\]"):
        affected.assert_clean_checkout(head, root=root)


@pytest.mark.parametrize("metadata_kind", ["gitfile", "symlink"])
def test_checkout_integrity_rejects_redirected_git_metadata(
    tmp_path: Path, metadata_kind: str
) -> None:
    root, head = _initialized_git_repo(tmp_path / "repo")
    metadata = root / ".git"
    external = tmp_path / "external-git"
    metadata.rename(external)
    if metadata_kind == "gitfile":
        metadata.write_text(f"gitdir: {external}\n", encoding="utf-8")
    else:
        metadata.symlink_to(external, target_is_directory=True)
    with pytest.raises(affected.SelectionError, match=r"\[AFFECTED-SELECT-019\]"):
        affected.assert_clean_checkout(head, root=root)


def test_checkout_integrity_rejects_symlinked_worktree_root(tmp_path: Path) -> None:
    root, head = _initialized_git_repo(tmp_path / "repo")
    alias = tmp_path / "repo-alias"
    alias.symlink_to(root, target_is_directory=True)
    with pytest.raises(affected.SelectionError, match=r"\[AFFECTED-SELECT-019\]"):
        affected.assert_clean_checkout(head, root=alias)


@pytest.mark.parametrize("flag", ["--assume-unchanged", "--skip-worktree"])
def test_checkout_integrity_rejects_hidden_index_flags(tmp_path: Path, flag: str) -> None:
    root, head = _initialized_git_repo(tmp_path / "repo")
    assert affected.run_git(["update-index", flag, "tracked.txt"], root=root).returncode == 0
    with pytest.raises(affected.SelectionError, match=r"\[AFFECTED-SELECT-019\]"):
        affected.assert_clean_checkout(head, root=root)


def test_git_metadata_ignores_inherited_repository_overrides(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    root, head = _initialized_git_repo(tmp_path / "repo")
    for name, value in {
        "GIT_CONFIG_COUNT": "1",
        "GIT_CONFIG_KEY_0": "core.bare",
        "GIT_CONFIG_VALUE_0": "true",
        "GIT_DIR": str(tmp_path / "spoofed-git-dir"),
        "GIT_INDEX_FILE": str(tmp_path / "spoofed-index"),
        "GIT_WORK_TREE": str(tmp_path / "spoofed-worktree"),
    }.items():
        monkeypatch.setenv(name, value)
    affected.assert_clean_checkout(head, root=root)


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
    "event,upload",
    [
        ("pull_request", "false"),
        ("merge_group", "false"),
        ("push", "true"),
        ("schedule", "true"),
        ("workflow_dispatch", "true"),
    ],
)
def test_bazelrc_runtime_contract_is_exact(tmp_path: Path, event: str, upload: str) -> None:
    root = _workspace(tmp_path / "repo")
    runner_temp = tmp_path / "runner-temp"
    runner_temp.mkdir()
    cache = runner_temp / "mindclade-bazel-disk-cache"
    cache.mkdir()
    lines = (
        affected.BAZELRC_FIXED_LINES[0],
        f"build --disk_cache={cache}",
        f"build --remote_upload_local_results={upload}",
        *affected.BAZELRC_FIXED_LINES[1:],
    )
    (root / "user.bazelrc").write_text("\n".join(lines) + "\n", encoding="utf-8")
    runtime_contract = affected.assert_bazelrc_contract(event, runner_temp, root=root)
    assert runtime_contract == affected.BazelRuntimeContract(
        disk_cache=cache,
        remote_upload_local_results=upload == "true",
    )
    assert runtime_contract.command_options()[:2] == (
        f"--disk_cache={cache}",
        f"--remote_upload_local_results={upload}",
    )

    (root / "user.bazelrc").write_text(
        "\n".join((*lines, "build --nobuild secret-option")) + "\n",
        encoding="utf-8",
    )
    with pytest.raises(affected.SelectionError) as captured:
        affected.assert_bazelrc_contract(event, runner_temp, root=root)
    assert captured.value.code == "AFFECTED-SELECT-020"
    assert "secret-option" not in str(captured.value)

    other_cache = tmp_path / "other-cache"
    other_cache.mkdir()
    wrong_lines = (lines[0], f"build --disk_cache={other_cache}", *lines[2:])
    (root / "user.bazelrc").write_text("\n".join(wrong_lines) + "\n", encoding="utf-8")
    with pytest.raises(affected.SelectionError, match=r"\[AFFECTED-SELECT-020\]"):
        affected.assert_bazelrc_contract(event, runner_temp, root=root)


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
    runtime_contract = affected.BazelRuntimeContract(
        disk_cache=tmp_path / "cache",
        remote_upload_local_results=True,
    )
    with pytest.raises(affected.SelectionError) as captured:
        affected.execute_selection(
            selection,
            evidence,
            runtime_contract=runtime_contract,
            root=root,
        )
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


def test_protected_execution_requires_runtime_contract(tmp_path: Path) -> None:
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
    with pytest.raises(affected.SelectionError) as captured:
        affected.execute_selection(selection, tmp_path / "evidence", root=tmp_path)
    assert captured.value.code == "AFFECTED-SELECT-020"


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
