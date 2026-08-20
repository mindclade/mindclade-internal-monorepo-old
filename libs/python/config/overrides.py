# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Explicit dotted-path configuration overrides."""

from __future__ import annotations

import json
import re
from collections.abc import MutableMapping
from typing import Any, Final

from libs.python.errors import InvalidArgument

MAXIMUM_OVERRIDE_LENGTH: Final = 4096
_PATH_PART: Final = re.compile(r"^[A-Za-z_][A-Za-z0-9_-]*$")


class OverrideError(InvalidArgument):
    pass


def parse_scalar(text: str) -> Any:
    try:
        return json.loads(text)
    except json.JSONDecodeError:
        return text


def apply_override(target: MutableMapping[str, Any], expression: str) -> tuple[str, Any]:
    if not isinstance(target, MutableMapping):
        raise OverrideError(
            "override target must be a mutable mapping",
            reason="override_target",
        )
    if not isinstance(expression, str) or len(expression) > MAXIMUM_OVERRIDE_LENGTH:
        raise OverrideError(
            "override must be bounded text",
            reason="override_expression",
        )
    if "=" not in expression:
        raise OverrideError("override must use path=value", reason="override_expression")
    path, raw = expression.split("=", 1)
    parts = path.split(".")
    if not parts or any(_PATH_PART.fullmatch(part) is None for part in parts):
        raise OverrideError(
            "override path must contain valid dot-separated names",
            reason="override_path",
        )
    cursor: MutableMapping[str, Any] = target
    for part in parts[:-1]:
        current = cursor.get(part)
        if current is None:
            cursor[part] = {}
            current = cursor[part]
        if not isinstance(current, MutableMapping):
            raise OverrideError(
                f"override path {path!r} traverses a scalar at {part!r}",
                reason="override_scalar_traversal",
            )
        cursor = current
    value = parse_scalar(raw)
    existing = cursor.get(parts[-1])
    if (
        parts[-1] in cursor
        and existing is not None
        and value is not None
        and type(existing) is not type(value)
    ):
        raise OverrideError(
            f"override changes type at {path!r}: "
            f"{type(existing).__name__} -> {type(value).__name__}",
            reason="override_type_change",
        )
    cursor[parts[-1]] = value
    return path, value
