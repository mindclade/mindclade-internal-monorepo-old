# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Deterministic smallest-fit shape bucket selection."""

from dataclasses import dataclass


@dataclass(frozen=True, order=True, slots=True)
class ShapeBucket:
    maximum_units: int
    name: str

    def __post_init__(self) -> None:
        if isinstance(self.maximum_units, bool) or not 1 <= self.maximum_units <= 2**31:
            raise ValueError("shape bucket limit is outside bounds")
        if not self.name or len(self.name) > 128:
            raise ValueError("shape bucket name is invalid")


class ShapeBucketSelector:
    def __init__(self, buckets: tuple[ShapeBucket, ...]) -> None:
        if not buckets or tuple(sorted(buckets)) != buckets:
            raise ValueError("shape buckets must be non-empty and sorted")
        if len({bucket.name for bucket in buckets}) != len(buckets):
            raise ValueError("shape bucket names must be unique")
        self._buckets = buckets

    def select(self, units: int) -> ShapeBucket:
        if isinstance(units, bool) or units <= 0:
            raise ValueError("shape units must be positive")
        for bucket in self._buckets:
            if units <= bucket.maximum_units:
                return bucket
        raise ValueError("request exceeds every shape bucket")
