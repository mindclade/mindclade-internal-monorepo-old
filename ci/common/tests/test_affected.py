# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

import json
import subprocess
from pathlib import Path

import pytest

from ci.common import affected


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


@pytest.mark.parametrize("status", ["C", "D", "R", "T", "U", "X", "B"])
def test_structural_changes_force_full_graph(tmp_path: Path, status: str) -> None:
    selection = _select(
        tmp_path,
        [affected.Change(status=status, path="pkg/library.py")],
        lambda _expression: pytest.fail("query ran"),
    )
    assert selection.mode == "full"
    assert selection.reason == f"structural_{status.lower()}"


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
        raise affected.SelectionError("repository fetch failed")

    with pytest.raises(affected.SelectionError, match="repository fetch failed"):
        _select(root, ["pkg/library.py"], fail)


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

    monkeypatch.setattr(affected, "_run_git", fake_run_git)
    changes = affected.git_changed("base", root=tmp_path)
    assert changes == (affected.Change(status="R", path="renamed.txt", old_path="original.txt"),)
    with pytest.raises(affected.SelectionError, match="invalid git revision"):
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
        event="pull_request",
    )
    assert affected.execute_selection(selection, evidence, root=root, job_started_epoch=0) == 0
    payload = json.loads((evidence / "selection.json").read_text(encoding="utf-8"))
    assert payload["schema_version"] == 1
    assert payload["execution"][0]["reason"] == "no_targets"
    assert payload["latency_slo_met"] is False

    with pytest.raises(affected.SelectionError, match="outside"):
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
    assert [command[1] for command in commands] == ["build", "test"]
    assert all(
        any(argument.startswith("--build_event_json_file=") for argument in command)
        for command in commands
    )


def test_unsafe_changed_path_is_rejected(tmp_path: Path) -> None:
    with pytest.raises(affected.SelectionError, match="unsafe"):
        _select(tmp_path, ["../outside"], lambda _expression: ())
