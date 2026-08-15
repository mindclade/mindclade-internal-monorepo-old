# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Fail-closed validation helpers for resolved configuration."""

from __future__ import annotations

from collections.abc import Iterable, Mapping
from typing import Any

from .schema import RequiredField, get_path


class ValidationError(ValueError):
    pass


def validate_required(value: Mapping[str, Any], fields: Iterable[RequiredField]) -> None:
    errors = []
    for field in fields:
        try:
            actual = get_path(value, field.path)
        except KeyError:
            errors.append(f"missing required field {field.path}")
            continue
        if not isinstance(actual, field.expected_type):
            errors.append(f"{field.path} has type {type(actual).__name__}")
    if errors:
        raise ValidationError("; ".join(errors))
