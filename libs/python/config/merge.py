# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Deterministic recursive configuration merge."""

from __future__ import annotations

from collections.abc import Iterable, Mapping
from copy import deepcopy
from typing import Any, Final

from libs.python.errors import InvalidArgument

MAXIMUM_MERGE_DEPTH: Final = 64
MAXIMUM_MERGE_LAYERS: Final = 128


class MergeError(InvalidArgument):
    pass


def _copy_value(value: Any, *, depth: int = 0) -> Any:
    if depth > MAXIMUM_MERGE_DEPTH:
        raise MergeError(
            f"configuration nesting exceeds {MAXIMUM_MERGE_DEPTH} levels",
            reason="configuration_depth",
        )
    if isinstance(value, Mapping):
        return _copy_mapping(value, depth=depth)
    if isinstance(value, list | tuple):
        return [_copy_value(item, depth=depth + 1) for item in value]
    return deepcopy(value)


def deep_merge(
    base: Mapping[str, Any], overlay: Mapping[str, Any], *, reject_type_changes: bool = True
) -> dict[str, Any]:
    """Return ``base`` recursively overlaid by ``overlay`` without mutating either input."""
    return deep_merge_many(base, (overlay,), reject_type_changes=reject_type_changes)


def deep_merge_many(
    base: Mapping[str, Any],
    overlays: Iterable[Mapping[str, Any]],
    *,
    reject_type_changes: bool = True,
) -> dict[str, Any]:
    """Merge many layers while copying the accumulated tree only once.

    Each input remains untouched and every value copied into the result remains
    unaliased. Compared with repeatedly calling :func:`deep_merge`, unchanged
    branches are not recopied for every layer.
    """
    if not isinstance(base, Mapping):
        raise MergeError(
            "configuration merge inputs must be mappings",
            reason="configuration_mapping",
        )
    if not isinstance(reject_type_changes, bool):
        raise MergeError(
            "reject_type_changes must be a boolean",
            reason="configuration_merge_option",
        )
    try:
        iterator = iter(overlays)
    except TypeError as error:
        raise MergeError(
            "configuration overlays must be iterable mappings",
            reason="configuration_mapping",
            cause=error,
        ) from error
    result = _copy_mapping(base)
    for index, overlay in enumerate(iterator):
        if index >= MAXIMUM_MERGE_LAYERS:
            raise MergeError(
                f"configuration merge exceeds {MAXIMUM_MERGE_LAYERS} layers",
                reason="configuration_merge_layers",
            )
        if not isinstance(overlay, Mapping):
            raise MergeError(
                "configuration merge inputs must be mappings",
                reason="configuration_mapping",
            )
        _merge_into(
            result,
            overlay,
            reject_type_changes=reject_type_changes,
            path=(),
            depth=0,
        )
    return result


def _copy_mapping(value: Mapping[str, Any], *, depth: int = 0) -> dict[str, Any]:
    if depth > MAXIMUM_MERGE_DEPTH:
        raise MergeError(
            f"configuration nesting exceeds {MAXIMUM_MERGE_DEPTH} levels",
            reason="configuration_depth",
        )
    result: dict[str, Any] = {}
    for key, item in value.items():
        if not isinstance(key, str) or not key:
            raise MergeError(
                "configuration keys must be non-empty strings",
                reason="configuration_key",
            )
        result[key] = _copy_value(item, depth=depth + 1)
    return result


def _merge_into(
    result: dict[str, Any],
    overlay: Mapping[str, Any],
    *,
    reject_type_changes: bool,
    path: tuple[str, ...],
    depth: int,
) -> None:
    if depth > MAXIMUM_MERGE_DEPTH:
        raise MergeError(
            f"configuration nesting exceeds {MAXIMUM_MERGE_DEPTH} levels",
            reason="configuration_depth",
        )
    for key, value in overlay.items():
        if not isinstance(key, str) or not key:
            raise MergeError(
                "configuration keys must be non-empty strings",
                reason="configuration_key",
            )
        current_path = (*path, key)
        if key in result and isinstance(result[key], Mapping) and isinstance(value, Mapping):
            nested = result[key]
            if not isinstance(nested, dict):  # pragma: no cover - copies normalize mappings
                nested = _copy_mapping(nested, depth=depth + 1)
                result[key] = nested
            _merge_into(
                nested,
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
            result[key] = _copy_value(value, depth=depth + 1)
