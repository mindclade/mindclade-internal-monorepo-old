# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Deterministic absolute, baseline-relative, and slice evaluation thresholds."""

from __future__ import annotations

import math
import re
from dataclasses import dataclass
from enum import StrEnum
from typing import Final

from libs.python.errors import InvalidArgument
from libs.python.identifiers import Digest

MAXIMUM_METRICS: Final = 256
MAXIMUM_SLICES: Final = 128
MAXIMUM_NAME_LENGTH: Final = 128
MAXIMUM_SAMPLE_COUNT: Final = 1_000_000_000

_NAME: Final = re.compile(r"^[a-z][a-z0-9_.-]{0,127}$")


class MetricCategory(StrEnum):
    QUALITY = "quality"
    SAFETY = "safety"
    ROBUSTNESS = "robustness"
    PRIVACY = "privacy"
    LATENCY = "latency"
    COST = "cost"


class Direction(StrEnum):
    AT_LEAST = "at-least"
    AT_MOST = "at-most"


@dataclass(frozen=True, slots=True)
class SliceObservation:
    """One aggregate for a declared slice; examples never enter gate evidence."""

    name: str
    value: float
    sample_count: int

    def __post_init__(self) -> None:
        _name(self.name, "slice name")
        _finite(self.value, "slice value")
        _samples(self.sample_count, "slice sample count")


@dataclass(frozen=True, slots=True)
class MetricObservation:
    name: str
    value: float
    sample_count: int
    dataset_digest: Digest
    slices: tuple[SliceObservation, ...] = ()

    def __post_init__(self) -> None:
        _name(self.name, "metric name")
        _finite(self.value, "metric value")
        _samples(self.sample_count, "metric sample count")
        _digest(self.dataset_digest, "metric dataset digest")
        if len(self.slices) > MAXIMUM_SLICES:
            raise _invalid("metric has too many slices", "metric_slices")
        names = [item.name for item in self.slices]
        if len(set(names)) != len(names):
            raise _invalid("metric slice names must be unique", "metric_slice_duplicate")


@dataclass(frozen=True, slots=True)
class ThresholdRule:
    """A predeclared gate; a candidate cannot change this after evaluation."""

    name: str
    category: MetricCategory
    direction: Direction
    threshold: float
    minimum_samples: int
    dataset_digest: Digest
    required_slices: tuple[str, ...] = ()
    baseline: float | None = None
    maximum_regression: float = 0.0

    def __post_init__(self) -> None:
        _name(self.name, "threshold name")
        if not isinstance(self.category, MetricCategory):
            raise _invalid("threshold category is invalid", "threshold_category")
        if not isinstance(self.direction, Direction):
            raise _invalid("threshold direction is invalid", "threshold_direction")
        _finite(self.threshold, "threshold value")
        _samples(self.minimum_samples, "threshold minimum samples")
        _digest(self.dataset_digest, "threshold dataset digest")
        if len(self.required_slices) > MAXIMUM_SLICES:
            raise _invalid("threshold has too many required slices", "threshold_slices")
        for name in self.required_slices:
            _name(name, "required slice")
        if len(set(self.required_slices)) != len(self.required_slices):
            raise _invalid("required slices must be unique", "threshold_slice_duplicate")
        if self.baseline is not None:
            _finite(self.baseline, "threshold baseline")
        _finite(self.maximum_regression, "maximum regression")
        if self.maximum_regression < 0:
            raise _invalid("maximum regression cannot be negative", "threshold_regression")
        if self.baseline is None and self.maximum_regression != 0:
            raise _invalid(
                "maximum regression requires a baseline", "threshold_regression_without_baseline"
            )


@dataclass(frozen=True, slots=True)
class ThresholdOutcome:
    name: str
    category: MetricCategory
    passed: bool
    actual: float | None
    threshold: float
    sample_count: int
    reason: str
    dataset_digest: Digest

    def __post_init__(self) -> None:
        _name(self.name, "outcome name")
        if not isinstance(self.category, MetricCategory) or not isinstance(self.passed, bool):
            raise _invalid("threshold outcome is invalid", "threshold_outcome")
        if self.actual is not None:
            _finite(self.actual, "outcome actual")
        _finite(self.threshold, "outcome threshold")
        if (
            isinstance(self.sample_count, bool)
            or not isinstance(self.sample_count, int)
            or not 0 <= self.sample_count <= MAXIMUM_SAMPLE_COUNT
        ):
            raise _invalid("outcome sample count is invalid", "outcome_samples")
        _name(self.reason, "outcome reason")
        _digest(self.dataset_digest, "outcome dataset digest")


def missing_outcome(rule: ThresholdRule) -> ThresholdOutcome:
    """Return explicit failed evidence for a missing scorer output."""

    return ThresholdOutcome(
        name=rule.name,
        category=rule.category,
        passed=False,
        actual=None,
        threshold=rule.threshold,
        sample_count=0,
        reason="missing-output",
        dataset_digest=rule.dataset_digest,
    )


def evaluate_threshold(rule: ThresholdRule, observed: MetricObservation) -> ThresholdOutcome:
    """Evaluate one observation without tolerances or post-hoc threshold changes."""

    if observed.name != rule.name:
        raise _invalid("observation name does not match threshold", "metric_name_mismatch")
    if observed.dataset_digest != rule.dataset_digest:
        return _outcome(rule, observed, False, "dataset-mismatch")
    if observed.sample_count < rule.minimum_samples:
        return _outcome(rule, observed, False, "insufficient-samples")

    slices = {item.name: item for item in observed.slices}
    for name in rule.required_slices:
        item = slices.get(name)
        if item is None:
            return _outcome(rule, observed, False, "missing-required-slice")
        if item.sample_count < rule.minimum_samples:
            return _outcome(rule, observed, False, "slice-insufficient-samples")
        if not _passes(rule, item.value):
            return _outcome(rule, observed, False, "slice-threshold-failed")

    if not _passes(rule, observed.value):
        return _outcome(rule, observed, False, "threshold-failed")
    return _outcome(rule, observed, True, "passed")


def _passes(rule: ThresholdRule, value: float) -> bool:
    if rule.direction is Direction.AT_LEAST:
        absolute = value >= rule.threshold
        relative = rule.baseline is None or value >= rule.baseline - rule.maximum_regression
    else:
        absolute = value <= rule.threshold
        relative = rule.baseline is None or value <= rule.baseline + rule.maximum_regression
    return absolute and relative


def _outcome(
    rule: ThresholdRule, observed: MetricObservation, passed: bool, reason: str
) -> ThresholdOutcome:
    return ThresholdOutcome(
        name=rule.name,
        category=rule.category,
        passed=passed,
        actual=observed.value,
        threshold=rule.threshold,
        sample_count=observed.sample_count,
        reason=reason,
        dataset_digest=observed.dataset_digest,
    )


def _name(value: object, label: str) -> None:
    if not isinstance(value, str) or _NAME.fullmatch(value) is None:
        raise _invalid(f"{label} must be a bounded canonical name", "canonical_name")


def _finite(value: object, label: str) -> None:
    if isinstance(value, bool) or not isinstance(value, int | float) or not math.isfinite(value):
        raise _invalid(f"{label} must be a finite number", "finite_number")


def _samples(value: object, label: str) -> None:
    if (
        isinstance(value, bool)
        or not isinstance(value, int)
        or not 1 <= value <= MAXIMUM_SAMPLE_COUNT
    ):
        raise _invalid(f"{label} is outside bounds", "sample_count")


def _digest(value: object, label: str) -> None:
    if not isinstance(value, Digest):
        raise _invalid(f"{label} must be a canonical digest", "digest")


def _invalid(message: str, reason: str) -> InvalidArgument:
    return InvalidArgument(message, reason=reason, operation="evaluation.qualification")
