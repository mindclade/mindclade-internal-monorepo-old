# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Bounded construction of optimizers supported by the reference trainer."""

from __future__ import annotations

import math
from collections.abc import Iterable
from dataclasses import dataclass
from typing import Final

import torch
from torch import nn

from libs.python.errors import InvalidArgument, ResourceExhausted

MAXIMUM_PARAMETER_TENSORS: Final = 100_000
MAXIMUM_PARAMETER_ELEMENTS: Final = 1 << 40
MAXIMUM_LEARNING_RATE: Final = 10.0
MAXIMUM_WEIGHT_DECAY: Final = 1.0
MAXIMUM_EPSILON: Final = 1.0


@dataclass(frozen=True, slots=True)
class SGDConfig:
    learning_rate: float
    momentum: float = 0.0
    weight_decay: float = 0.0

    def __post_init__(self) -> None:
        _bounded_float(
            self.learning_rate,
            name="learning_rate",
            minimum=0.0,
            maximum=MAXIMUM_LEARNING_RATE,
            minimum_inclusive=False,
        )
        _bounded_float(
            self.momentum,
            name="momentum",
            minimum=0.0,
            maximum=1.0,
            maximum_inclusive=False,
        )
        _bounded_float(
            self.weight_decay,
            name="weight_decay",
            minimum=0.0,
            maximum=MAXIMUM_WEIGHT_DECAY,
        )


@dataclass(frozen=True, slots=True)
class AdamWConfig:
    learning_rate: float
    betas: tuple[float, float] = (0.9, 0.999)
    epsilon: float = 1e-8
    weight_decay: float = 0.01

    def __post_init__(self) -> None:
        _bounded_float(
            self.learning_rate,
            name="learning_rate",
            minimum=0.0,
            maximum=MAXIMUM_LEARNING_RATE,
            minimum_inclusive=False,
        )
        if (
            not isinstance(self.betas, tuple)
            or len(self.betas) != 2
            or any(isinstance(value, bool) for value in self.betas)
        ):
            raise InvalidArgument(
                "AdamW betas must be a pair of finite floats",
                reason="training_optimizer_betas",
            )
        for index, value in enumerate(self.betas):
            _bounded_float(
                value,
                name=f"beta_{index}",
                minimum=0.0,
                maximum=1.0,
                maximum_inclusive=False,
            )
        _bounded_float(
            self.epsilon,
            name="epsilon",
            minimum=0.0,
            maximum=MAXIMUM_EPSILON,
            minimum_inclusive=False,
        )
        _bounded_float(
            self.weight_decay,
            name="weight_decay",
            minimum=0.0,
            maximum=MAXIMUM_WEIGHT_DECAY,
        )


OptimizerConfig = SGDConfig | AdamWConfig


def build_optimizer(
    parameters: Iterable[nn.Parameter], config: OptimizerConfig
) -> torch.optim.Optimizer:
    """Validate a finite parameter collection and construct SGD or AdamW."""

    if not isinstance(config, SGDConfig | AdamWConfig):
        raise InvalidArgument(
            "optimizer config must be SGDConfig or AdamWConfig",
            reason="training_optimizer_config",
        )
    try:
        iterator = iter(parameters)
    except TypeError as error:
        raise InvalidArgument(
            "optimizer parameters must be iterable",
            reason="training_optimizer_parameters",
            cause=error,
        ) from error

    resolved: list[nn.Parameter] = []
    identities: set[int] = set()
    total_elements = 0
    device: torch.device | None = None
    for parameter in iterator:
        if len(resolved) == MAXIMUM_PARAMETER_TENSORS:
            raise ResourceExhausted(
                "optimizer parameter tensor count exceeds bounds",
                reason="training_optimizer_parameter_count",
            )
        if not isinstance(parameter, nn.Parameter):
            raise InvalidArgument(
                "optimizer inputs must contain nn.Parameter values",
                reason="training_optimizer_parameter_type",
            )
        identity = id(parameter)
        if identity in identities:
            raise InvalidArgument(
                "optimizer parameters must not contain duplicates",
                reason="training_optimizer_parameter_duplicate",
            )
        if not parameter.requires_grad:
            raise InvalidArgument(
                "optimizer parameters must not be frozen",
                reason="training_optimizer_parameter_frozen",
            )
        if not parameter.is_leaf:
            raise InvalidArgument(
                "optimizer parameters must be leaf tensors",
                reason="training_optimizer_parameter_leaf",
            )
        if parameter.device.type not in {"cpu", "cuda"} or parameter.dtype is not torch.float32:
            raise InvalidArgument(
                "optimizer parameters must be CPU or CUDA float32",
                reason="training_optimizer_parameter_placement",
            )
        if device is None:
            device = parameter.device
        elif parameter.device != device:
            raise InvalidArgument(
                "optimizer parameters must be on one explicit device",
                reason="training_optimizer_parameter_device",
            )
        total_elements += parameter.numel()
        if total_elements > MAXIMUM_PARAMETER_ELEMENTS:
            raise ResourceExhausted(
                "optimizer parameter element count exceeds bounds",
                reason="training_optimizer_parameter_elements",
            )
        identities.add(identity)
        resolved.append(parameter)

    if not resolved:
        raise InvalidArgument(
            "optimizer requires at least one trainable parameter",
            reason="training_optimizer_parameter_empty",
        )

    if isinstance(config, SGDConfig):
        return torch.optim.SGD(
            resolved,
            lr=float(config.learning_rate),
            momentum=float(config.momentum),
            weight_decay=float(config.weight_decay),
            foreach=False,
        )
    return torch.optim.AdamW(
        resolved,
        lr=float(config.learning_rate),
        betas=(float(config.betas[0]), float(config.betas[1])),
        eps=float(config.epsilon),
        weight_decay=float(config.weight_decay),
        foreach=False,
        fused=False,
    )


def _bounded_float(
    value: object,
    *,
    name: str,
    minimum: float,
    maximum: float,
    minimum_inclusive: bool = True,
    maximum_inclusive: bool = True,
) -> float:
    if isinstance(value, bool) or not isinstance(value, int | float) or not math.isfinite(value):
        raise InvalidArgument(
            f"optimizer {name} must be finite",
            reason="training_optimizer_value",
            fields={"field": name},
        )
    normalized = float(value)
    lower_valid = normalized >= minimum if minimum_inclusive else normalized > minimum
    upper_valid = normalized <= maximum if maximum_inclusive else normalized < maximum
    if not lower_valid or not upper_valid:
        raise InvalidArgument(
            f"optimizer {name} is outside bounds",
            reason="training_optimizer_value",
            fields={"field": name},
        )
    return normalized
