# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Validate and execute the repository-owned connected-GPU Bazel matrix."""

from __future__ import annotations

import argparse
import json
import os
import stat
import subprocess
from dataclasses import dataclass
from pathlib import Path
from typing import Any

MAXIMUM_MATRIX_BYTES = 1024 * 1024
_SANITIZERS = frozenset({"memcheck", "racecheck", "initcheck", "synccheck"})
_TARGET_ARCHITECTURES = {
    "cuda": frozenset({"sm_90", "sm_100", "sm_120"}),
    "hip": frozenset({"gfx90a", "gfx942", "gfx950"}),
}


@dataclass(frozen=True, slots=True)
class GpuTarget:
    name: str
    target: str
    architecture: str
    runner_label: str
    qualification: bool
    required_sanitizers: tuple[str, ...]
    bazel_targets: tuple[str, ...]

    def __post_init__(self) -> None:
        if any(
            not isinstance(value, str) or not value.strip()
            for value in (self.name, self.target, self.architecture, self.runner_label)
        ):
            raise ValueError("GPU targets require complete runner and architecture identity")
        if not isinstance(self.qualification, bool):
            raise TypeError("qualification must be a boolean")
        if self.target not in _TARGET_ARCHITECTURES:
            raise ValueError("GPU target backend is unsupported")
        if self.architecture not in _TARGET_ARCHITECTURES[self.target]:
            raise ValueError("GPU architecture does not match its target backend")
        if not self.bazel_targets or any(not item.startswith("//") for item in self.bazel_targets):
            raise ValueError("GPU pipelines must invoke explicit repository Bazel targets")
        if len(self.bazel_targets) != len(set(self.bazel_targets)):
            raise ValueError("GPU pipeline Bazel targets must be unique")
        if any(tool not in _SANITIZERS for tool in self.required_sanitizers):
            raise ValueError("GPU matrix contains an unsupported sanitizer")
        if len(self.required_sanitizers) != len(set(self.required_sanitizers)):
            raise ValueError("GPU matrix sanitizer requirements must be unique")
        if self.qualification and not self.required_sanitizers:
            raise ValueError("qualification targets require an explicit sanitizer suite")

    @classmethod
    def from_dict(cls, payload: dict[str, Any]) -> GpuTarget:
        expected = {
            "architecture",
            "bazel_targets",
            "name",
            "qualification",
            "required_sanitizers",
            "runner_label",
            "target",
        }
        if not isinstance(payload, dict) or set(payload) != expected:
            raise ValueError("GPU target contains missing or unknown fields")
        required_sanitizers = payload["required_sanitizers"]
        bazel_targets = payload["bazel_targets"]
        if not isinstance(required_sanitizers, list) or not all(
            isinstance(value, str) for value in required_sanitizers
        ):
            raise TypeError("required_sanitizers must be a string array")
        if not isinstance(bazel_targets, list) or not all(
            isinstance(value, str) for value in bazel_targets
        ):
            raise TypeError("bazel_targets must be a string array")
        return cls(
            name=payload["name"],
            target=payload["target"],
            architecture=payload["architecture"],
            runner_label=payload["runner_label"],
            qualification=payload["qualification"],
            required_sanitizers=tuple(required_sanitizers),
            bazel_targets=tuple(bazel_targets),
        )

    def command(self) -> tuple[str, ...]:
        return (
            "tools/dev/nixw",
            "develop",
            ".#ci-bazel",
            "--command",
            "tools/dev/bazelw",
            "test",
            *self.bazel_targets,
            "--config=ci",
            "--test_output=errors",
            f"--test_env=MINDCLADE_EXPECTED_GPU_TARGET={self.target}",
            f"--test_env=MINDCLADE_EXPECTED_GPU_ARCHITECTURE={self.architecture}",
            "--test_env=MINDCLADE_ACCELERATOR_COMPILER_VERSION",
            "--test_env=MINDCLADE_ACCELERATOR_DRIVER_VERSION",
            "--test_env=MINDCLADE_RUNTIME_IMAGE_DIGEST",
        )


def _read_matrix(path: Path) -> str:
    try:
        path_metadata = os.lstat(path)
    except OSError as exc:
        raise ValueError(f"refusing unreadable GPU matrix {path}") from exc
    runfile_roots = tuple(
        Path(value).absolute()
        for name in ("RUNFILES_DIR", "TEST_SRCDIR")
        if (value := os.environ.get(name))
    )
    absolute_path = path.absolute()
    is_declared_runfile = any(
        absolute_path == root or root in absolute_path.parents for root in runfile_roots
    )
    is_symlink = stat.S_ISLNK(path_metadata.st_mode)
    if is_symlink and not is_declared_runfile:
        raise ValueError(f"refusing symbolic-link GPU matrix {path}")
    flags = os.O_RDONLY
    if not is_declared_runfile:
        flags |= getattr(os, "O_NOFOLLOW", 0)
    try:
        descriptor = os.open(path, flags)
    except OSError as exc:
        raise ValueError(f"refusing unreadable or symbolic-link GPU matrix {path}") from exc
    try:
        metadata = os.fstat(descriptor)
        if not stat.S_ISREG(metadata.st_mode):
            raise ValueError("GPU matrix must be a regular file")
        if metadata.st_size > MAXIMUM_MATRIX_BYTES:
            raise ValueError("GPU matrix exceeds its byte limit")
        with os.fdopen(descriptor, "rb", closefd=False) as handle:
            payload = handle.read(MAXIMUM_MATRIX_BYTES + 1)
        if len(payload) > MAXIMUM_MATRIX_BYTES:
            raise ValueError("GPU matrix exceeds its byte limit")
        return payload.decode("utf-8")
    finally:
        os.close(descriptor)


def load_matrix(path: Path) -> tuple[GpuTarget, ...]:
    source = "\n".join(
        line for line in _read_matrix(path).splitlines() if not line.lstrip().startswith("#")
    )

    def reject_duplicate_keys(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
        result: dict[str, Any] = {}
        for key, value in pairs:
            if key in result:
                raise ValueError(f"duplicate GPU matrix key: {key}")
            result[key] = value
        return result

    def reject_nonfinite_number(value: str) -> float:
        raise ValueError(f"non-finite GPU matrix number: {value}")

    payload = json.loads(
        source,
        object_pairs_hook=reject_duplicate_keys,
        parse_constant=reject_nonfinite_number,
    )
    if not isinstance(payload, dict) or set(payload) != {"schema_version", "targets"}:
        raise ValueError("GPU matrix contains missing or unknown fields")
    if type(payload["schema_version"]) is not int or payload["schema_version"] != 1:
        raise ValueError("unsupported connected-GPU matrix schema")
    raw_targets = payload["targets"]
    if not isinstance(raw_targets, list):
        raise TypeError("GPU matrix targets must be a JSON array")
    targets = tuple(GpuTarget.from_dict(item) for item in raw_targets)
    keys = [(target.target, target.architecture) for target in targets]
    if not targets or len(keys) != len(set(keys)):
        raise ValueError("GPU matrix targets must be non-empty and unique")
    return targets


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--matrix",
        type=Path,
        default=Path(__file__).with_name("targets.yaml"),
    )
    parser.add_argument("--architecture", required=True)
    parser.add_argument("--execute", action="store_true")
    return parser


def main() -> int:
    args = _parser().parse_args()
    matches = [
        target for target in load_matrix(args.matrix) if target.architecture == args.architecture
    ]
    if len(matches) != 1:
        raise ValueError("requested architecture must resolve to exactly one GPU matrix target")
    target = matches[0]
    command = target.command()
    if args.execute:
        subprocess.run(command, check=True, cwd=Path(__file__).resolve().parents[2])
    else:
        if os.environ.get("CI", "").lower() in {"1", "true", "yes"}:
            raise ValueError("CI GPU jobs must pass --execute")
        print(
            json.dumps(
                {
                    "architecture": target.architecture,
                    "command": command,
                    "promotion_eligible": target.qualification,
                    "required_sanitizers": target.required_sanitizers,
                    "runner_label": target.runner_label,
                    "target": target.name,
                },
                separators=(",", ":"),
            )
        )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
