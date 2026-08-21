# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Canonical serialization used for content identity, never data coercion."""

from __future__ import annotations

import base64
import datetime as dt
import json
import math
from collections.abc import Mapping, Sequence
from typing import Any

MAX_NESTING_DEPTH = 32


def canonical_json(value: object) -> bytes:
    """Encode supported values deterministically with explicit byte/time markers."""

    normalized = _normalize(value, depth=0)
    return json.dumps(
        normalized,
        ensure_ascii=True,
        allow_nan=False,
        sort_keys=True,
        separators=(",", ":"),
    ).encode("ascii")


def _normalize(value: object, *, depth: int) -> Any:
    if depth > MAX_NESTING_DEPTH:
        raise ValueError("canonical value exceeds nesting bound")
    if value is None or isinstance(value, bool | int | str):
        return value
    if isinstance(value, float):
        if not math.isfinite(value):
            raise ValueError("canonical numbers must be finite")
        return value
    if isinstance(value, bytes):
        return {"$bytes_base64": base64.b64encode(value).decode("ascii")}
    if isinstance(value, dt.datetime):
        if value.tzinfo is None or value.utcoffset() is None:
            raise ValueError("canonical timestamps must be timezone-aware")
        rendered = value.astimezone(dt.UTC).isoformat(timespec="microseconds").replace(
            "+00:00", "Z"
        )
        return {"$timestamp": rendered}
    if isinstance(value, Mapping):
        if len(value) > 4096 or any(not isinstance(key, str) or not key for key in value):
            raise ValueError("canonical mappings require bounded non-empty string keys")
        return {key: _normalize(value[key], depth=depth + 1) for key in sorted(value)}
    if isinstance(value, Sequence):
        if len(value) > 1_000_000:
            raise ValueError("canonical sequence exceeds item bound")
        return [_normalize(item, depth=depth + 1) for item in value]
    raise ValueError(f"unsupported canonical value type: {type(value).__name__}")
