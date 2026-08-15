"""Minimal schema contract; full JSON Schema validation is supplied by the build toolchain."""

from __future__ import annotations

from collections.abc import Mapping
from dataclasses import dataclass
from typing import Any


@dataclass(frozen=True)
class RequiredField:
    path: str
    expected_type: type | tuple[type, ...]


def get_path(value: Mapping[str, Any], path: str) -> Any:
    cur: Any = value
    for part in path.split("."):
        if not isinstance(cur, Mapping) or part not in cur:
            raise KeyError(path)
        cur = cur[part]
    return cur
