# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

import os
import stat
import subprocess
import textwrap
from pathlib import Path

import pytest

SCRIPT = Path(__file__).resolve().parents[1] / "prepare_nix_runner.sh"
MINIMUM_ROOT_FREE_BYTES = 50 * 1024 * 1024 * 1024


def _write_executable(path: Path, contents: str) -> None:
    path.write_text(textwrap.dedent(contents).lstrip(), encoding="utf-8")
    path.chmod(path.stat().st_mode | stat.S_IXUSR)


def _run_script(
    tmp_path: Path,
    *,
    free_bytes: int | str = MINIMUM_ROOT_FREE_BYTES,
    github_actions: str = "true",
    runner_environment: str = "github-hosted",
    kernel: str = "Linux",
) -> tuple[subprocess.CompletedProcess[str], str]:
    fake_bin = tmp_path / "bin"
    fake_bin.mkdir()
    call_log = tmp_path / "sudo-calls"
    _write_executable(
        fake_bin / "uname",
        f"""
        #!/usr/bin/env bash
        printf '%s\\n' {kernel!r}
        """,
    )
    _write_executable(
        fake_bin / "df",
        """
        #!/usr/bin/env bash
        if [[ "$*" == "--output=avail -B1 /" ]]; then
          printf 'Avail\\n%s\\n' "${FAKE_ROOT_FREE_BYTES}"
        else
          printf 'Filesystem Size Used Avail Use%% Mounted on\\n/dev/fake 100G 1G 99G 1%% /\\n'
        fi
        """,
    )
    _write_executable(
        fake_bin / "sudo",
        """
        #!/usr/bin/env bash
        printf '%s\\n' "$*" >> "${FAKE_SUDO_CALL_LOG}"
        """,
    )
    environment = os.environ.copy()
    environment.update(
        {
            "FAKE_ROOT_FREE_BYTES": str(free_bytes),
            "FAKE_SUDO_CALL_LOG": str(call_log),
            "GITHUB_ACTIONS": github_actions,
            "PATH": f"{fake_bin}:{environment['PATH']}",
            "RUNNER_ENVIRONMENT": runner_environment,
        }
    )
    result = subprocess.run(
        [SCRIPT],
        check=False,
        capture_output=True,
        env=environment,
        text=True,
    )
    calls = call_log.read_text(encoding="utf-8") if call_log.exists() else ""
    return result, calls


def test_cleanup_removes_only_nix_unused_hosted_payloads(tmp_path: Path) -> None:
    result, calls = _run_script(tmp_path)

    assert result.returncode == 0, result.stderr
    for path in (
        "/home/linuxbrew",
        "/home/runner/.cargo",
        "/home/runner/.nvm",
        "/home/runner/.rustup",
        "/opt/google",
        "/opt/pipx",
        "/usr/local/kotlinc",
        "/usr/local/lib/node_modules",
        "/usr/local/share/boost",
        "/usr/share/miniconda",
    ):
        assert path in calls
    assert "find /usr/local -mindepth 1 -maxdepth 1 -type d -name julia*" in calls


@pytest.mark.parametrize(
    ("github_actions", "runner_environment", "kernel"),
    (
        ("false", "github-hosted", "Linux"),
        ("true", "self-hosted", "Linux"),
        ("true", "github-hosted", "Darwin"),
    ),
)
def test_cleanup_rejects_non_ephemeral_hosts(
    tmp_path: Path, github_actions: str, runner_environment: str, kernel: str
) -> None:
    result, calls = _run_script(
        tmp_path,
        github_actions=github_actions,
        runner_environment=runner_environment,
        kernel=kernel,
    )

    assert result.returncode == 2
    assert calls == ""


def test_cleanup_fails_when_root_space_is_below_minimum(tmp_path: Path) -> None:
    result, _ = _run_script(tmp_path, free_bytes=MINIMUM_ROOT_FREE_BYTES - 1)

    assert result.returncode == 1
    assert "requires at least 53687091200 bytes (50 GiB)" in result.stderr


def test_cleanup_fails_when_root_space_cannot_be_measured(tmp_path: Path) -> None:
    result, _ = _run_script(tmp_path, free_bytes="unknown")

    assert result.returncode == 2
    assert "could not determine free space" in result.stderr
