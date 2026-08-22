# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Run a bounded, parity-first TileLang tuning matrix in a worker subprocess.

The subprocess boundary supplies a deadline and a strict response protocol. It is
not an operating-system sandbox; callers that execute untrusted workers must provide
container, filesystem, network, credential, and accelerator isolation externally.
"""

from __future__ import annotations

import argparse
import contextlib
import json
import math
import os
import stat
import subprocess
import tempfile
from pathlib import Path
from typing import Any, BinaryIO, NoReturn

from kernels.tilelang.autotune import Candidate, TuningBudget, run_candidates
from kernels.tilelang.autotune.budget import MAXIMUM_CANDIDATES

MAXIMUM_SPEC_BYTES = 1024 * 1024
MAXIMUM_WORKER_STREAM_BYTES = 64 * 1024
MAXIMUM_WORKER_ARGUMENTS = 32
MAXIMUM_WORKER_ARGUMENT_BYTES = 4 * 1024
MAXIMUM_CONFIGURATION_FIELDS = 64
MAXIMUM_CONFIGURATION_KEY_BYTES = 128
MAXIMUM_CONFIGURATION_STRING_BYTES = 1024
MAXIMUM_CONFIGURATION_INTEGER = 2**63 - 1

_SPEC_KEYS = frozenset({"budget", "candidates", "environment_digest", "source_digest"})
_SPEC_REQUIRED_KEYS = frozenset({"candidates", "environment_digest", "source_digest"})
_WORKER_RESPONSE_KEYS = frozenset({"generated_source_digest", "parity_passed", "samples_ms"})


def candidates_from_spec(
    payload: dict[str, Any], *, maximum_candidates: int = MAXIMUM_CANDIDATES
) -> tuple[Candidate, ...]:
    _require_object_schema(
        payload,
        allowed=_SPEC_KEYS,
        required=_SPEC_REQUIRED_KEYS,
        description="tuning specification",
    )
    if isinstance(maximum_candidates, bool) or not isinstance(maximum_candidates, int):
        raise TypeError("maximum_candidates must be an integer")
    if not 1 <= maximum_candidates <= MAXIMUM_CANDIDATES:
        raise ValueError(f"maximum_candidates must be between one and {MAXIMUM_CANDIDATES}")

    source_digest = _require_sha256("source_digest", payload["source_digest"])
    environment_digest = _require_sha256("environment_digest", payload["environment_digest"])
    raw_candidates = payload.get("candidates")
    if not isinstance(raw_candidates, list) or not raw_candidates:
        raise ValueError("tuning specification requires a non-empty candidate list")
    if len(raw_candidates) > maximum_candidates:
        raise ValueError(
            "tuning specification contains more candidates than its active budget "
            f"({len(raw_candidates)} > {maximum_candidates})"
        )
    candidates = tuple(
        Candidate(
            _validated_configuration(configuration, index=index),
            source_digest,
            environment_digest,
        )
        for index, configuration in enumerate(raw_candidates)
    )
    if len({candidate.digest for candidate in candidates}) != len(candidates):
        raise ValueError("tuning candidates must be unique")
    return candidates


def execute_worker(
    candidate: Candidate,
    budget: TuningBudget,
    *,
    worker: tuple[str, ...],
) -> tuple[bool, str, tuple[float, ...]]:
    _validate_worker_command(worker)
    request = json.dumps(
        {
            "benchmark_samples": budget.benchmark_samples,
            "candidate_digest": candidate.digest,
            "configuration": candidate.config,
            "environment_digest": candidate.environment_digest,
            "source_digest": candidate.source_digest,
            "warmup_samples": budget.warmup_samples,
        },
        sort_keys=True,
        separators=(",", ":"),
    )
    with tempfile.TemporaryFile(mode="w+b") as stdout, tempfile.TemporaryFile(mode="w+b") as stderr:
        try:
            completed = subprocess.run(
                (*worker, request),
                check=False,
                close_fds=True,
                stdin=subprocess.DEVNULL,
                stdout=stdout,
                stderr=stderr,
                timeout=budget.compile_timeout_seconds + budget.candidate_timeout_seconds,
            )
        except subprocess.TimeoutExpired as exc:
            raise TimeoutError("tuning worker exceeded its configured deadline") from exc

        stdout_text = _bounded_stream_text(stdout, description="worker stdout")
        _bounded_stream_text(stderr, description="worker stderr")
        if completed.returncode != 0:
            raise RuntimeError(f"tuning worker exited with status {completed.returncode}")

    response = _parse_json_object(stdout_text, description="tuning worker response")
    _require_object_schema(
        response,
        allowed=_WORKER_RESPONSE_KEYS,
        required=_WORKER_RESPONSE_KEYS,
        description="tuning worker response",
    )
    parity_passed = response["parity_passed"]
    if not isinstance(parity_passed, bool):
        raise TypeError("tuning worker parity_passed must be a JSON boolean")
    generated_source_digest = _require_sha256(
        "generated_source_digest", response["generated_source_digest"]
    )
    samples = _validated_worker_samples(response["samples_ms"], budget=budget)
    if parity_passed:
        if len(samples) != budget.benchmark_samples:
            raise ValueError("passing tuning workers must return the requested sample count")
    elif samples:
        raise ValueError("failing tuning workers cannot return benchmark samples")
    return parity_passed, generated_source_digest, samples


def _validated_configuration(
    configuration: object, *, index: int
) -> tuple[tuple[str, int | float | str | bool], ...]:
    if not isinstance(configuration, dict):
        raise TypeError(f"candidate {index} configuration must be a JSON object")
    if len(configuration) > MAXIMUM_CONFIGURATION_FIELDS:
        raise ValueError(
            f"candidate {index} exceeds the {MAXIMUM_CONFIGURATION_FIELDS}-field limit"
        )

    validated: list[tuple[str, int | float | str | bool]] = []
    for key, value in configuration.items():
        if not isinstance(key, str):
            raise TypeError(f"candidate {index} configuration keys must be text")
        if not key or key.strip() != key:
            raise ValueError(f"candidate {index} configuration keys must be non-empty and trimmed")
        if _utf8_size(key, description=f"candidate {index} configuration key") > (
            MAXIMUM_CONFIGURATION_KEY_BYTES
        ):
            raise ValueError(
                f"candidate {index} configuration keys exceed the "
                f"{MAXIMUM_CONFIGURATION_KEY_BYTES}-byte limit"
            )

        if type(value) not in (bool, int, float, str):
            raise TypeError(f"candidate {index} configuration values must be JSON scalars")
        if isinstance(value, float) and not math.isfinite(value):
            raise ValueError(f"candidate {index} configuration values must be finite")
        if (
            isinstance(value, int)
            and not isinstance(value, bool)
            and abs(value) > (MAXIMUM_CONFIGURATION_INTEGER)
        ):
            raise ValueError(f"candidate {index} integer values exceed the signed 64-bit limit")
        if (
            isinstance(value, str)
            and _utf8_size(value, description=f"candidate {index} string value")
            > MAXIMUM_CONFIGURATION_STRING_BYTES
        ):
            raise ValueError(
                f"candidate {index} string values exceed the "
                f"{MAXIMUM_CONFIGURATION_STRING_BYTES}-byte limit"
            )
        validated.append((key, value))
    return tuple(sorted(validated))


def _validated_worker_samples(value: object, *, budget: TuningBudget) -> tuple[float, ...]:
    if not isinstance(value, list):
        raise TypeError("tuning worker samples_ms must be a JSON array")
    if len(value) > budget.benchmark_samples:
        raise ValueError("tuning worker returned more samples than requested")

    maximum_sample_ms = budget.candidate_timeout_seconds * 1000.0
    samples: list[float] = []
    for sample in value:
        if isinstance(sample, bool) or not isinstance(sample, (int, float)):
            raise TypeError("tuning worker samples must be real JSON numbers")
        converted = float(sample)
        if not math.isfinite(converted) or not 0 < converted <= maximum_sample_ms:
            raise ValueError(
                "tuning worker samples must be finite, positive, and within the candidate timeout"
            )
        samples.append(converted)
    return tuple(samples)


def _validate_worker_command(worker: tuple[str, ...]) -> None:
    if not isinstance(worker, tuple):
        raise TypeError("worker command must be a tuple")
    if not worker:
        raise ValueError("worker command cannot be empty")
    if len(worker) > MAXIMUM_WORKER_ARGUMENTS:
        raise ValueError(f"worker command exceeds the {MAXIMUM_WORKER_ARGUMENTS}-argument limit")
    for argument in worker:
        if not isinstance(argument, str):
            raise TypeError("worker command arguments must be text")
        if not argument or "\0" in argument:
            raise ValueError("worker command arguments must be non-empty and cannot contain NUL")
        if _utf8_size(argument, description="worker command argument") > (
            MAXIMUM_WORKER_ARGUMENT_BYTES
        ):
            raise ValueError(
                f"worker command arguments exceed the {MAXIMUM_WORKER_ARGUMENT_BYTES}-byte limit"
            )


def _bounded_stream_text(stream: BinaryIO, *, description: str) -> str:
    size = stream.tell()
    if size > MAXIMUM_WORKER_STREAM_BYTES:
        raise ValueError(f"{description} exceeds the {MAXIMUM_WORKER_STREAM_BYTES}-byte limit")
    stream.seek(0)
    payload = stream.read(MAXIMUM_WORKER_STREAM_BYTES + 1)
    try:
        return payload.decode("utf-8")
    except UnicodeDecodeError as exc:
        raise ValueError(f"{description} must be valid UTF-8") from exc


def _read_bounded_text(path: Path, *, maximum_bytes: int, description: str) -> str:
    flags = os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0)
    try:
        descriptor = os.open(path, flags)
    except OSError as exc:
        raise ValueError(f"refusing unreadable or symbolic-link {description}") from exc
    try:
        metadata = os.fstat(descriptor)
        if not stat.S_ISREG(metadata.st_mode):
            raise ValueError(f"{description} must be a regular file")
        if metadata.st_size > maximum_bytes:
            raise ValueError(f"{description} exceeds the {maximum_bytes}-byte limit")
        with os.fdopen(descriptor, "rb", closefd=False) as handle:
            payload = handle.read(maximum_bytes + 1)
    finally:
        os.close(descriptor)
    if len(payload) > maximum_bytes:
        raise ValueError(f"{description} exceeds the {maximum_bytes}-byte limit")
    try:
        return payload.decode("utf-8")
    except UnicodeDecodeError as exc:
        raise ValueError(f"{description} must be valid UTF-8") from exc


def _parse_json_object(payload: str, *, description: str) -> dict[str, Any]:
    try:
        parsed = json.loads(
            payload,
            object_pairs_hook=_object_without_duplicate_keys,
            parse_constant=_reject_json_constant,
        )
    except (json.JSONDecodeError, RecursionError) as exc:
        raise ValueError(f"{description} must be valid bounded JSON") from exc
    if not isinstance(parsed, dict):
        raise TypeError(f"{description} must be a JSON object")
    return parsed


def _object_without_duplicate_keys(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    result: dict[str, Any] = {}
    for key, value in pairs:
        if key in result:
            raise ValueError(f"duplicate JSON object key: {key!r}")
        result[key] = value
    return result


def _reject_json_constant(value: str) -> NoReturn:
    raise ValueError(f"non-finite JSON number is forbidden: {value}")


def _require_object_schema(
    payload: object,
    *,
    allowed: frozenset[str],
    required: frozenset[str],
    description: str,
) -> None:
    if not isinstance(payload, dict):
        raise TypeError(f"{description} must be a JSON object")
    keys = set(payload)
    if any(not isinstance(key, str) for key in keys):
        raise TypeError(f"{description} keys must be text")
    missing = sorted(required - keys)
    unknown = sorted(keys - allowed)
    if missing or unknown:
        raise ValueError(f"{description} schema mismatch: missing={missing!r}, unknown={unknown!r}")


def _require_sha256(name: str, value: object) -> str:
    if (
        not isinstance(value, str)
        or len(value) != 64
        or any(character not in "0123456789abcdef" for character in value)
    ):
        raise ValueError(f"{name} must be a lowercase SHA-256 digest")
    return value


def _utf8_size(value: str, *, description: str) -> int:
    try:
        return len(value.encode("utf-8"))
    except UnicodeEncodeError as exc:
        raise ValueError(f"{description} must be valid UTF-8") from exc


def _atomic_write(path: Path, payload: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    descriptor, temporary = tempfile.mkstemp(prefix=f".{path.name}.", dir=path.parent)
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8") as handle:
            handle.write(payload)
            handle.flush()
            os.fsync(handle.fileno())
        try:
            os.link(temporary, path, follow_symlinks=False)
        except FileExistsError as exc:
            raise ValueError("refusing to overwrite immutable tuning results") from exc
        directory = os.open(path.parent, os.O_RDONLY | getattr(os, "O_DIRECTORY", 0))
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
    finally:
        with contextlib.suppress(FileNotFoundError):
            os.unlink(temporary)


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--spec", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--worker", required=True, nargs="+")
    return parser


def main() -> int:
    args = _parser().parse_args()
    payload = _parse_json_object(
        _read_bounded_text(
            args.spec,
            maximum_bytes=MAXIMUM_SPEC_BYTES,
            description="tuning specification",
        ),
        description="tuning specification",
    )
    _require_object_schema(
        payload,
        allowed=_SPEC_KEYS,
        required=_SPEC_REQUIRED_KEYS,
        description="tuning specification",
    )
    raw_budget = payload.get("budget", {})
    if not isinstance(raw_budget, dict):
        raise TypeError("tuning budget must be a JSON object")
    budget = TuningBudget(**raw_budget)
    candidates = candidates_from_spec(payload, maximum_candidates=budget.max_candidates)
    results = run_candidates(
        candidates,
        budget=budget,
        execute=lambda candidate, active_budget: execute_worker(
            candidate,
            active_budget,
            worker=tuple(args.worker),
        ),
    )
    _atomic_write(args.output, results.to_json())
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
