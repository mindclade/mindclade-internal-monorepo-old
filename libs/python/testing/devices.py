# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Pure device-spec parsing and deterministic test-device selection."""

from __future__ import annotations

import re
from collections.abc import Sequence
from dataclasses import dataclass

from libs.python.errors import FailedPrecondition, InvalidArgument

_DEVICE = re.compile(r"^(cpu|mps|cuda)(?::([0-9]{1,4}))?$")


@dataclass(frozen=True, slots=True, order=True)
class DeviceSpec:
    kind: str
    index: int | None = None

    def __post_init__(self) -> None:
        if self.kind not in {"cpu", "cuda", "mps"}:
            raise InvalidArgument("unsupported test device kind", reason="testing_device_kind")
        if self.kind != "cuda" and self.index is not None:
            raise InvalidArgument(
                "only CUDA device specs may have an index",
                reason="testing_device_index",
            )
        if self.index is not None and (
            isinstance(self.index, bool)
            or not isinstance(self.index, int)
            or not 0 <= self.index <= 9999
        ):
            raise InvalidArgument(
                "device index must be an integer in [0, 9999]",
                reason="testing_device_index",
            )

    @classmethod
    def parse(cls, value: object) -> DeviceSpec:
        if not isinstance(value, str):
            raise InvalidArgument("device spec must be text", reason="testing_device_spec")
        match = _DEVICE.fullmatch(value)
        if match is None:
            raise InvalidArgument(
                "device spec must be cpu, mps, cuda, or cuda:<index>",
                reason="testing_device_spec",
            )
        index = int(match[2]) if match[2] is not None else None
        return cls(match[1], index)

    def __str__(self) -> str:
        return self.kind if self.index is None else f"{self.kind}:{self.index}"


def select_device(preferred: Sequence[str], available: Sequence[str]) -> DeviceSpec:
    """Select the first preferred device present in an explicitly supplied inventory."""
    if isinstance(preferred, str) or isinstance(available, str):
        raise InvalidArgument("device inventories must be sequences", reason="testing_devices")
    available_specs = frozenset(DeviceSpec.parse(value) for value in available)
    for value in preferred:
        candidate = DeviceSpec.parse(value)
        if candidate in available_specs:
            return candidate
    raise FailedPrecondition(
        "none of the preferred test devices is available",
        reason="testing_device_unavailable",
    )
