# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Validated metric values with bounded label cardinality per point."""

from __future__ import annotations

import math
import re
from collections.abc import Mapping
from dataclasses import dataclass, field
from enum import StrEnum
from types import MappingProxyType
from typing import Final

from libs.python.errors import InvalidArgument, ResourceExhausted

from .redaction import redact_fields

MAXIMUM_METRIC_LABELS: Final = 16
MAXIMUM_LABEL_LENGTH: Final = 128
MAXIMUM_METRIC_INTEGER: Final = (1 << 63) - 1
_METRIC_NAME = re.compile(r"^[a-z][a-z0-9_.]{0,127}$")
_LABEL_NAME = re.compile(r"^[a-z][a-z0-9_]{0,63}$")


class MetricKind(StrEnum):
    COUNTER = "counter"
    GAUGE = "gauge"


@dataclass(frozen=True, slots=True)
class MetricPoint:
    """One provider-neutral metric observation."""

    name: str
    value: int | float
    kind: MetricKind = MetricKind.GAUGE
    labels: Mapping[str, str] = field(default_factory=dict)

    def __post_init__(self) -> None:
        if not isinstance(self.name, str) or not _METRIC_NAME.fullmatch(self.name):
            raise InvalidArgument(
                "metric names must be canonical lowercase identifiers",
                reason="observability_metric_name",
            )
        if isinstance(self.value, bool) or not isinstance(self.value, int | float):
            raise InvalidArgument(
                "metric values must be finite numbers",
                reason="observability_metric_value",
            )
        if isinstance(self.value, int):
            if not -MAXIMUM_METRIC_INTEGER - 1 <= self.value <= MAXIMUM_METRIC_INTEGER:
                raise InvalidArgument(
                    "integer metric values must fit signed 64 bits",
                    reason="observability_metric_value",
                )
        elif not math.isfinite(self.value):
            raise InvalidArgument(
                "metric values must be finite numbers",
                reason="observability_metric_value",
            )
        if not isinstance(self.kind, MetricKind):
            raise InvalidArgument("metric kind is invalid", reason="observability_metric_kind")
        if self.kind is MetricKind.COUNTER and self.value < 0:
            raise InvalidArgument(
                "counter observations must be non-negative",
                reason="observability_counter_value",
            )
        if not isinstance(self.labels, Mapping):
            raise InvalidArgument(
                "metric labels must be a mapping",
                reason="observability_metric_labels",
            )
        if len(self.labels) > MAXIMUM_METRIC_LABELS:
            raise ResourceExhausted(
                f"metric point exceeds {MAXIMUM_METRIC_LABELS} labels",
                reason="observability_metric_labels",
            )
        labels: dict[str, str] = {}
        for key, value in self.labels.items():
            if not isinstance(key, str) or not _LABEL_NAME.fullmatch(key):
                raise InvalidArgument(
                    "metric label names must be canonical lowercase identifiers",
                    reason="observability_metric_label_name",
                )
            if not isinstance(value, str) or len(value) > MAXIMUM_LABEL_LENGTH:
                raise InvalidArgument(
                    f"metric label values must be strings of at most {MAXIMUM_LABEL_LENGTH} characters",
                    reason="observability_metric_label_value",
                )
            labels[key] = value
        safe_labels = redact_fields(labels)
        object.__setattr__(
            self,
            "labels",
            MappingProxyType({key: str(value) for key, value in safe_labels.items()}),
        )

    def to_document(self) -> dict[str, object]:
        return {
            "kind": self.kind.value,
            "labels": dict(self.labels),
            "name": self.name,
            "value": self.value,
        }
