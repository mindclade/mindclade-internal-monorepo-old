# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Fail-closed validation helpers for resolved configuration."""

from __future__ import annotations

from collections.abc import Iterable, Mapping
from typing import Any

from libs.python.errors import InvalidArgument

from .schema import RequiredField, get_path


class ValidationError(InvalidArgument):
    pass


def validate_required(value: Mapping[str, Any], fields: Iterable[RequiredField]) -> None:
    if not isinstance(value, Mapping):
        raise ValidationError(
            "configuration value must be a mapping",
            reason="configuration_schema_input",
        )
    errors = []
    for field in fields:
        if not isinstance(field, RequiredField):
            raise ValidationError(
                "configuration schema entries must be RequiredField values",
                reason="configuration_schema_input",
            )
        try:
            actual = get_path(value, field.path)
        except KeyError:
            errors.append(f"missing required field {field.path}")
            continue
        expected = (
            field.expected_type
            if isinstance(field.expected_type, tuple)
            else (field.expected_type,)
        )
        # ``bool`` is an ``int`` subclass in Python, but a boolean configuration value is not
        # interchangeable with an integer on the wire or in scientific configuration.
        boolean_as_number = isinstance(actual, bool) and any(
            expected_type in (int, float) for expected_type in expected
        )
        if boolean_as_number or not isinstance(actual, field.expected_type):
            errors.append(f"{field.path} has type {type(actual).__name__}")
    if errors:
        raise ValidationError("; ".join(errors), reason="configuration_schema")
