# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import os
import shutil
import subprocess
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]


def executable(path: Path, body: str) -> None:
    path.write_text("#!/usr/bin/env bash\nset -euo pipefail\n" + body, encoding="utf-8")
    path.chmod(0o755)


def repository(tmp_path: Path) -> tuple[Path, Path, Path]:
    repo = tmp_path / "repo"
    dev = repo / "tools/dev"
    launchers = tmp_path / "launchers"
    nested = repo / "one/two"
    dev.mkdir(parents=True)
    launchers.mkdir()
    nested.mkdir(parents=True)
    shutil.copy2(ROOT / "tools/dev/bazelw", dev / "bazelw")
    (dev / "bazelw").chmod(0o755)
    (repo / ".bazelversion").write_text("9.1.1\n", encoding="utf-8")
    return repo, launchers, nested


def run(wrapper: Path, cwd: Path, launchers: Path, *arguments: str, log: Path | None = None):
    env = os.environ.copy()
    env["PATH"] = f"{launchers}:/bin:/usr/bin"
    if log is not None:
        env["MINDCLADE_FAKE_BAZEL_LOG"] = str(log)
    return subprocess.run(
        [str(wrapper), *arguments],
        cwd=cwd,
        env=env,
        capture_output=True,
        check=False,
        text=True,
    )


def recording_launcher(path: Path, identity: str) -> None:
    executable(
        path,
        f'printf "{identity}\\n%s\\n" "$PWD" > "$MINDCLADE_FAKE_BAZEL_LOG"\n'
        'printf "%s\\n" "$@" >> "$MINDCLADE_FAKE_BAZEL_LOG"\n',
    )


def test_bazelisk_is_preferred_and_arguments_pass_through(tmp_path: Path) -> None:
    repo, launchers, nested = repository(tmp_path)
    log = tmp_path / "invocation.log"
    recording_launcher(launchers / "bazelisk", "bazelisk")
    recording_launcher(launchers / "bazel", "bazel")

    completed = run(
        repo / "tools/dev/bazelw", nested, launchers, "test", "//pkg:target", "--config=ci", log=log
    )

    assert completed.returncode == 0
    assert log.read_text(encoding="utf-8").splitlines() == [
        "bazelisk",
        str(repo),
        "test",
        "//pkg:target",
        "--config=ci",
    ]


def test_plain_bazel_with_wrong_version_is_rejected(tmp_path: Path) -> None:
    repo, launchers, nested = repository(tmp_path)
    executable(
        launchers / "bazel",
        'if [[ "${1:-}" == "--version" ]]; then echo "bazel 8.4.2"; exit 0; fi\nexit 99\n',
    )

    completed = run(repo / "tools/dev/bazelw", nested, launchers, "query", "//...")

    assert completed.returncode == 2
    assert "requires 'bazel 9.1.1'" in completed.stderr


def test_matching_plain_bazel_is_accepted(tmp_path: Path) -> None:
    repo, launchers, nested = repository(tmp_path)
    log = tmp_path / "invocation.log"
    executable(
        launchers / "bazel",
        'if [[ "${1:-}" == "--version" ]]; then echo "bazel 9.1.1"; exit 0; fi\n'
        'printf "bazel\\n%s\\n" "$PWD" > "$MINDCLADE_FAKE_BAZEL_LOG"\n'
        'printf "%s\\n" "$@" >> "$MINDCLADE_FAKE_BAZEL_LOG"\n',
    )

    completed = run(repo / "tools/dev/bazelw", nested, launchers, "query", "//...", log=log)

    assert completed.returncode == 0
    assert log.read_text(encoding="utf-8").splitlines() == ["bazel", str(repo), "query", "//..."]


def test_missing_launcher_has_install_diagnostic(tmp_path: Path) -> None:
    repo, launchers, nested = repository(tmp_path)

    completed = run(repo / "tools/dev/bazelw", nested, launchers, "query", "//...")

    assert completed.returncode == 127
    assert "neither bazelisk nor bazel is on PATH" in completed.stderr
