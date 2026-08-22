# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Shared bounds for AdamW progress carried by checkpoint state."""

from __future__ import annotations

import math
from collections.abc import Mapping, Set

import torch

from libs.python.errors import InvalidArgument
from training.contracts.state import MAXIMUM_PROGRESS_COUNTER


def validate_adamw_steps(
    states: object,
    *,
    expected_parameter_count: int,
    expected_optimizer_steps: int,
    allowed_device_types: Set[str],
    reason: str,
    description: str,
) -> None:
    """Require one exact bounded AdamW step value for every parameter state."""

    if (
        isinstance(expected_parameter_count, bool)
        or not isinstance(expected_parameter_count, int)
        or expected_parameter_count <= 0
        or isinstance(expected_optimizer_steps, bool)
        or not isinstance(expected_optimizer_steps, int)
        or not 0 <= expected_optimizer_steps <= MAXIMUM_PROGRESS_COUNTER
        or not isinstance(allowed_device_types, Set)
        or not allowed_device_types
        or any(not isinstance(device, str) or not device for device in allowed_device_types)
    ):
        raise InvalidArgument(
            f"{description} validation inputs are invalid",
            reason=reason,
        )
    if not isinstance(states, Mapping):
        raise InvalidArgument(
            f"{description} state must be a mapping",
            reason=reason,
        )
    if not states:
        if expected_optimizer_steps == 0:
            return
        raise InvalidArgument(
            f"{description} state is missing committed optimizer progress",
            reason=reason,
        )
    if len(states) != expected_parameter_count:
        raise InvalidArgument(
            f"{description} state must cover every optimized parameter exactly once",
            reason=reason,
        )

    observed_steps: set[int] = set()
    for state in states.values():
        if not isinstance(state, Mapping) or "step" not in state:
            raise InvalidArgument(
                f"{description} state is missing a per-parameter step",
                reason=reason,
            )
        step = state["step"]
        if (
            not isinstance(step, torch.Tensor)
            or step.ndim != 0
            or step.dtype is not torch.float32
            or step.device.type not in allowed_device_types
        ):
            raise InvalidArgument(
                f"{description} step state must be a scalar float32 tensor on an allowed device",
                reason=reason,
            )
        value = float(step.detach().item())
        if (
            not math.isfinite(value)
            or not value.is_integer()
            or not 0 <= value <= MAXIMUM_PROGRESS_COUNTER
        ):
            raise InvalidArgument(
                f"{description} step state must be a finite bounded integer",
                reason=reason,
            )
        observed_steps.add(int(value))

    if len(observed_steps) != 1:
        raise InvalidArgument(
            f"{description} per-parameter step values must be identical",
            reason=reason,
        )
    if next(iter(observed_steps)) != expected_optimizer_steps:
        raise InvalidArgument(
            f"{description} step state does not match committed TrainingState optimizer_steps",
            reason=reason,
        )


__all__ = ["validate_adamw_steps"]
