# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Versioned field contracts for immutable dataset records."""

from __future__ import annotations

import math
import re
from dataclasses import dataclass
from enum import StrEnum
from typing import Final

MAXIMUM_ALLOWED_VALUES: Final = 128
_FIELD_NAME = re.compile(r"[a-z][a-z0-9_]{0,62}")


class FieldType(StrEnum):
    BOOLEAN = "boolean"
    BYTES = "bytes"
    FLOAT = "float"
    INTEGER = "integer"
    STRING = "string"
    TIMESTAMP = "timestamp"


class Sensitivity(StrEnum):
    PUBLIC = "public"
    INTERNAL = "internal"
    PROPRIETARY_INTERNAL = "proprietary-internal"
    RESTRICTED = "restricted"


class LogPolicy(StrEnum):
    ALLOW = "allow"
    DROP = "drop"
    REDACT = "redact"


Scalar = bool | float | int | str


@dataclass(frozen=True)
class FieldContract:
    """One bounded, language-neutral record field."""

    name: str
    data_type: FieldType
    nullable: bool = False
    minimum: float | int | None = None
    maximum: float | int | None = None
    allowed_values: tuple[Scalar, ...] = ()
    sensitivity: Sensitivity = Sensitivity.INTERNAL
    log_policy: LogPolicy = LogPolicy.REDACT
    units: str | None = None

    def __post_init__(self) -> None:
        if not isinstance(self.name, str) or not _FIELD_NAME.fullmatch(self.name):
            raise ValueError("field name must be lowercase snake_case and at most 63 bytes")
        if not isinstance(self.data_type, FieldType):
            raise ValueError("data_type must be a FieldType")
        if not isinstance(self.sensitivity, Sensitivity):
            raise ValueError("sensitivity must be a Sensitivity")
        if not isinstance(self.log_policy, LogPolicy):
            raise ValueError("log_policy must be a LogPolicy")
        if not isinstance(self.nullable, bool):
            raise ValueError("nullable must be a boolean")
        if self.minimum is not None and (
            isinstance(self.minimum, bool) or not isinstance(self.minimum, float | int)
        ):
            raise ValueError("minimum must be numeric")
        if self.maximum is not None and (
            isinstance(self.maximum, bool) or not isinstance(self.maximum, float | int)
        ):
            raise ValueError("maximum must be numeric")
        if self.minimum is not None and self.maximum is not None and self.minimum > self.maximum:
            raise ValueError("minimum may not exceed maximum")
        if any(
            isinstance(value, float) and not math.isfinite(value)
            for value in (self.minimum, self.maximum)
            if value is not None
        ):
            raise ValueError("numeric bounds must be finite")
        if (self.minimum is not None or self.maximum is not None) and self.data_type not in {
            FieldType.FLOAT,
            FieldType.INTEGER,
        }:
            raise ValueError("numeric bounds require an integer or float field")
        values = tuple(self.allowed_values)
        if len(values) > MAXIMUM_ALLOWED_VALUES:
            raise ValueError(f"allowed_values accepts at most {MAXIMUM_ALLOWED_VALUES} entries")
        if any(
            value is None or not isinstance(value, bool | float | int | str) for value in values
        ):
            raise ValueError("allowed_values entries must be non-null JSON scalars")
        expected_type = {
            FieldType.BOOLEAN: lambda value: isinstance(value, bool),
            FieldType.FLOAT: lambda value: isinstance(value, float),
            FieldType.INTEGER: lambda value: isinstance(value, int) and not isinstance(value, bool),
            FieldType.STRING: lambda value: isinstance(value, str),
        }.get(self.data_type)
        if values and (expected_type is None or any(not expected_type(value) for value in values)):
            raise ValueError("allowed_values entries must match the declared field type")
        if any(isinstance(value, float) and not math.isfinite(value) for value in values):
            raise ValueError("allowed_values entries must be finite")
        if any(
            (self.minimum is not None and value < self.minimum)
            or (self.maximum is not None and value > self.maximum)
            for value in values
            if isinstance(value, float | int) and not isinstance(value, bool)
        ):
            raise ValueError("allowed_values entries must satisfy numeric bounds")
        if len({(type(value).__name__, repr(value)) for value in values}) != len(values):
            raise ValueError("allowed_values must be unique")
        if self.units is not None and (
            not isinstance(self.units, str) or not self.units.strip() or len(self.units) > 64
        ):
            raise ValueError("units must be non-empty and bounded")
        if (
            self.sensitivity in {Sensitivity.PROPRIETARY_INTERNAL, Sensitivity.RESTRICTED}
            and self.log_policy is LogPolicy.ALLOW
        ):
            raise ValueError("sensitive fields may not be emitted verbatim to logs")
        object.__setattr__(self, "allowed_values", values)
