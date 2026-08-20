# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Deterministic recursive configuration merge."""

from __future__ import annotations

from collections.abc import Mapping
from copy import deepcopy
from typing import Any, Final

from libs.python.errors import InvalidArgument

MAXIMUM_MERGE_DEPTH: Final = 64


class MergeError(InvalidArgument):
    pass


def _copy_value(value: Any, *, depth: int = 0) -> Any:
    if depth > MAXIMUM_MERGE_DEPTH:
        raise MergeError(
            f"configuration nesting exceeds {MAXIMUM_MERGE_DEPTH} levels",
            reason="configuration_depth",
        )
    if isinstance(value, Mapping):
        return {key: _copy_value(item, depth=depth + 1) for key, item in value.items()}
    if isinstance(value, list | tuple):
        return [_copy_value(item, depth=depth + 1) for item in value]
    return deepcopy(value)


def deep_merge(
    base: Mapping[str, Any], overlay: Mapping[str, Any], *, reject_type_changes: bool = True
) -> dict[str, Any]:
    """Return ``base`` recursively overlaid by ``overlay`` without mutating either input."""
    return _deep_merge(base, overlay, reject_type_changes=reject_type_changes, path=(), depth=0)


def _deep_merge(
    base: Mapping[str, Any],
    overlay: Mapping[str, Any],
    *,
    reject_type_changes: bool,
    path: tuple[str, ...],
    depth: int,
) -> dict[str, Any]:
    if not isinstance(base, Mapping) or not isinstance(overlay, Mapping):
        raise MergeError(
            "configuration merge inputs must be mappings",
            reason="configuration_mapping",
        )
    if not isinstance(reject_type_changes, bool):
        raise MergeError(
            "reject_type_changes must be a boolean",
            reason="configuration_merge_option",
        )
    if depth > MAXIMUM_MERGE_DEPTH:
        raise MergeError(
            f"configuration nesting exceeds {MAXIMUM_MERGE_DEPTH} levels",
            reason="configuration_depth",
        )
    result: dict[str, Any] = {}
    for key, value in base.items():
        if not isinstance(key, str) or not key:
            raise MergeError(
                "configuration keys must be non-empty strings",
                reason="configuration_key",
            )
        result[key] = _copy_value(value)
    for key, value in overlay.items():
        if not isinstance(key, str) or not key:
            raise MergeError(
                "configuration keys must be non-empty strings",
                reason="configuration_key",
            )
        current_path = (*path, key)
        if key in result and isinstance(result[key], Mapping) and isinstance(value, Mapping):
            result[key] = _deep_merge(
                result[key],
                value,
                reject_type_changes=reject_type_changes,
                path=current_path,
                depth=depth + 1,
            )
        elif (
            reject_type_changes
            and key in result
            and result[key] is not None
            and value is not None
            and type(result[key]) is not type(value)
        ):
            raise MergeError(
                "configuration type changed at "
                f"{'.'.join(current_path)!r}: "
                f"{type(result[key]).__name__} -> {type(value).__name__}",
                reason="configuration_type_change",
            )
        else:
            result[key] = _copy_value(value)
    return result
