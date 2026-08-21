# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Canonical JSON golden encoding and comparison without implicit rewrites."""

from __future__ import annotations

import json
from collections.abc import Mapping, Sequence


def canonical_json(value: object) -> bytes:
    _validate_json(value, depth=0)
    return (
        json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=False) + "\n"
    ).encode()


def assert_golden(actual: object, expected: bytes) -> None:
    rendered = canonical_json(actual)
    if rendered != expected:
        raise AssertionError(
            "canonical golden mismatch\n"
            f"expected: {expected.decode(errors='replace')}"
            f"actual:   {rendered.decode(errors='replace')}"
        )


def _validate_json(value: object, *, depth: int) -> None:
    if depth > 64:
        raise ValueError("golden value exceeds nesting limit")
    if value is None or isinstance(value, bool | int | float | str):
        return
    if isinstance(value, Mapping):
        if len(value) > 100_000 or any(not isinstance(key, str) for key in value):
            raise ValueError("golden mapping is outside bounds")
        for nested in value.values():
            _validate_json(nested, depth=depth + 1)
        return
    if isinstance(value, Sequence) and not isinstance(value, bytes | bytearray | str):
        if len(value) > 1_000_000:
            raise ValueError("golden sequence is outside bounds")
        for nested in value:
            _validate_json(nested, depth=depth + 1)
        return
    raise ValueError(f"golden value contains unsupported type {type(value).__name__}")
