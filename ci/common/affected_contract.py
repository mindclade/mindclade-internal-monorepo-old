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


class ContractError(RuntimeError):
    """The global-input contract is missing, malformed, or unsafe."""


@dataclass(frozen=True)
class GlobalInputContract:
    """Reviewed repository-wide inputs that force complete Bazel validation."""

    exact_paths: frozenset[str]
    prefixes: tuple[str, ...]
    review_boundaries: tuple[tuple[str, tuple[str, ...]], ...]


def _contract_strings(value: Any, *, field: str) -> tuple[str, ...]:
    if not isinstance(value, list) or not all(isinstance(item, str) and item for item in value):
        raise ContractError(f"[AFFECTED-GLOBAL-002] invalid {field} contract")
    strings = tuple(value)
    if strings != tuple(sorted(set(strings))):
        raise ContractError(f"[AFFECTED-GLOBAL-003] unordered or duplicate {field} contract")
    return strings


def _contract_path(value: str, *, prefix: bool) -> str:
    candidate = value[:-1] if prefix and value.endswith("/") else value
    path = PurePosixPath(candidate)
    if (
        not candidate
        or "\\" in candidate
        or path.is_absolute()
        or ".." in path.parts
        or path.as_posix() != candidate
    ):
        raise ContractError("[AFFECTED-GLOBAL-004] unsafe global-input path")
    if prefix and not value.endswith("/"):
        raise ContractError("[AFFECTED-GLOBAL-005] global-input prefix lacks separator")
    return value


@functools.cache
def load_global_input_contract(path: Path) -> GlobalInputContract:
    """Load and strictly validate the affected-selection global-input contract."""

    try:
        payload = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as error:
        raise ContractError("[AFFECTED-GLOBAL-001] global-input contract is unreadable") from error
    if not isinstance(payload, dict) or payload.get("schema_version") != 1:
        raise ContractError("[AFFECTED-GLOBAL-002] unsupported global-input contract schema")
    if set(payload) != {
        "activation",
        "exact_paths",
        "prefixes",
        "review_boundaries",
        "schema_version",
    }:
        raise ContractError("[AFFECTED-GLOBAL-002] unexpected global-input contract field")

    exact_paths = frozenset(
        _contract_path(value, prefix=False)
        for value in _contract_strings(payload.get("exact_paths"), field="exact_paths")
    )
    prefixes = tuple(
        _contract_path(value, prefix=True)
        for value in _contract_strings(payload.get("prefixes"), field="prefixes")
    )
    if any(path.startswith(prefix) for path in exact_paths for prefix in prefixes):
        raise ContractError("[AFFECTED-GLOBAL-003] overlapping global-input contract")
    if any(left != right and left.startswith(right) for left in prefixes for right in prefixes):
        raise ContractError("[AFFECTED-GLOBAL-003] overlapping global-input prefixes")

    boundaries_payload = payload.get("review_boundaries")
    if not isinstance(boundaries_payload, dict) or list(boundaries_payload) != sorted(
        boundaries_payload
    ):
        raise ContractError("[AFFECTED-GLOBAL-002] invalid review-boundary contract")
    review_boundaries: list[tuple[str, tuple[str, ...]]] = []
    for boundary, entries_payload in boundaries_payload.items():
        if not isinstance(boundary, str):
            raise ContractError("[AFFECTED-GLOBAL-002] invalid review-boundary path")
        if boundary:
            _contract_path(boundary, prefix=False)
        entries = _contract_strings(
            entries_payload, field=f"review_boundaries.{boundary or 'root'}"
        )
        if any("/" in entry or entry in {".", ".."} for entry in entries):
            raise ContractError("[AFFECTED-GLOBAL-004] unsafe review-boundary entry")
        review_boundaries.append((boundary, entries))
    return GlobalInputContract(
        exact_paths=exact_paths,
        prefixes=prefixes,
        review_boundaries=tuple(review_boundaries),
    )
