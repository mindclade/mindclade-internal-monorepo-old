# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Minimal schema contract; full JSON Schema validation is supplied by the build toolchain."""

from __future__ import annotations

from collections.abc import Mapping
from dataclasses import dataclass
from typing import Any

from libs.python.errors import InvalidArgument


@dataclass(frozen=True)
class RequiredField:
    path: str
    expected_type: type | tuple[type, ...]

    def __post_init__(self) -> None:
        if not self.path or any(not part for part in self.path.split(".")):
            raise InvalidArgument(
                "required-field paths must contain non-empty dot-separated names",
                reason="schema_path",
            )
        expected = (
            self.expected_type if isinstance(self.expected_type, tuple) else (self.expected_type,)
        )
        if not expected or any(not isinstance(item, type) for item in expected):
            raise InvalidArgument(
                "required-field expected types must be runtime classes",
                reason="schema_type",
            )


def get_path(value: Mapping[str, Any], path: str) -> Any:
    if not path or any(not part for part in path.split(".")):
        raise KeyError(path)
    cur: Any = value
    for part in path.split("."):
        if not isinstance(cur, Mapping) or part not in cur:
            raise KeyError(path)
        cur = cur[part]
    return cur
