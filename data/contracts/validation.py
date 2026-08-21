# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Deterministic, non-coercing validation for data records."""

from __future__ import annotations

import datetime as dt
import math
from collections.abc import Mapping
from dataclasses import dataclass
from typing import Any

from .dataset import DatasetContract
from .record import FieldContract, FieldType


@dataclass(frozen=True, order=True)
class ValidationIssue:
    field: str
    code: str
    detail: str


def _type_matches(value: Any, field: FieldContract) -> bool:
    if field.data_type is FieldType.BOOLEAN:
        return isinstance(value, bool)
    if field.data_type is FieldType.BYTES:
        return isinstance(value, bytes)
    if field.data_type is FieldType.FLOAT:
        return isinstance(value, float)
    if field.data_type is FieldType.INTEGER:
        return isinstance(value, int) and not isinstance(value, bool)
    if field.data_type is FieldType.STRING:
        return isinstance(value, str)
    if field.data_type is FieldType.TIMESTAMP:
        return (
            isinstance(value, dt.datetime)
            and value.tzinfo is not None
            and value.utcoffset() is not None
        )


def validate_record(record: object, contract: DatasetContract) -> tuple[ValidationIssue, ...]:
    """Return stable issues without mutating, coercing, logging, or dropping fields."""

    if not isinstance(record, Mapping):
        return (ValidationIssue("<record>", "type", "record must be a mapping"),)
    fields = {field.name: field for field in contract.fields}
    issues: list[ValidationIssue] = []
    unknown = sorted(
        (name for name in record if not isinstance(name, str) or name not in fields),
        key=lambda value: (type(value).__name__, repr(value)),
    )
    for name in unknown:
        issues.append(ValidationIssue(str(name), "unknown", "field is not declared"))
    for name, field in sorted(fields.items()):
        if name not in record:
            if not field.nullable:
                issues.append(ValidationIssue(name, "missing", "required field is absent"))
            continue
        value = record[name]
        if value is None:
            if not field.nullable:
                issues.append(ValidationIssue(name, "null", "field may not be null"))
            continue
        if not _type_matches(value, field):
            issues.append(ValidationIssue(name, "type", f"expected {field.data_type.value}"))
            continue
        if isinstance(value, float) and not math.isfinite(value):
            issues.append(ValidationIssue(name, "finite", "numeric value must be finite"))
            continue
        if field.allowed_values and value not in field.allowed_values:
            issues.append(ValidationIssue(name, "domain", "value is outside the declared domain"))
        if field.minimum is not None and value < field.minimum:
            issues.append(ValidationIssue(name, "minimum", "value is below the declared minimum"))
        if field.maximum is not None and value > field.maximum:
            issues.append(ValidationIssue(name, "maximum", "value is above the declared maximum"))
    event_time = record.get(contract.event_time_field)
    ingestion_time = record.get(contract.ingestion_time_field)
    if isinstance(event_time, dt.datetime) and isinstance(ingestion_time, dt.datetime):
        lateness = (ingestion_time - event_time).total_seconds()
        if lateness < 0:
            issues.append(
                ValidationIssue(
                    contract.ingestion_time_field, "time-order", "ingestion precedes event time"
                )
            )
        elif lateness > contract.allowed_lateness_seconds:
            issues.append(
                ValidationIssue(
                    contract.ingestion_time_field, "late", "record exceeds allowed lateness"
                )
            )
    return tuple(sorted(issues))
