# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Shell-free, deadline- and output-bounded subprocesses for integration tests."""

from __future__ import annotations

import os
import selectors
import subprocess
import time
from collections.abc import Mapping, Sequence
from dataclasses import dataclass
from pathlib import Path
from typing import IO, Final, cast

from libs.python.errors import (
    DeadlineExceeded,
    FailedPrecondition,
    InvalidArgument,
    ResourceExhausted,
)

MAXIMUM_PROCESS_ARGUMENTS: Final = 256
MAXIMUM_PROCESS_OUTPUT_BYTES: Final = 1 << 20
MAXIMUM_PROCESS_TIMEOUT_SECONDS: Final = 300.0


@dataclass(frozen=True, slots=True)
class ProcessResult:
    args: tuple[str, ...]
    returncode: int
    stdout: str
    stderr: str
    duration_seconds: float


def _validated_args(args: Sequence[str]) -> tuple[str, ...]:
    if isinstance(args, str | bytes) or not isinstance(args, Sequence) or not args:
        raise InvalidArgument(
            "process args must be a non-empty sequence", reason="testing_process_args"
        )
    if len(args) > MAXIMUM_PROCESS_ARGUMENTS:
        raise ResourceExhausted(
            f"process args exceed {MAXIMUM_PROCESS_ARGUMENTS} entries",
            reason="testing_process_args",
        )
    checked: list[str] = []
    for value in args:
        if not isinstance(value, str) or not value or "\x00" in value:
            raise InvalidArgument(
                "process arguments must be non-empty strings without NUL bytes",
                reason="testing_process_arg",
            )
        checked.append(value)
    return tuple(checked)


def _capture(
    process: subprocess.Popen[bytes], *, deadline: float, maximum_output_bytes: int
) -> tuple[bytes, bytes]:
    selector = selectors.DefaultSelector()
    stdout = process.stdout
    stderr = process.stderr
    if stdout is None or stderr is None:  # pragma: no cover - Popen configuration guarantees pipes
        raise AssertionError("subprocess capture pipe missing")
    streams: dict[IO[bytes], bytearray] = {stdout: bytearray(), stderr: bytearray()}
    for stream in streams:
        os.set_blocking(stream.fileno(), False)
        selector.register(stream, selectors.EVENT_READ)
    total = 0
    try:
        while selector.get_map():
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                process.kill()
                process.wait()
                raise DeadlineExceeded(
                    "test process exceeded its deadline",
                    reason="testing_process_deadline",
                )
            for key, _ in selector.select(timeout=min(remaining, 0.05)):
                stream = cast(IO[bytes], key.fileobj)
                chunk = os.read(stream.fileno(), 65_536)
                if not chunk:
                    selector.unregister(stream)
                    continue
                total += len(chunk)
                if total > maximum_output_bytes:
                    process.kill()
                    process.wait()
                    raise ResourceExhausted(
                        f"test process output exceeds {maximum_output_bytes} bytes",
                        reason="testing_process_output",
                    )
                streams[stream].extend(chunk)
        try:
            process.wait(timeout=max(0.0, deadline - time.monotonic()))
        except subprocess.TimeoutExpired as error:
            process.kill()
            process.wait()
            raise DeadlineExceeded(
                "test process exceeded its deadline",
                reason="testing_process_deadline",
                cause=error,
            ) from error
    finally:
        selector.close()
        for stream in streams:
            stream.close()
        if process.poll() is None:
            process.kill()
            process.wait()
    return bytes(streams[stdout]), bytes(streams[stderr])


def run_process(
    args: Sequence[str],
    *,
    timeout_seconds: float = 30.0,
    maximum_output_bytes: int = MAXIMUM_PROCESS_OUTPUT_BYTES,
    cwd: Path | None = None,
    env: Mapping[str, str] | None = None,
    check: bool = True,
) -> ProcessResult:
    """Run an executable directly (never through a shell) with hard resource bounds."""
    checked_args = _validated_args(args)
    if (
        isinstance(timeout_seconds, bool)
        or not isinstance(timeout_seconds, int | float)
        or not 0 < timeout_seconds <= MAXIMUM_PROCESS_TIMEOUT_SECONDS
    ):
        raise InvalidArgument(
            f"process timeout must be in (0, {MAXIMUM_PROCESS_TIMEOUT_SECONDS}]",
            reason="testing_process_timeout",
        )
    if (
        isinstance(maximum_output_bytes, bool)
        or not isinstance(maximum_output_bytes, int)
        or not 1 <= maximum_output_bytes <= MAXIMUM_PROCESS_OUTPUT_BYTES
    ):
        raise InvalidArgument(
            f"maximum output must be in [1, {MAXIMUM_PROCESS_OUTPUT_BYTES}]",
            reason="testing_process_output_limit",
        )
    if cwd is not None and not isinstance(cwd, Path):
        raise InvalidArgument(
            "process cwd must be a pathlib.Path",
            reason="testing_process_cwd",
        )
    if not isinstance(check, bool):
        raise InvalidArgument(
            "process check must be a boolean",
            reason="testing_process_check",
        )
    checked_env: dict[str, str] | None = None
    if env is not None:
        if not isinstance(env, Mapping):
            raise InvalidArgument(
                "process environment must be a string mapping",
                reason="testing_process_environment",
            )
        checked_env = {}
        for key, value in env.items():
            if (
                not isinstance(key, str)
                or not key
                or "=" in key
                or "\x00" in key
                or not isinstance(value, str)
                or "\x00" in value
            ):
                raise InvalidArgument(
                    "process environment keys and values must be valid strings",
                    reason="testing_process_environment",
                )
            checked_env[key] = value
    started = time.monotonic()
    try:
        process = subprocess.Popen(
            checked_args,
            cwd=cwd,
            env=checked_env,
            stdin=subprocess.DEVNULL,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            shell=False,
        )
    except (OSError, ValueError) as error:
        raise FailedPrecondition(
            "test process could not be started",
            reason="testing_process_start",
            cause=error,
        ) from error
    stdout, stderr = _capture(
        process,
        deadline=started + float(timeout_seconds),
        maximum_output_bytes=maximum_output_bytes,
    )
    result = ProcessResult(
        checked_args,
        process.returncode,
        stdout.decode("utf-8", errors="replace"),
        stderr.decode("utf-8", errors="replace"),
        time.monotonic() - started,
    )
    if check and result.returncode != 0:
        raise FailedPrecondition(
            f"test process exited with status {result.returncode}",
            reason="testing_process_exit",
        )
    return result
