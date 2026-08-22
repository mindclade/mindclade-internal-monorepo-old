# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Bootstrap-compatible loader for affected-selection global inputs."""

from __future__ import annotations

import functools
import json
from dataclasses import dataclass
from pathlib import Path, PurePosixPath
from typing import Any

REQUIRED_EXACT_PATH_ANCHORS = frozenset(
    {
        ".bazelignore",
        ".bazelrc",
        ".bazelversion",
        ".buildifier.json",
        "BUILD",
        "BUILD.bazel",
        "Cargo.lock",
        "Cargo.toml",
        "MODULE.bazel",
        "MODULE.bazel.lock",
        "REPO.bazel",
        "WORKSPACE",
        "WORKSPACE.bazel",
        "bazel_downloader.cfg",
        "components.toml",
        "deny.toml",
        "flake.lock",
        "flake.nix",
        "go.mod",
        "go.sum",
        "maturity.toml",
        "nix.conf",
        "package.json",
        "pnpm-lock.yaml",
        "pnpm-workspace.yaml",
        "pyproject.toml",
        "requirements.darwin.lock.txt",
        "requirements.lock.txt",
        "rust-toolchain.toml",
        "tools/analysis/check_affected_presubmit.py",
        "tools/analysis/run_architecture_checks.py",
        "tools/analysis/workflow_yaml.py",
        "tools/dev/bazelw",
        "tools/dev/nixw",
        "uv.lock",
    }
)
REQUIRED_PREFIX_ANCHORS = frozenset(
    {
        ".buildkite/",
        ".github/",
        "architecture/",
        "ci/",
        "protocols/",
        "qualification/",
        "tools/build/",
        "tools/dev/bazel-repo-bin/",
        "tools/qualification/",
    }
)
REQUIRED_REVIEW_BOUNDARY_ENTRIES = {
    "": frozenset(
        {
            ".agents",
            ".bazelignore",
            ".bazelrc",
            ".bazelversion",
            ".buildifier.json",
            ".buildkite",
            ".dockerignore",
            ".editorconfig",
            ".envrc",
            ".gitattributes",
            ".github",
            ".gitignore",
            ".golangci.yml",
            ".pre-commit-config.yaml",
            ".yamllint.yaml",
            ".yamllintignore",
            "AGENTS.md",
            "BUILD.bazel",
            "CHANGELOG.md",
            "CODE_OF_CONDUCT.md",
            "CONTRIBUTING.md",
            "Cargo.lock",
            "Cargo.toml",
            "GOVERNANCE.md",
            "LEGAL.md",
            "LICENSE",
            "MODULE.bazel",
            "MODULE.bazel.lock",
            "Makefile",
            "NOTICE",
            "OWNERS.toml",
            "QUALIFICATION.md",
            "README.md",
            "REPO.bazel",
            "REPOSITORY_STATUS.md",
            "SCAFFOLD_STATUS.md",
            "SECURITY.md",
            "SUPPORT.md",
            "THIRD_PARTY_NOTICES.md",
            "VALIDATION.md",
            "apps",
            "architecture",
            "bazel_downloader.cfg",
            "ci",
            "components.toml",
            "configs",
            "contracts",
            "control",
            "data",
            "deny.toml",
            "docs",
            "evaluation",
            "examples",
            "flake.lock",
            "flake.nix",
            "go.mod",
            "go.sum",
            "infra",
            "kernels",
            "libs",
            "maturity.toml",
            "models",
            "nix.conf",
            "package.json",
            "pnpm-lock.yaml",
            "pnpm-workspace.yaml",
            "preprocessing",
            "protocols",
            "pyproject.toml",
            "qualification",
            "renovate.json5",
            "requirements.darwin.lock.txt",
            "requirements.lock.txt",
            "research",
            "rust-toolchain.toml",
            "rustfmt.toml",
            "scripts",
            "sdk",
            "security",
            "services",
            "serving",
            "tests",
            "todo.txt",
            "tools",
            "training",
            "tsconfig.base.json",
            "uv.lock",
        }
    ),
    "tools": frozenset(
        {
            "BUILD.bazel",
            "README.md",
            "analysis",
            "build",
            "codegen",
            "dev",
            "docs",
            "license",
            "qualification",
            "release",
        }
    ),
}


class ContractError(RuntimeError):
    """The global-input contract is missing, malformed, or unsafe."""

    def __init__(self, code: str, message: str) -> None:
        self.code = code
        self.public_message = message
        super().__init__(f"[{code}] {message}")


@dataclass(frozen=True)
class GlobalInputContract:
    """Reviewed repository-wide inputs that force complete Bazel validation."""

    exact_paths: frozenset[str]
    prefixes: tuple[str, ...]
    review_boundaries: tuple[tuple[str, tuple[str, ...]], ...]


def _contract_strings(value: Any) -> tuple[str, ...]:
    if not isinstance(value, list) or not all(isinstance(item, str) and item for item in value):
        raise ContractError("AFFECTED-GLOBAL-002", "global-input string-list contract is invalid")
    strings = tuple(value)
    if strings != tuple(sorted(set(strings))):
        raise ContractError(
            "AFFECTED-GLOBAL-003", "global-input contract is unordered or duplicated"
        )
    return strings


def _contract_path(value: str, *, prefix: bool) -> str:
    candidate = value[:-1] if prefix and value.endswith("/") else value
    path = PurePosixPath(candidate)
    if (
        not candidate
        or candidate == "."
        or "\\" in candidate
        or any(ord(character) < 32 or ord(character) == 127 for character in candidate)
        or path.is_absolute()
        or ".." in path.parts
        or path.as_posix() != candidate
    ):
        raise ContractError("AFFECTED-GLOBAL-004", "global-input path is unsafe")
    if prefix and not value.endswith("/"):
        raise ContractError("AFFECTED-GLOBAL-005", "global-input prefix lacks a separator")
    return value


def _unique_json_object(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    result: dict[str, Any] = {}
    for key, value in pairs:
        if key in result:
            raise ContractError(
                "AFFECTED-GLOBAL-011", "global-input contract contains a duplicate JSON key"
            )
        result[key] = value
    return result


def _reject_json_constant(_value: str) -> None:
    raise ContractError("AFFECTED-GLOBAL-002", "global-input contract contains an invalid value")


def load_global_input_payload(path: Path) -> dict[str, Any]:
    """Read strict JSON without exposing content or filesystem details in errors."""

    try:
        if path.is_symlink():
            raise OSError("symbolic link")
        raw = path.read_text(encoding="utf-8")
    except (OSError, UnicodeError) as error:
        raise ContractError("AFFECTED-GLOBAL-001", "global-input contract is unreadable") from error
    try:
        payload = json.loads(
            raw,
            object_pairs_hook=_unique_json_object,
            parse_constant=_reject_json_constant,
        )
    except ContractError:
        raise
    except (json.JSONDecodeError, UnicodeError, RecursionError) as error:
        raise ContractError("AFFECTED-GLOBAL-001", "global-input contract is unreadable") from error
    if not isinstance(payload, dict):
        raise ContractError("AFFECTED-GLOBAL-002", "global-input contract root must be an object")
    return payload


@functools.cache
def load_global_input_contract(path: Path) -> GlobalInputContract:
    """Load and strictly validate the affected-selection global-input contract."""

    payload = load_global_input_payload(path)
    if payload.get("schema_version") != 1:
        raise ContractError("AFFECTED-GLOBAL-002", "global-input contract schema is unsupported")
    if set(payload) != {
        "activation",
        "exact_paths",
        "prefixes",
        "review_boundaries",
        "schema_version",
    }:
        raise ContractError("AFFECTED-GLOBAL-002", "global-input contract fields are invalid")
    if not isinstance(payload.get("activation"), dict):
        raise ContractError("AFFECTED-GLOBAL-002", "global-input activation contract is invalid")

    exact_paths = frozenset(
        _contract_path(value, prefix=False)
        for value in _contract_strings(payload.get("exact_paths"))
    )
    prefixes = tuple(
        _contract_path(value, prefix=True) for value in _contract_strings(payload.get("prefixes"))
    )
    if (
        not exact_paths
        or not prefixes
        or not REQUIRED_EXACT_PATH_ANCHORS.issubset(exact_paths)
        or not REQUIRED_PREFIX_ANCHORS.issubset(prefixes)
    ):
        raise ContractError("AFFECTED-GLOBAL-012", "required global-input anchors are missing")
    if any(path.startswith(prefix) for path in exact_paths for prefix in prefixes):
        raise ContractError("AFFECTED-GLOBAL-003", "global-input contract overlaps")
    if any(left != right and left.startswith(right) for left in prefixes for right in prefixes):
        raise ContractError("AFFECTED-GLOBAL-003", "global-input prefixes overlap")

    boundaries_payload = payload.get("review_boundaries")
    if not isinstance(boundaries_payload, dict) or list(boundaries_payload) != sorted(
        boundaries_payload
    ):
        raise ContractError("AFFECTED-GLOBAL-002", "review-boundary contract is invalid")
    if set(boundaries_payload) != set(REQUIRED_REVIEW_BOUNDARY_ENTRIES):
        raise ContractError(
            "AFFECTED-GLOBAL-013", "required review boundaries are missing or unexpected"
        )
    review_boundaries: list[tuple[str, tuple[str, ...]]] = []
    for boundary, entries_payload in boundaries_payload.items():
        if not isinstance(boundary, str):
            raise ContractError("AFFECTED-GLOBAL-002", "review-boundary path is invalid")
        if boundary:
            _contract_path(boundary, prefix=False)
        entries = _contract_strings(entries_payload)
        if not entries or any(
            "/" in entry
            or "\\" in entry
            or entry in {".", ".."}
            or any(ord(character) < 32 or ord(character) == 127 for character in entry)
            for entry in entries
        ):
            raise ContractError("AFFECTED-GLOBAL-004", "review-boundary entry is unsafe")
        if frozenset(entries) != REQUIRED_REVIEW_BOUNDARY_ENTRIES[boundary]:
            raise ContractError(
                "AFFECTED-GLOBAL-013", "required review-boundary anchors are missing or unexpected"
            )
        review_boundaries.append((boundary, entries))
    return GlobalInputContract(
        exact_paths=exact_paths,
        prefixes=prefixes,
        review_boundaries=tuple(review_boundaries),
    )
