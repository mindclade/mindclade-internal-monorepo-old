"""Explicit dotted-path configuration overrides."""

from __future__ import annotations

import json
from collections.abc import MutableMapping
from typing import Any


class OverrideError(ValueError):
    pass


def parse_scalar(text: str) -> Any:
    try:
        return json.loads(text)
    except json.JSONDecodeError:
        return text


def apply_override(target: MutableMapping[str, Any], expression: str) -> tuple[str, Any]:
    if "=" not in expression:
        raise OverrideError("override must use path=value")
    path, raw = expression.split("=", 1)
    parts = [p for p in path.split(".") if p]
    if not parts:
        raise OverrideError("override path is empty")
    cursor: MutableMapping[str, Any] = target
    for part in parts[:-1]:
        current = cursor.get(part)
        if current is None:
            cursor[part] = {}
            current = cursor[part]
        if not isinstance(current, MutableMapping):
            raise OverrideError(f"override path {path!r} traverses a scalar at {part!r}")
        cursor = current
    value = parse_scalar(raw)
    cursor[parts[-1]] = value
    return path, value
